# Market Data Hub

Daily EOD market-data warehouse for personal trading tools and AI agents. It is a batch data layer, not a real-time quote service.

## What It Builds

- Go CLI for daily sync, history rebuild, and file validation.
- Cloudflare R2 object layout for daily, latest, symbol, history, and run files.
- Cloudflare D1 schema for symbols, latest quotes, and ingestion logs.
- Cloudflare Worker API for quote/history/daily/universe/context queries.
- GitHub Actions schedule for weekday US market EOD sync.

## Local Usage

Run a small local sync with the configured watchlist:

```bash
go run ./cmd/sync-daily --storage local --root data --market us --date 2026-05-31 --limit 5
```

Validate a generated file:

```bash
go run ./cmd/validate-data --file data/daily/us/2026/2026-05-31.json.gz
```

Rebuild history and latest from local daily files:

```bash
go run ./cmd/rebuild-history --storage local --root data --market us --from 2026-05-01 --to 2026-05-31
```

Backfill historical data from Stooq into daily, history, latest, and D1 latest indexes:

```bash
go run ./cmd/backfill-history --storage r2 --market us --watchlist config/watchlist.yaml --from 2024-01-01 --to 2024-12-31
```

Use explicit symbols instead of the watchlist:

```bash
go run ./cmd/backfill-history --storage r2 --market us --symbols NVDA,AAPL,MSFT --from 2024-01-01 --to 2024-12-31
```

By default, backfill merges fetched records into existing objects. Add `--replace` to replace target symbol records inside the requested date range while preserving other symbols and dates outside the range.

## Cloudflare Setup

Required sync environment variables:

- `STOOQ_API_KEY` when Stooq requires authenticated CSV downloads.
- `R2_BUCKET`
- `CLOUDFLARE_ACCOUNT_ID`
- `CLOUDFLARE_API_TOKEN`
- `D1_DATABASE_ID`

Apply the D1 migration in `worker/migrations/0001_initial.sql`, then set `worker/wrangler.toml` bindings for the actual R2 bucket and D1 database id.

GitHub Actions:

- `Sync Market Data` runs daily and supports manual one-day sync.
- `Backfill Market History` is manual only and supports `from`, `to`, `symbols`, `all`, and `replace` inputs.

## Worker

```bash
cd worker
npm install
npm run typecheck
npm run deploy
```

Routes:

- `GET /quote/:symbol`
- `GET /history/:symbol?range=1y`
- `GET /daily/us/:date`
- `GET /universe/us`
- `GET /context/:symbol`

## Boundaries

This MVP does not implement real-time quotes, intraday bars, options chains, earnings data, automatic trading signals, or multi-source arbitration. R2 is the source of truth; D1 stores metadata and latest indexes only.
