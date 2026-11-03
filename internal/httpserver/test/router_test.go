package test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cpa-usage-ranking/internal/httpserver"
	"github.com/gin-gonic/gin"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRouteBoundaries(t *testing.T) {
	router := httpserver.NewRouter(quietLogger())
	for _, tc := range []struct {
		method, path string
		status       int
		field, value string
	}{
		{http.MethodGet, "/healthz", 200, "status", "ok"},
		{http.MethodGet, "/readyz", 503, "status", "not_ready"},
		{http.MethodGet, "/v1/reports", 404, "code", "not_found"},
		{http.MethodGet, "/missing", 404, "code", "not_found"},
		{http.MethodPost, "/healthz", 405, "code", "method_not_allowed"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.status || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("unexpected response: %d %s", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing response protections")
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.field == "code" {
				body = body["error"].(map[string]any)
			}
			if body[tc.field] != tc.value {
				t.Fatalf("body = %v", body)
			}
		})
	}
}

func TestPanicDoesNotLeakRequestOrPanicDetails(t *testing.T) {
	var logs bytes.Buffer
	router := httpserver.NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)))
	router.POST("/panic", func(c *gin.Context) { panic("secret-in-panic") })
	req := httptest.NewRequest(http.MethodPost, "/panic?token=secret-in-query", strings.NewReader("secret-in-body"))
	req.Header.Set("Authorization", "Bearer secret-in-header")
	req.Header.Set("Cookie", "session=secret-in-cookie")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "internal_error") {
		t.Fatalf("unexpected panic response: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(logs.String()+rec.Body.String(), "secret-in-") {
		t.Fatal("sensitive details leaked")
	}
	if !strings.Contains(logs.String(), "http handler panicked") {
		t.Fatal("panic was not logged")
	}
}

func TestPanicAfterResponseStartedAbortsConnection(t *testing.T) {
	router := httpserver.NewRouter(quietLogger())
	router.GET("/partial", func(c *gin.Context) {
		c.String(http.StatusOK, "partial")
		panic("failure")
	})
	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler {
			t.Fatalf("expected net/http connection abort, got %v", recovered)
		}
	}()
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/partial", nil))
}

func TestForwardedHeadersAreNotTrustedByDefault(t *testing.T) {
	router := httpserver.NewRouter(quietLogger())
	router.GET("/source", func(c *gin.Context) { c.String(200, c.ClientIP()) })
	req := httptest.NewRequest(http.MethodGet, "/source", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Body.String() != "192.0.2.10" {
		t.Fatalf("untrusted proxy header changed client IP: %s", rec.Body.String())
	}
}
