// Package registration combines identity persistence and validated public profiles.
package registration

import (
	"context"
	"errors"
	"sync"
	"time"

	"cpa-usage-ranking/internal/avatars"
	"cpa-usage-ranking/internal/clients"
	"cpa-usage-ranking/internal/profiles"
)

var (
	ErrDisabled           = errors.New("client is disabled")
	ErrInvalidAvatar      = errors.New("invalid avatar selection")
	ErrRegistrationClosed = errors.New("new identity registration is closed")
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

type Options struct {
	CreationLimits       CreationLimits
	NewIdentitiesEnabled bool
	Now                  func() time.Time
}

func DefaultOptions() Options {
	return Options{CreationLimits: DefaultCreationLimits(), NewIdentitiesEnabled: true}
}

type Service struct {
	mu                   sync.Mutex
	repo                 *clients.Repository
	catalog              *avatars.Catalog
	limits               CreationLimits
	now                  func() time.Time
	creations            map[string][]time.Time
	newIdentitiesEnabled bool
}

// Use one Service per process so admission and successful-create accounting share a lock.
func NewService(repo *clients.Repository, catalog *avatars.Catalog, options Options) (*Service, error) {
	if err := options.CreationLimits.Validate(); err != nil {
		return nil, err
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{repo: repo, catalog: catalog, limits: options.CreationLimits,
		now: options.Now, creations: make(map[string][]time.Time), newIdentitiesEnabled: options.NewIdentitiesEnabled}, nil
}

// Register accepts a validated credential digest, never the credential text.
// An existing digest wins before profile validation, including disabled status.
func (s *Service) Register(ctx context.Context, source string, hash clients.CredentialHash, profile Profile) (Result, error) {
	// 进程内串行准入直到 INSERT 完成，无预扣名额，也不在持有写事务时再借写池。
	s.mu.Lock()
	defer s.mu.Unlock()
	client, err := s.repo.ByCredentialHash(ctx, hash)
	if err == nil {
		return existing(client)
	}
	if !errors.Is(err, clients.ErrNotFound) {
		return Result{}, err
	}
	// 关闭只影响尚不存在的身份；安全重试与禁用判断优先，鉴权使用独立路径。
	if !s.newIdentitiesEnabled {
		return Result{}, ErrRegistrationClosed
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
	if err := s.checkCreationQuota(source, s.now()); err != nil {
		return Result{}, err
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
	// 只对真正成功的新建记账；失败和摘要冲突不消耗创建额度。
	s.creations[source] = append(s.creations[source], s.now())
	return Result{Client: client, Created: true}, nil
}

func existing(client clients.Client) (Result, error) {
	if client.Disabled {
		return Result{}, ErrDisabled
	}
	return Result{Client: client}, nil
}
