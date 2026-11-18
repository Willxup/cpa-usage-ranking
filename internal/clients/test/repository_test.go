package test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-ranking/internal/clients"
	"cpa-usage-ranking/internal/storage"
)

func openRepository(t *testing.T, path string) (*storage.DB, *clients.Repository) {
	t.Helper()
	db, err := storage.Open(context.Background(), path)
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
	return db, clients.NewRepository(db)
}

// 固定字符串仅用于测试摘要；实际凭据的生成和校验不属于存储层。
func credentialHash(label string) clients.CredentialHash { return sha256.Sum256([]byte(label)) }

func create(t *testing.T, repository *clients.Repository, label string) clients.Client {
	t.Helper()
	client, err := repository.Create(context.Background(), clients.NewClient{
		CredentialHash: credentialHash(label), DisplayName: "同名 🚀", AvatarID: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func assertClient(t *testing.T, got, want clients.Client) {
	t.Helper()
	if got.ID != want.ID || got.DisplayName != want.DisplayName || got.AvatarID != want.AvatarID || got.Disabled != want.Disabled || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("client = %+v, want %+v", got, want)
	}
	if got.CreatedAt.Location() != time.UTC {
		t.Fatalf("creation timestamp is not UTC: %s", got.CreatedAt)
	}
}

func TestCreateAndLookupKeepSameNameClientsDistinct(t *testing.T) {
	_, repository := openRepository(t, filepath.Join(t.TempDir(), "ranking.db"))
	before := time.Now().UTC().Truncate(time.Millisecond)
	one, two := create(t, repository, "one"), create(t, repository, "two")
	if one.ID <= 0 || two.ID <= 0 || one.ID == two.ID {
		t.Fatal("client identities were not independently allocated")
	}
	if one.Disabled || one.CreatedAt.Before(before) || one.CreatedAt.After(time.Now()) {
		t.Fatalf("invalid initial metadata: %+v", one)
	}
	for _, entry := range []struct {
		label  string
		client clients.Client
	}{{"one", one}, {"two", two}} {
		byID, err := repository.ByID(context.Background(), entry.client.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertClient(t, byID, entry.client)
		byHash, err := repository.ByCredentialHash(context.Background(), credentialHash(entry.label))
		if err != nil {
			t.Fatal(err)
		}
		assertClient(t, byHash, entry.client)
	}
}

func TestDuplicateCredentialDoesNotOverwriteExistingClient(t *testing.T) {
	_, repository := openRepository(t, filepath.Join(t.TempDir(), "ranking.db"))
	original := create(t, repository, "same credential")
	if err := repository.SetDisabled(context.Background(), original.ID, true); err != nil {
		t.Fatal(err)
	}
	original.Disabled = true
	_, err := repository.Create(context.Background(), clients.NewClient{
		CredentialHash: credentialHash("same credential"), DisplayName: "不得覆盖", AvatarID: 1,
	})
	if !errors.Is(err, clients.ErrCredentialExists) {
		t.Fatalf("duplicate error = %v", err)
	}
	got, err := repository.ByCredentialHash(context.Background(), credentialHash("same credential"))
	if err != nil {
		t.Fatal(err)
	}
	assertClient(t, got, original)
}

func TestProfileUpdatePreservesIdentityCredentialTimestampAndStatus(t *testing.T) {
	_, repository := openRepository(t, filepath.Join(t.TempDir(), "ranking.db"))
	original := create(t, repository, "profile")
	if err := repository.SetDisabled(context.Background(), original.ID, true); err != nil {
		t.Fatal(err)
	}
	name := "新昵称 ' ; -- 🚀"
	for range 2 {
		if err := repository.UpdateProfile(context.Background(), original.ID, name, 1<<40); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repository.ByCredentialHash(context.Background(), credentialHash("profile"))
	if err != nil {
		t.Fatal(err)
	}
	want := original
	want.DisplayName, want.AvatarID, want.Disabled = name, 1<<40, true
	assertClient(t, got, want)
	// 只有显式状态操作可以恢复启用，修改资料不会顺带重置状态。
	for range 2 {
		if err := repository.SetDisabled(context.Background(), original.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	got, err = repository.ByID(context.Background(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	want.Disabled = false
	assertClient(t, got, want)
}

func TestClientSurvivesReopenAndMigrationReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ranking.db")
	db, repository := openRepository(t, path)
	original := create(t, repository, "persisted")
	if err := repository.UpdateProfile(context.Background(), original.ID, "重开后仍保留", 512); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetDisabled(context.Background(), original.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, reopened := openRepository(t, path)
	got, err := reopened.ByCredentialHash(context.Background(), credentialHash("persisted"))
	if err != nil {
		t.Fatal(err)
	}
	original.DisplayName, original.AvatarID, original.Disabled = "重开后仍保留", 512, true
	assertClient(t, got, original)
	if create(t, reopened, "new").ID <= original.ID {
		t.Fatal("reopened database reused an identity")
	}
}

func TestMissingClientAndCancellationErrors(t *testing.T) {
	db, repository := openRepository(t, filepath.Join(t.TempDir(), "ranking.db"))
	ctx := context.Background()
	_, byID := repository.ByID(ctx, 999)
	_, byHash := repository.ByCredentialHash(ctx, credentialHash("missing"))
	for _, err := range []error{byID, byHash, repository.UpdateProfile(ctx, 999, "missing", 1), repository.SetDisabled(ctx, 999, true)} {
		if !errors.Is(err, clients.ErrNotFound) {
			t.Fatalf("missing client returned %v", err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err := repository.Create(cancelled, clients.NewClient{CredentialHash: credentialHash("cancelled"), DisplayName: "cancelled", AvatarID: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled creation returned %v", err)
	}
	_, err = repository.ByID(cancelled, 999)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup returned %v", err)
	}
	var count int
	if err := db.Reader.QueryRow("SELECT COUNT(*) FROM clients").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("cancelled creation left a row")
	}
}

func TestConcurrentDuplicateCredentialCreatesOneClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ranking.db")
	db, repository := openRepository(t, path)
	_, other := openRepository(t, path)
	repositories := []*clients.Repository{repository, other}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type result struct {
		client clients.Client
		err    error
	}
	results := make(chan result, 8)
	start := make(chan struct{})
	for i := range 8 {
		go func() {
			<-start
			client, err := repositories[i%2].Create(ctx, clients.NewClient{
				CredentialHash: credentialHash("concurrent"), DisplayName: fmt.Sprintf("client-%d", i), AvatarID: int64(i + 1),
			})
			results <- result{client, err}
		}()
	}
	close(start)
	var winner clients.Client
	created := 0
	for range 8 {
		result := <-results
		if result.err == nil {
			created++
			winner = result.client
		} else if !errors.Is(result.err, clients.ErrCredentialExists) {
			t.Errorf("unexpected creation error: %v", result.err)
		}
	}
	if created != 1 {
		t.Fatalf("created %d clients", created)
	}
	got, err := repository.ByCredentialHash(ctx, credentialHash("concurrent"))
	if err != nil {
		t.Fatal(err)
	}
	assertClient(t, got, winner)
	var count int
	if err := db.Reader.QueryRow("SELECT COUNT(*) FROM clients").Scan(&count); err != nil || count != 1 {
		t.Fatalf("row count=%d, error=%v", count, err)
	}
}

func TestInvalidAvatarDoesNotCreateOrPartiallyUpdate(t *testing.T) {
	_, repository := openRepository(t, filepath.Join(t.TempDir(), "ranking.db"))
	original := create(t, repository, "valid")
	for _, avatar := range []int64{0, -1} {
		_, err := repository.Create(context.Background(), clients.NewClient{CredentialHash: credentialHash("invalid"), DisplayName: "invalid", AvatarID: avatar})
		if err == nil || errors.Is(err, clients.ErrCredentialExists) {
			t.Fatalf("invalid avatar creation: %v", err)
		}
		if err := repository.UpdateProfile(context.Background(), original.ID, "must not replace", avatar); err == nil {
			t.Fatal("invalid avatar update succeeded")
		}
	}
	got, err := repository.ByID(context.Background(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertClient(t, got, original)
	if _, err := repository.ByCredentialHash(context.Background(), credentialHash("invalid")); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("invalid creation left a client: %v", err)
	}
}

func TestDeletedIdentityIsNotReallocated(t *testing.T) {
	db, repository := openRepository(t, filepath.Join(t.TempDir(), "ranking.db"))
	original := create(t, repository, "old")
	// 不提供删除 API；这里直接验证数据库不会将已用过的编号分配给新身份。
	if _, err := db.Writer.Exec("DELETE FROM clients WHERE id=?", original.ID); err != nil {
		t.Fatal(err)
	}
	if create(t, repository, "replacement").ID <= original.ID {
		t.Fatal("database reused a prior identity")
	}
}
