package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// InitSchema atomically initializes a new database or reopens the current schema.
// It does not upgrade or convert databases from other schemas.
func InitSchema(ctx context.Context, writer *sql.DB) error {
	tx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema initialization: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS clients (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_hash BLOB NOT NULL UNIQUE
			CHECK (typeof(token_hash) = 'blob' AND length(token_hash) = 32),
		display_name TEXT NOT NULL,
		avatar_id INTEGER NOT NULL CHECK (avatar_id > 0),
		disabled INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
		created_at_ms INTEGER NOT NULL,
		ranking_hidden INTEGER NOT NULL DEFAULT 0 CHECK (ranking_hidden IN (0, 1))
	);
	CREATE INDEX IF NOT EXISTS clients_ranking_hidden_id ON clients(ranking_hidden, id);
	CREATE TABLE IF NOT EXISTS models (
		id INTEGER PRIMARY KEY AUTOINCREMENT CHECK (id > 0),
		model_name TEXT NOT NULL UNIQUE
	);
	CREATE TABLE IF NOT EXISTS report_window (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		closed_before_day TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS daily_reports (
		day TEXT NOT NULL,
		client_id INTEGER NOT NULL REFERENCES clients(id),
		revision INTEGER NOT NULL CHECK (revision >= 0),
		digest BLOB NOT NULL CHECK (typeof(digest) = 'blob' AND length(digest) = 32),
		received_at_ms INTEGER NOT NULL,
		PRIMARY KEY (day, client_id)
	) WITHOUT ROWID;
	CREATE TABLE IF NOT EXISTS daily_stats (
		day TEXT NOT NULL,
		client_id INTEGER NOT NULL,
		model_id INTEGER NOT NULL CHECK (model_id >= 0),
		tier INTEGER NOT NULL CHECK (tier IN (0, 1, 2)),
		total_tokens INTEGER NOT NULL CHECK (total_tokens >= 0),
		input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
		output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
		cache_read_tokens INTEGER NOT NULL CHECK (cache_read_tokens >= 0),
		cache_write_tokens INTEGER NOT NULL CHECK (cache_write_tokens >= 0),
		reasoning_tokens INTEGER NOT NULL CHECK (reasoning_tokens >= 0),
		request_count INTEGER NOT NULL CHECK (request_count > 0),
		success_count INTEGER NOT NULL CHECK (success_count >= 0),
		ttft_sum_ms REAL NOT NULL CHECK (ttft_sum_ms >= 0),
		ttft_samples INTEGER NOT NULL CHECK (ttft_samples >= 0),
		latency_sum_ms REAL NOT NULL CHECK (latency_sum_ms >= 0),
		latency_samples INTEGER NOT NULL CHECK (latency_samples >= 0),
		PRIMARY KEY (day, client_id, model_id, tier),
		FOREIGN KEY (day, client_id) REFERENCES daily_reports(day, client_id) ON DELETE CASCADE,
		CHECK (model_id > 0 OR tier = 0)
	) WITHOUT ROWID;
	CREATE INDEX IF NOT EXISTS daily_stats_totals ON daily_stats(day, client_id) WHERE model_id=0;
	CREATE TABLE IF NOT EXISTS period_stats (
		kind TEXT NOT NULL CHECK (kind IN ('day', 'week', 'month', 'all')),
		start_day TEXT NOT NULL,
		model_id INTEGER NOT NULL CHECK (model_id >= 0),
		tier INTEGER NOT NULL CHECK (tier IN (0, 1, 2)),
		client_id INTEGER NOT NULL REFERENCES clients(id),
		total_tokens INTEGER NOT NULL CHECK (typeof(total_tokens) = 'integer' AND total_tokens >= 0),
		input_tokens INTEGER NOT NULL CHECK (typeof(input_tokens) = 'integer' AND input_tokens >= 0),
		output_tokens INTEGER NOT NULL CHECK (typeof(output_tokens) = 'integer' AND output_tokens >= 0),
		cache_read_tokens INTEGER NOT NULL CHECK (typeof(cache_read_tokens) = 'integer' AND cache_read_tokens >= 0),
		cache_write_tokens INTEGER NOT NULL CHECK (typeof(cache_write_tokens) = 'integer' AND cache_write_tokens >= 0),
		reasoning_tokens INTEGER NOT NULL CHECK (typeof(reasoning_tokens) = 'integer' AND reasoning_tokens >= 0),
		request_count INTEGER NOT NULL CHECK (typeof(request_count) = 'integer' AND request_count > 0),
		success_count INTEGER NOT NULL CHECK (typeof(success_count) = 'integer' AND success_count >= 0),
		ttft_sum_ms REAL NOT NULL CHECK (ttft_sum_ms >= 0),
		ttft_samples INTEGER NOT NULL CHECK (typeof(ttft_samples) = 'integer' AND ttft_samples >= 0),
		latency_sum_ms REAL NOT NULL CHECK (latency_sum_ms >= 0),
		latency_samples INTEGER NOT NULL CHECK (typeof(latency_samples) = 'integer' AND latency_samples >= 0),
		PRIMARY KEY (kind, start_day, model_id, tier, client_id),
		CHECK (model_id > 0 OR tier = 0),
		CHECK (kind <> 'day' OR model_id = 0),
		CHECK (kind <> 'all' OR start_day = '0001-01-01')
	) WITHOUT ROWID;
	CREATE TABLE IF NOT EXISTS admin_accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash BLOB NOT NULL,
		created_at_ms INTEGER NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("initialize schema: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema initialization: %w", err)
	}
	return nil
}
