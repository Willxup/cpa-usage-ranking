// Package registration combines identity persistence and validated public profiles.
package registration

import (
	"context"
	"errors"

	"cpa-usage-ranking/internal/avatars"
	"cpa-usage-ranking/internal/clients"
	"cpa-usage-ranking/internal/profiles"
)

var (
	ErrDisabled      = errors.New("client is disabled")
	ErrInvalidAvatar = errors.New("invalid avatar selection")
)

// Profile only describes a new identity; it cannot claim an existing client ID.
type Profile struct {
	DisplayName string
	AvatarID    *int64
}

type Result struct {
	Client  clients.Client
	Created bool
}

type Service struct {
	repo    *clients.Repository
	catalog *avatars.Catalog
}

func NewService(repo *clients.Repository, catalog *avatars.Catalog) *Service {
	return &Service{repo: repo, catalog: catalog}
}

// Register accepts a validated credential digest, never the credential text.
// An existing digest wins before profile validation, including disabled status.
func (s *Service) Register(ctx context.Context, hash clients.CredentialHash, profile Profile) (Result, error) {
	client, err := s.repo.ByCredentialHash(ctx, hash)
	if err == nil {
		return existing(client)
	}
	if !errors.Is(err, clients.ErrNotFound) {
		return Result{}, err
	}
	name, err := profiles.NormalizeDisplayName(profile.DisplayName)
	if err != nil {
		return Result{}, err
	}
	avatarID := s.catalog.DefaultID()
	if profile.AvatarID != nil {
		avatarID = *profile.AvatarID
	}
	if _, ok := s.catalog.Lookup(avatarID); !ok {
		return Result{}, ErrInvalidAvatar
	}
	// 单条 INSERT 原子保存身份与完整资料；摘要唯一约束处理跨请求并发。
	client, err = s.repo.Create(ctx, clients.NewClient{CredentialHash: hash, DisplayName: name, AvatarID: avatarID})
	if errors.Is(err, clients.ErrCredentialExists) {
		client, err = s.repo.ByCredentialHash(ctx, hash)
		if err != nil {
			return Result{}, err
		}
		return existing(client)
	}
	if err != nil {
		return Result{}, err
	}
	return Result{Client: client, Created: true}, nil
}

func existing(client clients.Client) (Result, error) {
	if client.Disabled {
		return Result{}, ErrDisabled
	}
	return Result{Client: client}, nil
}
