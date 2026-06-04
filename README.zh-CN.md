# Market Data Hub

[English](README.md) | [中文](README.zh-CN.md)

面向个人交易工具和 AI Agent 的每日 EOD 市场数据仓库。它是一个批处理数据层，不是实时行情服务。

## 构建内容

- 用于每日同步、历史重建和文件校验的 Go CLI。
- Cloudflare R2 对象布局，包含 daily、latest、symbols、history 和 runs 文件。
- Cloudflare D1 schema，用于 symbols、latest quotes 和 ingestion logs。
- Cloudflare Worker API，用于 quote/history/daily/universe/context 查询。
- 用于美股工作日 EOD 同步的 VPS/systemd 部署脚本。

## 本地使用

运行一次本地同步。默认情况下，daily sync 会同步 Nasdaq Trader universe 中所有 active 标的：

```bash
go run ./cmd/sync-daily --storage local --root data --market us --date 2026-05-31
```

通过 `--watchlist` 将 daily sync 限制到指定 watchlist：

```bash
go run ./cmd/sync-daily --storage local --root data --market us --date 2026-05-31 --watchlist config/watchlist.yaml
```

Daily sync 默认使用 16 个并行 R2/object-store worker 更新每个 symbol 的 history 对象。可以用 `--r2-concurrency` 调整上传吞吐或限流压力：

```bash
go run ./cmd/sync-daily --storage r2 --market us --date 2026-05-31 --r2-concurrency 32
```

校验生成的文件：

```bash
go run ./cmd/validate-data --file data/daily/us/2026/2026-05-31.json.gz
```

从本地 daily 文件重建 history 和 latest：

```bash
go run ./cmd/rebuild-history --storage local --root data --market us --from 2026-05-01 --to 2026-05-31
```

从 Stooq 回填历史数据到 daily、history、latest 和 D1 latest indexes：

```bash
go run ./cmd/backfill-history --storage r2 --market us
```

将 backfill 限制到 watchlist：

```bash
go run ./cmd/backfill-history --storage r2 --market us --watchlist config/watchlist.yaml
```

用可选的 `--from` 和 `--to` 限制写入日期范围：

```bash
go run ./cmd/backfill-history \
  --storage r2 \
  --market us \
  --from 2010-01-01 \
  --to 2026-05-29
```

大规模运行时，backfill 会先下载一次 Stooq US daily archive，然后在本地过滤。如果 Stooq 要求人机验证，可以手动下载 archive，并传给命令：

```bash
go run ./cmd/backfill-history \
  --storage r2 \
  --market us \
  --stooq-archive-file /path/to/d_us_txt.zip
```

默认情况下，backfill 会同步 Nasdaq Trader universe 中所有 active 标的，以及 archive 中所有带日期的记录。传入 `--watchlist` 可以限制 symbols，传入 `--from` 和/或 `--to` 可以限制日期。

默认情况下，backfill 会把获取到的记录 merge 到已有对象中。添加 `--replace` 后，会替换请求日期范围内的目标 symbol 记录，同时保留其他 symbols 和范围外日期。

## 数据布局

本地存储和 R2 使用相同的 object keys。本地模式写入 `data/`；R2 模式把相同路径写入配置的 bucket。

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

- `symbols/us/latest.json`：来自 Nasdaq Trader 的最新 US symbol universe。
- `symbols/us/YYYY/YYYY-MM-DD.json`：某次运行使用的 symbol universe 日期快照。
- `daily/us/YYYY/YYYY-MM-DD.json.gz`：某交易日的全市场 daily EOD 文件。
- `history/us/SYMBOL/daily.json.gz`：每个 symbol 一份 daily history，按日期排序。
- `latest/us/latest.json`：按 symbol 索引的完整 latest quote。
- `latest/us/latest.min.json`：面向轻量客户端的 compact latest quote。
- `runs/YYYY/*.json`：daily sync 和 backfill 的 ingestion logs。

`daily` 文件包含一个 `DailyMarketFile`：

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

`latest/us/latest.json` 存储派生 quote 字段：

- `price`, `change`, `changePct`, `volume`
- `open`, `high`, `low`, `prevClose`
- `date`, `source`, `stale`, `flags`

`latest/us/latest.min.json` 以 compact numeric 格式存储同一份 latest index，并用 `schema` 数组描述每个 quote vector。

D1 是派生查询索引，不是数据源事实表。它存储：

- `symbols`：可搜索的 symbol metadata。
- `latest_quotes`：用于快速 `/quote/:symbol` 读取的 latest quote rows。
- `ingestion_runs`：运行状态和失败摘要。
- `symbol_aliases`：预留给未来 ticker alias。

## 数据组织流程

有三条数据生产流程：

1. Daily sync：`cmd/sync-daily`
2. Historical backfill：`cmd/backfill-history`
3. Derived rebuild：`cmd/rebuild-history`

Daily sync 是常规工作日更新路径。它加载 Nasdaq Trader symbol universe，默认同步完整 active universe；如果提供 `--watchlist`，则只同步 watchlist 中的 symbols。它从 Massive 获取 EOD quotes，校验并规范化 records，写入或覆盖该日期的 daily 文件，用可配置的并行 workers 合并到每个 symbol history，重建 `latest`，更新 D1，并写入 run log。CLI 会把进度日志输出到 stderr，包括 target counts、fetch/validation counts、object write phases、D1 update phases，以及每 1,000 条的 symbol-history progress。

如果省略 `--date`，daily sync 会使用数据源返回的最新日期。如果提供 `--date`，不匹配该日期的 records 会被视为失败。对已有日期重新运行 daily sync 会覆盖该日期的 daily 文件，并更新派生的 latest/history/run objects；symbol histories 会替换相同日期的 records，不会重复追加。

Historical backfill 是大规模历史数据构建路径。它会下载 Stooq US daily archive（设置 `STOOQ_API_KEY` 时使用 `https://static.stooq.com/db/h/{STOOQ_API_KEY}/d_us_txt.zip`），按目标 symbols 和可选 `--from`/`--to` 范围过滤，然后写入：

- 日期范围内的 daily files
- per-symbol history files
- latest full 和 minified indexes
- D1 `latest_quotes`
- symbol snapshots 和 run logs

Backfill 默认目标是 active universe 中所有 symbols。如果提供 `--watchlist`，则只同步这些 symbols。如果省略 `--from`，backfill 会从 archive 中最早的带日期记录开始；如果省略 `--to`，会写到 archive 中最新的带日期记录。如果 Stooq 用人机验证阻止自动 archive 下载，可以手动下载 `d_us_txt.zip` 并在本地传入 `--stooq-archive-file /path/to/d_us_txt.zip`，或在 GitHub workflow 中提供 `stooq_archive_url`。`STOOQ_ARCHIVE_FILE` 和 `STOOQ_ARCHIVE_URL` 可以为本地运行、镜像或 fixtures 覆盖 archive 输入。

Backfill 默认是 merge 模式。Merge 模式只插入或替换获取到的 symbol/date records，同时保留所有其他已有记录。`--replace` 只作用于请求的 symbols 和日期范围：它会删除这些 symbols 在范围内的已有 records，然后写入获取到的 records，同时保留其他 symbols 和范围外日期。

Rebuild 是派生数据修复路径。它读取已有 daily files，然后重新生成 symbol histories 和 latest indexes。它不会获取市场数据。

预期的数据所有权模型是：

- R2/local object storage 是持久化数据仓库。
- D1 是可替换的 serving index，用于 latest quotes 和 metadata。
- Worker APIs 从 D1 读取快速 latest quotes，从 R2 读取 history、daily files 和 universe files。

## 操作成本估算

这些估算统计的是本仓库执行的应用层操作。不包含上游市场数据 API 调用、Cloudflare API overhead、重试、失败的部分写入或 Worker API reads。

定义：

- `S`：成功处理的 symbols。对 daily sync 来说，它也等于写入该 daily 文件的成功 quote records。
- `T`：尝试处理的目标 symbols，等于 `recordsTotal`。
- `D`：historical backfill 写入的交易日数量。
- `U`：从 Nasdaq Trader 获取到的 symbol universe 行数。
- `L`：写入 `latest_quotes` 的 quotes；对 clean daily sync 来说通常等于 `S`，再加上从 previous latest 保留的 stale quotes。

### Daily Sync

`cmd/sync-daily` 写入一个 daily 文件，更新 latest 文件，将每条成功记录合并进 symbol history，写入 symbol snapshots，并写入 run log。

R2 object operations：

| Operation class | Formula | Per 1,000 successful records | Per 10,000 successful records |
| --- | ---: | ---: | ---: |
| A class writes | `S + 6` | `1,006` | `10,006` |
| B class reads | `S + 2` | `1,002` | `10,002` |

R2 A class writes 包括：

- `1` `symbols/latest`
- `1` `symbols/{date}`
- `1` `daily/{date}`
- `1` `latest.json`
- `1` `latest.min.json`
- `S` `history/{symbol}/daily.json.gz`
- `1` `runs/{date}`

R2 B class reads 包括：

- `1` previous `latest.json`
- `1` `daily/{date}` existence check
- `S` existing symbol history reads

配置 D1 环境变量时的 D1 operations：

| Operation | Formula | Notes |
| --- | ---: | --- |
| Row writes | `U + L + 1` | `symbols` upserts、`latest_quotes` upserts，以及一次 `ingestion_runs` upsert。 |
| Row reads | up to `U` | `symbols` upserts 用每个 universe row 一次 lookup 保留 `first_seen`；首次运行时实际返回行可能更少。 |

如果未配置 D1，会跳过 D1 reads 和 writes。

### Historical Backfill

`cmd/backfill-history` 读取一个 Stooq archive，写入 symbol snapshots，为每个成功 symbol 写入一个 history object，为每个交易日写入一个 daily file，更新 latest files，更新 D1，并写入 run log。

R2 object operations：

| Operation class | Formula | Per 1,000 successful symbols | Per 10,000 successful symbols |
| --- | ---: | ---: | ---: |
| A class writes | `S + D + 5` | `1,005 + D` | `10,005 + D` |
| B class reads | `2S + D + 1` | `2,001 + D` | `20,001 + D` |

R2 A class writes 包括：

- `1` `symbols/latest`
- `1` `symbols/{effectiveTo}`
- `S` `history/{symbol}/daily.json.gz`
- `D` `daily/{date}.json.gz`
- `1` `latest.json`
- `1` `latest.min.json`
- `1` `runs/{effectiveTo}/backfill`

R2 B class reads 包括：

- `S` merge backfill records 前读取已有 symbol history
- `D` merge daily records 前读取已有 daily file
- `1` previous `latest.json`
- `S` 推导 latest quote fields 时读取 symbol history

配置 D1 环境变量时的 D1 operations：

| Operation | Formula | Notes |
| --- | ---: | --- |
| Row writes | `U + L + 1` | `symbols` upserts、`latest_quotes` upserts，以及一次 `ingestion_runs` upsert。 |
| Row reads | up to `U` | `symbols` upserts 用每个 universe row 一次 lookup 保留 `first_seen`；首次运行时实际返回行可能更少。 |

如果未配置 D1，会跳过 D1 reads 和 writes。

### Derived Rebuild

成本估算：TBD。

## Cloudflare 配置

Daily sync 必需的环境变量：

- `MASSIVE_API_KEY`：用于每日 Massive market-summary sync。
- `R2_BUCKET`
- `CLOUDFLARE_ACCOUNT_ID`
- `CLOUDFLARE_API_TOKEN`
- `D1_DATABASE_ID`

Backfill 使用 Stooq 可下载的 US daily archive。可以设置 `STOOQ_API_KEY` 来构建带 key 的静态 archive URL，使用 `--stooq-archive-file` 提供本地手动下载的 archive，或在 GitHub workflow 中用 `stooq_archive_url` 输入提供 archive URL。

应用 `worker/migrations/0001_initial.sql` 中的 D1 migration，然后在 `worker/wrangler.toml` 中设置实际的 R2 bucket 和 D1 database id bindings。

GitHub Actions：

- `Sync Market Data` 是手动 fallback workflow。
- `Backfill Market History` 仅手动运行，支持 `from`、`to`、`watchlist`、`replace` 和 `stooq_archive_url` inputs。

## VPS 每日同步

生产 daily sync 应该通过 VPS 上的 `systemd timer` 运行。GitHub Actions 可以保留为手动 fallback，但它不是主要 scheduler。

在 VPS 上安装 Go、clone 仓库，然后运行：

```bash
./scripts/install.sh
```

Installer 会：

- 将 `cmd/sync-daily` 构建为 `bin/sync-daily`
- 将 runtime files 安装到 `/opt/market-data-hub`
- 如果 `/etc/market-data-hub/env` 不存在，则从 `deploy/vps/env.example` 创建
- 安装 `market-data-sync.service` 和 `market-data-sync.timer`
- enable 并 start timer

在 `/etc/market-data-hub/env` 中填入真实 secrets：

```bash
sudo editor /etc/market-data-hub/env
```

必需值：

```bash
MARKET_DATA_MARKET=us
MARKET_DATA_STORAGE=r2
R2_BUCKET=...
MASSIVE_API_KEY=...
CLOUDFLARE_ACCOUNT_ID=...
CLOUDFLARE_API_TOKEN=...
D1_DATABASE_ID=...
```

生产 wrapper 不传 `--date`，因此 syncer 会使用数据源返回的最新日期。这可以避免 scheduler delay 跨过 midnight 导致的失败。它也不传 `--watchlist`，因此 VPS daily sync 目标是完整 active US universe。

检查 timer：

```bash
systemctl list-timers market-data-sync.timer
systemctl status market-data-sync.timer
```

手动运行一次 sync：

```bash
sudo systemctl start market-data-sync.service
```

查看日志：

```bash
journalctl -u market-data-sync.service -n 100 --no-pager
```

卸载 VPS service 和已安装 runtime files：

```bash
./scripts/uninstall.sh
```

卸载脚本默认保留 `/etc/market-data-hub/env`，因为里面包含 secrets。如需一并删除：

```bash
./scripts/uninstall.sh --purge-env
```

Timer 配置为：

```text
Mon..Fri 19:30 America/New_York
```

`Persistent=true` 会让 systemd 在 VPS 恢复在线后补跑错过的 sync。Wrapper script 使用文件锁，因此重叠的 sync attempts 会被跳过，而不是并发运行。

## Worker

```bash
cd worker
npm install
npm run typecheck
npm run deploy
```

Routes：

- `GET /quote/:symbol`
- `GET /history/:symbol?range=1y`
- `GET /daily/us/:date`
- `GET /universe/us`
- `GET /context/:symbol`

## 边界

这个 MVP 不实现 real-time quotes、intraday bars、options chains、earnings data、automatic trading signals 或 multi-source arbitration。R2 是 source of truth；D1 只存储 metadata 和 latest indexes。
