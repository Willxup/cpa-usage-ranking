package reports

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/mattn/go-sqlite3"
)

// withReportWriter shares the cancellable SQLite lock wait between snapshots
// and window checkpoints. Each attempt must finish/roll back its transaction
// before returning; this is private to the two report-writing paths.
func withReportWriter(ctx context.Context, writer *sql.DB, apply func(*sql.Conn) error) (resultErr error) {
	conn, err := writer.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire report writer: %w", err)
	}
	defer conn.Close()
	var busyTimeout int
	if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		return fmt.Errorf("read sqlite busy timeout: %w", err)
	}
	defer func() {
		// Restore the connection-local setting before returning it to other writers.
		if _, err := conn.ExecContext(context.Background(), fmt.Sprintf("PRAGMA busy_timeout=%d", busyTimeout)); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			resultErr = errors.Join(resultErr, fmt.Errorf("restore sqlite busy timeout: %w", err))
		}
	}()
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout=0"); err != nil {
		return fmt.Errorf("set sqlite busy timeout: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := apply(conn)
		if errors.Is(err, ErrWindowAdvance) {
			continue
		}
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked) {
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
				continue
			}
		}
		return err
	}
}
