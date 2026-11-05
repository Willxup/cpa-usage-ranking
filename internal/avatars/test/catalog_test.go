package test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"cpa-usage-ranking/internal/avatars"
)

const largeAvatarID int64 = 1 << 40

func TestLoadCatalogAndLookup(t *testing.T) {
	catalog, err := avatars.Load(os.DirFS("testdata/catalog"))
	if err != nil {
		t.Fatal(err)
	}
	if catalog.DefaultID() != largeAvatarID {
		t.Fatalf("default ID = %d, want %d", catalog.DefaultID(), largeAvatarID)
	}
	want := []avatars.Avatar{
		{ID: 67, Path: "images/circle.svg"},
		{ID: largeAvatarID, Path: "images/square.svg"},
	}
	if got := catalog.List(); !reflect.DeepEqual(got, want) {
		t.Fatalf("list = %+v, want %+v", got, want)
	}
	for _, avatar := range want {
		if got, ok := catalog.Lookup(avatar.ID); !ok || got != avatar {
			t.Fatalf("lookup %d = %+v, %t", avatar.ID, got, ok)
		}
	}
	// 成员检查不猜测编号范围，也不把显式的非法编号替换成默认项。
	for _, id := range []int64{-1, 0, 1, 66, 255, 256, largeAvatarID + 1} {
		if got, ok := catalog.Lookup(id); ok || got != (avatars.Avatar{}) {
			t.Fatalf("unknown ID %d = %+v, %t", id, got, ok)
		}
	}
	list := catalog.List()
	list[0].ID, list[0].Path = 1, "changed.svg"
	if got := catalog.List(); !reflect.DeepEqual(got, want) {
		t.Fatalf("caller changed the catalog through List: %+v", got)
	}
	if got, ok := catalog.Lookup(67); !ok || got != want[0] {
		t.Fatalf("caller changed lookup metadata: %+v, %t", got, ok)
	}
}

func fixtureWithCatalog(t *testing.T, data string) fs.FS {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata/catalog")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalog.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return os.DirFS(dir)
}

func TestAppendAvatarWithoutChangingExistingIDs(t *testing.T) {
	catalog, err := avatars.Load(fixtureWithCatalog(t, `{
		"default_avatar_id": 1099511627776,
		"avatars": [
			{"avatar_id": 67, "path": "images/circle.svg"},
			{"avatar_id": 1099511627776, "path": "images/square.svg"},
			{"avatar_id": 4096, "path": "images/circle.svg"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if catalog.DefaultID() != largeAvatarID || len(catalog.List()) != 3 {
		t.Fatalf("default/order changed during extension: %d, %+v", catalog.DefaultID(), catalog.List())
	}
	for id, path := range map[int64]string{67: "images/circle.svg", largeAvatarID: "images/square.svg", 4096: "images/circle.svg"} {
		if got, ok := catalog.Lookup(id); !ok || got.Path != path {
			t.Fatalf("lookup %d after extension = %+v, %t", id, got, ok)
		}
	}
}

func TestRejectInvalidCatalog(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"invalid JSON", `{`, "decode avatar catalog"},
		{"trailing JSON", `{"default_avatar_id":1,"avatars":[]} {}`, "decode avatar catalog"},
		{"trailing garbage", `{"default_avatar_id":1,"avatars":[]} invalid`, "decode avatar catalog"},
		{"unknown field", `{"default_avatar_id":1,"avatars":[],"avtars":[]}`, "decode avatar catalog"},
		{"null", `null`, "avatars must not be empty"},
		{"empty entries", `{"default_avatar_id":1,"avatars":[]}`, "avatars must not be empty"},
		{"missing entries", `{"default_avatar_id":1}`, "avatars must not be empty"},
		{"zero ID", `{"default_avatar_id":0,"avatars":[{"avatar_id":0,"path":"images/circle.svg"}]}`, "avatar_id must be positive"},
		{"negative ID", `{"default_avatar_id":-1,"avatars":[{"avatar_id":-1,"path":"images/circle.svg"}]}`, "avatar_id must be positive"},
		{"fractional ID", `{"default_avatar_id":1,"avatars":[{"avatar_id":1.5,"path":"images/circle.svg"}]}`, "decode avatar catalog"},
		{"overflow ID", `{"default_avatar_id":1,"avatars":[{"avatar_id":9223372036854775808,"path":"images/circle.svg"}]}`, "decode avatar catalog"},
		{"duplicate ID", `{"default_avatar_id":1,"avatars":[{"avatar_id":1,"path":"images/circle.svg"},{"avatar_id":1,"path":"images/square.svg"}]}`, "duplicate avatar_id 1"},
		{"missing default", `{"avatars":[{"avatar_id":1,"path":"images/circle.svg"}]}`, "default_avatar_id 0 is not in the catalog"},
		{"unknown default", `{"default_avatar_id":2,"avatars":[{"avatar_id":1,"path":"images/circle.svg"}]}`, "default_avatar_id 2 is not in the catalog"},
		{"empty path", `{"default_avatar_id":1,"avatars":[{"avatar_id":1}]}`, "invalid resource path"},
		{"directory resource", `{"default_avatar_id":1,"avatars":[{"avatar_id":1,"path":"images"}]}`, "resource must be a regular file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			catalog, err := avatars.Load(fixtureWithCatalog(t, tc.data))
			if err == nil || catalog != nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load = %+v, %v; want nil catalog and error containing %q", catalog, err, tc.want)
			}
		})
	}
}

func TestRejectNonLocalResourcePaths(t *testing.T) {
	for _, path := range []string{".", "../outside.svg", "/outside.svg", "images/../images/circle.svg", "images//circle.svg", `images\circle.svg`, "https://example.test/avatar.svg", "file:circle.svg", "images/circle.svg?x=1", "images/circle.svg#fragment"} {
		t.Run(path, func(t *testing.T) {
			// JSON 编码只转义输入，不替加载器做路径清理。
			encodedPath, err := json.Marshal(path)
			if err != nil {
				t.Fatal(err)
			}
			data := `{"default_avatar_id":1,"avatars":[{"avatar_id":1,"path":` + string(encodedPath) + `}]}`
			catalog, err := avatars.Load(fixtureWithCatalog(t, data))
			if err == nil || catalog != nil || !strings.Contains(err.Error(), "invalid resource path") {
				t.Fatalf("Load path %q = %+v, %v", path, catalog, err)
			}
		})
	}
}

func TestMissingCatalogAndResourceErrors(t *testing.T) {
	t.Run("catalog", func(t *testing.T) {
		catalog, err := avatars.Load(os.DirFS(t.TempDir()))
		if catalog != nil || !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("missing catalog = %+v, %v", catalog, err)
		}
	})
	for _, missing := range []string{"default resource", "non-default resource"} {
		t.Run(missing, func(t *testing.T) {
			defaultID := "2"
			if missing == "default resource" {
				defaultID = "1"
			}
			catalog, err := avatars.Load(fixtureWithCatalog(t, `{"default_avatar_id":`+defaultID+`,"avatars":[{"avatar_id":1,"path":"images/missing.svg"},{"avatar_id":2,"path":"images/circle.svg"}]}`))
			if catalog != nil || !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "images/missing.svg") {
				t.Fatalf("missing resource = %+v, %v", catalog, err)
			}
		})
	}
}
