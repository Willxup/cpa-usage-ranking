# Registration service

`internal/registration.Service` composes the client repository, display-name
rules, production avatar catalog and source creation quotas. It accepts only a validated credential
digest (see [credential encoding](client-credentials.md)), not a caller-supplied
client ID, IP-based claim or nickname lookup.

- A digest already present returns the **same client ID and current profile**,
  without modifying data. New-profile validation is not required for this retry:
  changed, omitted or invalid new-profile fields cannot prevent recovery of an
  active existing identity.
- Disabled identities are explicitly rejected and never re-enabled by retries.
- Only new identities validate/normalize the display name and avatar. An omitted
  avatar uses the catalog default; explicitly unknown/zero/negative IDs fail
  instead of silently falling back.
- A single SQLite INSERT saves the identity and profile atomically. The unique
  digest constraint arbitrates concurrent retries; a duplicate reads back the
  original identity, including its disabled status. Failed writes/cancellation
  do not produce partial profiles. No extra table or schema migration is needed.
- Equal names/avatars never merge identities; a different secret creates a
  different identity, not an ownership claim over an existing profile.

`Register` returns `{Client, Created}` internally; `Client` contains no credential
or digest. The client must save its original secret before the first attempt
and retain it for future requests. The service does not retrieve lost secrets,
overwrite profiles through registration or issue replacement credentials.

## Successful-creation quotas

One service instance per process serializes new-identity admission, the atomic
INSERT and successful-creation accounting under one mutex. No write transaction
borrows its own single-connection write pool. Source keys come from the trusted
HTTP source resolver, never from profile/body fields. Safe retries (including
from another source) are checked **before** quotas and source-state capacity.

| Variable | Default | Meaning |
| --- | --- | --- |
| `RANKING_REGISTRATION_HOURLY_CREATIONS` | `3` | Successful new identities per source in rolling 1 hour |
| `RANKING_REGISTRATION_DAILY_CREATIONS` | `10` | Successful new identities per source in rolling 24 hours |
| `RANKING_REGISTRATION_CREATION_MAX_SOURCES` | `10000` | Maximum sources with live creation history |

All counts must be positive. Windows use elapsed instants, not local calendar
days or hours. Only successful new INSERTs append timestamps. Invalid profiles,
failed/cancelled writes, digest conflicts and idempotent retries spend no
creation quota. Concurrent different credentials cannot exceed a source's limit.
Exhausted quotas return a temporary `creation_quota_exceeded` limit error whose
Retry-After covers both rolling windows.

Each source retains at most the configured daily count of successful timestamps.
Expired events are discarded when that source is checked; when source capacity
is needed, wholly expired sources are reclaimed. Capacity exhaustion rejects
new sources with `registration_capacity` and the earliest expiry, rather than
evicting active history. Existing-identity retries remain available.

This is **single-process, in-memory** accounting: restart clears it, separate
instances do not share it, and it is not a promise of one human per identity.
Request limits are independent and still count failed attempts and safe retries.
The service is internal only; no registration HTTP route is exposed until all
safeguards and the creation switch are connected.
Tests in `internal/registration/test/` use real SQLite files, including concurrent
safe retries, database failures, disabled identities and profile changes.
