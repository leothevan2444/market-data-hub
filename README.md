# Market Data Hub

[English](README.md) | [中文](README.zh-CN.md)

Daily EOD market-data warehouse for personal trading tools and AI agents. It is a batch data layer, not a real-time quote service.

## What It Builds

- Go CLI for daily sync, history rebuild, and file validation.
- Cloudflare R2 object layout for daily, latest, symbol, history, and run files.
- Cloudflare D1 schema for symbols, latest quotes, and ingestion logs.
- Cloudflare Worker API for quote/history/daily/universe/context queries.
- VPS/systemd deployment scripts for weekday US market EOD sync.

## Local Usage

Run a local sync. By default, daily sync targets all active symbols from the Nasdaq Trader universe:

```bash
go run ./cmd/sync-daily --storage local --root data --market us --date 2026-05-31
```

Restrict daily sync to a watchlist by passing `--watchlist`:

```bash
go run ./cmd/sync-daily --storage local --root data --market us --date 2026-05-31 --watchlist config/watchlist.yaml
```

Daily sync updates per-symbol history objects with 16 parallel R2/object-store workers by default. Tune this with `--r2-concurrency` when upload throughput or rate pressure needs adjustment:

```bash
go run ./cmd/sync-daily --storage r2 --market us --date 2026-05-31 --r2-concurrency 32
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
go run ./cmd/backfill-history --storage r2 --market us
```

Restrict backfill to a watchlist:

```bash
go run ./cmd/backfill-history --storage r2 --market us --watchlist config/watchlist.yaml
```

Limit the write range with optional `--from` and `--to` filters:

```bash
go run ./cmd/backfill-history \
  --storage r2 \
  --market us \
  --from 2010-01-01 \
  --to 2026-05-29
```

For large runs, backfill downloads the Stooq US daily archive once, then filters it locally. If Stooq requires human verification, download the archive manually and pass it to the command:

```bash
go run ./cmd/backfill-history \
  --storage r2 \
  --market us \
  --stooq-archive-file /path/to/d_us_txt.zip
```

By default, backfill targets all active symbols from the Nasdaq Trader universe and all dated records in the archive. Pass `--watchlist` to restrict symbols, and pass `--from` and/or `--to` to restrict dates.

By default, backfill merges fetched records into existing objects. Add `--replace` to replace target symbol records inside the requested date range while preserving other symbols and dates outside the range.

## Data Layout

Local storage and R2 use the same object keys. Local mode writes them under `data/`; R2 mode writes the same paths into the configured bucket.

```text
data/
  symbols/us/latest.json
  symbols/us/YYYY/YYYY-MM-DD.json
  daily/us/YYYY/YYYY-MM-DD.json.gz
  history/us/SYMBOL/daily.json.gz
  latest/us/latest.json
  latest/us/latest.min.json
  runs/YYYY/YYYY-MM-DD.json
  runs/YYYY/us-backfill-FROM-TO-HHMMSS.json
```

- `symbols/us/latest.json`: latest US symbol universe from Nasdaq Trader.
- `symbols/us/YYYY/YYYY-MM-DD.json`: dated snapshot of the symbol universe used by a run.
- `daily/us/YYYY/YYYY-MM-DD.json.gz`: one market-wide daily EOD file for a trading date.
- `history/us/SYMBOL/daily.json.gz`: one per-symbol daily history file, sorted by date.
- `latest/us/latest.json`: full latest quote index keyed by symbol.
- `latest/us/latest.min.json`: compact latest quote index for lightweight clients.
- `runs/YYYY/*.json`: ingestion logs for daily syncs and backfills.

`daily` files contain a `DailyMarketFile`:

```json
{
  "market": "US",
  "date": "2026-06-01",
  "type": "eod",
  "source": ["massive"],
  "count": 1,
  "schemaVersion": 1,
  "records": [
    {
      "symbol": "NVDA",
      "date": "2026-06-01",
      "open": 100.5,
      "high": 103.2,
      "low": 99.8,
      "close": 102.4,
      "adjClose": 102.4,
      "volume": 123456789,
      "source": "massive",
      "updatedAt": "2026-06-02T01:00:00Z"
    }
  ]
}
```

`latest/us/latest.json` stores derived quote fields:

- `price`, `change`, `changePct`, `volume`
- `open`, `high`, `low`, `prevClose`
- `date`, `source`, `stale`, `flags`

`latest/us/latest.min.json` stores the same latest index in a compact numeric format with a `schema` array describing each quote vector.

D1 is a derived query index, not the source of truth. It stores:

- `symbols`: searchable symbol metadata.
- `latest_quotes`: latest quote rows for fast `/quote/:symbol` reads.
- `ingestion_runs`: run status and failure summaries.
- `symbol_aliases`: reserved for future ticker aliases.

## Data Organization Flow

There are three data-producing flows:

1. Daily sync: `cmd/sync-daily`
2. Historical backfill: `cmd/backfill-history`
3. Derived rebuild: `cmd/rebuild-history`

Daily sync is the normal weekday update path. It loads the Nasdaq Trader symbol universe, targets the full active universe by default, or targets only `--watchlist` symbols when a watchlist path is provided. It fetches EOD quotes from Massive, validates and normalizes records, writes or overwrites the daily file for that date, merges those records into each symbol history file with configurable parallel workers, rebuilds `latest`, updates D1, and writes a run log. The CLI prints progress logs to stderr, including target counts, fetch/validation counts, object write phases, D1 update phases, and symbol-history progress every 1,000 records.

If `--date` is omitted, daily sync starts from the latest market date that should be closed in `America/New_York` time, then walks backward through weekdays until Massive returns daily records. This handles before-close manual runs, weekends, and market holidays. If `--date` is provided, only that date is fetched. Re-running daily sync for an existing date overwrites that date's daily file and updates derived latest/history/run objects; symbol histories replace records with the same date instead of appending duplicates.

Historical backfill is the large dataset construction path. It downloads the Stooq US daily archive (`https://static.stooq.com/db/h/{STOOQ_API_KEY}/d_us_txt.zip` when `STOOQ_API_KEY` is set), filters it to the requested target symbols and optional `--from`/`--to` range, then writes:

- daily files for each date in the range
- per-symbol history files
- latest full and minified indexes
- D1 `latest_quotes`
- symbol snapshots and run logs

Backfill targets all active universe symbols by default. If `--watchlist` is provided, it targets only those symbols. If `--from` is omitted, backfill starts from the earliest dated archive record; if `--to` is omitted, it writes through the latest dated archive record. If Stooq blocks automated archive downloads with human verification, download `d_us_txt.zip` manually and pass `--stooq-archive-file /path/to/d_us_txt.zip` locally, or provide `stooq_archive_url` when running the GitHub workflow. `STOOQ_ARCHIVE_FILE` and `STOOQ_ARCHIVE_URL` can override the archive input for local runs, mirrors, or fixtures.

Backfill defaults to merge mode. Merge mode inserts or replaces only fetched symbol/date records while preserving all other existing records. `--replace` is scoped to the requested symbols and date range: it removes existing records for those symbols inside the range, then writes the fetched records, while preserving other symbols and dates outside the range.

Rebuild is the repair path for derived data. It reads existing daily files, then regenerates symbol histories and latest indexes. It does not fetch market data.

The intended ownership model is:

- R2/local object storage is the durable data warehouse.
- D1 is a replaceable serving index for latest quotes and metadata.
- Worker APIs read D1 for fast latest quotes and R2 for history, daily files, and universe files.

## Operation Cost Estimates

These estimates count application-level operations performed by this repo. They do not include upstream market-data API calls, Cloudflare API overhead, retries, failed partial writes, or Worker API reads.

Definitions:

- `S`: successful symbols processed. For daily sync, this is also the successful quote records written to that daily file.
- `T`: target symbols attempted, equal to `recordsTotal`.
- `D`: trading dates written by a historical backfill.
- `U`: symbol universe rows fetched from Nasdaq Trader.
- `L`: quotes written to `latest_quotes`; for a clean daily sync this is usually equal to `S`, plus any stale quotes retained from a previous latest file.

### Daily Sync

`cmd/sync-daily` writes one daily file, updates latest files, merges each successful record into symbol history, writes symbol snapshots, and writes a run log.

R2 object operations:

| Operation class | Formula | Per 1,000 successful records | Per 10,000 successful records |
| --- | ---: | ---: | ---: |
| A class writes | `S + 6` | `1,006` | `10,006` |
| B class reads | `S + 2` | `1,002` | `10,002` |

R2 A class writes are:

- `1` `symbols/latest`
- `1` `symbols/{date}`
- `1` `daily/{date}`
- `1` `latest.json`
- `1` `latest.min.json`
- `S` `history/{symbol}/daily.json.gz`
- `1` `runs/{date}`

R2 B class reads are:

- `1` previous `latest.json`
- `1` `daily/{date}` existence check
- `S` existing symbol history reads

D1 operations, when D1 environment variables are configured:

| Operation | Formula | Notes |
| --- | ---: | --- |
| Row writes | `U + L + 1` | `symbols` upserts, `latest_quotes` upserts, and one `ingestion_runs` upsert. |
| Row reads | up to `U` | `symbols` upserts preserve `first_seen` with one lookup per universe row; first-time runs may return fewer rows. |

If D1 is not configured, D1 reads and writes are skipped.

### Historical Backfill

`cmd/backfill-history` reads one Stooq archive, writes symbol snapshots, writes one history object per successful symbol, writes one daily file per trading date, updates latest files, updates D1, and writes a run log.

R2 object operations:

| Operation class | Formula | Per 1,000 successful symbols | Per 10,000 successful symbols |
| --- | ---: | ---: | ---: |
| A class writes | `S + D + 5` | `1,005 + D` | `10,005 + D` |
| B class reads | `2S + D + 1` | `2,001 + D` | `20,001 + D` |

R2 A class writes are:

- `1` `symbols/latest`
- `1` `symbols/{effectiveTo}`
- `S` `history/{symbol}/daily.json.gz`
- `D` `daily/{date}.json.gz`
- `1` `latest.json`
- `1` `latest.min.json`
- `1` `runs/{effectiveTo}/backfill`

R2 B class reads are:

- `S` existing symbol history reads before merging backfill records
- `D` existing daily file reads before merging daily records
- `1` previous `latest.json`
- `S` symbol history reads while deriving latest quote fields

D1 operations, when D1 environment variables are configured:

| Operation | Formula | Notes |
| --- | ---: | --- |
| Row writes | `U + L + 1` | `symbols` upserts, `latest_quotes` upserts, and one `ingestion_runs` upsert. |
| Row reads | up to `U` | `symbols` upserts preserve `first_seen` with one lookup per universe row; first-time runs may return fewer rows. |

If D1 is not configured, D1 reads and writes are skipped.

### Derived Rebuild

Cost estimate: TBD.

## Cloudflare Setup

Required sync environment variables:

- `MASSIVE_API_KEY` for daily Massive market-summary sync.
- `R2_BUCKET`
- `CLOUDFLARE_ACCOUNT_ID`
- `CLOUDFLARE_API_TOKEN`
- `D1_DATABASE_ID`

Backfill uses Stooq's downloadable US daily archive. Set `STOOQ_API_KEY` to build the keyed static archive URL, provide a local manually downloaded archive with `--stooq-archive-file`, or provide an archive URL with the GitHub workflow `stooq_archive_url` input.

Apply the D1 migration in `worker/migrations/0001_initial.sql`, then set `worker/wrangler.toml` bindings for the actual R2 bucket and D1 database id.

GitHub Actions:

- `Sync Market Data` is a manual fallback workflow.
- `Backfill Market History` is manual only and supports `from`, `to`, `watchlist`, `replace`, and `stooq_archive_url` inputs.

## VPS Daily Sync

The production daily sync should run from a VPS with `systemd timer`. GitHub Actions can remain as a manual fallback, but it is not the primary scheduler.

On the VPS, install Go, clone the repo, then run:

```bash
./scripts/install.sh
```

The installer:

- builds `cmd/sync-daily` into `bin/sync-daily`
- installs runtime files under `/opt/market-data-hub`
- creates `/etc/market-data-hub/env` from `deploy/vps/env.example` if it does not exist
- installs `market-data-sync.service` and `market-data-sync.timer`
- enables and starts the timer

Fill in `/etc/market-data-hub/env` with real secrets:

```bash
sudo editor /etc/market-data-hub/env
```

Required values:

```bash
MARKET_DATA_MARKET=us
MARKET_DATA_STORAGE=r2
R2_BUCKET=...
MASSIVE_API_KEY=...
CLOUDFLARE_ACCOUNT_ID=...
CLOUDFLARE_API_TOKEN=...
D1_DATABASE_ID=...
```

The production wrapper does not pass `--date`, so the syncer automatically resolves the latest closed trading date with available Massive records. This avoids failures caused by scheduler delays crossing midnight, weekends, and market holidays. It also does not pass `--watchlist`, so the daily VPS sync targets the full active US universe.

Check the timer:

```bash
systemctl list-timers market-data-sync.timer
systemctl status market-data-sync.timer
```

Run one sync manually:

```bash
sudo systemctl start market-data-sync.service
```

Inspect logs:

```bash
journalctl -u market-data-sync.service -n 100 --no-pager
```

Uninstall the VPS service and installed runtime files:

```bash
./scripts/uninstall.sh
```

The uninstall script keeps `/etc/market-data-hub/env` by default because it contains secrets. To remove it too:

```bash
./scripts/uninstall.sh --purge-env
```

The timer is configured to run after midnight on the next US market day so Massive free-plan data for the previous session is available:

```text
Tue..Sat 00:30 America/New_York
```

`Persistent=true` lets systemd run a missed sync after the VPS comes back online. The wrapper script uses a file lock so overlapping sync attempts are skipped instead of running concurrently.

## Worker

The Worker is the public read API. It binds to:

- `MARKET_DATA`: R2 bucket binding.
- `DB`: D1 database binding.

Deploy:

```bash
cd worker
npm install
npm run typecheck
npm run deploy
```

All API responses include:

- `Content-Type: application/json; charset=utf-8`
- `Cache-Control: public, max-age=60`
- CORS headers allowing browser `GET` requests from any origin.

The Worker handles `OPTIONS` preflight requests with `204 No Content`.

### Worker API

#### `GET /quote/:symbol`

Returns the latest quote for one symbol from D1 `latest_quotes`.

Example:

```bash
curl https://<worker-host>/quote/NVDA
```

Response:

```json
{
  "symbol": "NVDA",
  "date": "2026-06-01",
  "price": 102.4,
  "change": 1.23,
  "changePct": 1.216,
  "volume": 123456789,
  "source": "massive"
}
```

Errors:

- `400 {"error":"bad_symbol"}` for malformed symbols.
- `404 {"error":"not_found","symbol":"NVDA"}` when no latest quote exists.

#### `GET /history/:symbol?range=1y`

Returns daily history for one symbol from R2 `history/us/{SYMBOL}/daily.json.gz`.

Supported `range` values:

- `1m`: latest 31 calendar days from the newest record.
- `3m`: latest 92 calendar days.
- `6m`: latest 183 calendar days.
- `1y`: latest 365 calendar days.
- `max` or omitted: all records.

Unknown range values currently fall back to all records.

Example:

```bash
curl "https://<worker-host>/history/NVDA?range=1y"
```

Response:

```json
{
  "symbol": "NVDA",
  "range": "1y",
  "records": [
    {
      "symbol": "NVDA",
      "date": "2026-06-01",
      "open": 100.5,
      "high": 103.2,
      "low": 99.8,
      "close": 102.4,
      "adjClose": 102.4,
      "volume": 123456789,
      "source": "massive",
      "updatedAt": "2026-06-02T01:00:00Z"
    }
  ]
}
```

Errors:

- `400 {"error":"bad_symbol"}` for malformed symbols.
- `404 {"error":"not_found","symbol":"NVDA"}` when the history object does not exist.

#### `GET /daily/us/:date`

Returns one market-wide daily file from R2 `daily/us/YYYY/YYYY-MM-DD.json.gz`.

Example:

```bash
curl https://<worker-host>/daily/us/2026-06-01
```

Response shape is the same as `DailyMarketFile` in the data layout section.

Errors:

- `404 {"error":"not_found","key":"daily/us/2026/2026-06-01.json.gz"}` when the daily object does not exist.

#### `GET /universe/us`

Returns the latest US symbol universe from R2 `symbols/us/latest.json`.

Example:

```bash
curl https://<worker-host>/universe/us
```

Response:

```json
{
  "date": "2026-06-01",
  "market": "US",
  "source": "nasdaqtrader",
  "symbols": [
    {
      "symbol": "NVDA",
      "name": "NVIDIA Corporation",
      "exchange": "NASDAQ",
      "assetType": "stock",
      "isEtf": false,
      "isActive": true
    }
  ]
}
```

#### `GET /context/:symbol`

Returns a compact context object by combining D1 `latest_quotes` and D1 `symbols`.

Example:

```bash
curl https://<worker-host>/context/NVDA
```

Response:

```json
{
  "symbol": "NVDA",
  "name": "NVIDIA Corporation",
  "market": "US",
  "exchange": "NASDAQ",
  "latest": {
    "date": "2026-06-01",
    "price": 102.4,
    "changePct": 1.216,
    "volume": 123456789
  },
  "stats": null
}
```

Errors:

- `400 {"error":"bad_symbol"}` for malformed symbols.
- `404 {"error":"not_found","symbol":"NVDA"}` when no latest quote exists.

Common errors:

- `404 {"error":"not_found"}` for unknown routes.
- `405 {"error":"method_not_allowed"}` for methods other than `GET` and `OPTIONS`.
- `500 {"error":"internal_error","message":"..."}` for unexpected Worker errors.

## Boundaries

This MVP does not implement real-time quotes, intraday bars, options chains, earnings data, automatic trading signals, or multi-source arbitration. R2 is the source of truth; D1 stores metadata and latest indexes only.
