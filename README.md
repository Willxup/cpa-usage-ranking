# CPA Usage Ranking

[简体中文](README.zh.md)

An independent usage-ranking service. The foundation is under construction;
registration, reporting, storage and leaderboards are not implemented yet.

## Development

Requires Go 1.26.10 or newer.

```sh
go test ./...
```

Tests live in each module's `test/` directory, not beside production files.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `RANKING_HTTP_ADDR` | `127.0.0.1:8080` | Listen address; numeric port 0 selects a free port |
| `RANKING_SHUTDOWN_TIMEOUT` | `10s` | Positive grace period for shutdown |

Explicitly empty or invalid settings fail validation. No configuration files
are loaded automatically. Do not commit credentials or local `.env` files.
