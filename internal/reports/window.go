package reports

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrInvalidDay = errors.New("invalid UTC day")
	ErrFutureDay  = errors.New("future UTC day")
	ErrExpiredDay = errors.New("expired UTC day")
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

func utcWindow(now time.Time) (today, yesterday string, err error) {
	n := now.UTC()
	start := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	today, yesterday = start.Format(dayLayout), start.AddDate(0, 0, -1).Format(dayLayout)
	if len(today) != 10 || len(yesterday) != 10 {
		return "", "", ErrInvalidDay
	}
	return today, yesterday, nil
}

// AdvanceWindow commits the observed UTC boundary independently of a report
// transaction. It must run at startup and before admission to a report write;
// unsuccessful reports must not roll the boundary back.
func AdvanceWindow(ctx context.Context, writer *sql.DB, now time.Time) error {
	_, yesterday, err := utcWindow(now)
	if err != nil {
		return err
	}
	_, err = writer.ExecContext(ctx, `INSERT INTO report_window (id, closed_before_day) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET closed_before_day=excluded.closed_before_day
		WHERE report_window.closed_before_day < excluded.closed_before_day`, yesterday)
	return err
}

// CheckWindow uses the write transaction's own connection, and must precede
// every revision decision. A midnight crossing may require a separate advance.
func CheckWindow(ctx context.Context, tx *sql.Tx, day string, now time.Time) error {
	today, yesterday, err := utcWindow(now)
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
