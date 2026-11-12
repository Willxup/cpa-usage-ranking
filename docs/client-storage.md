# Client identity and profile storage

The `internal/clients` repository persists client identities and profiles in the
`clients` table introduced by schema version 1. Startup applies the complete
migration catalog; it does not create sample clients. Registration,
authentication and readiness checks are separate from this storage layer.

## Stored fields

| Column | Meaning |
| --- | --- |
| `id` | SQLite-generated positive 64-bit integer; the single `client_id` identity |
| `token_hash` | Unique 32-byte BLOB containing the caller-supplied SHA-256 credential digest, never the raw credential |
| `display_name` | Current display name; duplicates are allowed |
| `avatar_id` | Positive integer catalog reference, represented as `int64`; not restricted to 66 or 255 |
| `disabled` | 0 (initial state) or 1; identity remains stored while disabled |
| `created_at_ms` | UTC Unix milliseconds, set by the creation operation and unchanged by profile/status updates |

`id` uses `INTEGER PRIMARY KEY AUTOINCREMENT` so deleting the highest row does
not reassign that historical identity to a new client. SQLite maintains its
internal `sqlite_sequence` table for this purpose. There is no separate user
account ID, avatar table or identity mapping based on names, avatars or IPs.

The database enforces credential uniqueness and shape, positive avatar IDs,
and the two-valued disabled flag. Name normalization/validation and catalog
membership checking are **not implemented here**; callers must supply validated
profile values, as the [registration service](registration.md) does. The repository neither
normalizes names nor guesses an avatar default. The separate
[display-name helper](display-names.md) provides name normalization/validation;
the [avatar catalog loader](avatar-catalog.md) provides ID membership lookup.

## Repository operations

Create a repository with `clients.NewRepository(db)` after
`storage.MigrateSchema` succeeds. The application owns the database lifetime;
the repository uses its bounded write pool and read-only pool directly.

| Operation | Behavior |
| --- | --- |
| `Create(ctx, NewClient)` | Atomically inserts one client with a newly allocated ID; returns its metadata only after the INSERT succeeds |
| `ByID(ctx, id)` | Reads one client's metadata |
| `ByCredentialHash(ctx, hash)` | Reads the matching client, **including its disabled status**; disabled is not treated as missing |
| `UpdateProfile(ctx, id, name, avatarID)` | Changes only name and avatar together, leaving identity, credential, creation time and status intact |
| `SetDisabled(ctx, id, disabled)` | Explicit internal status change, not an authorization or administrative endpoint |

`CredentialHash` is a fixed 32-byte value. The module does not accept credential
plaintext or implement hashing/verification. Returned `Client` values omit the
hash; they are internal metadata, **not** the public API response contract.

A duplicate digest returns `clients.ErrCredentialExists`, without overwriting
or re-enabling the existing client. The repository does not hide registration
retry logic inside an upsert. Missing lookups/updates return
`clients.ErrNotFound`; database and context errors remain distinguishable via
`errors.Is`/`errors.As`. Repeating a profile/status update with the same values
still succeeds for an existing client.

There is no deletion, credential rotation, credential recovery, registration
quota or permission-checking API in this module. In particular, the existence
of `ByCredentialHash` is not an authentication service: an HTTP handler must
not assume that any returned client is authorized regardless of status.

## Verification

Tests live in `internal/clients/test/` and `internal/storage/test/`, using real
SQLite files. They cover independent same-name identities, credential conflicts
(including concurrent writes through separate pools), profile/status isolation,
large avatar IDs, persistence across reopening and migration replay, missing
records, cancellation and database constraints. The separate
[protected registration API](registration-api.md) has real HTTP and SQLite
tests; storage tests do not mock a registration service.
