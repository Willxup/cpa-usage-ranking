# Registration HTTP API

## Transport and credential ownership

Expose the service through **HTTPS**. The backend's default loopback HTTP
listener is for local testing or a private TLS-terminating reverse proxy, not
public cleartext credential transport. Restrict direct access to that listener.
Exclude Authorization, bodies and secret-bearing URLs from proxy/access/error
logs. The service's request errors are generic and do not log these values.

Before its first attempt, the client generates **32 CSPRNG bytes**, encodes them
as canonical **unpadded base64url** (43 ASCII characters), and durably saves the
secret locally. Send exactly one `Authorization: Bearer <secret>` header; the
scheme is case-insensitive with one separating space. The server hashes the
**decoded 32 bytes** with SHA-256 and stores only the unique 32-byte digest.
It does not issue, return, recover or rotate credentials. Format validation
cannot prove the client's entropy source. See [credential rules](client-credentials.md).

## Request

```http
POST /v1/clients/register
Authorization: Bearer <locally-persisted-secret>
Content-Type: application/json

{"display_name":"  Ａlice 🙂  ","avatar_id":66}
```

- A single UTF-8 JSON object; no trailing JSON or unknown fields. There are only
  `display_name` (string; omission/null is invalid for a new identity) and
  optional `avatar_id` (int64 JSON integer).
- The body is bounded to 4096 bytes by default, including unknown/chunked length
  requests. No decompression is supported. Send `application/json` (charset
  parameters are allowed), without Content-Encoding or with `identity`.
- A **new** identity requires a [valid public display name](display-names.md).
  The server applies NFKC and trims outer whitespace, then checks 1–32 Unicode
  code points, at most 128 bytes, visible content and disallowed controls.
- For a **new** identity, absent `avatar_id` uses the catalog default (currently
  1). An explicit zero, negative, unknown ID or `null` is invalid (422), never
  defaulted. Read [the catalog](avatar-catalog.md) at `/avatars/catalog.json`;
  resolve relative image paths against `/avatars/`. IDs have no 66/255 limit.
- `client_id`, source/IP, credential/body tokens and nickname-based ownership
  claims are not request fields. Unknown fields are rejected, not used to select
  an identity. Query parameters are not registration inputs.

## Response and safe retries

New identity: **201 Created**. Active existing credential: **200 OK**. Both return
only these public fields, with no credential, digest, disabled flag or timestamp:

```json
{"client_id":1,"display_name":"Alice 🙂","avatar_id":66}
```

`client_id` is the service's unique positive signed-64-bit identity, not a user
name or source address. Preserve it without numeric truncation in client code.
The server returns the normalized name and selected avatar; use those values.
Equal names and avatars do not merge identities.

Retry with the **same saved secret** after a lost response/client ID. The
server returns the same identity and **current stored profile** without
modification; profile parameters need not match the first request. For example,
`{}` or a syntactically/type-correct object with an invalid new name/avatar still
recovers an active existing identity. JSON syntax/types, body/transport bounds
and request limits apply to all attempts; semantic profile validation applies
only to new identities. A changed secret is a different identity, not recovery.
Disabled identities return 403 and cannot be re-enabled through registration.
No lost-secret recovery is provided.

SQLite digest uniqueness protects concurrent retries. Only a successful atomic
new INSERT spends creation quota; failures and retries do not. A request timeout
can hide a committed success: keep the secret and retry, never auto-register
using a new secret. Registration never edits an existing profile.

## Admission and shutdown of new creation

After successful source resolution, every registration POST passes the
source/global request gate **before** credential and body validation, including
malformed inputs and idempotent retries. Source resolution failure returns
400 `invalid_source` without consuming request quota. Sources default
to canonical RemoteAddr IPs, with IPv4-mapped IPv6 unmapped and native IPv6 /64
grouping enabled. Forwarded headers are ignored unless the peer is explicitly
trusted; see [proxy deployment](registration-sources.md).

New identities have rolling source quotas: default **3 in 1 hour**, **10 in
24 hours**. The service serializes admission through insertion/accounting so
concurrent different secrets cannot exceed a quota. Active existing retries
are resolved before creation quotas and creation-state capacity checks. They
remain subject to request budgets and request-state capacity.

`RANKING_REGISTRATION_ENABLED=false` (restart required) closes only **new**
identities with 503 `registration_closed`. Existing active retries still return
200; disabled identities remain 403. Credential authentication, health, avatars
and the frontend remain independent. This is not whole-site maintenance mode.
There is no hot-reload/configuration UI. Existing clients can still use the [daily report API](report-snapshots.md) while new registration is closed.

Both request and creation state are bounded, **single-process, in-memory** and
reset on restart; they are not shared across processes. Expired records are
reclaimed on demand. Full state rejects new sources, not active-history eviction.
Source quotas do not prove one-human-one-identity; shared exits may be affected
and proxy pools can evade source controls. No Redis, OTP, CAPTCHA or account
system is included. See [request admission](registration-admission.md) and
[creation accounting](registration.md).

## Error contract

```json
{"error":{"code":"invalid_avatar","message":"Invalid avatar selection"}}
```

Errors do not echo request values or internal database failures.

| Status | Code | Meaning |
| --- | --- | --- |
| 400 | `invalid_request` | Invalid JSON, UTF-8, field types, unknown fields or trailing data |
| 400 | `invalid_source` | No valid source address could be resolved |
| 401 | `invalid_credential` | Missing, duplicate or malformed Bearer credential; includes `WWW-Authenticate: Bearer` |
| 403 | `client_disabled` | Credential belongs to a disabled identity |
| 413 | `request_too_large` | Body exceeds the configured bound |
| 415 | `unsupported_media_type` | Not uncompressed application/json |
| 422 | `invalid_display_name` | New-identity name fails public-name rules |
| 422 | `invalid_avatar` | New-identity avatar selection is not in the catalog (including null/zero) |
| 429 | `registration_rate_limited` | Source or global request window exhausted |
| 429 | `creation_quota_exceeded` | Successful-creation rolling quota exhausted |
| 429 | `registration_capacity` | Request or creation source-state capacity full |
| 503 | `registration_closed` | New-identity creation is disabled |
| 500 | `internal_error` | Storage/internal failure, no successful acknowledgment |

429 includes integer **Retry-After seconds**, rounded up to the relevant window
or earliest capacity expiry, minimum 1. Wait and add jitter; a later attempt
must still pass every gate. Responses are `Cache-Control: no-store`. Validation
order means rate limiting may take precedence over other errors. Unsupported
methods retain JSON 405; unknown routes retain JSON 404.

## Startup configuration

All numeric counts/bounds and durations must be positive. Explicit empty or
invalid values fail startup. No configuration file is loaded automatically.

| Variable | Default |
| --- | --- |
| `RANKING_REGISTRATION_ENABLED` | `true` |
| `RANKING_REGISTRATION_MAX_BODY_BYTES` | `4096` |
| `RANKING_REGISTRATION_SOURCE_REQUESTS` | `20` |
| `RANKING_REGISTRATION_GLOBAL_REQUESTS` | `200` |
| `RANKING_REGISTRATION_REQUEST_WINDOW` | `1m` (fixed window from first attempt) |
| `RANKING_REGISTRATION_REQUEST_MAX_SOURCES` | `10000` |
| `RANKING_REGISTRATION_HOURLY_CREATIONS` | `3` (rolling 1 hour) |
| `RANKING_REGISTRATION_DAILY_CREATIONS` | `10` (rolling 24 hours) |
| `RANKING_REGISTRATION_CREATION_MAX_SOURCES` | `10000` |
| `RANKING_REGISTRATION_IPV6_64` | `true` |
| `RANKING_TRUSTED_PROXIES` | unset, trust none; otherwise comma-separated proxy IPs/CIDRs |

Tune for shared exits and measured deployment load. This API does not establish
100,000-client capacity. `/healthz` is process liveness; `/readyz` requires the database, catalogs and a complete real leaderboard cache round. Authenticated daily reporting is available; see the [daily report API](report-snapshots.md). There is no login/registration webpage, production `/me`, profile edit or administration API.
