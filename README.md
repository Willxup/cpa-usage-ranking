# CPA Usage Ranking

[简体中文](README.zh.md)

An independent usage-ranking service under construction. Protected client
registration, daily snapshot ingestion, background settlement and SQLite persistence
are available. Five real leaderboards are built in a unified cache for six fixed UTC periods; see [leaderboard calculations](docs/leaderboards.md). No website account UI is provided. The
[model catalog](docs/model-catalog.md) is validated at startup and maps exact
names and aliases to stable IDs; there is no model management API. Reference
prices are not independently verified live quotes and may be outdated.

## Docker deployment

Build and run the image with Docker Compose; the example publishes HTTP to
loopback only and uses a persistent SQLite volume:

```sh
docker compose -p cpa-ranking up -d --build
```

Use HTTPS through a trusted reverse proxy before accepting remote credentials.
See [Docker deployment](docs/docker.md) for both supported architectures,
permissions, health checks, backup, restore and upgrades. The project does not
provide standalone binary releases.

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

Tests live in each module's `test/` directory, not beside production files. The
file-backed HTTP ingestion lifecycle regression covers registration, retries,
corrections, settlement, cache reads and restart recovery:

```sh
go test ./internal/httpserver/test -run '^TestCompleteIngestionLifecycle$'
```

For isolated synthetic fixtures and bounded HTTP load, see the optional
[capacity test tool](docs/capacity-tool.md). Never target a production service.

## Storage foundation

Startup opens `data/ranking.db` (override `RANKING_SQLITE_PATH`) before accepting
HTTP requests. It creates missing parent directories, prepares the migration
ledger and enables WAL, FULL synchronization and foreign keys. Each declared
migration and its version record commit atomically; already-applied versions
are skipped. Schema version 1 creates `clients` for identity/profile persistence;
version 2 creates the stable canonical-model ID dictionary and startup registers
current catalog names transactionally. Versions 3–4 add a persistent reporting
window and daily snapshot tables; version 5 adds finalized period statistics.
Startup does not seed participants.
Database failures stop startup; they do not
trigger automatic repair or fallback. Shutdown drains HTTP before closing the
bounded write/read connection pools. See [SQLite storage](docs/storage.md) and
[client storage](docs/client-storage.md). Readiness also requires a complete real cache round.

Registration applies [public display-name rules](docs/display-names.md) and the
static avatar catalog; it is not a profile-edit endpoint.
The [static avatar catalog](docs/avatar-catalog.md) validates IDs, defaults and
resources. The binary embeds 66 approved WebP avatars (default ID 1), served at
`/avatars/catalog.json` and `/avatars/images/001.webp` through `066.webp`.
These routes work without a frontend directory.
[Client credential parsing/hashing](docs/client-credentials.md) accepts a
client-persisted 32-byte CSPRNG secret as canonical unpadded base64url Bearer
text; the server stores only its SHA-256 digest. No secrets are issued.
`POST /v1/clients/register` now exposes the [protected registration API](docs/registration-api.md):
201 for a new identity, 200 for same-secret retries returning the current profile
without changes. Use HTTPS and persist a 32-byte CSPRNG secret before the first
attempt. Source/global request gates count failures and retries; new identities
also have rolling quotas (default 3/hour and 10/24 hours). All admission state
is bounded, process-local and resets on restart. `RANKING_REGISTRATION_ENABLED=false`
closes only new creation after restart, not existing retries or authentication.
See the API document for requests, status codes, proxy trust and all deployment
parameters. Internal [authentication middleware](docs/client-credentials.md#internal-identity-enforcement)
selects identity only by credential; no demonstration `/me` API is added.

`PUT /v1/reports/{utc_day}` accepts an authenticated full snapshot for UTC
today or yesterday. Revisions, catalog-independent idempotency, persistent
window closure, limits and atomic replacement are described in the
[daily report API](docs/report-snapshots.md). Closed snapshots are finalized in
bounded background transactions; expired daily and weekly summaries are pruned
without deleting unsettled data. Internal candidate reads combine settled and
pending usage in a consistent SQLite snapshot; daily model-level history is not
retained. See [storage](docs/storage.md).

## Frontend development

Requires Node.js 24.15+ (24.x) and npm. Runtime dependencies are only React and
React DOM. Tailwind runs
at build time. Shared colors, typography and focus styling live in
`web/src/styles/tokens.css`; no external font or icon services are required.
The selected React Bits control uses CSS only and adds no animation runtime.
See [third-party source notices](THIRD_PARTY_NOTICES.md) for its pinned source,
local adaptations and MIT + Commons Clause license.

The interface uses a light, wide layout with coral/violet accents and compact
desktop sizing without page zoom; mobile sizing remains independent. The page reads the real cached views and does not fabricate summaries. Project icons and regeneration details
are documented in [Brand icons](docs/brand-icons.md).

The responsive page offers five boards and today/yesterday/current week/current month, defaulting to today; yearly and all-time data remain maintained by the backend but have no page controls. It has loading, empty, error and retry states and a separate service-status panel. During a switch or retry, any previous result retains its own board, period and cache-generation label. It does not simulate standings or usage totals. Names are rendered as text; avatars use the server catalog and its default, with a neutral fallback if loading fails. The check button calls the real
`GET /healthz` endpoint on demand, validates the JSON contract, times out after
8 seconds, supports retry and cancels requests on unmount. There is no polling.
A successful check does not mark the ranking business as ready.

Decorative motion respects `prefers-reduced-motion` and stops when the control
is offscreen, disabled or the document is hidden. Missing observer APIs fall
back to a static control. Observers/listeners are removed on unmount; the page
has no canvas, WebGL, external fonts or background JavaScript animation loop.

For local development, run `go run ./cmd/ranking` and `npm --prefix web run dev`
in separate terminals. Vite binds to loopback and proxies health, leaderboard and avatar endpoints to
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
make browser-install # optional: install a browser if none can be reused
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
and 404 boundaries, all 66 avatar decodes and real registration/safe retries,
then stops the process. It refuses to reuse an existing
service. Override `RANKING_TEST_PORT` if occupied. Results default to
`web/test/results/`; `PLAYWRIGHT_OUTPUT_DIR` and `PLAYWRIGHT_BROWSERS_PATH` can
relocate artifacts and browser downloads. Browser checks use a separate SQLite
file under a `databases/<port>/` sibling of the results directory, never the
application's default database. A minimal Linux host may first need
Playwright's Chromium system dependencies installed by its administrator.
These smoke checks do not establish 100,000-client capacity.

Playwright has no exact-version requirement (`@playwright/test: "*"`). The npm
lockfile records the resolved packages for clean installs; it is not a runtime
version check. Existing compatible dependencies can be reused without running
`make deps` again. Browser tests do not install browsers automatically or require
a project-specific browser revision. To reuse an existing Chromium executable:

```sh
PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/path/to/chromium make test-browser
```

Alternatively, set `PLAYWRIGHT_CHROMIUM_CHANNEL=chrome` to use an installed
Chrome channel. Set only one of these options. Without either, Playwright uses
its normal browser lookup, including `PLAYWRIGHT_BROWSERS_PATH`. That cache-path
setting alone does not make a different browser revision discoverable: use the
executable-path option for that. Run `make browser-install` only when needed;
compatibility must be verified with the actual installed runner and browser.

## Local frontend serving from source

```sh
npm --prefix web ci
npm --prefix web run build
go build -o bin/ranking ./cmd/ranking
RANKING_WEB_DIR=web/dist ./bin/ranking
```

Go serves the built application and API on the same origin. This is a source
development example; for deployment use the Docker image, which includes the
model catalog, frontend and third-party notices/licenses. `RANKING_WEB_DIR` is
relative to the process working directory unless absolute. If unset, only
backend routes are enabled. If set to an incomplete build, startup fails instead
of silently serving a broken site.

Only `/`, `/index.html`, `/favicon.svg` and actual files below `/assets/` are
served. There is no arbitrary SPA fallback or directory listing. Missing files,
unknown APIs and unknown pages remain 404. Traversal and symlink escapes are
rejected. HTML, non-fingerprinted files and stable files in public asset
subdirectories revalidate; top-level fingerprinted build assets can be cached
for a year. Do not modify assets in place while serving;
stop the process, replace the build, then restart.

## HTTP foundation

- `GET /healthz`: 200 with `{"status":"ok"}` means the process is alive.
- `GET /readyz`: 200 only after database and catalogs are initialized and a
  complete real cache round exists. Before that it returns 503 even when `/healthz` is healthy. Failed
  later refreshes retain the last complete snapshot; no expiry SLA is imposed.
- `POST /v1/clients/register`: protected creation/safe retry; see the
  [registration API](docs/registration-api.md).
- `PUT /v1/reports/{utc_day}`: protected full daily snapshot; see the
  [daily report API](docs/report-snapshots.md).
- `GET /v1/leaderboards/{board}/{period}`: fixed public cache views; see the [leaderboard API and scoring](docs/leaderboards.md).
- `GET`/`HEAD /avatars/catalog.json` and listed `/avatars/images/*` resources.
- Unknown routes and unsupported methods return JSON errors (404/405).
- Panic responses are generic; request bodies, headers and panic values are not logged.
- Forwarded client-IP headers are not trusted by default. See
  [registration sources](docs/registration-sources.md) for explicit proxy trust.

## Configuration

The defaults below apply when running from source; the [Docker image](docs/docker.md#configuration-and-health) overrides four paths/listener settings.

| Variable | Default | Meaning |
| --- | --- | --- |
| `RANKING_HTTP_ADDR` | `127.0.0.1:8080` | Listen address; numeric port 0 selects a free port |
| `RANKING_SHUTDOWN_TIMEOUT` | `10s` | Positive grace period for shutdown |
| `RANKING_CACHE_REFRESH_INTERVAL` | `5m` | Positive internal cache refresh interval |
| `RANKING_WEB_DIR` | unset | Optional built frontend directory; unset means no frontend routes |
| `RANKING_SQLITE_PATH` | `data/ranking.db` | SQLite file path, relative to the working directory unless absolute |
| `RANKING_MODELS_PATH` | `config/models.json` | Model catalog file, relative to the working directory unless absolute |
| `RANKING_TRUSTED_PROXIES` | unset (none) | Comma-separated trusted proxy IPs/CIDRs |
| `RANKING_REGISTRATION_IPV6_64` | `true` | Group native IPv6 registration sources by /64 |

Registration limits, body bound and the creation switch are listed in
[registration API configuration](docs/registration-api.md#startup-configuration);
report limits and writer settings are in the [report API](docs/report-snapshots.md).
Explicitly empty or invalid settings fail validation. The model catalog is
required at startup; environment variables are not loaded from `.env` files.
Do not commit credentials or local `.env` files.
