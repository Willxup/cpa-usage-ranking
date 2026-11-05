# Registration service

`internal/registration.Service` composes the client repository, display-name
rules and the production avatar catalog. It accepts only a validated credential
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

The service is internal only. No registration HTTP route is exposed until
request admission, source quotas and the creation switch are integrated.
Tests in `internal/registration/test/` use real SQLite files, including concurrent
safe retries, database failures, disabled identities and profile changes.
