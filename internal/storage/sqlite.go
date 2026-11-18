// Package storage 管理 SQLite 连接与表结构初始化，业务存取由对应模块负责。
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// DB 持有一个串行写池和一个按需建立连接的只读池。
// 调用方须在关闭 HTTP 与后台任务之后调用 Close。
type DB struct {
	Writer *sql.DB
	Reader *sql.DB
}

// Open 接受普通文件路径，不接受 SQLite DSN。连接参数在每次建连时生效。
func Open(ctx context.Context, path string) (*DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}
	options := url.Values{
		"mode": {"rwc"}, "_journal_mode": {"WAL"}, "_synchronous": {"FULL"},
		"_foreign_keys": {"on"}, "_busy_timeout": {"5000"}, "_txlock": {"immediate"},
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: options.Encode()}
	writer, err := openPool(ctx, uri.String(), 1, 1)
	if err != nil {
		return nil, fmt.Errorf("open sqlite writer: %w", err)
	}
	var mode string
	if err := writer.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		writer.Close()
		return nil, fmt.Errorf("read sqlite journal mode: %w", err)
	}
	if mode != "wal" {
		writer.Close()
		return nil, fmt.Errorf("sqlite requires WAL, got %q", mode)
	}

	// 读池不修改文件的 journal 模式，也不获取写事务锁。
	options.Set("mode", "ro")
	options.Del("_journal_mode")
	options.Del("_txlock")
	uri.RawQuery = options.Encode()
	reader, err := openPool(ctx, uri.String(), 8, 4)
	if err != nil {
		writer.Close()
		return nil, fmt.Errorf("open sqlite reader: %w", err)
	}
	return &DB{Writer: writer, Reader: reader}, nil
}

func openPool(ctx context.Context, dsn string, maxOpen, maxIdle int) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) Close() error {
	return errors.Join(db.Reader.Close(), db.Writer.Close())
}
