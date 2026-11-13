package reports

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"cpa-usage-ranking/internal/periods"
)

// MaintenanceOptions 为日结扫描、失败冷却及观测设置有限预算。
type MaintenanceOptions struct {
	BatchSize           int
	Rounds              int
	Interval            time.Duration
	Cooldown            time.Duration
	ObservationInterval time.Duration
	ReadTimeout         time.Duration
	DailyRetentionDays  int
}

func DefaultMaintenanceOptions() MaintenanceOptions {
	return MaintenanceOptions{BatchSize: 16, Rounds: 4, Interval: time.Second, Cooldown: time.Minute, ObservationInterval: time.Minute, ReadTimeout: 250 * time.Millisecond, DailyRetentionDays: 31}
}

type settlementKey struct {
	day    string
	client int64
}

// Maintenance 只有一个生产者；状态为常量空间，不保存逐客户端失败表。
type Maintenance struct {
	reader, db      *sql.DB
	writer          *Writer
	logger          *slog.Logger
	options         MaintenanceOptions
	cursor          settlementKey
	cooldownUntil   time.Time
	lastWarning     time.Time
	suppressed      int64
	lastObservation time.Time
	nextRetention   time.Time
}

func NewMaintenance(reader, db *sql.DB, writer *Writer, logger *slog.Logger, options MaintenanceOptions) (*Maintenance, error) {
	if reader == nil || db == nil || writer == nil || logger == nil || options.BatchSize <= 0 || options.Rounds <= 0 || options.Interval <= 0 || options.Cooldown <= 0 || options.ObservationInterval <= 0 || options.ReadTimeout <= 0 || options.DailyRetentionDays < 1 || options.DailyRetentionDays > MaxDailyRetentionDays {
		return nil, errors.New("invalid maintenance options")
	}
	return &Maintenance{reader: reader, db: db, writer: writer, logger: logger, options: options}, nil
}

// Run 必须恰好调用一次。取消后停止生产；已入队事务会收到同一取消信号。
func (m *Maintenance) Run(ctx context.Context) {
	ticker := time.NewTicker(m.options.Interval)
	defer ticker.Stop()
	for {
		m.observe(ctx)
		m.advance(ctx)
		m.prune(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Maintenance) advance(ctx context.Context) {
	for round := 0; round < m.options.Rounds && ctx.Err() == nil; round++ {
		page, err := m.page(ctx)
		if err != nil {
			if ctx.Err() == nil {
				m.warn("settlement scan failed", err, settlementKey{})
			}
			return
		}
		if len(page) == 0 {
			// 失败只影响下一轮从头扫描的最早时间，不阻塞本轮后续客户端。
			if !time.Now().Before(m.cooldownUntil) {
				m.cursor = settlementKey{}
			}
			return
		}
		for _, key := range page {
			if ctx.Err() != nil {
				return
			}
			result, err := m.writer.EnqueueMaintenance(ctx, func(txctx context.Context) error {
				_, err := Finalize(txctx, m.db, key.day, key.client)
				return err
			})
			if err != nil {
				if ctx.Err() == nil && !errors.Is(err, ErrQueueFull) {
					m.warn("settlement enqueue failed", err, key)
				}
				return
			}
			select {
			case <-ctx.Done():
				return
			case err = <-result:
			}
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				m.cooldownUntil = time.Now().Add(m.options.Cooldown)
				m.warn("settlement item failed", err, key)
			}
			m.cursor = key
		}
	}
}

func (m *Maintenance) prune(ctx context.Context) {
	if ctx.Err() != nil || time.Now().Before(m.nextRetention) {
		return
	}
	more, failed := false, false
	for _, kind := range []string{periods.Day, periods.Week} {
		var deleted int64
		result, err := m.writer.EnqueueMaintenance(ctx, func(txctx context.Context) error {
			var err error
			deleted, err = Prune(txctx, m.db, time.Now(), m.options.DailyRetentionDays, kind, m.options.BatchSize)
			return err
		})
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, ErrQueueFull) {
				m.warn("retention enqueue failed", err, settlementKey{})
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case err = <-result:
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			failed = true
			m.warn("retention batch failed", err, settlementKey{})
		} else if deleted == int64(m.options.BatchSize) {
			more = true
		}
	}
	// 无过期行时避免每秒写事务；存在积压则每轮继续一小批。
	if failed {
		m.nextRetention = time.Now().Add(m.options.Cooldown)
	} else if !more {
		m.nextRetention = time.Now().Add(m.options.ObservationInterval)
	}
}

func (m *Maintenance) page(ctx context.Context) ([]settlementKey, error) {
	readCtx, cancel := context.WithTimeout(ctx, m.options.ReadTimeout)
	defer cancel()
	rows, err := m.reader.QueryContext(readCtx, `SELECT day,client_id FROM daily_reports
		WHERE (day,client_id) > (?,?) AND day < (SELECT closed_before_day FROM report_window WHERE id=1)
		ORDER BY day,client_id LIMIT ?`, m.cursor.day, m.cursor.client, m.options.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("query settlement page: %w", err)
	}
	defer rows.Close()
	page := make([]settlementKey, 0, m.options.BatchSize)
	for rows.Next() {
		var key settlementKey
		if err := rows.Scan(&key.day, &key.client); err != nil {
			return nil, err
		}
		page = append(page, key)
	}
	return page, rows.Err()
}

func (m *Maintenance) warn(message string, err error, key settlementKey) {
	now := time.Now()
	if !m.lastWarning.IsZero() && now.Sub(m.lastWarning) < m.options.ObservationInterval {
		m.suppressed++
		return
	}
	m.logger.Warn(message, "day", key.day, "client_id", key.client, "error", err, "suppressed", m.suppressed)
	m.lastWarning, m.suppressed = now, 0
}

func (m *Maintenance) observe(ctx context.Context) {
	now := time.Now()
	if !m.lastObservation.IsZero() && now.Sub(m.lastObservation) < m.options.ObservationInterval {
		return
	}
	m.lastObservation = now
	readCtx, cancel := context.WithTimeout(ctx, m.options.ReadTimeout)
	defer cancel()
	var count int64
	var oldest sql.NullString
	err := m.reader.QueryRowContext(readCtx, `SELECT count(*),min(day) FROM daily_reports WHERE day < (SELECT closed_before_day FROM report_window WHERE id=1)`).Scan(&count, &oldest)
	if err != nil {
		if ctx.Err() == nil {
			m.warn("settlement backlog observation failed", err, settlementKey{})
		}
		return
	}
	age := time.Duration(0)
	if oldest.Valid {
		if day, err := periods.ParseBusinessDay(oldest.String); err == nil && now.After(day) {
			age = now.Sub(day)
		}
	}
	m.logger.Info("settlement backlog", "count", count, "oldest_day", oldest.String, "oldest_age", age.String())
}
