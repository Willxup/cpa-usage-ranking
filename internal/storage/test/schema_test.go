package test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"cpa-usage-ranking/internal/storage"
)

func queryInt(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var value int
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCurrentSchemaInitializesAndReopens(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ranking.db")
	db := openDatabase(t, path)
	if err := storage.InitSchema(ctx, db.Writer); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"clients", "models", "report_window", "daily_reports", "daily_stats", "period_stats", "admin_accounts"} {
		var count int
		if err := db.Reader.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?", name).Scan(&count); err != nil || count != 1 {
			t.Fatalf("missing table %s: %v", name, err)
		}
	}
	if queryInt(t, db.Reader, "SELECT count(*) FROM sqlite_schema WHERE name='schema_migrations'") != 0 ||
		queryInt(t, db.Reader, "SELECT count(*) FROM clients") != 0 || queryInt(t, db.Reader, "SELECT count(*) FROM models") != 0 {
		t.Fatal("initialization created a ledger or seeded data")
	}
	execSQL(t, db.Writer, `INSERT INTO clients(token_hash,display_name,avatar_id,created_at_ms) VALUES(zeroblob(32),'preserved',1,123)`)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openDatabase(t, path)
	for range 2 {
		if err := storage.InitSchema(ctx, db.Writer); err != nil {
			t.Fatal(err)
		}
	}
	if queryInt(t, db.Reader, "SELECT count(*) FROM clients WHERE display_name='preserved' AND ranking_hidden=0 AND created_at_ms=123") != 1 {
		t.Fatal("reinitialization changed existing data")
	}
}

func TestSchemaInitializationRollsBackOnError(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	// A conflicting index name causes initialization to fail after creating tables.
	execSQL(t, db.Writer, `CREATE TABLE blocker(value INTEGER); CREATE TABLE clients_ranking_hidden_id(value INTEGER)`)
	if err := storage.InitSchema(context.Background(), db.Writer); err == nil {
		t.Fatal("initialization unexpectedly succeeded")
	}
	if queryInt(t, db.Reader, "SELECT count(*) FROM sqlite_schema WHERE name='clients'") != 0 {
		t.Fatal("failed initialization left partial schema")
	}
}

func TestClientSchemaEnforcesCredentialAndStateShape(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	if err := storage.InitSchema(context.Background(), db.Writer); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		hash     any
		disabled int
	}{
		{nil, 0}, {[]byte{1}, 0}, {"01234567890123456789012345678901", 0}, {make([]byte, 32), 2},
	} {
		if _, err := db.Writer.Exec("INSERT INTO clients (token_hash, display_name, avatar_id, disabled, created_at_ms) VALUES (?, 'test', 1, ?, 0)", input.hash, input.disabled); err == nil {
			t.Fatal("invalid credential or state persisted")
		}
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM clients") != 0 {
		t.Fatal("failed inserts left rows")
	}
}
