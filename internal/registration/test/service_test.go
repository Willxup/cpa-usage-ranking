package test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"cpa-usage-ranking/internal/avatars"
	"cpa-usage-ranking/internal/clients"
	"cpa-usage-ranking/internal/profiles"
	"cpa-usage-ranking/internal/registration"
	"cpa-usage-ranking/internal/storage"
)

func setup(t *testing.T) (*storage.DB, *clients.Repository, *registration.Service) {
	t.Helper()
	return setupOptions(t, registration.DefaultOptions())
}

func setupOptions(t *testing.T, options registration.Options) (*storage.DB, *clients.Repository, *registration.Service) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "registration.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := storage.InitSchema(context.Background(), db.Writer); err != nil {
		t.Fatal(err)
	}
	repo := clients.NewRepository(db)
	catalog, err := avatars.Load(avatars.BuiltinFS())
	if err != nil {
		t.Fatal(err)
	}
	service, err := registration.NewService(repo, catalog, options)
	if err != nil {
		t.Fatal(err)
	}
	return db, repo, service
}

func digest(i byte) clients.CredentialHash { return clients.CredentialHash{i} }
func id(i int64) *int64                    { return &i }

func count(t *testing.T, db *storage.DB) int {
	t.Helper()
	var n int
	if err := db.Reader.QueryRow("SELECT COUNT(*) FROM clients").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRegistrationAndCurrentProfileRetry(t *testing.T) {
	db, repo, service := setup(t)
	ctx := context.Background()
	first, err := service.Register(ctx, "source", digest(1), registration.Profile{DisplayName: "  Ａlice 🙂  "})
	if err != nil || !first.Created || first.Client.ID <= 0 || first.Client.DisplayName != "Alice 🙂" || first.Client.AvatarID != 1 {
		t.Fatalf("registration: %+v %v", first, err)
	}
	if err := repo.UpdateProfile(ctx, first.Client.ID, "Current profile", 66); err != nil {
		t.Fatal(err)
	}
	// 已有身份忽略新建资料，不以参数未变为重试前提。
	for _, profile := range []registration.Profile{{}, {DisplayName: "replacement", AvatarID: id(2)}, {DisplayName: "\x00", AvatarID: id(-1)}} {
		retry, err := service.Register(ctx, "source", digest(1), profile)
		if err != nil || retry.Created || retry.Client.ID != first.Client.ID || retry.Client.DisplayName != "Current profile" || retry.Client.AvatarID != 66 || !retry.Client.CreatedAt.Equal(first.Client.CreatedAt) {
			t.Fatalf("retry changed identity/profile: %+v %v", retry, err)
		}
	}
	other, err := service.Register(ctx, "source", digest(2), registration.Profile{DisplayName: "Current profile", AvatarID: id(66)})
	if err != nil || other.Client.ID == first.Client.ID || !other.Created || count(t, db) != 2 {
		t.Fatalf("same profile claimed identity: %+v %v", other, err)
	}
}

func TestNewIdentityValidationAndDisabledRetry(t *testing.T) {
	db, repo, service := setup(t)
	ctx := context.Background()
	for _, c := range []struct {
		profile registration.Profile
		want    error
	}{
		{registration.Profile{}, profiles.ErrInvalidDisplayName},
		{registration.Profile{DisplayName: "\x00"}, profiles.ErrInvalidDisplayName},
		{registration.Profile{DisplayName: "name", AvatarID: id(0)}, registration.ErrInvalidAvatar},
		{registration.Profile{DisplayName: "name", AvatarID: id(-1)}, registration.ErrInvalidAvatar},
		{registration.Profile{DisplayName: "name", AvatarID: id(67)}, registration.ErrInvalidAvatar},
	} {
		_, err := service.Register(ctx, "source", digest(1), c.profile)
		if !errors.Is(err, c.want) || count(t, db) != 0 {
			t.Fatalf("validation: %v", err)
		}
	}
	first, err := service.Register(ctx, "source", digest(1), registration.Profile{DisplayName: "name", AvatarID: id(66)})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetDisabled(ctx, first.Client.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Register(ctx, "source", digest(1), registration.Profile{DisplayName: "changed"}); !errors.Is(err, registration.ErrDisabled) {
		t.Fatalf("disabled retry: %v", err)
	}
	stored, err := repo.ByID(ctx, first.Client.ID)
	if err != nil || !stored.Disabled || stored.DisplayName != "name" || stored.AvatarID != 66 || count(t, db) != 1 {
		t.Fatalf("disabled identity mutated: %+v %v", stored, err)
	}
}

func TestConcurrentSafeRetriesAcrossServices(t *testing.T) {
	db, repo, service := setup(t)
	catalog, err := avatars.Load(avatars.BuiltinFS())
	if err != nil {
		t.Fatal(err)
	}
	other, err := registration.NewService(repo, catalog, registration.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	const n = 32
	results := make(chan registration.Result, n)
	failures := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := service
			if i%2 != 0 {
				s = other
			}
			result, err := s.Register(context.Background(), "source", digest(1), registration.Profile{DisplayName: "same name"})
			if err != nil {
				failures <- err
				return
			}
			results <- result
		}(i)
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var clientID int64
	created := 0
	received := 0
	for r := range results {
		received++
		if r.Created {
			created++
		}
		if clientID == 0 {
			clientID = r.Client.ID
		}
		if r.Client.ID != clientID {
			t.Fatal("retry created another identity")
		}
	}
	if created != 1 || received != n || count(t, db) != 1 {
		t.Fatalf("created=%d results=%d", created, received)
	}
}

func TestDatabaseFailureLeavesNoPartialIdentity(t *testing.T) {
	db, _, service := setup(t)
	ctx := context.Background()
	if _, err := db.Writer.Exec(`CREATE TRIGGER reject_registration BEFORE INSERT ON clients BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Register(ctx, "source", digest(1), registration.Profile{DisplayName: "name"}); err == nil || count(t, db) != 0 {
		t.Fatal("failed insert left identity")
	}
	if _, err := db.Writer.Exec("DROP TRIGGER reject_registration"); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.Register(cancelled, "source", digest(1), registration.Profile{DisplayName: "name"}); !errors.Is(err, context.Canceled) || count(t, db) != 0 {
		t.Fatalf("cancelled registration: %v", err)
	}
	r, err := service.Register(ctx, "source", digest(1), registration.Profile{DisplayName: "name"})
	if err != nil || !r.Created || count(t, db) != 1 {
		t.Fatalf("retry after failure: %+v %v", r, err)
	}
}
