package test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cpa-usage-ranking/internal/httpserver"
)

func frontendFixture(t *testing.T) (string, http.Handler) {
	t.Helper()
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "assets", "brand"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"index.html":                        "<!doctype html><html><body>ranking shell</body></html>",
		"favicon.svg":                       "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>",
		"assets/index-Abcd1234.js":          "console.log('real asset')",
		"assets/plain.css":                  "body { color: white; }",
		"assets/.secret":                    "do not expose",
		"assets/brand/project-icon-512.png": "stable icon export",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	frontend, err := httpserver.OpenFrontend(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := frontend.Close(); err != nil {
			t.Error(err)
		}
	})
	router := httpserver.NewRouter(quietLogger())
	frontend.Mount(router)
	return directory, router
}

func TestFrontendServing(t *testing.T) {
	_, router := frontendFixture(t)
	for _, tc := range []struct{ path, contains, cache string }{
		{"/", "ranking shell", "no-cache"},
		{"/index.html", "ranking shell", "no-cache"},
		{"/favicon.svg", "<svg", "no-cache"},
		{"/assets/index-Abcd1234.js", "real asset", "public, max-age=31536000, immutable"},
		{"/assets/plain.css", "color: white", "no-cache"},
		{"/assets/brand/project-icon-512.png", "stable icon export", "no-cache"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != 200 || !strings.Contains(response.Body.String(), tc.contains) {
				t.Fatalf("response: %d %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != tc.cache {
				t.Fatalf("cache: %s", response.Header().Get("Cache-Control"))
			}
			if response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing nosniff")
			}
		})
	}
}

func TestFrontendMissingAndUnsafePathsDoNotBecomeHTML(t *testing.T) {
	_, router := frontendFixture(t)
	for _, path := range []string{"/api", "/api/missing", "/missing-page", "/missing.js", "/assets/missing.js", "/assets/", "/assets/.secret", "/assets/../index.html", "/assets/%2e%2e/index.html", "/assets/a%5cb", "/assets//plain.css"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Accept", "text/html")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 404 || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("response: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestFrontendCannotFollowSymlinkOutsideRoot(t *testing.T) {
	directory, router := frontendFixture(t)
	outside := filepath.Join(t.TempDir(), "secret.js")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "assets", "escape.js")); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/assets/escape.js", nil))
	if response.Code != 404 || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("response: %d %s", response.Code, response.Body.String())
	}
}

func TestFrontendMethodsAndBackendRoutes(t *testing.T) {
	_, router := frontendFixture(t)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodHead, "/", 200}, {http.MethodHead, "/assets/plain.css", 200},
		{http.MethodPost, "/", 405}, {http.MethodGet, "/healthz", 200}, {http.MethodGet, "/readyz", 503},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
		if response.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, response.Code)
		}
		if tc.method == http.MethodHead && response.Body.Len() != 0 {
			t.Fatal("HEAD included a body")
		}
	}
}

func TestFrontendFailsForIncompleteBuild(t *testing.T) {
	for _, kind := range []string{"missing-root", "missing-index", "empty-index", "directory-index", "external-index"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			switch kind {
			case "missing-root":
				dir = filepath.Join(dir, "missing")
			case "empty-index":
				if err := os.WriteFile(filepath.Join(dir, "index.html"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "directory-index":
				if err := os.Mkdir(filepath.Join(dir, "index.html"), 0700); err != nil {
					t.Fatal(err)
				}
			case "external-index":
				outside := filepath.Join(t.TempDir(), "index.html")
				if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(dir, "index.html")); err != nil {
					t.Fatal(err)
				}
			}
			frontend, err := httpserver.OpenFrontend(dir)
			if err == nil {
				frontend.Close()
				t.Fatal("expected startup error")
			}
		})
	}
}
