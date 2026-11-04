# CPA Usage Ranking

[简体中文](README.zh.md)

An independent usage-ranking service. The foundation is under construction;
registration, reporting and leaderboards are not implemented yet. SQLite
connections and transactional migration tracking are available, but no business
tables exist.

## Development

Requires Go 1.26.10 or newer, CGO enabled and a C compiler (such as GCC).

```sh
go test ./...
go run ./cmd/ranking
```

The service listens on `http://127.0.0.1:8080` by default. SIGINT/SIGTERM
stop admission and drain active requests within the configured grace period.
A shutdown timeout closes remaining connections and exits unsuccessfully.
Startup and shutdown events are logged as JSON to stderr.

Tests live in each module's `test/` directory, not beside production files.

## Storage foundation

Startup opens `data/ranking.db` (override `RANKING_SQLITE_PATH`) before accepting
HTTP requests. It creates missing parent directories, prepares the migration
ledger and enables WAL, FULL synchronization and foreign keys. Each declared
migration and its version record commit atomically; already-applied versions
are skipped. Currently only `schema_migrations` is created. Database failures stop startup; they do not
trigger automatic repair or fallback. Shutdown drains HTTP before closing the
bounded write/read connection pools. See [SQLite storage](docs/storage.md).
Business readiness remains 503.

## Frontend development

Requires Node.js 24.15+ (24.x) and npm. Runtime dependencies are only React and
React DOM; the application does not have business features yet. Tailwind runs
at build time. Shared colors, typography and focus styling live in
`web/src/styles/tokens.css`; no external font or icon services are required.
The selected React Bits control uses CSS only and adds no animation runtime.
See [third-party source notices](THIRD_PARTY_NOTICES.md) for its pinned source,
local adaptations and MIT + Commons Clause license.

The interface uses a light, wide layout with coral/violet accents and compact
desktop sizing without page zoom; mobile sizing remains independent. Summary
values stay unknown (`—`), and time/search/model filters remain disabled until
business functionality is implemented. Project icons and regeneration details
are documented in [Brand icons](docs/brand-icons.md).

The responsive preview has in-page navigation, an explicit empty-data view and
a service-status panel with unchecked/loading/success/error states. It does
not simulate standings or usage totals. The check button calls the real
`GET /healthz` endpoint on demand, validates the JSON contract, times out after
8 seconds, supports retry and cancels requests on unmount. There is no polling.
A successful check does not mark the ranking business as ready.

Decorative motion respects `prefers-reduced-motion` and stops when the control
is offscreen, disabled or the document is hidden. Missing observer APIs fall
back to a static control. Observers/listeners are removed on unmount; the page
has no canvas, WebGL, external fonts or background JavaScript animation loop.

For local development, run `go run ./cmd/ranking` and `npm --prefix web run dev`
in separate terminals. Vite binds to loopback and proxies health endpoints to
`http://127.0.0.1:8080`; override `RANKING_DEV_PROXY_TARGET` if needed. No CORS
relaxation or production Node server is required.

```sh
npm --prefix web ci
npm --prefix web run dev
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web test
npm --prefix web run build
```

Frontend tests and setup live under `web/src/**/test/`. TypeScript stays on
6.0 while the lint parser supports versions below 6.1. The lint parser's
`minimatch` is overridden to 11.0.1 to address GHSA-g3hv-75wg-m249; remove the
override when its upstream dependency range includes a fixed release.

## Full-stack verification

With Go, Node/npm and Make installed:

```sh
make deps       # Go modules and frozen npm lockfile
make verify     # formatting, vet, Go tests, web type/lint/tests, both builds
make test-race  # Go concurrency checks
make browser-install # install the single headless Chromium test browser
make verify-all      # verify + race + production browser smoke checks
make run        # build and serve the frontend with Go on port 8080
```

`BIN_DIR` defaults to `bin`; `GO_TEST_FLAGS` defaults to `-p 2` and is overridable.
Builds need no local secrets or planning files. The frontend build includes
React Bits attribution and its complete license at
`web/dist/assets/third-party-notices.txt`. Keep that file when deploying.

Browser tests/configuration live in `web/test/`. `make test-browser` builds and
starts an isolated Go process on loopback port 4187, checks desktop/mobile
layouts, real health requests, keyboard/touch access, reduced motion, retry
and 404 boundaries, then stops the process. It refuses to reuse an existing
service. Override `RANKING_TEST_PORT` if occupied. Results default to
`web/test/results/`; `PLAYWRIGHT_OUTPUT_DIR` and `PLAYWRIGHT_BROWSERS_PATH` can
relocate artifacts and browser downloads. Browser checks use a separate SQLite
file under a `databases/<port>/` sibling of the results directory, never the
application's default database. A minimal Linux host may first need
Playwright's Chromium system dependencies installed by its administrator.
These smoke checks do not establish 100,000-client capacity.

## Production frontend serving

```sh
npm --prefix web ci
npm --prefix web run build
go build -o bin/ranking ./cmd/ranking
RANKING_WEB_DIR=web/dist ./bin/ranking
```

Go serves the built application and API on the same origin. Deploy the binary,
`web/dist/` and the third-party notices/licenses together; Node is needed only
for building. `RANKING_WEB_DIR` is relative to the process working directory
unless absolute. If unset, only backend routes are enabled. If set to an
incomplete build, startup fails instead of silently serving a broken site.

Only `/`, `/index.html`, `/favicon.svg` and actual files below `/assets/` are
served. There is no arbitrary SPA fallback or directory listing. Missing files,
unknown APIs and unknown pages remain 404. Traversal and symlink escapes are
rejected. HTML, non-fingerprinted files and stable files in public asset
subdirectories revalidate; top-level fingerprinted build assets can be cached
for a year. Do not modify assets in place while serving;
stop the process, replace the build, then restart.

## HTTP foundation

- `GET /healthz`: 200 with `{"status":"ok"}` means the process is alive.
- `GET /readyz`: 503 while business services are not implemented.
- Unknown routes and unsupported methods return JSON errors (404/405).
- Panic responses are generic; request bodies, headers and panic values are not logged.
- Forwarded client-IP headers are not trusted by default.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `RANKING_HTTP_ADDR` | `127.0.0.1:8080` | Listen address; numeric port 0 selects a free port |
| `RANKING_SHUTDOWN_TIMEOUT` | `10s` | Positive grace period for shutdown |
| `RANKING_WEB_DIR` | unset | Optional built frontend directory; unset means no frontend routes |
| `RANKING_SQLITE_PATH` | `data/ranking.db` | SQLite file path, relative to the working directory unless absolute |

Explicitly empty or invalid settings fail validation. No configuration files
are loaded automatically. Do not commit credentials or local `.env` files.
