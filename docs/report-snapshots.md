# Daily report snapshots

The service has internal version 1 report decoding, validation and UTC-window components; a report HTTP endpoint is not yet available.

A report object contains `schema_version` (JSON integer `1`), `revision` (nonnegative int64 decimal string, starting at `0`), and `items` (an array, possibly empty). Each item requires `model`, six token fields (`total_tokens`, `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`, `reasoning_tokens`), `request_count`, `success_count`, `ttft_avg_ms`, `ttft_samples`, `latency_avg_ms`, and `latency_samples`. All counts and token values use decimal strings; averages use JSON numbers or `null`. An omitted `service_tier` means `default`; if present it must be `default`, `priority`, or `flex`, not null or empty.

Every listed field is required even when zero or null, except `service_tier`. Duplicate and unknown JSON object fields, invalid UTF-8, unpaired Unicode surrogates in model names, out-of-range integers, and extra JSON values are rejected. Decimal strings may have leading zeroes; a plus sign is not accepted. All metrics must be nonnegative int64 values. Successes and each sample count cannot exceed requests; cache read plus cache write cannot exceed input, reasoning cannot exceed output, and input plus output cannot exceed the reported total. Total tokens remain the client's value and may include an unclassified remainder. A nonzero usage item must have requests. Averages must be null when their sample count is zero, and otherwise finite and nonnegative; averages are multiplied by their own sample counts with overflow checks. Missing measurements must not be invented as zero. Any invalid item rejects the whole report, including an unknown model.

Raw model names must be valid UTF-8, at most 128 bytes, without control characters, and have a nonempty final path component after trimming. Duplicate raw model plus effective tier pairs are rejected. A stable, typed, order-independent digest includes every item, including valid unknown and zero-execution items, before catalog matching; it does not include catalog or price configuration. Different paths or aliases that resolve to the same model and tier are combined using sums and actual sample counts. Unknown models are skipped from statistics and totals. Zero-execution items produce no rows. Integer and floating-point aggregation overflow rejects a replacement.

For each client and UTC day, the first revision may be zero. A higher revision may skip numbers and replaces the snapshot; an identical revision and digest confirms the existing result without reprocessing; an older revision or the same revision with a different digest conflicts. A replacement atomically commits the revision, digest, received time, changed detail rows, and total together. It can decrease values, delete disappeared rows, or leave an empty snapshot with only its version. Identical retries do not rewrite statistics or received time and do not rematch the current catalog. A higher revision with identical statistics updates only revision metadata. A failed transaction does not acknowledge a replacement.

The reporting window is UTC today and yesterday. Future or invalid dates are rejected; older dates are expired. The database has separate daily revision metadata and 12-value statistics tables; each nonempty matched snapshot has one internal total row, separate from model/tier detail. A persistent `closed_before_day` watermark advances only forward from observed server UTC time, independently of any report transaction. Startup advances it; a report write must advance it separately before its transaction and recheck time and watermark inside the transaction, including before commit. Client disabled state is also rechecked in that transaction.

The internal writer serializes one report at a time with a default 128-entry pending queue and a five-second transaction timeout. Full or closed queues reject new submissions. A canceled request is not acknowledged early; if its response is lost around commit, retry the same revision. Shutdown stops admission, drains accepted work within its deadline, then cancels unfinished work and waits for rollback before closing the database.

Report admission is separate from registration. Configuration is read at startup; values must be positive. These conservative starting defaults are deployment parameters, not a validated 100,000-client capacity promise:

| Environment variable | Default | Meaning |
| --- | --- | --- |
| `RANKING_REPORT_MAX_BODY_BYTES` | 131072 | Maximum ordinary JSON body bytes |
| `RANKING_REPORT_MAX_ITEMS` | 128 | Maximum raw items before matching or merging |
| `RANKING_REPORT_DECODE_CONCURRENCY` | 16 | Concurrent body reads and decodes |
| `RANKING_REPORT_QUEUE_CAPACITY` | 128 | Pending report writes |
| `RANKING_REPORT_TRANSACTION_TIMEOUT` | `5s` | Per-report writer execution deadline |
| `RANKING_REPORT_CLIENT_INTERVAL` | `1m` | One token per interval per authenticated client |
| `RANKING_REPORT_CLIENT_BURST` | 2 | Per-client token bucket capacity |
| `RANKING_REPORT_GLOBAL_INTERVAL` | `5ms` | One global token per interval |
| `RANKING_REPORT_GLOBAL_BURST` | 400 | Global token bucket capacity |
| `RANKING_REPORT_MAX_CLIENTS` | 100000 | Maximum tracked client rate states |
| `RANKING_REPORT_STATE_TTL` | `10m` | Idle time before a fully refilled state may expire |

Rate state is bounded in memory and resets on restart. Capacity exhaustion rejects new clients instead of evicting active state. No request decompression is provided. Rejected reports, failed transactions, retries, clock rollback and restart cannot reopen an already recorded boundary.
