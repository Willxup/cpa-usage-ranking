// Package statistics 提供内部候选统计，不定义公开榜单或查询接口。
package statistics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"cpa-usage-ranking/internal/models"
	"cpa-usage-ranking/internal/periods"
	"cpa-usage-ranking/internal/reports"
)

const (
	DefaultMaxCandidates = 100000
	DefaultReadTimeout   = 15 * time.Second
)

var (
	ErrInvalidSelection = errors.New("invalid statistics selection")
	ErrCapacity         = errors.New("statistics candidate capacity exceeded")
	ErrOverflow         = errors.New("statistics metrics overflow")
)

// Selection 指定一种视角；日期仅用于选择包含它的 UTC 周期。
// 全模型合计是 ModelID=0、Tier=0 的独立视角，不能与明细相加。
type Selection struct {
	Kind    string
	Day     string
	ModelID int64
	Tier    models.Tier
}

type Snapshot struct {
	Kind, Start, End string                    // End 为空表示累计周期无上界。
	Rows             map[int64]reports.Metrics // client ID -> 原始统计
}

type Reader struct {
	db            *sql.DB
	maxCandidates int
	timeout       time.Duration
}

func NewReader(db *sql.DB, maxCandidates int, timeout time.Duration) (*Reader, error) {
	if db == nil || maxCandidates <= 0 || timeout <= 0 {
		return nil, errors.New("invalid statistics reader options")
	}
	return &Reader{db: db, maxCandidates: maxCandidates, timeout: timeout}, nil
}

// Read 的两部分查询和数值合并处于同一真实 SQLite 快照中；返回前结束事务。
// 已结日摘要只有全模型合计，因此日视角不支持模型明细，即使该日尚未日结。
func (r *Reader) Read(ctx context.Context, s Selection) (Snapshot, error) {
	bounds, err := periods.ForDay(s.Day)
	if err != nil || s.ModelID < 0 || s.Tier > models.Flex || (s.ModelID == 0 && s.Tier != models.Default) {
		return Snapshot{}, ErrInvalidSelection
	}
	result := Snapshot{Kind: s.Kind}
	switch s.Kind {
	case periods.Day:
		if s.ModelID != 0 {
			return Snapshot{}, ErrInvalidSelection
		}
		d, _ := time.Parse("2006-01-02", s.Day)
		result.Start, result.End = s.Day, d.AddDate(0, 0, 1).Format("2006-01-02")
		if len(result.End) != 10 {
			return Snapshot{}, ErrInvalidSelection
		}
	case periods.Week:
		result.Start, result.End = bounds.WeekStart, bounds.WeekEnd
	case periods.Month:
		result.Start, result.End = bounds.MonthStart, bounds.MonthEnd
	case periods.All:
		result.Start = periods.AllStart
	default:
		return Snapshot{}, ErrInvalidSelection
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Snapshot{}, fmt.Errorf("begin statistics read: %w", err)
	}
	defer tx.Rollback()
	result.Rows = make(map[int64]reports.Metrics)
	columns := `client_id,total_tokens,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,reasoning_tokens,request_count,success_count,ttft_sum_ms,ttft_samples,latency_sum_ms,latency_samples`
	if err := r.merge(ctx, tx, result.Rows, `SELECT `+columns+` FROM period_stats WHERE kind=? AND start_day=? AND model_id=? AND tier=?`, s.Kind, result.Start, s.ModelID, s.Tier); err != nil {
		return Snapshot{}, err
	}
	query := `SELECT ` + columns + ` FROM daily_stats WHERE model_id=? AND tier=?`
	args := []any{s.ModelID, s.Tier}
	if s.Kind != periods.All {
		query += ` AND day>=? AND day<?`
		args = append(args, result.Start, result.End)
	}
	if err := r.merge(ctx, tx, result.Rows, query, args...); err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, fmt.Errorf("finish statistics read: %w", err)
	}
	return result, nil
}

func (r *Reader) merge(ctx context.Context, tx *sql.Tx, dest map[int64]reports.Metrics, query string, args ...any) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query statistics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var m reports.Metrics
		if err := rows.Scan(&id, &m.TotalTokens, &m.InputTokens, &m.OutputTokens, &m.CacheReadTokens, &m.CacheWriteTokens, &m.ReasoningTokens, &m.RequestCount, &m.SuccessCount, &m.TTFTSumMS, &m.TTFTSamples, &m.LatencySumMS, &m.LatencySamples); err != nil {
			return fmt.Errorf("scan statistics: %w", err)
		}
		old, exists := dest[id]
		if !exists && len(dest) >= r.maxCandidates {
			return ErrCapacity
		}
		merged, ok := reports.AddMetrics(old, m)
		if !ok {
			return ErrOverflow
		}
		dest[id] = merged
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate statistics: %w", err)
	}
	return nil
}
