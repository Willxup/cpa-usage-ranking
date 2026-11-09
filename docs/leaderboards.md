# Leaderboard calculations

The cache builds five views from all eligible clients: overall, token usage,
cache reuse, conservative success score, and measured mean time to first token
(TTFT). Eligibility requires an enabled client and at least one accepted
execution in the selected UTC period. There is no minimum sample count or success
rate. Only all-model totals are used; totals and model details are not added.

The six fixed periods are today, yesterday, current Monday-start week, current
month, current calendar year, and all time. Period ends are exclusive. Yearly
raw totals combine finalized monthly sums and all unsettled daily sums of that
calendar year in one SQLite read snapshot; there is no year table. Cumulative
statistics remain persisted and maintained. Profiles, disabled state and raw
statistics are read in the same snapshot before scoring; disabled clients are
excluded from all references. Daily and weekly retention is unchanged.

Overall has at most 100 points: usage 40, success 30, cache 20, TTFT 10.
Positive token totals use their mid-percentile among positive totals (equal
values share a percentile); zero earns no usage points. Success uses the lower
bound of the two-sided 95% Wilson interval, not the observed success
percentage. Cache is cache-read tokens divided by input tokens. Missing input
cannot produce a cache rate; actual zero cache reads are valid. TTFT response
performance is `1/(1 + mean / median_positive_mean)` multiplied by the
fraction of requests with TTFT samples. If all measured means are zero, the
performance factor is 0.5; no samples add no TTFT points. Each weighted
contribution is rounded half-up to 0.01 points before summation; missing terms
are not redistributed.

Usage requires positive tokens, cache requires positive input, and TTFT requires
at least one sample; other eligible clients can still enter the overall and
success views. Usage is sorted by exact int64 totals. Overall is sorted by its
integer hundredths of points. The cache percentage, conservative success score
and TTFT mean are rounded half-up to two decimals before both sorting and
display; equal displayed values tie. Higher is better except TTFT, where lower
is better. Equal values receive competition ranks (1, 1, 3); stable client ID
orders tied rows without affecting rank. Each view is truncated to 100 only
after all candidates and references are considered, without expanding ties.
TTFT can be based on very few samples and is not a controlled performance
certification. These self-reported results are not proof of authenticity or SLA.

A single serial cache worker publishes all six period items together. An empty
view is a valid result; an empty build-item list is not. A failed build keeps
the previous complete snapshot, including its actual period and generation time.
The candidate and encoded-round limits are safeguards, not capacity claims.
