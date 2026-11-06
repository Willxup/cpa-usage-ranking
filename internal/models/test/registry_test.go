package test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-ranking/internal/models"
	"cpa-usage-ranking/internal/storage"
)

func openModelDB(t *testing.T, path string) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.MigrateSchema(context.Background(), db.Writer); err != nil {
		t.Fatal(err)
	}
	return db
}

func mustCatalog(t *testing.T, entries ...string) *models.Catalog {
	t.Helper()
	catalog, err := loadCatalog(t, catalogJSON(entries...))
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func register(t *testing.T, db *storage.DB, entries ...string) *models.Registry {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	registry, err := models.Register(ctx, db.Writer, mustCatalog(t, entries...))
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func modelID(t *testing.T, registry *models.Registry, name string) int64 {
	t.Helper()
	id, ok := registry.CanonicalID(name)
	if !ok || id <= 0 {
		t.Fatalf("missing positive ID for %q: %d, %v", name, id, ok)
	}
	return id
}

func TestRegistrationPreservesIDsAcrossCatalogChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ranking.db")
	db := openModelDB(t, path)
	alpha := item("Alpha", []string{"alpha-old"}, `{"default":{"input":"0"}}`)
	beta := item("Beta", []string{"beta-alias"}, `{}`)
	initial := register(t, db, alpha, beta)
	alphaID, betaID := modelID(t, initial, "Alpha"), modelID(t, initial, "Beta")
	if alphaID == betaID {
		t.Fatal("distinct canonical models received the same ID")
	}
	if _, ok := initial.CanonicalID("alpha-old"); ok {
		t.Fatal("alias became a canonical dictionary name")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openModelDB(t, path)
	alphaChanged := item("Alpha", []string{"alpha-new"}, `{"priority":{"input":"999.1"}}`)
	gamma := item("Gamma", []string{}, `{}`)
	changed := register(t, db, beta, gamma, alphaChanged)
	gammaID := modelID(t, changed, "Gamma")
	if modelID(t, changed, "Alpha") != alphaID || modelID(t, changed, "Beta") != betaID || gammaID == alphaID || gammaID == betaID {
		t.Fatal("reopen, reorder, addition, alias or price change altered an ID")
	}
	removed := register(t, db, gamma, alphaChanged)
	if _, ok := removed.CanonicalID("Beta"); ok {
		t.Fatal("historical dictionary row remained in current catalog")
	}
	var stored int
	if err := db.Reader.QueryRow("SELECT COUNT(*) FROM models").Scan(&stored); err != nil || stored != 3 {
		t.Fatalf("removal discarded history or stored aliases: count=%d err=%v", stored, err)
	}
	readded := register(t, db, beta, alphaChanged, gamma)
	if modelID(t, readded, "Beta") != betaID || modelID(t, readded, "Gamma") != gammaID {
		t.Fatal("re-added model did not recover the historical ID")
	}
}

func TestRegistrationRollsBackEntireBatch(t *testing.T) {
	db := openModelDB(t, filepath.Join(t.TempDir(), "ranking.db"))
	old := item("existing", []string{}, `{}`)
	oldID := modelID(t, register(t, db, old), "existing")
	_, err := db.Writer.Exec(`CREATE TRIGGER reject_model BEFORE INSERT ON models
		WHEN NEW.model_name='blocked' BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	first, blocked := item("new-before-failure", []string{}, `{}`), item("blocked", []string{}, `{}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	registry, err := models.Register(ctx, db.Writer, mustCatalog(t, old, first, blocked))
	if err == nil || !strings.Contains(err.Error(), "injected failure") || registry != nil {
		t.Fatalf("expected failed transaction, got registry=%v err=%v", registry, err)
	}
	var count int
	var id int64
	if err := db.Reader.QueryRow("SELECT COUNT(*), MIN(id) FROM models").Scan(&count, &id); err != nil || count != 1 || id != oldID {
		t.Fatalf("partial registration persisted: count=%d id=%d err=%v", count, id, err)
	}
	if _, err := db.Writer.Exec("DROP TRIGGER reject_model"); err != nil {
		t.Fatal(err)
	}
	if modelID(t, register(t, db, old, first, blocked), "existing") != oldID {
		t.Fatal("retry changed existing ID")
	}
}

func TestConcurrentRegistrationAndDictionaryConstraints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ranking.db")
	one, two := openModelDB(t, path), openModelDB(t, path)
	catalog := mustCatalog(t, item("Alpha", []string{"a"}, `{}`), item("Beta", []string{}, `{}`))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	registries := make([]*models.Registry, 2)
	errs := make([]error, 2)
	for i, db := range []*storage.DB{one, two} {
		wg.Add(1)
		go func(i int, writer *sql.DB) {
			defer wg.Done()
			registries[i], errs[i] = models.Register(ctx, writer, catalog)
		}(i, db.Writer)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("registration %d: %v", i, err)
		}
	}
	if modelID(t, registries[0], "Alpha") != modelID(t, registries[1], "Alpha") || modelID(t, registries[0], "Beta") != modelID(t, registries[1], "Beta") {
		t.Fatal("concurrent registrants disagreed on IDs")
	}
	for _, query := range []string{
		`INSERT INTO models (id, model_name) VALUES (0, 'reserved')`,
		`INSERT INTO models (id, model_name) VALUES (-1, 'negative')`,
		`INSERT INTO models (model_name) VALUES ('Alpha')`,
		`INSERT INTO models (model_name) VALUES (NULL)`,
	} {
		if _, err := one.Writer.Exec(query); err == nil {
			t.Fatalf("dictionary accepted invalid row: %s", query)
		}
	}
	var count int
	if err := one.Reader.QueryRow("SELECT COUNT(*) FROM models").Scan(&count); err != nil || count != 2 {
		t.Fatalf("unexpected dictionary rows: %d, %v", count, err)
	}
}
