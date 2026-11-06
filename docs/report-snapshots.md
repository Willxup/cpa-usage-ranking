# Daily report snapshots

The service has an internal version 1 report decoder; a report HTTP endpoint is not yet available.

A report object contains `schema_version` (JSON integer `1`), `revision` (nonnegative int64 decimal string, starting at `0`), and `items` (an array, possibly empty). Each item requires `model`, six token fields (`total_tokens`, `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`, `reasoning_tokens`), `request_count`, `success_count`, `ttft_avg_ms`, `ttft_samples`, `latency_avg_ms`, and `latency_samples`. All counts and token values use decimal strings; averages use JSON numbers or `null`. An omitted `service_tier` means `default`; if present it must be `default`, `priority`, or `flex`, not null or empty.

Every listed field is required even when zero or null, except `service_tier`. Duplicate and unknown JSON object fields, invalid UTF-8, unpaired Unicode surrogates in model names, out-of-range integers, and extra JSON values are rejected. Decimal strings may have leading zeroes; a plus sign is not accepted. Metric consistency checks are a separate validation step.
