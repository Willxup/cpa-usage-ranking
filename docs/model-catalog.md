# Model catalog

The server loads `config/models.json` once at startup, before opening the
database or listening for HTTP. Set `RANKING_MODELS_PATH` to use another file;
relative paths are resolved from the process working directory. Missing,
invalid or conflicting configuration stops startup. Edits take effect only
after restart; there is no remote fetching, automatic fallback or hot reload.
Deploy the file with the binary even when the frontend is not used.

`currency` must be `USD` and `price_unit` must be `per_million_tokens`.
`models` is an array of records with `model_name`, `alias` (an array, possibly
empty) and `prices`. Names are nonempty valid UTF-8 of at most 128 bytes, have
no leading/trailing whitespace, control characters or paths, and are matched
exactly and case-sensitively. A name cannot belong to different models; aliases
cannot occupy another model's canonical name. Explicit case variants are
allowed when assigned to the intended model.

`prices` maps the supported `default`, `priority`, and `flex` tiers to objects
with `input`, `output`, `cache_read`, and `cache_write`. Each known value is a
non-negative decimal string: ASCII digits, optionally followed by a decimal
point and more digits. Missing or null fields mean unknown; `"0"` means
explicitly free. A missing tier does not inherit the default tier. Values are
kept as strings, without floating-point conversion. Prices are references only:
there is no cost calculation, billing, price history or synchronization.

The shipped catalog and `config/model-sources.json` are copied from an earlier
catalog exercise. The sources file preserves its historical provenance and
claims; **prices were not independently reverified for this delivery** and are
not a statement of current live prices. Actual pricing can depend on date, plan,
region, context length and other conditions; consult providers before relying
on it. Only `models.json` is loaded by the service.
