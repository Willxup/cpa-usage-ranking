package reports

import (
	"context"
	"cpa-usage-ranking/internal/periods"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrInvalidDay = errors.New("invalid business day")
	ErrFutureDay  = errors.New("future business day")
	ErrExpiredDay = errors.New("expired business day")
	// ErrWindowAdvance means the caller must roll back, persist the watermark
	// separately, then recheck in a new transaction.
	ErrWindowAdvance = errors.New("report window watermark needs advancement")
)

const dayLayout = "2006-01-02"

func parseDay(day string) (time.Time, error) {
	parsed, err := time.Parse(dayLayout, day)
	if err != nil || len(day) != len(dayLayout) || parsed.Format(dayLayout) != day {
		return time.Time{}, ErrInvalidDay
	}
	return parsed, nil
}

func businessWindow(now time.Time) (today, yesterday string, err error) {
	start := periods.StartOfDay(now)
	today, yesterday = start.Format(dayLayout), start.AddDate(0, 0, -1).Format(dayLayout)
	if len(today) != 10 || len(yesterday) != 10 {
		return "", "", ErrInvalidDay
	}
	return today, yesterday, nil
}

// AdvanceWindow commits the observed Shanghai business-day boundary independently of a report
// transaction. It must run at startup and before admission to a report write;
// unsuccessful reports must not roll the boundary back.
func AdvanceWindow(ctx context.Context, writer *sql.DB, now time.Time) error {
	// Retain the connection's usual five-second lock-wait budget while allowing
	// startup/shutdown cancellation to interrupt a background checkpoint sooner.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return withReportWriter(ctx, writer, func(conn *sql.Conn) error {
		return advanceWindow(ctx, now, conn.ExecContext)
	})
}

func advanceWindow(ctx context.Context, now time.Time, exec func(context.Context, string, ...any) (sql.Result, error)) error {
	_, yesterday, err := businessWindow(now)
	if err != nil {
		return err
	}
	_, err = exec(ctx, `INSERT INTO report_window (id, closed_before_day) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET closed_before_day=excluded.closed_before_day
		WHERE report_window.closed_before_day < excluded.closed_before_day`, yesterday)
	return err
}

// CheckWindow uses the write transaction's own connection, and must precede
// every revision decision. A midnight crossing may require a separate advance.
func CheckWindow(ctx context.Context, tx *sql.Tx, day string, now time.Time) error {
	today, yesterday, err := businessWindow(now)
	if err != nil {
		return err
	}
	var watermark string
	err = tx.QueryRowContext(ctx, "SELECT closed_before_day FROM report_window WHERE id=1").Scan(&watermark)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && watermark < yesterday) {
		return ErrWindowAdvance
	}
	if err != nil {
		return err
	}
	if _, err := parseDay(day); err != nil {
		return err
	}
	if day > today {
		return ErrFutureDay
	}
	if day < watermark || day < yesterday {
		return ErrExpiredDay
	}
	return nil
}
