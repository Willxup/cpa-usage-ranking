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

## Synchronized clients and recovery

`burst` is a separate bounded mode; it does not change `load`. For a fresh
fixture and test-service database, for example:

```sh
/private/test/ranking-capacity burst \
  --fixture /private/test/fixture.json --url http://127.0.0.1:4210 \
  --duration 2m --cycle 10m --changed 2 \
  --ledger /private/test/attempts.jsonl \
  --events /private/test/tasks.jsonl --samples /private/test/samples.jsonl
```

It prepares one client per synthetic identity and releases today's first
request at a barrier. Every tenth client has a yesterday task queued behind
its own confirmed today task. Each identity processes all dates serially.
An independent clock continues to create one today update per identity per
cycle, plus one yesterday update per cycle for every tenth identity. Thus the
planned background rate is approximately `1.1 * clients / cycle_seconds`;
slow responses do not pause that clock. Initial yesterday tasks are counted as
pending even while waiting for today's confirmation. K remains eight full
models; the normal `--changed 2` changes two model rows.

Each retry retains its logical task, revision and body. Transport errors,
408, 429 and 5xx can be retried; retry delay observes `Retry-After` and adds
bounded identity-specific jitter. Defaults are `--retry-spread 1m`,
`--max-retry-delay 2m`, `--max-attempts 30`, and `--request-timeout 20s`.
Other 4xx, a bad success acknowledgement, exhausted retry bounds, a full
per-client queue, the total pending limit or evidence-write failure stop the
run explicitly; they do not silently discard work. The defaults are
`--queue-per-client 16` and `--max-pending 4*clients`; this mode permits up
to 20,000 clients and one million planned tasks. Every output path must be
new, and partial files can remain after an error.

The attempt ledger records task ID, attempt number, status, Retry-After,
planned/start/connection acquisition/local request-write/completion timestamps
and confirmation; it never records bearer secrets or bodies. The task event
file records each created task even if it is never sent. The `release` event
supplies the initial tasks' common planned time (their created events use
zero); only a valid matching revision is confirmed. One-second samples include
produced, confirmed, pending, retries, statuses, connection waiting and the
number/peak of **written but unfinished** attempts. For a normal duration
expiry, `load_end` is captured before canceling outstanding requests;
external cancellation or a fatal error can race with that sample. The
`after_cancel` sample includes stopping effects. The summary reports a full
first-attempt write span only if every initial client wrote; otherwise that
field is null, and `observed_initial_write_span_ms` covers only the subset.
The delay from release likewise covers the last observed write. A late trace
callback makes the measurement incomplete. The `WroteRequest` timestamp is a write by the local
HTTP client to its connection, not proof of receipt or execution at a remote
server behind a tunnel; connection waiting is distinguishable from it.
Neither client count nor goroutine count is an in-flight measurement.

At the duration or an external cancellation the generator stops producing and
cancels outstanding requests; it does not silently drain the queue after load.
Reconcile created task events against confirmed attempts and retain all
unconfirmed tasks. A separate observer is necessary for server-side settlement
backlog and resource/cache measurements. Ensure the load generator and its
network path can actually produce the desired wave before interpreting a
capacity result.

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
model/total metric against the attempted canonical snapshot. It accepts repeated
versions only when attempts have the same task ID and content and consecutive,
unique attempt numbers; legacy receipts without a task ID must not reuse a
version. A final revision with only explicit 4xx attempts is an error. If a
rejection and a confirmed or ambiguous attempt share that version,
`rejections_mixed` counts the rejection without claiming whether it wrote.
An unconfirmed 5xx, transport failure or incorrect success body can be an
ambiguous commit; if its revision is the final committed version,
`unconfirmed_commits` counts it separately rather than treating it as
confirmed. A rejected revision later superseded by another commit cannot be
proven unchanged from final state: `rejections_superseded` reports this
limitation, while `rejections_checked` counts rejections with an older final
version. Keep rejection and subsequent recovery identities disjoint when that
proof matters.

`inspect` reports raw counts and totals. `probe` separately times real
statistics-reader calls and sums their returned metrics; it does **not** time a
complete background cache refresh. Its timing depends on the machine, resource
limits and cache state, so measure and disclose those separately. Full rotation,
cache refreshes, rejected load, maintenance and consistency need independent
coverage; a short probe alone cannot establish stable capacity.
