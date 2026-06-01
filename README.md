# Market Data Hub

Daily EOD market-data warehouse for personal trading tools and AI agents. It is a batch data layer, not a real-time quote service.

## What It Builds

- Go CLI for daily sync, history rebuild, and file validation.
- Cloudflare R2 object layout for daily, latest, symbol, history, and run files.
- Cloudflare D1 schema for symbols, latest quotes, and ingestion logs.
- Cloudflare Worker API for quote/history/daily/universe/context queries.
- GitHub Actions schedule for weekday US market EOD sync.

## Local Usage

Run a local sync. By default, daily sync targets all active symbols from the Nasdaq Trader universe:

```bash
go run ./cmd/sync-daily --storage local --root data --market us --date 2026-05-31
```

Restrict daily sync to a watchlist by passing `--watchlist`:

```bash
go run ./cmd/sync-daily --storage local --root data --market us --date 2026-05-31 --watchlist config/watchlist.yaml
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

For large runs, split the universe into batches and throttle Stooq requests:

```bash
go run ./cmd/backfill-history \
  --storage r2 \
  --market us \
  --all \
  --from 2010-01-01 \
  --to 2026-05-29 \
  --offset 0 \
  --batch-size 200 \
  --sleep-ms 1000 \
  --max-retries 3 \
  --retry-backoff-ms 2000
```

`--symbols-file` accepts newline or comma-separated symbol lists. Explicit `--symbols` wins over `--symbols-file`; `--symbols-file` wins over `--all`; `--all` wins over the default watchlist.

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

Daily sync is the normal weekday update path. It loads the Nasdaq Trader symbol universe, targets the full active universe by default, or targets only `--watchlist` symbols when a watchlist path is provided. It fetches EOD quotes from Massive, validates and normalizes records, writes one immutable daily file, merges those records into each symbol history file, rebuilds `latest`, updates D1, and writes a run log.

If `--date` is omitted, daily sync uses the latest date returned by the data source. If `--date` is provided, records that do not match that date are treated as failed. Existing daily files are not overwritten by daily sync; use backfill or rebuild when a date must be corrected.

Historical backfill is the large dataset construction path. It fetches historical daily bars from Stooq per symbol, filters them to `--from` and `--to`, then writes:

- daily files for each date in the range
- per-symbol history files
- latest full and minified indexes
- D1 `latest_quotes`
- symbol snapshots and run logs

Backfill target priority is `--symbols`, then `--symbols-file`, then `--all`, then `config/watchlist.yaml`. Use `--offset`, `--batch-size`, `--sleep-ms`, `--max-retries`, and `--retry-backoff-ms` to split full-market work into resumable chunks and reduce pressure on upstream data sources.

Backfill defaults to merge mode. Merge mode inserts or replaces only fetched symbol/date records while preserving all other existing records. `--replace` is scoped to the requested symbols and date range: it removes existing records for those symbols inside the range, then writes the fetched records, while preserving other symbols and dates outside the range.

Rebuild is the repair path for derived data. It reads existing daily files, then regenerates symbol histories and latest indexes. It does not fetch market data.

The intended ownership model is:

- R2/local object storage is the durable data warehouse.
- D1 is a replaceable serving index for latest quotes and metadata.
- Worker APIs read D1 for fast latest quotes and R2 for history, daily files, and universe files.

## Operation Cost Estimates

These estimates count application-level operations performed by this repo. They do not include upstream market-data API calls, Cloudflare API overhead, retries, failed partial writes, or Worker API reads.

Definitions:

- `S`: successful quote records written to the daily file, equal to `recordsSuccess`.
- `T`: target symbols attempted, equal to `recordsTotal`.
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

Cost estimate: TBD.

### Derived Rebuild

Cost estimate: TBD.

## Cloudflare Setup

Required sync environment variables:

- `MASSIVE_API_KEY` for daily Massive market-summary sync.
- `R2_BUCKET`
- `CLOUDFLARE_ACCOUNT_ID`
- `CLOUDFLARE_API_TOKEN`
- `D1_DATABASE_ID`

Backfill still uses Stooq and may require `STOOQ_API_KEY`.

Apply the D1 migration in `worker/migrations/0001_initial.sql`, then set `worker/wrangler.toml` bindings for the actual R2 bucket and D1 database id.

GitHub Actions:

- `Sync Market Data` runs daily and supports manual one-day sync.
- `Backfill Market History` is manual only and supports `from`, `to`, `symbols`, `symbols_file`, `all`, `replace`, `offset`, `batch_size`, `sleep_ms`, `max_retries`, and `retry_backoff_ms` inputs.

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
