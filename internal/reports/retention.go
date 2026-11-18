package reports

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"cpa-usage-ranking/internal/periods"
)

const MaxDailyRetentionDays = 36500

// Prune 删除一小批已日结的日或周统计；绝不操作尚未结算的源表。
// 日保留包含今天在内的 retainDays 个 UTC 日期，周保留当前周与上一周。
func Prune(ctx context.Context, writer *sql.DB, now time.Time, retainDays int, kind string, limit int) (int64, error) {
	if retainDays < 1 || retainDays > MaxDailyRetentionDays || limit < 1 || (kind != periods.Day && kind != periods.Week) {
		return 0, fmt.Errorf("invalid retention options")
	}
	today := now.UTC()
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	bounds, err := periods.ForDay(day.Format(dayLayout))
	if err != nil {
		return 0, err
	}
	cutoff := day.AddDate(0, 0, -(retainDays - 1)).Format(dayLayout)
	if kind == periods.Week {
		week, err := time.Parse(dayLayout, bounds.WeekStart)
		if err != nil {
			return 0, err
		}
		cutoff = week.AddDate(0, 0, -7).Format(dayLayout)
	}
	var deleted int64
	err = withReportWriter(ctx, writer, func(conn *sql.Conn) error {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin retention: %w", err)
		}
		defer tx.Rollback()
		// 主键前缀限制 kind 和日期，再按完整主键取有限行；单次事务不锁住整段历史。
		result, err := tx.ExecContext(ctx, `DELETE FROM period_stats WHERE (kind,start_day,model_id,client_id) IN (
			SELECT kind,start_day,model_id,client_id FROM period_stats
			WHERE kind=? AND start_day<? ORDER BY start_day,model_id,client_id LIMIT ?
		)`, kind, cutoff, limit)
		if err != nil {
			return fmt.Errorf("delete retained periods: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit retention: %w", err)
		}
		deleted = count
		return nil
	})
	return deleted, err
}
