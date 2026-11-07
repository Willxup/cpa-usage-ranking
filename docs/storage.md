# SQLite storage

The service opens its database and applies schema migrations before listening
for HTTP requests. Schema version 1 creates `clients` for
[client identity/profile persistence](client-storage.md); startup creates no
participant records. Version 2 creates the model-name dictionary. After the
migration, startup atomically registers the current catalog's canonical names.
IDs are positive and stable; ID 0 is reserved for internal totals and is not a
dictionary row. Removed names remain in the dictionary but are not active until
reintroduced in the catalog. Version 3 adds the persistent report-window watermark;
startup advances it to at least UTC yesterday. Version 4 adds daily report
revision metadata and daily statistics tables; it does not add periodic summaries.
Opening a database does **not** make `/readyz`
return success; the ranking service is not implemented yet.

## Build and file location

The standard build uses `database/sql` with `github.com/mattn/go-sqlite3`
v1.14.53, bundling SQLite **3.54.1**. It requires CGO and a C compiler; it does
not require the system `sqlite3` command or a separately installed SQLite
library. Tests query the actual linked engine, not the command-line utility.
This release includes the official [WAL-reset fix](https://sqlite.org/wal.html#walreset)
(first fixed in 3.51.3). Builds using a different system SQLite library must
be verified separately.

```sh
RANKING_SQLITE_PATH=/srv/cpa-ranking/data/ranking.db go run ./cmd/ranking
```

- Default: `data/ranking.db`, relative to the process working directory.
- This is a **file path**, not a DSN. URI characters in the filename are escaped;
  they cannot override connection settings. No in-memory database mode is offered.
- Missing parent directories are created with mode 0700 (subject to umask).
  Existing directory permissions are unchanged: protect the configured data
  directory and use local persistent storage writable by the service account.
- WAL requires local filesystem coordination; do not use an NFS/network share.
- SQLite may create `-wal` and `-shm` sidecars. Do not remove or copy them
  independently while the database is open. Stop the service before an offline
  copy, or use SQLite-aware backup tools.
- An inaccessible path or invalid database fails startup. The application does
  not delete, repair, rename or replace an existing database to make startup succeed.

## Connections

| Setting | Current value |
| --- | --- |
| Write pool | 1 open / 1 idle connection |
| Read pool | 8 open / 4 idle connections, created on demand |
| Reader access | SQLite `mode=ro` |
| Journal mode | WAL, verified at open |
| Synchronization | FULL |
| Foreign keys | enabled |
| Busy timeout | 5 seconds |
| Writer transaction start | IMMEDIATE |

Connection-local settings are supplied in the driver DSN, so new connections
and reconnects receive the same settings. Readers use independent snapshots;
a read transaction does not prevent a writer from committing in WAL mode.
Keep future read transactions short so they do not hold back checkpoints.
SQLite's automatic checkpoint behavior is unchanged; there is no custom
checkpoint worker, application retry loop or transaction abstraction layer.

The SQL connection bound is not a write-admission queue or a throughput promise.
Storage is closed after HTTP shutdown; both pools are closed even if one close
returns an error. Tests live under `internal/storage/test/` and use real files.

## Transactional migrations

`storage.Migrate(ctx, db.Writer, migrations)` accepts an explicit ordered slice
of `Migration{Version, Apply}`. `Version` must be a positive, increasing integer;
`Apply` receives the context and a `*sql.Tx`. The application calls
`storage.MigrateSchema(ctx, db.Writer)` to apply the explicit built-in list in
`internal/storage/schema.go`. Version 1 creates `clients` and version 2 creates
`models` with only an ID and a unique canonical name; aliases and prices are
not copied into the database. Version 3 adds the single-row report-window state.
Version 4 creates `daily_reports` and `daily_stats` using composite primary keys
and `WITHOUT ROWID`; a partial index supports total-row reads. The total uses
model ID 0 and tier 0 without inserting a model dictionary row; real details use
positive model IDs. Totals and details are alternative views and must not be
summed together. Existing databases upgrade through the same path.
Migrations do not silently adopt an untracked table with the same name.

For each version, the runner starts an IMMEDIATE transaction, checks the ledger,
runs unapplied work, records the version and commits. The record is never
committed separately from the schema/data changes. A callback, version-record,
cancellation or commit failure is returned with the version and cause; failed
work rolls back. Previously committed versions stay committed, so the next
startup can continue rather than rerunning successful work. Reopening the file
does not rerun recorded versions.

The version check occurs **after** acquiring SQLite's writer lock. Independent
service instances therefore cannot apply the same version simultaneously. The
normal five-second SQLite busy timeout still applies; there is no extra retry
loop, process lock or migration scheduler.

When adding migrations:

- Keep the released catalog append-only with higher version numbers. Do not
  change already-applied migrations or remove their entries to force replay.
- Use only the supplied transaction for database changes. Do not call the outer
  write pool from inside `Apply` (its sole connection is already held).
- Do not manually commit/roll back, include transaction-control SQL, or make
  network/filesystem changes in `Apply`; those cannot be rolled back atomically
  with the database.
- Initialization of the ledger is separate from individual versions. A failed
  first migration may leave an empty ledger, never a successful version record.
- There is no automatic downgrade, SQL discovery, checksum reconciliation,
  schema repair or legacy-database conversion.
