package test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cpa-usage-ranking/internal/auth"
	"cpa-usage-ranking/internal/avatars"
	"cpa-usage-ranking/internal/clients"
	"cpa-usage-ranking/internal/httpserver"
	"cpa-usage-ranking/internal/registration"
	"cpa-usage-ranking/internal/storage"
	"github.com/gin-gonic/gin"
)

type registrationSettings struct {
	service   registration.Options
	requests  registration.RequestLimits
	proxies   []string
	bodyBytes int64
}

func defaultRegistrationSettings() registrationSettings {
	return registrationSettings{service: registration.DefaultOptions(), requests: registration.RequestLimits{SourceRequests: 1000, GlobalRequests: 10000, Window: registration.DefaultRequestLimits().Window, MaxSources: 100}, bodyBytes: 4096}
}

type registrationFixture struct {
	db     *storage.DB
	repo   *clients.Repository
	router *gin.Engine
	server *httptest.Server
	logs   bytes.Buffer
}

func newRegistrationFixture(t *testing.T, settings registrationSettings) *registrationFixture {
	t.Helper()
	f := &registrationFixture{}
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "http-registration.db"))
	if err != nil {
		t.Fatal(err)
	}
	f.db = db
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := storage.MigrateSchema(context.Background(), db.Writer); err != nil {
		t.Fatal(err)
	}
	f.repo = clients.NewRepository(db)
	files := avatars.BuiltinFS()
	catalog, err := avatars.Load(files)
	if err != nil {
		t.Fatal(err)
	}
	service, err := registration.NewService(f.repo, catalog, settings.service)
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := registration.NewRequestLimiter(settings.requests, settings.service.Now)
	if err != nil {
		t.Fatal(err)
	}
	f.router = httpserver.NewRouter(slog.New(slog.NewJSONHandler(&f.logs, nil)))
	if err := f.router.SetTrustedProxies(settings.proxies); err != nil {
		t.Fatal(err)
	}
	if err := httpserver.MountAvatars(f.router, catalog, files); err != nil {
		t.Fatal(err)
	}
	if err := httpserver.MountRegistration(f.router, service, limiter, httpserver.RegistrationHTTPOptions{GroupIPv6By64: true, MaxBodyBytes: settings.bodyBytes}); err != nil {
		t.Fatal(err)
	}
	// 仅测试异常恢复，生产路由没有该端点。
	f.router.POST("/test/secret-panic", func(c *gin.Context) { panic(c.GetHeader("Authorization")) })
	f.server = httptest.NewServer(f.router)
	t.Cleanup(f.server.Close)
	return f
}

func testSecret(value byte) string {
	raw := make([]byte, 32)
	raw[0] = value
	return base64.RawURLEncoding.EncodeToString(raw)
}

type registrationResponse struct {
	status int
	header http.Header
	body   []byte
}

func (f *registrationFixture) send(t *testing.T, body, secret string, headers map[string]string) registrationResponse {
	t.Helper()
	r, err := http.NewRequest("POST", f.server.URL+"/v1/clients/register", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	if secret != "" {
		r.Header.Set("Authorization", "Bearer "+secret)
	}
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	response, err := f.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("registration must not be cached")
	}
	return registrationResponse{status: response.StatusCode, header: response.Header, body: data}
}

type publicIdentity struct {
	ID       int64  `json:"client_id"`
	Name     string `json:"display_name"`
	AvatarID int64  `json:"avatar_id"`
}

func identity(t *testing.T, r registrationResponse) publicIdentity {
	t.Helper()
	if r.status != 200 && r.status != 201 {
		t.Fatalf("not a success: %d %s", r.status, r.body)
	}
	var result publicIdentity
	if err := json.Unmarshal(r.body, &result); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(r.body, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || result.ID <= 0 {
		t.Fatalf("unsafe public identity response: %s", r.body)
	}
	return result
}

func requireHTTPError(t *testing.T, r registrationResponse, status int, code string) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.body, &body); err != nil || r.status != status || body.Error.Code != code || body.Error.Message == "" {
		t.Fatalf("response: %d %s; want %d %s", r.status, r.body, status, code)
	}
}

func (f *registrationFixture) seed(t *testing.T, secret string) clients.Client {
	t.Helper()
	hash, err := auth.HashCredential(secret)
	if err != nil {
		t.Fatal(err)
	}
	client, err := f.repo.Create(context.Background(), clients.NewClient{CredentialHash: hash, DisplayName: "Seeded", AvatarID: 66})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestRegistrationHTTPCreateAndSafeRetry(t *testing.T) {
	f := newRegistrationFixture(t, defaultRegistrationSettings())
	secret := testSecret(1)
	first := f.send(t, `{"display_name":"  Ａlice 🙂  "}`, secret, nil)
	if first.status != 201 {
		t.Fatalf("new registration: %d %s", first.status, first.body)
	}
	created := identity(t, first)
	if created.Name != "Alice 🙂" || created.AvatarID != 1 {
		t.Fatalf("profile not normalized: %+v", created)
	}
	if err := f.repo.UpdateProfile(context.Background(), created.ID, "Current", 66); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{}`, `{"display_name":"replacement","avatar_id":2}`, `{"display_name":"\u0000","avatar_id":-1}`, `{"avatar_id":null}`} {
		r := f.send(t, body, secret, nil)
		current := identity(t, r)
		if r.status != 200 || current.ID != created.ID || current.Name != "Current" || current.AvatarID != 66 {
			t.Fatalf("retry changed identity/profile: %+v", current)
		}
	}
	other := identity(t, f.send(t, `{"display_name":"Current","avatar_id":66}`, testSecret(2), nil))
	if other.ID == created.ID {
		t.Fatal("public profile claimed another identity")
	}
	for _, path := range []string{"/me", "/v1/clients", "/v1/reports/2026-11-09"} {
		r, err := f.server.Client().Get(f.server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Fatalf("unauthorized extra API: %s %d", path, r.StatusCode)
		}
	}
	r, err := f.server.Client().Get(f.server.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 503 {
		t.Fatal("registration must not imply business readiness")
	}
}

func TestRegistrationHTTPInvalidInput(t *testing.T) {
	f := newRegistrationFixture(t, defaultRegistrationSettings())
	secret := testSecret(1)
	for _, tc := range []struct {
		body, token string
		headers     map[string]string
		status      int
		code        string
	}{
		{`{"display_name":"Valid"}`, "", nil, 401, "invalid_credential"},
		{`{"display_name":"Valid"}`, "invalid", nil, 401, "invalid_credential"},
		{`{`, secret, nil, 400, "invalid_request"},
		{`null`, secret, nil, 400, "invalid_request"},
		{`[]`, secret, nil, 400, "invalid_request"},
		{`{} {}`, secret, nil, 400, "invalid_request"},
		{`{"client_id":1,"display_name":"Valid"}`, secret, nil, 400, "invalid_request"},
		{`{"source":"192.0.2.1","display_name":"Valid"}`, secret, nil, 400, "invalid_request"},
		{`{"display_name":1}`, secret, nil, 400, "invalid_request"},
		{`{"display_name":"Valid","avatar_id":"1"}`, secret, nil, 400, "invalid_request"},
		{`{"display_name":"Valid","avatar_id":1.5}`, secret, nil, 400, "invalid_request"},
		{"{\"display_name\":\"\xff\"}", secret, nil, 400, "invalid_request"},
		{`{}`, secret, nil, 422, "invalid_display_name"},
		{`{"display_name":"\u0000"}`, secret, nil, 422, "invalid_display_name"},
		{`{"display_name":"Valid","avatar_id":0}`, secret, nil, 422, "invalid_avatar"},
		{`{"display_name":"Valid","avatar_id":null}`, secret, nil, 422, "invalid_avatar"},
		{`{"display_name":"Valid","avatar_id":-1}`, secret, nil, 422, "invalid_avatar"},
		{`{"display_name":"Valid","avatar_id":67}`, secret, nil, 422, "invalid_avatar"},
		{`{"display_name":"Valid"}`, secret, map[string]string{"Content-Type": "text/plain"}, 415, "unsupported_media_type"},
		{`{"display_name":"Valid"}`, secret, map[string]string{"Content-Encoding": "gzip"}, 415, "unsupported_media_type"},
	} {
		requireHTTPError(t, f.send(t, tc.body, tc.token, tc.headers), tc.status, tc.code)
	}
	var n int
	if err := f.db.Reader.QueryRow("SELECT COUNT(*) FROM clients").Scan(&n); err != nil || n != 0 {
		t.Fatalf("invalid request created identity: %d %v", n, err)
	}
	identity(t, f.send(t, `{"display_name":"Valid","avatar_id":66}`, secret, nil))
}
