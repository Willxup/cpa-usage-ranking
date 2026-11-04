package test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-ranking/internal/storage"
	"github.com/mattn/go-sqlite3"
)

func openDatabase(t *testing.T, path string) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func execSQL(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), statement); err != nil {
		t.Fatal(err)
	}
}

func TestFileReopensAndEscapesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "usage ?mode=memory# 数据.db")
	db := openDatabase(t, path)
	execSQL(t, db.Writer, "CREATE TABLE sample (value TEXT); INSERT INTO sample VALUES ('persisted')")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	reopened := openDatabase(t, path)
	var value string
	if err := reopened.Reader.QueryRow("SELECT value FROM sample").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "persisted" {
		t.Fatalf("value = %q", value)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	for _, pool := range []*sql.DB{reopened.Writer, reopened.Reader} {
		if err := pool.Ping(); err == nil {
			t.Fatal("closed pool still accepts requests")
		}
	}
}

func checkSettings(t *testing.T, connection *sql.Conn) {
	t.Helper()
	for pragma, want := range map[string]int{"synchronous": 2, "foreign_keys": 1, "busy_timeout": 5000} {
		var got int
		if err := connection.QueryRowContext(context.Background(), "PRAGMA "+pragma).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s = %d, want %d", pragma, got, want)
		}
	}
	var journal string
	if err := connection.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if journal != "wal" {
		t.Fatalf("journal = %q", journal)
	}
}

func TestSettingsApplyToEveryConnectionAndReconnect(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	if db.Writer.Stats().MaxOpenConnections != 1 || db.Reader.Stats().MaxOpenConnections != 8 {
		t.Fatal("unexpected writer/reader pool bounds")
	}
	if db.Reader.Stats().OpenConnections != 1 {
		t.Fatal("reader connections should be created on demand")
	}
	connections := make([]*sql.Conn, 0, 8)
	for range 8 {
		connection, err := db.Reader.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { connection.Close() })
		connections = append(connections, connection)
		checkSettings(t, connection)
	}
	for _, connection := range connections {
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if db.Reader.Stats().Idle != 4 {
		t.Fatalf("idle readers = %d", db.Reader.Stats().Idle)
	}
	for _, pool := range []*sql.DB{db.Writer, db.Reader} {
		// 关闭空闲连接，下一次借用必须重新建立物理连接。
		pool.SetMaxIdleConns(0)
		for range 2 {
			connection, err := pool.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			checkSettings(t, connection)
			if err := connection.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestForeignKeysAndReadOnlyPool(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	execSQL(t, db.Writer, "CREATE TABLE parent (id INTEGER PRIMARY KEY); CREATE TABLE child (parent_id INTEGER REFERENCES parent(id))")
	_, err := db.Writer.Exec("INSERT INTO child VALUES (99)")
	var sqliteError sqlite3.Error
	if !errors.As(err, &sqliteError) || sqliteError.ExtendedCode != sqlite3.ErrConstraintForeignKey {
		t.Fatalf("foreign key insert returned %v", err)
	}
	_, err = db.Reader.Exec("INSERT INTO parent VALUES (1)")
	if !errors.As(err, &sqliteError) || sqliteError.Code != sqlite3.ErrReadonly {
		t.Fatalf("read-only write returned %v", err)
	}
}

func TestWALReadSnapshotDoesNotBlockWriter(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	execSQL(t, db.Writer, "CREATE TABLE sample (value INTEGER); INSERT INTO sample VALUES (1)")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tx, err := db.Reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var value int
	if err := tx.QueryRowContext(ctx, "SELECT value FROM sample").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != 1 {
		t.Fatal("unexpected initial value")
	}
	if _, err := db.Writer.ExecContext(ctx, "UPDATE sample SET value=2"); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT value FROM sample").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != 1 {
		t.Fatal("read transaction lost its snapshot")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := db.Reader.QueryRowContext(ctx, "SELECT value FROM sample").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != 2 {
		t.Fatal("new reader did not see committed data")
	}
}

func TestWriterPoolWaitCanBeCancelled(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	connection, err := db.Writer.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	other, err := db.Writer.Conn(ctx)
	if err == nil {
		other.Close()
		t.Fatal("writer pool allowed a second connection")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting error: %v", err)
	}
}

func TestOpenErrorsAreReturnedWithoutRepair(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "not-a-directory")
	corrupt := filepath.Join(root, "not-a-database")
	for _, path := range []string{blocked, corrupt} {
		if err := os.WriteFile(path, []byte("keep this content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{root, filepath.Join(blocked, "ranking.db"), corrupt} {
		db, err := storage.Open(context.Background(), path)
		if err == nil {
			db.Close()
			t.Fatalf("expected open failure for %s", path)
		}
	}
	content, err := os.ReadFile(corrupt)
	if err != nil || string(content) != "keep this content" {
		t.Fatalf("existing file changed: %q, %v", content, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = storage.Open(ctx, filepath.Join(root, "cancelled.db"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled open: %v", err)
	}
}

func TestLinkedSQLiteContainsWALResetFix(t *testing.T) {
	db := openDatabase(t, filepath.Join(t.TempDir(), "ranking.db"))
	var actual string
	if err := db.Writer.QueryRow("SELECT sqlite_version()").Scan(&actual); err != nil {
		t.Fatal(err)
	}
	version, number, _ := sqlite3.Version()
	t.Logf("actual SQLite version: %s (%d)", actual, number)
	// 默认捆绑版应包含官方 WAL-reset 修复；不依赖宿主 sqlite3 命令的版本。
	if actual != version || number < 3051003 {
		t.Fatalf("unsupported linked SQLite: %s (%d)", actual, number)
	}
}
