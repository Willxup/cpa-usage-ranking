// Package readiness 将已经初始化的依赖与首轮真实缓存组合为业务就绪条件。
package readiness

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"cpa-usage-ranking/internal/avatars"
	"cpa-usage-ranking/internal/cache"
	"cpa-usage-ranking/internal/models"
	"cpa-usage-ranking/internal/storage"
)

type Check struct {
	db      *storage.DB
	models  *models.Registry
	avatars *avatars.Catalog
	cache   *cache.Cache
}

func New(db *storage.DB, registry *models.Registry, catalog *avatars.Catalog, snapshots *cache.Cache) *Check {
	return &Check{db: db, models: registry, avatars: catalog, cache: snapshots}
}

// Ready 不构建缓存；一次成功之后的刷新失败不使旧的完整快照失效。
func (c *Check) Ready(ctx context.Context) bool {
	if c.db == nil || c.db.Reader == nil || c.db.Writer == nil || c.models == nil || c.avatars == nil || c.cache == nil {
		return false
	}
	if _, err := c.cache.Current(); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := c.db.Writer.PingContext(ctx); err != nil {
		return false
	}
	var row int
	err := c.db.Reader.QueryRowContext(ctx, `SELECT 1 FROM period_stats LIMIT 1`).Scan(&row)
	return err == nil || errors.Is(err, sql.ErrNoRows)
}
