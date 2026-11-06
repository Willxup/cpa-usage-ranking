package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistrationHTTPConcurrentSafeRetries(t *testing.T) {
	f := newRegistrationFixture(t, defaultRegistrationSettings())
	const n = 24
	results := make(chan registrationResponse, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- f.send(t, `{"display_name":"Concurrent"}`, testSecret(1), nil) }()
	}
	wg.Wait()
	close(results)
	var clientID int64
	created := 0
	received := 0
	for r := range results {
		received++
		value := identity(t, r)
		if r.status == 201 {
			created++
		}
		if clientID == 0 {
			clientID = value.ID
		}
		if value.ID != clientID {
			t.Fatal("retry created a different identity")
		}
	}
	var stored int
	if err := f.db.Reader.QueryRow("SELECT COUNT(*) FROM clients").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if received != n || created != 1 || stored != 1 {
		t.Fatalf("results=%d created=%d stored=%d", received, created, stored)
	}
}

func TestRegistrationHTTPConcurrentQuotaAndForgedProxies(t *testing.T) {
	settings := defaultRegistrationSettings()
	settings.service.Now = func() time.Time { return time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC) }
	f := newRegistrationFixture(t, settings)
	var successes atomic.Int32
	var wg sync.WaitGroup
	var firstSecret string
	var firstMu sync.Mutex
	for i := byte(1); i <= 16; i++ {
		wg.Add(1)
		go func(i byte) {
			defer wg.Done()
			secret := testSecret(i)
			r := f.send(t, `{"display_name":"Same name"}`, secret, map[string]string{"X-Forwarded-For": fmt.Sprintf("192.0.2.%d", i), "X-Real-IP": fmt.Sprintf("198.51.100.%d", i)})
			if r.status == 201 {
				successes.Add(1)
				firstMu.Lock()
				firstSecret = secret
				firstMu.Unlock()
				return
			}
			requireHTTPError(t, r, 429, "creation_quota_exceeded")
			if r.header.Get("Retry-After") != "3600" {
				t.Errorf("Retry-After: %s", r.header.Get("Retry-After"))
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 3 {
		t.Fatalf("forged headers bypassed source quota: %d", successes.Load())
	}
	if r := f.send(t, `{}`, firstSecret, map[string]string{"X-Forwarded-For": "203.0.113.99"}); r.status != 200 {
		t.Fatalf("full quota blocked retry: %d %s", r.status, r.body)
	}
}

func TestRegistrationHTTPRequestRateAndCapacity(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		global, source, capacity int
		trusted                  bool
		secondCode               string
	}{
		{"source including failed attempts", 100, 1, 10, false, "registration_rate_limited"},
		{"global across sources", 1, 100, 10, true, "registration_rate_limited"},
		{"capacity cannot evict", 100, 1, 1, true, "registration_capacity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := defaultRegistrationSettings()
			settings.requests.SourceRequests = tc.source
			settings.requests.GlobalRequests = tc.global
			settings.requests.MaxSources = tc.capacity
			var now atomic.Int64
			now.Store(time.Date(2026, 11, 9, 23, 59, 59, 0, time.UTC).UnixNano())
			settings.service.Now = func() time.Time { return time.Unix(0, now.Load()) }
			if tc.trusted {
				settings.proxies = []string{"127.0.0.1"}
			}
			f := newRegistrationFixture(t, settings)
			// 无效凭据仍计入请求限流，且伪造转发来源默认不能绕过。
			requireHTTPError(t, f.send(t, `{}`, "", map[string]string{"X-Forwarded-For": "192.0.2.1"}), 401, "invalid_credential")
			r := f.send(t, `{"display_name":"Name"}`, testSecret(1), map[string]string{"X-Forwarded-For": "192.0.2.2"})
			requireHTTPError(t, r, 429, tc.secondCode)
			if r.header.Get("Retry-After") != "60" {
				t.Fatal("wrong rate/capacity Retry-After")
			}
			if tc.secondCode == "registration_capacity" {
				requireHTTPError(t, f.send(t, `{}`, "", map[string]string{"X-Forwarded-For": "192.0.2.1"}), 429, "registration_rate_limited")
			}
			for _, path := range []string{"/healthz", "/avatars/catalog.json", "/avatars/images/001.webp"} {
				response, err := f.server.Client().Get(f.server.URL + path)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != 200 {
					t.Fatalf("registration affected %s", path)
				}
			}
			// 时间注入仅在同步请求之间变化，不依赖 sleep。
			now.Add(int64(time.Minute))
			if r := f.send(t, `{"display_name":"Name"}`, testSecret(1), map[string]string{"X-Forwarded-For": "192.0.2.2"}); r.status != 201 {
				t.Fatalf("expired request state: %d %s", r.status, r.body)
			}
		})
	}
}

func TestRegistrationHTTPCreationCapacityAndCrossSourceRetry(t *testing.T) {
	settings := defaultRegistrationSettings()
	settings.service.CreationLimits.MaxSources = 1
	settings.service.Now = func() time.Time { return time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC) }
	settings.proxies = []string{"127.0.0.1"}
	f := newRegistrationFixture(t, settings)
	first := identity(t, f.send(t, `{"display_name":"Name"}`, testSecret(1), map[string]string{"X-Forwarded-For": "192.0.2.1"}))
	r := f.send(t, `{"display_name":"Name"}`, testSecret(2), map[string]string{"X-Forwarded-For": "192.0.2.2"})
	requireHTTPError(t, r, 429, "registration_capacity")
	if r.header.Get("Retry-After") != "86400" {
		t.Fatal("creation capacity retry time")
	}
	retry := f.send(t, `{"avatar_id":-1}`, testSecret(1), map[string]string{"X-Forwarded-For": "192.0.2.2"})
	if retry.status != 200 || identity(t, retry).ID != first.ID {
		t.Fatal("creation capacity blocked existing retry")
	}
}

func TestRegistrationHTTPClosedDisabledAndStorageFailure(t *testing.T) {
	settings := defaultRegistrationSettings()
	settings.service.NewIdentitiesEnabled = false
	f := newRegistrationFixture(t, settings)
	secret := testSecret(1)
	seeded := f.seed(t, secret)
	r := f.send(t, `{"display_name":"\u0000","avatar_id":-1}`, secret, nil)
	if r.status != 200 || identity(t, r).ID != seeded.ID {
		t.Fatal("closed switch blocked retry")
	}
	requireHTTPError(t, f.send(t, `{"display_name":"New"}`, testSecret(2), nil), 503, "registration_closed")
	if err := f.repo.SetDisabled(context.Background(), seeded.ID, true); err != nil {
		t.Fatal(err)
	}
	requireHTTPError(t, f.send(t, `{}`, secret, nil), 403, "client_disabled")
	stored, err := f.repo.ByID(context.Background(), seeded.ID)
	if err != nil || !stored.Disabled {
		t.Fatal("retry re-enabled identity")
	}
	// 关闭数据库不能伪装为关闭注册或成功重试。
	if err := f.db.Reader.Close(); err != nil {
		t.Fatal(err)
	}
	requireHTTPError(t, f.send(t, `{}`, secret, nil), 500, "internal_error")
}

func TestRegistrationHTTPBodyBoundForKnownAndChunkedLength(t *testing.T) {
	settings := defaultRegistrationSettings()
	settings.bodyBytes = 64
	f := newRegistrationFixture(t, settings)
	large := `{"display_name":"` + strings.Repeat("x", 128) + `"}`
	requireHTTPError(t, f.send(t, large, testSecret(1), nil), 413, "request_too_large")
	// 隐藏 Reader 的已知长度，走真实 HTTP chunked body。
	request, err := http.NewRequest("POST", f.server.URL+"/v1/clients/register", io.NopCloser(strings.NewReader(large)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testSecret(1))
	response, err := f.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	requireHTTPError(t, registrationResponse{status: response.StatusCode, body: body, header: response.Header}, 413, "request_too_large")
	identity(t, f.send(t, `{"display_name":"Short"}`, testSecret(1), nil))
}
