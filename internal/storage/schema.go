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
