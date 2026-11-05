package test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-ranking/internal/httpserver"
	"cpa-usage-ranking/internal/registration"
	"github.com/gin-gonic/gin"
)

func TestRegistrationAdmissionCountsFailuresAndLeavesHealthStaticAlone(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	limiter, err := registration.NewRequestLimiter(registration.RequestLimits{SourceRequests: 1, GlobalRequests: 100, Window: 1500 * time.Millisecond, MaxSources: 1}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	router := httpserver.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)))
	router.POST("/test/register", httpserver.RegistrationAdmission(limiter, true), func(c *gin.Context) { c.JSON(400, gin.H{"error": "test-invalid-input"}) })
	send := func(method, path, source string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = source + ":1000"
		r.Header.Set("X-Forwarded-For", "203.0.113.1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := send("POST", "/test/register", "192.0.2.1"); w.Code != 400 {
		t.Fatal(w.Code)
	}
	now = now.Add(100 * time.Millisecond)
	if w := send("POST", "/test/register", "192.0.2.1"); w.Code != 429 || w.Header().Get("Retry-After") != "2" {
		t.Fatalf("failed request not counted: %d %v", w.Code, w.Header())
	}
	if w := send("POST", "/test/register", "192.0.2.2"); w.Code != 429 || w.Header().Get("Retry-After") != "2" {
		t.Fatalf("capacity: %d %v", w.Code, w.Header())
	}
	for _, path := range []string{"/healthz", "/readyz", "/assets/missing.js"} {
		if w := send("GET", path, "192.0.2.1"); w.Code == 429 || w.Header().Get("Retry-After") != "" {
			t.Fatalf("non-registration limited: %s", path)
		}
	}
	now = now.Add(1400 * time.Millisecond)
	if w := send("POST", "/test/register", "192.0.2.2"); w.Code != 400 {
		t.Fatalf("expired source not reclaimed: %d", w.Code)
	}
}
