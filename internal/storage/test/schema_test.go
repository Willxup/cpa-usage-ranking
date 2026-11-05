package test

import (
	"context"
	"path/filepath"
	"testing"

	"cpa-usage-ranking/internal/storage"
)

func TestCurrentSchemaUpgradesEmptyLedgerAndReplaysSafely(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	// 模拟 C14 已建立版本账本、尚无业务表的数据库。
	if err := storage.Migrate(context.Background(), db.Writer, nil); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := storage.MigrateSchema(context.Background(), db.Writer); err != nil {
			t.Fatal(err)
		}
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM clients") != 0 {
		t.Fatal("migration seeded client data")
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM schema_migrations") != 1 || queryInt(t, db.Reader, "SELECT version FROM schema_migrations") != 1 {
		t.Fatal("current schema version was not recorded exactly once")
	}
}

func TestCurrentSchemaDoesNotAdoptUnknownClientTable(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	execSQL(t, db.Writer, "CREATE TABLE clients (preserved INTEGER); INSERT INTO clients VALUES (42)")
	if err := storage.MigrateSchema(context.Background(), db.Writer); err == nil {
		t.Fatal("unknown client table was silently accepted")
	}
	if queryInt(t, db.Reader, "SELECT preserved FROM clients") != 42 || queryInt(t, db.Reader, "SELECT COUNT(*) FROM schema_migrations") != 0 {
		t.Fatal("failed migration changed existing data or recorded success")
	}
}

func TestClientSchemaEnforcesCredentialAndStateShape(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	if err := storage.MigrateSchema(context.Background(), db.Writer); err != nil {
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
