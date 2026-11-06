package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cpa-usage-ranking/internal/models"
)

func loadCatalog(t *testing.T, content string) (*models.Catalog, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return models.Load(path)
}

func catalogJSON(items ...string) string {
	return `{"currency":"USD","price_unit":"per_million_tokens","models":[` + strings.Join(items, ",") + `]}`
}

func item(name string, aliases []string, prices string) string {
	n, _ := json.Marshal(name)
	a, _ := json.Marshal(aliases)
	return `{"model_name":` + string(n) + `,"alias":` + string(a) + `,"prices":` + prices + `}`
}

func TestShippedCatalogLoads(t *testing.T) {
	catalog, err := models.Load(filepath.Join("..", "..", "..", "config", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries := catalog.List()
	aliases := 0
	for _, entry := range entries {
		aliases += len(entry.Aliases)
	}
	if len(entries) != 105 || aliases != 96 {
		t.Fatalf("shipped seed: %d models, %d aliases", len(entries), aliases)
	}
}

func TestPricesPreserveUnknownFreeAndPrecision(t *testing.T) {
	long := "123456789012345678901234567890.000000000000000000000000000123"
	catalog, err := loadCatalog(t, catalogJSON(item("Model", []string{"model"}, `{"default":{"input":"0","output":null,"cache_read":"`+long+`"},"flex":{"output":"1.25"}}`)))
	if err != nil {
		t.Fatal(err)
	}
	entry := catalog.List()[0]
	price := entry.Prices[models.Default]
	if price.Input == nil || *price.Input != "0" || price.Output != nil || price.CacheWrite != nil || price.CacheRead == nil || *price.CacheRead != long {
		t.Fatalf("price not preserved: %+v", price)
	}
	if _, ok := entry.Prices[models.Priority]; ok {
		t.Fatal("missing priority inherited a price")
	}
	if *entry.Prices[models.Flex].Output != "1.25" {
		t.Fatal("flex price not preserved")
	}
	// 返回值中的别名、map 和价格指针不能改写已校验目录。
	entry.Aliases[0] = "changed"
	*price.Input = "42"
	delete(entry.Prices, models.Default)
	copy := catalog.List()[0]
	if copy.Aliases[0] != "model" || *copy.Prices[models.Default].Input != "0" {
		t.Fatal("List exposed mutable catalog state")
	}
}

func TestInvalidCatalogsFail(t *testing.T) {
	valid := item("alpha", []string{}, `{}`)
	cases := map[string]string{
		"broken JSON": `{`, "multiple JSON values": catalogJSON(valid) + ` {}`,
		"no models":                 `{"currency":"USD","price_unit":"per_million_tokens"}`,
		"null models":               `{"currency":"USD","price_unit":"per_million_tokens","models":null}`,
		"wrong currency":            strings.Replace(catalogJSON(valid), `"USD"`, `"EUR"`, 1),
		"wrong unit":                strings.Replace(catalogJSON(valid), `"per_million_tokens"`, `"token"`, 1),
		"unknown field":             strings.Replace(catalogJSON(valid), `"currency"`, `"currencY_typo"`, 1),
		"null alias":                catalogJSON(item("alpha", nil, `{}`)),
		"duplicate standard":        catalogJSON(valid, valid),
		"alias vs later standard":   catalogJSON(item("alpha", []string{"beta"}, `{}`), item("beta", []string{}, `{}`)),
		"alias vs earlier standard": catalogJSON(item("beta", []string{}, `{}`), item("alpha", []string{"beta"}, `{}`)),
		"alias vs alias":            catalogJSON(item("alpha", []string{"shared"}, `{}`), item("beta", []string{"shared"}, `{}`)),
		"unknown tier":              catalogJSON(item("alpha", []string{}, `{"prority":{}}`)),
		"null tier object":          catalogJSON(item("alpha", []string{}, `{"default":null}`)),
		"unknown price field":       catalogJSON(item("alpha", []string{}, `{"default":{"inpt":"1"}}`)),
		"numeric price":             catalogJSON(item("alpha", []string{}, `{"default":{"input":0}}`)),
		"negative price":            catalogJSON(item("alpha", []string{}, `{"default":{"input":"-0.1"}}`)),
		"exponent price":            catalogJSON(item("alpha", []string{}, `{"default":{"input":"1e2"}}`)),
		"decimal missing digits":    catalogJSON(item("alpha", []string{}, `{"default":{"input":"1."}}`)),
		"invalid UTF-8":             catalogJSON(`{"model_name":"` + string([]byte{0xff}) + `","alias":[],"prices":{}}`),
		"lone high surrogate":       catalogJSON(`{"model_name":"\ud800","alias":[],"prices":{}}`),
		"lone low surrogate":        catalogJSON(`{"model_name":"alpha","alias":["\udc00"],"prices":{}}`),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadCatalog(t, input); err == nil {
				t.Fatal("invalid catalog was accepted")
			}
		})
	}
	if _, err := models.Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestCatalogNameBoundaries(t *testing.T) {
	for _, name := range []string{"", " alpha", "alpha\u00a0", "a/b", `a\b`, "a\n", "a\u0085b", strings.Repeat("a", 129), strings.Repeat("界", 43)} {
		for _, alias := range []bool{false, true} {
			t.Run(name+"/alias="+map[bool]string{true: "yes", false: "no"}[alias], func(t *testing.T) {
				entry := item(name, []string{}, `{}`)
				if alias {
					entry = item("alpha", []string{name}, `{}`)
				}
				if _, err := loadCatalog(t, catalogJSON(entry)); err == nil {
					t.Fatalf("accepted invalid name %q", name)
				}
			})
		}
	}
	for _, name := range []string{strings.Repeat("a", 128), strings.Repeat("界", 42) + "ab", "模型", "�"} {
		if _, err := loadCatalog(t, catalogJSON(item(name, []string{}, `{}`))); err != nil {
			t.Fatalf("valid name %q: %v", name, err)
		}
	}
	if _, err := loadCatalog(t, catalogJSON(`{"model_name":"\ud83d\ude00","alias":[],"prices":{}}`)); err != nil {
		t.Fatalf("valid surrogate pair: %v", err)
	}
	// 相同模型可明确列出大小写变体，不进行大小写折叠。
	if _, err := loadCatalog(t, catalogJSON(item("Alpha", []string{"alpha"}, `{}`), item("ALPHA", []string{}, `{}`))); err != nil {
		t.Fatalf("case-sensitive names: %v", err)
	}
}
