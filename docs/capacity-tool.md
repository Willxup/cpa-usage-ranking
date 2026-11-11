# Synthetic capacity test tool

`ranking-capacity` is an opt-in development tool, not part of the deployed
service. Run it only against an isolated test service and synthetic database:
it sends real authenticated writes and can overload its target. The tool does
not enforce host resource budgets or protect other services; arrange external
limits, monitoring, a stop mechanism and sufficient disk space first. Treat
results as specific to the fixture, load schedule and measured environment, not
as an unconditional client-capacity guarantee.

## Fixture and load

Build with the same Go/CGO requirements as the service. Choose a private,
new test directory and run from the repository root:

```sh
install -d -m 700 /private/test
go build -o /private/test/ranking-capacity ./cmd/ranking-capacity
/private/test/ranking-capacity seed \
  --db /private/test/seed.sqlite --fixture /private/test/fixture.json \
  --models config/models.json --clients 6000 --history-days 90 --backlog 1000
```

Both output files must not already exist; a failed seed may leave partial files.
The manifest contains a random seed that derives **synthetic bearer secrets**:
keep it private, do not log it or commit it, and do not reuse it with real
identities. Fixture and SQLite files are created mode 0600, subject to umask.
`--day` defaults to the current UTC date; a running service accepts only its
current reporting window. The fixture prepopulates identities, today/yesterday
snapshots and synthetic historical period aggregates; historical prefill is
not evidence of actual settlement. `--backlog` retains the newest closed day
as pending source data for that many identities so the real service can
finalize it. Eight catalog models and default tiers are used.

Start a separate test service against a **copy** of the closed seed database,
with its own data directory, origin and resource controls. After readiness and
any separately measured initialization, for example:

```sh
/private/test/ranking-capacity load \
  --fixture /private/test/fixture.json --url http://127.0.0.1:4210 \
  --rps 10 --duration 10m --revision 2 --changed 2 \
  --ledger /private/test/steady.jsonl
```

The bounded open-loop schedule distributes requests evenly across identities;
it does not simulate jitter or automatic client retry. `--changed` selects
1–8 changed model rows per full snapshot. `--both-days` alternates today and
yesterday for each identity, and `--offset` selects a zero-based starting
identity index. Revisions increase automatically after a complete rotation:
for every identity/day, a later phase must use a revision greater than **all**
its previously attempted revisions, including rejected attempts. Every phase
requires a new ledger; keep all of them. Summary JSON reports scheduled, sent,
confirmed, generator drops, statuses and successful-request latencies. Status
0 denotes a transport failure; an incorrect 200 body is not confirmation.

## Offline checks

Stop the test service and preserve a consistent SQLite database, including any
required WAL sidecars. Supply **every** ledger from that database, comma-separated:

```sh
/private/test/ranking-capacity inspect --db /private/test/running.sqlite
/private/test/ranking-capacity verify --fixture /private/test/fixture.json \
  --db /private/test/running.sqlite --ledgers /private/test/steady.jsonl
/private/test/ranking-capacity probe --db /private/test/running.sqlite \
  --day "$(date -u +%F)"
```

`verify` checks each touched identity/day's latest version, digest and every
model/total metric against the attempted canonical snapshot. It rejects reused
identity/day revisions and a latest commit with a 4xx response. An unconfirmed
5xx, transport failure or incorrect success body can be an ambiguous commit;
if its revision is the final committed version, `unconfirmed_commits` counts
it separately rather than treating it as confirmed. A rejected
revision later superseded by another commit cannot be proven unchanged from
final state: `rejections_superseded` reports this limitation, while
`rejections_checked` counts rejections with an older final version. Keep
rejection and subsequent recovery identities disjoint when that proof matters.

`inspect` reports raw counts and totals. `probe` separately times real
statistics-reader calls and sums their returned metrics; it does **not** time a
complete background cache refresh. Its timing depends on the machine, resource
limits and cache state, so measure and disclose those separately. Full rotation,
cache refreshes, rejected load, maintenance and consistency need independent
coverage; a short probe alone cannot establish stable capacity.
