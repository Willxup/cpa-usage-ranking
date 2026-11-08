package storage

import (
	"context"
	"database/sql"
)

// MigrateSchema 应用当前程序的显式迁移列表；后续版本只在列表末尾追加。
func MigrateSchema(ctx context.Context, writer *sql.DB) error {
	return Migrate(ctx, writer, []Migration{
		{Version: 1, Apply: createClientsV1},
		{Version: 2, Apply: createModelsV2},
		{Version: 3, Apply: createReportWindowV3},
		{Version: 4, Apply: createDailySnapshotsV4},
		{Version: 5, Apply: createPeriodStatsV5},
	})
}

func createClientsV1(ctx context.Context, tx *sql.Tx) error {
	// 不使用 IF NOT EXISTS 接管未知旧表；重复启动由迁移版本记录保证幂等。
	// AUTOINCREMENT 避免历史身份编号在记录被删除后重新分配。
	_, err := tx.ExecContext(ctx, `CREATE TABLE clients (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_hash BLOB NOT NULL UNIQUE
			CHECK (typeof(token_hash) = 'blob' AND length(token_hash) = 32),
		display_name TEXT NOT NULL,
		avatar_id INTEGER NOT NULL CHECK (avatar_id > 0),
		disabled INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
		created_at_ms INTEGER NOT NULL
	)`)
	return err
}

func createPeriodStatsV5(ctx context.Context, tx *sql.Tx) error {
	// 周期键的顺序供按周期和模型读取；日摘要只允许全模型合计。
	_, err := tx.ExecContext(ctx, `CREATE TABLE period_stats (
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
	) WITHOUT ROWID`)
	return err
}

func createDailySnapshotsV4(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE daily_reports (
		day TEXT NOT NULL,
		client_id INTEGER NOT NULL REFERENCES clients(id),
		revision INTEGER NOT NULL CHECK (revision >= 0),
		digest BLOB NOT NULL CHECK (typeof(digest) = 'blob' AND length(digest) = 32),
		received_at_ms INTEGER NOT NULL,
		PRIMARY KEY (day, client_id)
	) WITHOUT ROWID;
	CREATE TABLE daily_stats (
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
	CREATE INDEX daily_stats_totals ON daily_stats(day, client_id) WHERE model_id=0`)
	return err
}

func createReportWindowV3(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE report_window (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		closed_before_day TEXT NOT NULL
	)`)
	return err
}

func createModelsV2(ctx context.Context, tx *sql.Tx) error {
	// ID 0 留给未来的合计语义；目录项始终使用正数且不复用已分配的 ID。
	_, err := tx.ExecContext(ctx, `CREATE TABLE models (
		id INTEGER PRIMARY KEY AUTOINCREMENT CHECK (id > 0),
		model_name TEXT NOT NULL UNIQUE
	)`)
	return err
}
