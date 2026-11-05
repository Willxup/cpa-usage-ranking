package storage

import (
	"context"
	"database/sql"
)

// MigrateSchema 应用当前程序的显式迁移列表；后续版本只在列表末尾追加。
func MigrateSchema(ctx context.Context, writer *sql.DB) error {
	return Migrate(ctx, writer, []Migration{
		{Version: 1, Apply: createClientsV1},
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
