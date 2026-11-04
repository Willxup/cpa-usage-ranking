# SQLite storage

The service opens its database before listening for HTTP requests. There are
currently no business tables. Opening a database does **not** make `/readyz`
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
