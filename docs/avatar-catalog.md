# Static avatar catalog loader

`internal/avatars` loads a server-maintained, extensible catalog from an
`io/fs.FS`. It does **not** serve images or add HTTP routes, database tables,
uploads, remote URLs, image decoding or generation. No production catalog or
default avatar is supplied, and startup does not load a catalog yet. The small
SVG catalog under `internal/avatars/test/testdata/` is a test fixture only.

## Directory format

Provide `catalog.json` at the supplied filesystem root. For example (test IDs
and assets, not production defaults):

```text
catalog.json
images/
  circle.svg
  square.svg
```

```json
{
  "default_avatar_id": 1099511627776,
  "avatars": [
    { "avatar_id": 67, "path": "images/circle.svg" },
    { "avatar_id": 1099511627776, "path": "images/square.svg" }
  ]
}
```

- The file contains exactly one JSON object with the fields shown above;
  unknown fields, malformed JSON and trailing data are errors.
- `avatars` must not be empty. Every `avatar_id` is a unique, positive `int64`
  integer, not an array index or `uint8`. IDs need not be consecutive; 66 and
  255 are **not** limits.
- `default_avatar_id` is required and must refer to a listed avatar. It need
  not be the first or smallest ID. There is no guessed or built-in default.
- `path` is a slash-separated file path relative to the **supplied filesystem
  root**, not a URL. Absolute paths, `.`/`..` components, empty components,
  backslashes, colons, query strings and fragments are rejected, not cleaned.
- Every resource, including the default, must exist as a regular file.
  Loading checks filesystem metadata, not image content or licensing. Broken
  or missing resources are errors; they are never silently dropped.

Extend the catalog by adding files and entries. Published IDs must keep their
meaning: do not renumber entries, reuse an ID for another image or remove
resources still referenced by stored clients. This is a catalog-maintenance
contract, not a comparison against previous releases or a database migration.
Multiple IDs may reference one resource. Array order is preserved for listing;
identity and lookup always use the explicit ID.

## Go interface

- `avatars.Load(files fs.FS) (*avatars.Catalog, error)` reads and validates the
  whole catalog. Any failure returns a nil catalog. Filesystem errors remain
  inspectable with `errors.Is`, including `fs.ErrNotExist`.
- `catalog.DefaultID() int64` returns the configured default. A caller uses it
  only when an avatar selection is **omitted**.
- `catalog.Lookup(id int64) (avatars.Avatar, bool)` performs membership lookup.
  An explicitly unknown ID, including zero or a negative value, returns false;
  it does not fall back. An HTTP registration boundary can map this to 422.
- `catalog.List() []avatars.Avatar` returns a copy in catalog order. `Avatar`
  contains `ID` (`avatar_id`) and `Path` (`path`). The loaded catalog is read-only
  and can be shared by concurrent readers.

The caller owns the filesystem boundary/lifetime. Use an embedded filesystem
(or `fs.Sub` of one) or an `os.Root.FS()` constrained to the intended asset root.
`os.DirFS` alone does **not** prevent symlink escapes. Future static serving must
use the same constrained root and define its URL prefix/cache policy; a local
`path` is not an authorization to serve arbitrary filesystem locations. Treat
the catalog and files as a release unit, not resources to mutate while serving.

The client repository remains independent of this loader and expects validated
IDs. Registration, omitted-selection handling, public catalog/resource routes
and the initial approved assets are not implemented here. Tests in
`internal/avatars/test/` use real files and cover sparse/large IDs, extension,
default membership, duplicates, invalid paths, missing resources and copy-safe
listing without local planning files.
