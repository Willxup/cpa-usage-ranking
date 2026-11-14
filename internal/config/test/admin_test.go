package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cpa-usage-ranking/internal/config"
)

func TestAdminConfigurationCannotBeServedAsStaticAsset(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.Mkdir(assets, 0700); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(assets, "admin.json")
	if err := os.WriteFile(private, []byte(`{"username":"owner","password":"isolated-test-password"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RANKING_WEB_DIR", root)
	t.Setenv("RANKING_ADMIN_CONFIG_FILE", private)
	if _, err := config.Load(); err == nil {
		t.Fatal("private file under public assets accepted")
	}
	link := filepath.Join(t.TempDir(), "admin.json")
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RANKING_ADMIN_CONFIG_FILE", link)
	if _, err := config.Load(); err == nil {
		t.Fatal("symlink into public assets accepted")
	}
}

func TestAdminConfiguration(t *testing.T) {
	cfg, err := config.Load()
	if err != nil || cfg.AdminOwner != nil || cfg.AdminAllowLocalHTTP {
		t.Fatalf("disabled defaults: %v", err)
	}
	path := filepath.Join(t.TempDir(), "admin.json")
	t.Setenv("RANKING_ADMIN_CONFIG_FILE", path)
	if _, err := config.Load(); err == nil {
		t.Fatal("missing file accepted")
	}
	for _, body := range []string{`{`, `{}`, `{"username":"owner","password":"short"}`, `{"username":"owner","password":"long-test-password","extra":1}`, `{"username":"owner","password":"long-test-password"} {}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.Load(); err == nil || strings.Contains(err.Error(), "long-test-password") {
			t.Fatal("invalid file accepted or leaked password")
		}
	}
	if err := os.WriteFile(path, []byte(`{"username":"owner","password":"long-test-password"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load()
	if err != nil || cfg.AdminOwner == nil || cfg.AdminOwner.Username != "owner" {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(); err == nil {
		t.Fatal("publicly readable password accepted")
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RANKING_ADMIN_ALLOW_INSECURE_LOCAL_HTTP", "true")
	t.Setenv("RANKING_HTTP_ADDR", "0.0.0.0:8080")
	if _, err := config.Load(); err == nil {
		t.Fatal("public insecure listener accepted")
	}
	t.Setenv("RANKING_HTTP_ADDR", "127.0.0.1:8080")
	if _, err := config.Load(); err != nil {
		t.Fatal(err)
	}
}
