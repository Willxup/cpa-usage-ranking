package test

import (
	"os"
	"strings"
	"testing"
	"time"

	"cpa-usage-ranking/internal/config"
)

func clearEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{"RANKING_HTTP_ADDR", "RANKING_SHUTDOWN_TIMEOUT", "RANKING_WEB_DIR", "RANKING_SQLITE_PATH"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDefaults(t *testing.T) {
	clearEnvironment(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" || cfg.ShutdownTimeout != 10*time.Second || cfg.WebDir != "" || cfg.SQLitePath != "data/ranking.db" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestOverrides(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", ":8080", "[::1]:65535", "localhost:9090"} {
		t.Run(addr, func(t *testing.T) {
			clearEnvironment(t)
			t.Setenv("RANKING_HTTP_ADDR", " "+addr+" ")
			t.Setenv("RANKING_SHUTDOWN_TIMEOUT", " 250ms ")
			t.Setenv("RANKING_WEB_DIR", " web/dist ")
			t.Setenv("RANKING_SQLITE_PATH", " custom/ranking.db ")
			cfg, err := config.Load()
			if err != nil || cfg.HTTPAddr != addr || cfg.ShutdownTimeout != 250*time.Millisecond || cfg.WebDir != "web/dist" || cfg.SQLitePath != "custom/ranking.db" {
				t.Fatalf("configuration = %+v, error = %v", cfg, err)
			}
		})
	}
}

func TestInvalidValues(t *testing.T) {
	for key, values := range map[string][]string{
		"RANKING_HTTP_ADDR":        {"", "localhost", "http://localhost:8080", "a/b:8080", "a b:8080", "::1:8080", ":", ":http", ":-1", ":+80", ":65536"},
		"RANKING_SHUTDOWN_TIMEOUT": {"", "0s", "-1s", "10", "forever", "999999999999999999999h"},
		"RANKING_WEB_DIR":          {"", "   "},
		"RANKING_SQLITE_PATH":      {"", "   "},
	} {
		for _, value := range values {
			t.Run(key+"/"+value, func(t *testing.T) {
				clearEnvironment(t)
				t.Setenv(key, value)
				_, err := config.Load()
				if err == nil || !strings.Contains(err.Error(), key) {
					t.Fatalf("expected an error naming %s, got %v", key, err)
				}
			})
		}
	}
}
