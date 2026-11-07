package reports

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"cpa-usage-ranking/internal/models"
)

var ErrClientDisabled = errors.New("client disabled")
var ErrClientMissing = errors.New("client missing")

type RevisionConflict struct{ Current int64 }

func (e *RevisionConflict) Error() string { return "report revision conflict" }

// Store replaces one client/day snapshot atomically using the single writer.
// The clock may be injected by tests; it must be safe for concurrent use.
type Store struct {
	writer   *sql.DB
	registry *models.Registry
	now      func() time.Time
}

func NewStore(writer *sql.DB, registry *models.Registry, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{writer: writer, registry: registry, now: now}
}

// Apply confirms only after commit, or after reading an identical committed
// version. Watermark advancement is intentionally a separate committed write.
func (s *Store) Apply(ctx context.Context, clientID int64, day string, prepared Prepared) (int64, error) {
	if !prepared.valid || clientID <= 0 {
		return 0, ErrInvalid
	}
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if err := AdvanceWindow(ctx, s.writer, s.now()); err != nil {
			return 0, fmt.Errorf("advance report window: %w", err)
		}
		revision, err := s.applyOnce(ctx, clientID, day, prepared)
		if errors.Is(err, ErrWindowAdvance) {
			continue
		}
		return revision, err
	}
}

func (s *Store) applyOnce(ctx context.Context, clientID int64, day string, prepared Prepared) (int64, error) {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin report: %w", err)
	}
	defer tx.Rollback()
	if err := CheckWindow(ctx, tx, day, s.now()); err != nil {
		return 0, err
	}
	var disabled bool
	err = tx.QueryRowContext(ctx, "SELECT disabled FROM clients WHERE id=?", clientID).Scan(&disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrClientMissing
	}
	if err != nil {
		return 0, fmt.Errorf("read client state: %w", err)
	}
	if disabled {
		return 0, ErrClientDisabled
	}

	var current Version
	var digest []byte
	err = tx.QueryRowContext(ctx, "SELECT revision,digest FROM daily_reports WHERE day=? AND client_id=?", day, clientID).Scan(&current.Revision, &digest)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("read report version: %w", err)
	}
	if exists {
		if len(digest) != len(current.Digest) {
			return 0, errors.New("invalid stored report digest")
		}
		copy(current.Digest[:], digest)
	}
	var previous *Version
	if exists {
		previous = &current
	}
	switch DecideVersion(previous, Version{Revision: prepared.revision, Digest: prepared.digest}) {
	case Conflict:
		if err := CheckWindow(ctx, tx, day, s.now()); err != nil {
			return 0, err
		}
		return 0, &RevisionConflict{Current: current.Revision}
	case Confirm:
		if err := CheckWindow(ctx, tx, day, s.now()); err != nil {
			return 0, err
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return current.Revision, nil
	}

	// Matching is intentionally after the decision: an old retry must not be
	// invalidated by current catalog changes or new aggregation overflow.
	rows, err := prepared.Aggregate(s.registry)
	if err != nil {
		return 0, err
	}
	old, err := loadStats(ctx, tx, day, clientID)
	if err != nil {
		return 0, err
	}
	if exists {
		_, err = tx.ExecContext(ctx, `UPDATE daily_reports SET revision=?,digest=?,received_at_ms=? WHERE day=? AND client_id=?`, prepared.revision, prepared.digest[:], s.now().UTC().UnixMilli(), day, clientID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO daily_reports(day,client_id,revision,digest,received_at_ms) VALUES(?,?,?,?,?)`, day, clientID, prepared.revision, prepared.digest[:], s.now().UTC().UnixMilli())
	}
	if err != nil {
		return 0, fmt.Errorf("write report version: %w", err)
	}
	for _, key := range sortedKeys(old) {
		if _, ok := rows[key]; !ok {
			if _, err := tx.ExecContext(ctx, `DELETE FROM daily_stats WHERE day=? AND client_id=? AND model_id=? AND tier=?`, day, clientID, key.ModelID, key.Tier); err != nil {
				return 0, fmt.Errorf("delete report row: %w", err)
			}
		}
	}
	for _, key := range sortedKeys(rows) {
		metric := rows[key]
		if before, ok := old[key]; ok && before == metric {
			continue
		}
		if err := writeStats(ctx, tx, day, clientID, key, metric); err != nil {
			return 0, err
		}
	}
	if err := CheckWindow(ctx, tx, day, s.now()); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit report: %w", err)
	}
	return prepared.revision, nil
}

func sortedKeys(rows map[Key]Metrics) []Key {
	keys := make([]Key, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].ModelID != keys[j].ModelID {
			return keys[i].ModelID < keys[j].ModelID
		}
		return keys[i].Tier < keys[j].Tier
	})
	return keys
}

func loadStats(ctx context.Context, tx *sql.Tx, day string, clientID int64) (map[Key]Metrics, error) {
	result, err := tx.QueryContext(ctx, `SELECT model_id,tier,total_tokens,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,reasoning_tokens,request_count,success_count,ttft_sum_ms,ttft_samples,latency_sum_ms,latency_samples FROM daily_stats WHERE day=? AND client_id=?`, day, clientID)
	if err != nil {
		return nil, fmt.Errorf("read report rows: %w", err)
	}
	defer result.Close()
	rows := make(map[Key]Metrics)
	for result.Next() {
		var k Key
		var m Metrics
		if err := result.Scan(&k.ModelID, &k.Tier, &m.TotalTokens, &m.InputTokens, &m.OutputTokens, &m.CacheReadTokens, &m.CacheWriteTokens, &m.ReasoningTokens, &m.RequestCount, &m.SuccessCount, &m.TTFTSumMS, &m.TTFTSamples, &m.LatencySumMS, &m.LatencySamples); err != nil {
			return nil, fmt.Errorf("scan report row: %w", err)
		}
		rows[k] = m
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("iterate report rows: %w", err)
	}
	return rows, nil
}

func writeStats(ctx context.Context, tx *sql.Tx, day string, clientID int64, k Key, m Metrics) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_stats(day,client_id,model_id,tier,total_tokens,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,reasoning_tokens,request_count,success_count,ttft_sum_ms,ttft_samples,latency_sum_ms,latency_samples)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(day,client_id,model_id,tier) DO UPDATE SET
		total_tokens=excluded.total_tokens,input_tokens=excluded.input_tokens,output_tokens=excluded.output_tokens,
		cache_read_tokens=excluded.cache_read_tokens,cache_write_tokens=excluded.cache_write_tokens,reasoning_tokens=excluded.reasoning_tokens,
		request_count=excluded.request_count,success_count=excluded.success_count,ttft_sum_ms=excluded.ttft_sum_ms,ttft_samples=excluded.ttft_samples,
		latency_sum_ms=excluded.latency_sum_ms,latency_samples=excluded.latency_samples`,
		day, clientID, k.ModelID, k.Tier, m.TotalTokens, m.InputTokens, m.OutputTokens, m.CacheReadTokens, m.CacheWriteTokens,
		m.ReasoningTokens, m.RequestCount, m.SuccessCount, m.TTFTSumMS, m.TTFTSamples, m.LatencySumMS, m.LatencySamples)
	if err != nil {
		return fmt.Errorf("write report row: %w", err)
	}
	return nil
}
