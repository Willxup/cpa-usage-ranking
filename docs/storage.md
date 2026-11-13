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
revision metadata and daily statistics tables. Version 5 adds storage for
finalized daily, weekly, monthly and cumulative statistics. Opening a database alone does **not** make `/readyz` return success: the first complete leaderboard cache round must also succeed.

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
Keep read transactions short so they do not hold back checkpoints. SQLite uses
automatic checkpoints; there is no custom checkpoint worker. The storage
connection layer has no general-purpose retry loop. Report and maintenance
writers implement bounded, cancellable SQLite lock retries; see
[daily report snapshots](report-snapshots.md).

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
positive model IDs. Version 5 adds `period_stats` with a composite primary key
`(kind, start_day, model_id, tier, client_id)` and `WITHOUT ROWID`. Its daily
rows are total-only; other periods keep totals and model/tier details separately.
Weeks start Monday UTC and months start on day 1 UTC, both with inclusive starts
and exclusive ends. Cumulative rows use the fixed internal start `0001-01-01`.
Each row stores the same 12 sums and counts, not averages. Totals and details
are alternative views and must not be summed together. Existing databases
upgrade through the same path.
Migrations do not silently adopt an untracked table with the same name.

Internal candidate reads combine one period's finalized rows with all unsettled
rows in that UTC period, in a single read-only SQLite transaction. Cumulative
reads include the full backlog. The transaction ends before derived processing;
reads time out independently and reject overflow or more than 100,000 clients
per candidate view. All-model totals and individual model/tier views are
separate. Daily views support totals only, because finalized model-level daily
rows are not retained. Annual views combine finalized monthly rows with the year's unsettled daily rows in the same transaction. Public builds also read profiles and filter disabled clients in that snapshot. The limit is not a verified client-capacity claim.

Candidate metrics may be derived from raw sums and counts without changing the
stored int64 values. Ratios and means are unavailable when their denominator
is zero; measured zero remains distinct from a missing measurement. Means use
actual sample counts, and coverage is samples divided by requests. Overall
output tokens/second is calculated only with complete latency coverage and a
positive latency sum. The estimate excluding first-token wait additionally
requires complete TTFT coverage and latency sum greater than TTFT sum. These
are aggregate estimates, not concurrent throughput or an arithmetic mean of
per-request speeds; incomplete coverage is not guessed.

The internal cache worker attempts initialization at startup and refreshes at
`RANKING_CACHE_REFRESH_INTERVAL` (default `5m`, a positive duration). Build
items run sequentially; every item releases its read transaction before encoding.
Only a complete successful round replaces the previous immutable snapshot. A
failure, timeout or capacity limit retains the previous round; a missing first
round is unavailable, and reads never rebuild it. Pending ticks during a build
are discarded. Uploads, profile changes and settlement do not trigger builds. Six real period items each build five leaderboards; see [leaderboard calculations](leaderboards.md). The initial
100,000-candidate, 16 MiB encoded-round and 32-item limits are safety bounds,
not evidence of tested client capacity.

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
