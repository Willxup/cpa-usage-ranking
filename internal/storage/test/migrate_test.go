package test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-ranking/internal/storage"
)

func sqlMigration(version int, statements ...string) storage.Migration {
	return storage.Migration{Version: version, Apply: func(ctx context.Context, tx *sql.Tx) error {
		for _, statement := range statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}}
}

func queryInt(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var value int
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestEmptyMigrationsOnlyCreateLedger(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	for range 2 {
		if err := storage.Migrate(context.Background(), db.Writer, nil); err != nil {
			t.Fatal(err)
		}
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM sqlite_schema WHERE type='table'") != 1 {
		t.Fatal("empty catalog created unexpected tables")
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM schema_migrations") != 0 {
		t.Fatal("empty catalog recorded a version")
	}
}

func TestMigrationsSkipAppliedVersionsAndContinueAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ranking.db")
	db := openDatabase(t, path)
	calls := 0
	first := storage.Migration{Version: 1, Apply: func(ctx context.Context, tx *sql.Tx) error {
		calls++
		_, err := tx.ExecContext(ctx, "CREATE TABLE sample (value INTEGER); INSERT INTO sample VALUES (1)")
		return err
	}}
	for range 2 {
		if err := storage.Migrate(context.Background(), db.Writer, []storage.Migration{first}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openDatabase(t, path)
	migrations := []storage.Migration{first, sqlMigration(2, "UPDATE sample SET value=2")}
	for range 2 {
		if err := storage.Migrate(context.Background(), db.Writer, migrations); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("version 1 applied %d times", calls)
	}
	if queryInt(t, db.Reader, "SELECT value FROM sample") != 2 || queryInt(t, db.Reader, "SELECT COUNT(*) FROM schema_migrations") != 2 {
		t.Fatal("new version did not persist exactly once")
	}
}

func TestFailedMigrationRollsBackDDLDataAndVersion(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	first := sqlMigration(1, "CREATE TABLE sample (value INTEGER)", "INSERT INTO sample VALUES (1)")
	changes := sqlMigration(2, "ALTER TABLE sample ADD COLUMN note TEXT", "UPDATE sample SET value=2", "CREATE TABLE scratch (value INTEGER)")
	failure := errors.New("injected migration failure")
	second := storage.Migration{Version: 2, Apply: func(ctx context.Context, tx *sql.Tx) error {
		if err := changes.Apply(ctx, tx); err != nil {
			return err
		}
		return failure
	}}
	err := storage.Migrate(context.Background(), db.Writer, []storage.Migration{first, second, sqlMigration(3, "CREATE TABLE later (value INTEGER)")})
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "migration 2") {
		t.Fatalf("migration error: %v", err)
	}
	if queryInt(t, db.Reader, "SELECT value FROM sample") != 1 {
		t.Fatal("failed migration changed data")
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM pragma_table_info('sample') WHERE name='note'") != 0 {
		t.Fatal("ALTER TABLE was not rolled back")
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM sqlite_schema WHERE name IN ('scratch','later')") != 0 {
		t.Fatal("failed or subsequent migration created a table")
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM schema_migrations") != 1 {
		t.Fatal("prior commit lost or failed version recorded")
	}
	if err := storage.Migrate(context.Background(), db.Writer, []storage.Migration{first, changes}); err != nil {
		t.Fatal(err)
	}
	if queryInt(t, db.Reader, "SELECT value FROM sample") != 2 {
		t.Fatal("failed version could not be retried")
	}
}

func TestVersionRecordFailureAlsoRollsBackMigration(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	if err := storage.Migrate(context.Background(), db.Writer, nil); err != nil {
		t.Fatal(err)
	}
	execSQL(t, db.Writer, "CREATE TRIGGER block_version BEFORE INSERT ON schema_migrations BEGIN SELECT RAISE(ABORT, 'blocked version record'); END")
	err := storage.Migrate(context.Background(), db.Writer, []storage.Migration{sqlMigration(1, "CREATE TABLE sample (value INTEGER)")})
	if err == nil || !strings.Contains(err.Error(), "record version") {
		t.Fatalf("version record error: %v", err)
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM sqlite_schema WHERE name='sample'") != 0 || queryInt(t, db.Reader, "SELECT COUNT(*) FROM schema_migrations") != 0 {
		t.Fatal("migration changes and version record were not atomic")
	}
}

func TestMigrationCommitFailureRollsBack(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	execSQL(t, db.Writer, "CREATE TABLE parent (id INTEGER PRIMARY KEY); CREATE TABLE child (parent_id INTEGER REFERENCES parent(id) DEFERRABLE INITIALLY DEFERRED)")
	// 延迟外键检查使 Apply 和版本写入都成功，直到 COMMIT 才产生真实 SQLite 错误。
	err := storage.Migrate(context.Background(), db.Writer, []storage.Migration{sqlMigration(1, "INSERT INTO child VALUES (99)")})
	if err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("commit error: %v", err)
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM child") != 0 || queryInt(t, db.Reader, "SELECT COUNT(*) FROM schema_migrations") != 0 {
		t.Fatal("failed commit persisted data or version")
	}
}

func TestCancelledMigrationRollsBack(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	migration := storage.Migration{Version: 1, Apply: func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE TABLE sample (value INTEGER)"); err != nil {
			return err
		}
		cancel()
		return ctx.Err()
	}}
	if err := storage.Migrate(ctx, db.Writer, []storage.Migration{migration}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error: %v", err)
	}
	if queryInt(t, db.Reader, "SELECT COUNT(*) FROM sqlite_schema WHERE name='sample'") != 0 || queryInt(t, db.Reader, "SELECT COUNT(*) FROM schema_migrations") != 0 {
		t.Fatal("cancelled migration persisted changes")
	}
}

func TestConcurrentMigrationsApplyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ranking.db")
	one, two := openDatabase(t, path), openDatabase(t, path)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var calls atomic.Int32
	migrations := []storage.Migration{{Version: 1, Apply: func(ctx context.Context, tx *sql.Tx) error {
		calls.Add(1)
		_, err := tx.ExecContext(ctx, "CREATE TABLE sample (value INTEGER); INSERT INTO sample VALUES (1)")
		return err
	}}}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, db := range []*storage.DB{one, two} {
		go func() { <-start; results <- storage.Migrate(ctx, db.Writer, migrations) }()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 || queryInt(t, one.Reader, "SELECT COUNT(*) FROM sample") != 1 || queryInt(t, one.Reader, "SELECT COUNT(*) FROM schema_migrations") != 1 {
		t.Fatal("concurrent startup duplicated a migration")
	}
}

func TestInvalidMigrationDeclarationsFailBeforeChanges(t *testing.T) {
	for name, migrations := range map[string][]storage.Migration{
		"zero": {sqlMigration(0)}, "negative": {sqlMigration(-1)},
		"duplicate": {sqlMigration(1), sqlMigration(1)}, "descending": {sqlMigration(2), sqlMigration(1)},
		"missing-function": {{Version: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
			if err := storage.Migrate(context.Background(), db.Writer, migrations); err == nil {
				t.Fatal("expected invalid declaration error")
			}
			if queryInt(t, db.Reader, "SELECT COUNT(*) FROM sqlite_schema WHERE type='table'") != 0 {
				t.Fatal("invalid declaration changed database")
			}
		})
	}
}

func TestMigrationInitializationErrorIsReturned(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	err := storage.Migrate(context.Background(), db.Reader, nil)
	if err == nil || !strings.Contains(err.Error(), "create migration ledger") {
		t.Fatalf("initialization error: %v", err)
	}
}
