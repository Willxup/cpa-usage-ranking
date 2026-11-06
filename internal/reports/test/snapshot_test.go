package test

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"cpa-usage-ranking/internal/models"
	"cpa-usage-ranking/internal/reports"
	"cpa-usage-ranking/internal/storage"
)

func reportDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.MigrateSchema(context.Background(), db.Writer); err != nil {
		t.Fatal(err)
	}
	return db
}

func reportRegistry(t *testing.T, db *storage.DB, entries string) *models.Registry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.json")
	data := `{"currency":"USD","price_unit":"per_million_tokens","models":[` + entries + `]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := models.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := models.Register(context.Background(), db.Writer, catalog)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func usage(model string, tier models.Tier, tokens, requests int64, avg float64) reports.Item {
	return reports.Item{Model: model, Tier: tier, TotalTokens: tokens, InputTokens: tokens, RequestCount: requests,
		SuccessCount: requests, TTFTAvgMS: number(avg), TTFTSamples: requests}
}

func TestPreparedDigestAndCatalogAggregation(t *testing.T) {
	db := reportDB(t)
	registry := reportRegistry(t, db, `{"model_name":"alpha","alias":["alias"],"prices":{}}`)
	a := usage("route/alpha", models.Default, 10, 2, 3)
	b := usage("alias", models.Default, 20, 1, 6)
	c := usage("alpha", models.Priority, 5, 1, 4)
	unknown := usage("unknown", models.Default, 7, 1, 2)
	zero := reports.Item{Model: "zero"}
	first := reports.Report{Revision: 8, Items: []reports.Item{a, b, c, unknown, zero}}
	p, err := reports.Prepare(first)
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := reports.Prepare(reports.Report{Revision: 8, Items: []reports.Item{zero, unknown, c, b, a}})
	if err != nil || p.Digest() != reordered.Digest() {
		t.Fatalf("order changed digest: %v", err)
	}
	rows, err := p.Aggregate(registry)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := registry.CanonicalID("alpha")
	defaultRow := rows[reports.Key{ModelID: id}]
	if len(rows) != 3 || defaultRow.TotalTokens != 30 || defaultRow.RequestCount != 3 || defaultRow.TTFTSumMS != 12 || defaultRow.TTFTSamples != 3 || rows[reports.Key{}].TotalTokens != 35 || rows[reports.Key{}].TTFTSumMS != 16 {
		t.Fatalf("bad aggregation: %+v", rows)
	}
	// Caller mutation and current catalog/price changes cannot change the prepared digest.
	*a.TTFTAvgMS = 99
	changed := reportRegistry(t, db, `{"model_name":"alpha","alias":[],"prices":{"default":{"input":"1"}}},{"model_name":"unknown","alias":[],"prices":{}}`)
	if p.Digest() != reordered.Digest() {
		t.Fatal("mutable source changed digest")
	}
	newRows, err := p.Aggregate(changed)
	if err != nil || newRows[reports.Key{}].TotalTokens != 22 {
		t.Fatalf("new catalog not used on new aggregation: %+v %v", newRows, err)
	}
	a.TTFTAvgMS = number(3)
	changedUnknown := unknown
	changedUnknown.TotalTokens++
	changedUnknown.InputTokens++
	other, err := reports.Prepare(reports.Report{Revision: 8, Items: []reports.Item{a, b, c, changedUnknown, zero}})
	if err != nil || other.Digest() == p.Digest() {
		t.Fatal("unknown item did not affect digest")
	}
	withoutZero, err := reports.Prepare(reports.Report{Revision: 8, Items: []reports.Item{a, b, c, unknown}})
	if err != nil || withoutZero.Digest() == p.Digest() {
		t.Fatal("zero item did not affect digest")
	}
}

func TestPreparedValidationAndOverflow(t *testing.T) {
	db := reportDB(t)
	registry := reportRegistry(t, db, `{"model_name":"alpha","alias":["alias"],"prices":{}},{"model_name":"beta","alias":[],"prices":{}}`)
	a := usage("alpha", models.Default, 1, 1, 1)
	for _, items := range [][]reports.Item{
		{a, a},
		{a, {Model: "vendor/"}},
		{a, {Model: "bad\nname"}},
		{a, {Model: "unknown", TotalTokens: 1}},
		{a, {Model: "unknown", Tier: 99}},
	} {
		if _, err := reports.Prepare(reports.Report{Items: items}); !errors.Is(err, reports.ErrInvalid) {
			t.Fatalf("accepted invalid input: %+v %v", items, err)
		}
	}
	for _, pair := range [][]reports.Item{
		{usage("alpha", 0, math.MaxInt64, 1, 1), usage("alias", 0, 1, 1, 1)},
		{usage("alpha", 0, math.MaxInt64, 1, 1), usage("beta", 0, 1, 1, 1)},
		{usage("alpha", 0, 1, 1, math.MaxFloat64), usage("alias", 0, 1, 1, math.MaxFloat64)},
	} {
		prepared, err := reports.Prepare(reports.Report{Items: pair})
		if err != nil {
			t.Fatalf("individual valid items rejected: %v", err)
		}
		if _, err := prepared.Aggregate(registry); !errors.Is(err, reports.ErrInvalid) {
			t.Fatalf("aggregation overflow accepted: %v", err)
		}
	}
	p, err := reports.Prepare(reports.Report{Items: []reports.Item{usage("unknown", 0, 1, 1, 1), {Model: "alpha"}}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := p.Aggregate(registry)
	if err != nil || len(rows) != 0 {
		t.Fatalf("unknown/zero produced rows: %+v %v", rows, err)
	}
}

func TestEquivalentNumericSpellingsHaveSameDigest(t *testing.T) {
	first, err := reports.Decode([]byte(validPayload))
	if err != nil {
		t.Fatal(err)
	}
	second, err := reports.Decode([]byte(validPayload))
	if err != nil {
		t.Fatal(err)
	}
	first.Items[0].LatencyAvgMS = number(math.Copysign(0, -1))
	p, err := reports.Prepare(first)
	if err != nil {
		t.Fatal(err)
	}
	q, err := reports.Prepare(second)
	if err != nil || p.Digest() != q.Digest() {
		t.Fatalf("signed zero changed digest: %v", err)
	}
}
