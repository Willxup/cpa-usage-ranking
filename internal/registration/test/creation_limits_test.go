package test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-ranking/internal/registration"
)

func TestRollingCreationWindowsAndUnrestrictedSafeRetries(t *testing.T) {
	now := time.Date(2026, 11, 9, 23, 50, 0, 0, time.UTC)
	opts := registration.DefaultOptions()
	opts.Now = func() time.Time { return now }
	db, _, service := setupOptions(t, opts)
	ctx := context.Background()
	next := byte(1)
	create := func() {
		t.Helper()
		r, err := service.Register(ctx, "source", digest(next), registration.Profile{DisplayName: "same name"})
		if err != nil || !r.Created {
			t.Fatalf("create %d: %+v %v", next, r, err)
		}
		next++
	}
	create()
	create()
	create()
	_, err := service.Register(ctx, "source", digest(next), registration.Profile{DisplayName: "same name"})
	requireLimit(t, err, "creation_quota_exceeded", time.Hour)
	// 额度满时，已有身份重试仍成功，且忽略新建资料和更换的来源。
	for _, source := range []string{"source", "different source"} {
		r, err := service.Register(ctx, source, digest(1), registration.Profile{AvatarID: id(-1)})
		if err != nil || r.Created {
			t.Fatalf("quota blocked retry: %+v %v", r, err)
		}
	}
	// 恰好一小时过期，跨 UTC 午夜不重置滚动日窗口。
	now = now.Add(time.Hour)
	create()
	create()
	create()
	now = now.Add(time.Hour)
	create()
	create()
	create()
	now = now.Add(time.Hour)
	create()
	_, err = service.Register(ctx, "source", digest(next), registration.Profile{DisplayName: "same name"})
	requireLimit(t, err, "creation_quota_exceeded", 21*time.Hour)
	now = now.Add(21*time.Hour - time.Nanosecond)
	_, err = service.Register(ctx, "source", digest(next), registration.Profile{DisplayName: "same name"})
	requireLimit(t, err, "creation_quota_exceeded", time.Nanosecond)
	now = now.Add(time.Nanosecond)
	create()
	if count(t, db) != 11 {
		t.Fatal("incorrect successful creation count")
	}
}

func TestFailedWritesAndInvalidProfilesDoNotSpendCreationQuota(t *testing.T) {
	opts := registration.DefaultOptions()
	opts.CreationLimits.Hourly = 1
	opts.CreationLimits.Daily = 1
	db, _, service := setupOptions(t, opts)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := service.Register(ctx, "source", digest(1), registration.Profile{}); err == nil {
			t.Fatal("invalid profile accepted")
		}
	}
	if _, err := db.Writer.Exec(`CREATE TRIGGER reject_quota BEFORE INSERT ON clients BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := service.Register(ctx, "source", digest(1), registration.Profile{DisplayName: "valid"}); err == nil {
			t.Fatal("failed write accepted")
		}
	}
	if _, err := db.Writer.Exec("DROP TRIGGER reject_quota"); err != nil {
		t.Fatal(err)
	}
	r, err := service.Register(ctx, "source", digest(1), registration.Profile{DisplayName: "valid"})
	if err != nil || !r.Created || count(t, db) != 1 {
		t.Fatalf("failed attempts spent creation quota: %+v %v", r, err)
	}
}

func TestConcurrentDifferentCredentialsCannotExceedQuota(t *testing.T) {
	db, _, service := setup(t)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := byte(1); i <= 32; i++ {
		wg.Add(1)
		go func(i byte) {
			defer wg.Done()
			r, err := service.Register(context.Background(), "same source", digest(i), registration.Profile{DisplayName: "same name"})
			if err == nil && r.Created {
				successes.Add(1)
				return
			}
			var limited *registration.LimitError
			if !errors.As(err, &limited) || limited.Code != "creation_quota_exceeded" {
				t.Errorf("unexpected admission: %+v %v", r, err)
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 3 || count(t, db) != 3 {
		t.Fatalf("created %d", successes.Load())
	}
}

func TestCreationCapacityRetainsActiveQuotaAndReclaimsExpired(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	opts := registration.DefaultOptions()
	opts.Now = func() time.Time { return now }
	opts.CreationLimits = registration.CreationLimits{Hourly: 1, Daily: 1, MaxSources: 1}
	_, _, service := setupOptions(t, opts)
	ctx := context.Background()
	profile := registration.Profile{DisplayName: "name"}
	if _, err := service.Register(ctx, "a", digest(1), profile); err != nil {
		t.Fatal(err)
	}
	_, err := service.Register(ctx, "b", digest(2), profile)
	requireLimit(t, err, "registration_capacity", 24*time.Hour)
	if r, err := service.Register(ctx, "b", digest(1), registration.Profile{}); err != nil || r.Created {
		t.Fatalf("capacity blocked retry: %+v %v", r, err)
	}
	_, err = service.Register(ctx, "a", digest(2), profile)
	requireLimit(t, err, "creation_quota_exceeded", 24*time.Hour)
	now = now.Add(24 * time.Hour)
	if _, err := service.Register(ctx, "b", digest(2), profile); err != nil {
		t.Fatalf("expired source not reclaimed: %v", err)
	}
	_, err = service.Register(ctx, "a", digest(3), profile)
	requireLimit(t, err, "registration_capacity", 24*time.Hour)
}

func TestCreationLimitsValidation(t *testing.T) {
	for _, limits := range []registration.CreationLimits{{}, {Hourly: 1, Daily: 1}, {Hourly: -1, Daily: 1, MaxSources: 1}} {
		if err := limits.Validate(); err == nil {
			t.Fatal("unbounded creation limits accepted")
		}
	}
}
