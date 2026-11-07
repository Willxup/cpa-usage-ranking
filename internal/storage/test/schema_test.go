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
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM models") != 0 {
		t.Fatal("schema migration unexpectedly registered models")
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM schema_migrations") != 3 || queryInt(t, db.Reader, "SELECT MAX(version) FROM schema_migrations") != 3 {
		t.Fatal("current schema versions were not recorded exactly once")
	}
}

func TestVersionOneDatabaseUpgradesWithoutChangingClients(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	execSQL(t, db.Writer, `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY);
		INSERT INTO schema_migrations VALUES (1);
		CREATE TABLE clients (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token_hash BLOB NOT NULL UNIQUE CHECK (typeof(token_hash) = 'blob' AND length(token_hash) = 32),
			display_name TEXT NOT NULL, avatar_id INTEGER NOT NULL CHECK (avatar_id > 0),
			disabled INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)), created_at_ms INTEGER NOT NULL
		);
		INSERT INTO clients (token_hash, display_name, avatar_id, created_at_ms) VALUES (zeroblob(32), 'preserved', 1, 123)`)
	if err := storage.MigrateSchema(context.Background(), db.Writer); err != nil {
		t.Fatal(err)
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM clients WHERE id=1 AND display_name='preserved' AND created_at_ms=123") != 1 ||
		queryInt(t, db.Reader, "SELECT COUNT(*) FROM schema_migrations WHERE version IN (1,2)") != 2 {
		t.Fatal("upgrade changed clients or missed a version")
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM pragma_table_info('models')") != 2 {
		t.Fatal("models dictionary contains unexpected fields")
	}
}

func TestCurrentSchemaDoesNotAdoptUnknownModelTable(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	execSQL(t, db.Writer, "CREATE TABLE models (preserved INTEGER); INSERT INTO models VALUES (42)")
	if err := storage.MigrateSchema(context.Background(), db.Writer); err == nil {
		t.Fatal("unknown model table was silently accepted")
	}
	if queryInt(t, db.Reader, "SELECT preserved FROM models") != 42 || queryInt(t, db.Reader, "SELECT COUNT(*) FROM schema_migrations WHERE version=2") != 0 {
		t.Fatal("failed model migration changed existing data or recorded success")
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
