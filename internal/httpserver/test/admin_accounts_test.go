package test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"cpa-usage-ranking/internal/admin"
	"cpa-usage-ranking/internal/httpserver"
	"cpa-usage-ranking/internal/storage"
)

func adminDatabaseRouter(t *testing.T) (http.Handler, *admin.Service, *storage.DB) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.MigrateSchema(context.Background(), db.Writer); err != nil {
		t.Fatal(err)
	}
	service, err := admin.New(admin.Owner{Username: "owner", Password: "isolated-test-password"}, db)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := httpserver.NewRouter(logger)
	if err := httpserver.MountAdmin(router, service, nil, logger, httpserver.AdminHTTPOptions{}); err != nil {
		t.Fatal(err)
	}
	return router, service, db
}

func adminLoginCookie(t *testing.T, service *admin.Service, username, password string) (string, admin.Session) {
	t.Helper()
	token, session, err := service.Login(context.Background(), username, password)
	if err != nil {
		t.Fatal(err)
	}
	return "ranking_admin=" + token, session
}

func TestAdminAccountHTTPPermissions(t *testing.T) {
	router, service, _ := adminDatabaseRouter(t)
	ownerCookie, owner := adminLoginCookie(t, service, "owner", "isolated-test-password")
	create := adminCall(router, "POST", "/v1/admin/accounts", `{"username":"staff","password":"ordinary-test-password"}`, ownerCookie, owner.CSRF, "https://ranking.test", true)
	if create.Code != 201 || strings.Contains(create.Body.String(), "password") {
		t.Fatal(create.Code, create.Body.String())
	}
	var account struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	cookie, ordinary := adminLoginCookie(t, service, "staff", "ordinary-test-password")
	for _, input := range []struct{ method, path, body string }{{"GET", "/v1/admin/accounts", ""}, {"POST", "/v1/admin/accounts", `{"username":"other","password":"ordinary-test-password","owner":true}`}, {"DELETE", "/v1/admin/accounts/" + account.ID, "{}"}} {
		if got := adminCall(router, input.method, input.path, input.body, cookie, ordinary.CSRF, "https://ranking.test", true).Code; got != 403 {
			t.Fatalf("ordinary %s: %d", input.method, got)
		}
	}
	if got := adminCall(router, "GET", "/v1/admin/accounts", "", "forged=value", "", "", true).Code; got != 401 {
		t.Fatal(got)
	}
	list := adminCall(router, "GET", "/v1/admin/accounts", "", ownerCookie, "", "", true)
	if list.Code != 200 || strings.Contains(list.Body.String(), "hash") || strings.Contains(list.Body.String(), "password") {
		t.Fatal("unsafe account list")
	}
	for _, body := range []string{`{"username":"owner","password":"ordinary-test-password"}`, `{"username":"staff","password":"ordinary-test-password"}`} {
		if got := adminCall(router, "POST", "/v1/admin/accounts", body, ownerCookie, owner.CSRF, "https://ranking.test", true).Code; got != 409 {
			t.Fatal(got)
		}
	}
	if got := adminCall(router, "DELETE", "/v1/admin/accounts/owner", "{}", ownerCookie, owner.CSRF, "https://ranking.test", true).Code; got != 403 {
		t.Fatal("owner deletion accepted")
	}
	if got := adminCall(router, "DELETE", "/v1/admin/accounts/"+account.ID, "{}", ownerCookie, "", "https://ranking.test", true).Code; got != 403 {
		t.Fatal("missing csrf")
	}
	if got := adminCall(router, "DELETE", "/v1/admin/accounts/"+account.ID, "{}", ownerCookie, owner.CSRF, "https://ranking.test", true).Code; got != 204 {
		t.Fatal(got)
	}
	if got := adminCall(router, "GET", "/v1/admin/accounts", "", cookie, "", "", true).Code; got != 401 {
		t.Fatal("deleted session accepted")
	}
	if got := adminCall(router, "POST", "/v1/admin/accounts", `{"username":"staff","password":"ordinary-test-password"}`, ownerCookie, owner.CSRF, "https://ranking.test", true).Code; got != 201 {
		t.Fatal(got)
	}
	if got := adminCall(router, "GET", "/v1/admin/accounts", "", cookie, "", "", true).Code; got != 401 {
		t.Fatal("recreation reused session")
	}
}
