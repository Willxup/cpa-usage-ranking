package test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"cpa-usage-ranking/internal/models"
)

func TestResolveExactNamesAliasesAndPaths(t *testing.T) {
	db := openModelDB(t, filepath.Join(t.TempDir(), "ranking.db"))
	max := strings.Repeat("x", 128)
	registry := register(t, db,
		item("model", []string{"MODEL", "model-20261001"}, `{}`),
		item("Model", []string{}, `{}`),
		item("é", []string{}, `{}`),
		item(max, []string{}, `{}`),
	)
	id := modelID(t, registry, "model")
	otherID := modelID(t, registry, "Model")
	cases := []struct {
		raw, normalized, canonical string
		id                         int64
	}{
		{"model", "model", "model", id},
		{"\u00a0model  ", "model", "model", id},
		{"provider/route/model", "model", "model", id},
		{"/provider//MODEL", "MODEL", "model", id},
		{"vendor/model-20261001", "model-20261001", "model", id},
		{"Model", "Model", "Model", otherID},
		{"MoDeL", "MoDeL", "", 0},
		{"vendor/model-20261002", "model-20261002", "", 0},
		{"vendor/unknown", "unknown", "", 0},
		{"vendor/ model", " model", "", 0},
		{"e\u0301", "e\u0301", "", 0},
		{`vendor\model`, `vendor\model`, "", 0},
		{max, max, max, modelID(t, registry, max)},
	}
	var before int
	if err := db.Reader.QueryRow("SELECT COUNT(*) FROM models").Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			result, err := registry.Resolve(tc.raw)
			if err != nil || result.NormalizedName != tc.normalized || result.CanonicalName != tc.canonical || result.ID != tc.id || result.Matched != (tc.id != 0) {
				t.Fatalf("Resolve(%q) = %+v, %v", tc.raw, result, err)
			}
		})
	}
	var after int
	if err := db.Reader.QueryRow("SELECT COUNT(*) FROM models").Scan(&after); err != nil || after != before {
		t.Fatalf("lookups changed dictionary: before=%d after=%d err=%v", before, after, err)
	}
}

func TestResolveRejectsMalformedRawNames(t *testing.T) {
	db := openModelDB(t, filepath.Join(t.TempDir(), "ranking.db"))
	registry := register(t, db, item("model", []string{}, `{}`))
	for _, raw := range []string{
		"", "    ", "/", "vendor/", " vendor/  ",
		"model\n", "\tmodel", "vendor/\x00model", "vendor/\u0085model",
		string([]byte{'x', '/', 0xff}), strings.Repeat("a", 129), strings.Repeat("界", 43),
		"x/" + strings.Repeat("a", 127),
	} {
		t.Run(raw, func(t *testing.T) {
			if result, err := registry.Resolve(raw); !errors.Is(err, models.ErrInvalidReportedName) || result.Matched {
				t.Fatalf("Resolve(%q) = %+v, %v; expected format error", raw, result, err)
			}
		})
	}
}

func TestResolveUsesCurrentCatalogNotHistoricalRows(t *testing.T) {
	db := openModelDB(t, filepath.Join(t.TempDir(), "ranking.db"))
	old := item("alpha", []string{"old-alias"}, `{}`)
	beta := item("beta", []string{"beta-alias"}, `{}`)
	first := register(t, db, old, beta)
	alphaID, betaID := modelID(t, first, "alpha"), modelID(t, first, "beta")
	changed := register(t, db, item("alpha", []string{"new-alias"}, `{}`))
	for _, raw := range []string{"old-alias", "beta", "beta-alias"} {
		result, err := changed.Resolve("vendor/" + raw)
		if err != nil || result.Matched || result.ID != 0 {
			t.Fatalf("historical name %q was treated as current: %+v, %v", raw, result, err)
		}
	}
	resolved, err := changed.Resolve("vendor/new-alias")
	if err != nil || !resolved.Matched || resolved.ID != alphaID {
		t.Fatalf("changed alias lost canonical ID: %+v, %v", resolved, err)
	}
	reintroduced := register(t, db, item("alpha", []string{"new-alias"}, `{}`), beta)
	resolved, err = reintroduced.Resolve("x/beta-alias")
	if err != nil || !resolved.Matched || resolved.ID != betaID {
		t.Fatalf("reintroduced alias lost historical canonical ID: %+v, %v", resolved, err)
	}
}

func TestReportedTierDistinguishesOmissionNullAndValues(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name    string
		present bool
		value   *string
		tier    models.Tier
		valid   bool
	}{
		{"omitted", false, nil, models.Default, true},
		{"default", true, str("default"), models.Default, true},
		{"priority", true, str("priority"), models.Priority, true},
		{"flex", true, str("flex"), models.Flex, true},
		{"null", true, nil, 0, false},
		{"empty", true, str(""), 0, false},
		{"typo", true, str("prority"), 0, false},
		{"uppercase", true, str("DEFAULT"), 0, false},
		{"space", true, str(" default"), 0, false},
	}
	if models.Default != 0 || models.Priority != 1 || models.Flex != 2 {
		t.Fatal("tier numeric codes changed")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, err := models.ReportedTier(tc.present, tc.value)
			if (err == nil) != tc.valid || (tc.valid && tier != tc.tier) {
				t.Fatalf("ReportedTier = %v, %v", tier, err)
			}
		})
	}
}
