# Registration request admission

Registration has its own source and global request budgets. These are distinct
from new-identity creation quotas: **every admitted attempt** is counted before
credential or body validation, including failed requests and safe retries, while
requests already rejected by per-source or source-capacity limits do not consume
remaining global slots. A globally exhausted gate stops immediately. This does
not apply registration budgets to health, readiness, frontend or avatar routes.

The internal `registration.RequestLimiter` and Gin `RegistrationAdmission`
middleware use fixed windows, starting at the first request for a source (and
independently for the global window), not calendar-minute buckets. Configuration
is read at startup; all values must be positive:

| Variable | Default | Meaning |
| --- | --- | --- |
| `RANKING_REGISTRATION_SOURCE_REQUESTS` | `20` | Attempts per source per window |
| `RANKING_REGISTRATION_GLOBAL_REQUESTS` | `200` | Attempts across all sources per window |
| `RANKING_REGISTRATION_REQUEST_WINDOW` | `1m` | Fixed request-window duration |
| `RANKING_REGISTRATION_REQUEST_MAX_SOURCES` | `10000` | Maximum live source records |

Source resolution follows [trusted-proxy rules](registration-sources.md).
A mutex arbitrates concurrent requests. Source records expire at the end of
their window and are reclaimed on admission; repeated attempts cannot extend a
window indefinitely. The expiration list and map each hold at most the configured
number of sources. When full, new sources are rejected instead of evicting
active sources. Existing sources retain their counters. No permanent IP history,
Redis, distributed locks or background scheduler is used.

Rejections are JSON 429 with `registration_rate_limited` or
`registration_capacity`. `Retry-After` is a positive integer number of seconds,
rounded up to the relevant window expiry (or earliest capacity expiry).
Clients should wait and add retry jitter; passing a rate gate is not a guarantee
of passing later validation/quota gates. Fixed windows may allow bursts across
adjacent windows; these defaults are deployment controls, not a rolling request
rate promise.

State is local to one process and resets on restart; it is not shared across
instances and does not prove one-human-one-identity. The gates are attached only
to the protected [registration POST endpoint](registration-api.md), before Bearer
and bounded JSON validation. Health and static resources are unaffected.
