// Package clients 保存客户端身份与当前展示资料，不负责注册、鉴权或资料校验。
package clients

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"cpa-usage-ranking/internal/storage"

	"github.com/mattn/go-sqlite3"
)

var (
	ErrNotFound         = errors.New("client not found")
	ErrCredentialExists = errors.New("client credential already exists")
)

// CredentialHash 是凭据的 SHA-256 摘要；存储层不接收或计算凭据原文。
type CredentialHash [32]byte

// Client 是存储层元数据，不是公开 API 响应结构；不携带凭据摘要。
type Client struct {
	ID            int64
	DisplayName   string
	AvatarID      int64
	Disabled      bool
	RankingHidden bool
	CreatedAt     time.Time
}

type NewClient struct {
	CredentialHash CredentialHash
	DisplayName    string
	AvatarID       int64
}

type Repository struct {
	writer *sql.DB
	reader *sql.DB
}

func NewRepository(db *storage.DB) *Repository {
	return &Repository{writer: db.Writer, reader: db.Reader}
}

// Create 只创建新身份。重复摘要返回冲突，不把注册重试或资料覆盖藏在存储操作中。
func (r *Repository) Create(ctx context.Context, input NewClient) (Client, error) {
	createdAt := time.Now().UTC().Truncate(time.Millisecond)
	result, err := r.writer.ExecContext(ctx,
		"INSERT INTO clients (token_hash, display_name, avatar_id, created_at_ms) VALUES (?, ?, ?, ?)",
		input.CredentialHash[:], input.DisplayName, input.AvatarID, createdAt.UnixMilli())
	if err != nil {
		var sqliteError sqlite3.Error
		if errors.As(err, &sqliteError) && sqliteError.ExtendedCode == sqlite3.ErrConstraintUnique {
			return Client{}, ErrCredentialExists
		}
		return Client{}, fmt.Errorf("create client: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Client{}, fmt.Errorf("read created client id: %w", err)
	}
	return Client{ID: id, DisplayName: input.DisplayName, AvatarID: input.AvatarID, CreatedAt: createdAt}, nil
}

const selectClient = "SELECT id, display_name, avatar_id, disabled, created_at_ms, ranking_hidden FROM clients"

func (r *Repository) ByID(ctx context.Context, id int64) (Client, error) {
	return scanClient(r.reader.QueryRowContext(ctx, selectClient+" WHERE id = ?", id))
}

// ByCredentialHash 保留禁用状态；禁用身份不能被上层误当作尚未注册。
func (r *Repository) ByCredentialHash(ctx context.Context, hash CredentialHash) (Client, error) {
	return scanClient(r.reader.QueryRowContext(ctx, selectClient+" WHERE token_hash = ?", hash[:]))
}

func scanClient(row *sql.Row) (Client, error) {
	var client Client
	var createdAt int64
	if err := row.Scan(&client.ID, &client.DisplayName, &client.AvatarID, &client.Disabled, &createdAt, &client.RankingHidden); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Client{}, ErrNotFound
		}
		return Client{}, fmt.Errorf("read client: %w", err)
	}
	client.CreatedAt = time.UnixMilli(createdAt).UTC()
	return client, nil
}

// UpdateProfile 只改当前展示资料，不改变身份、凭据、创建时间或禁用状态。
func (r *Repository) UpdateProfile(ctx context.Context, id int64, displayName string, avatarID int64) error {
	return updateResult(r.writer.ExecContext(ctx,
		"UPDATE clients SET display_name = ?, avatar_id = ? WHERE id = ?", displayName, avatarID, id))
}

// SetDisabled 是内部存储操作，不构成鉴权或管理接口。
func (r *Repository) SetDisabled(ctx context.Context, id int64, disabled bool) error {
	return updateResult(r.writer.ExecContext(ctx, "UPDATE clients SET disabled = ? WHERE id = ?", disabled, id))
}

func updateResult(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("update client: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated client count: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
