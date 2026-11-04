package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// Migration 的版本必须为正且按递增顺序声明；已发布的迁移保持不变。
// Apply 只通过传入的事务执行数据库操作，不自行提交、回滚或产生外部副作用。
type Migration struct {
	Version int
	Apply   func(context.Context, *sql.Tx) error
}

// Migrate 逐版本提交，每个版本的变更和成功记录处于同一事务。
// writer 使用 Open 返回的写池；当前没有业务迁移时传入空列表即可。
func Migrate(ctx context.Context, writer *sql.DB, migrations []Migration) error {
	previous := 0
	for _, migration := range migrations {
		if migration.Version <= previous || migration.Apply == nil {
			return fmt.Errorf("invalid migration %d: versions must be positive and increasing, and Apply must be set", migration.Version)
		}
		previous = migration.Version
	}
	if _, err := writer.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)"); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	for _, migration := range migrations {
		if err := applyMigration(ctx, writer, migration); err != nil {
			return fmt.Errorf("migration %d: %w", migration.Version, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, writer *sql.DB, migration Migration) error {
	// 写池以 IMMEDIATE 开始事务：拿到写锁后再查版本，避免两个启动者重复执行。
	tx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	var applied int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", migration.Version).Scan(&applied); err != nil {
		return fmt.Errorf("read version: %w", err)
	}
	if applied == 0 {
		if err := migration.Apply(ctx, tx); err != nil {
			return fmt.Errorf("apply: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", migration.Version); err != nil {
			return fmt.Errorf("record version: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
