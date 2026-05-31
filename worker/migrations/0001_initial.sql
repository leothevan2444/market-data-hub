CREATE TABLE IF NOT EXISTS symbols (
  symbol TEXT PRIMARY KEY,
  name TEXT,
  market TEXT NOT NULL,
  exchange TEXT,
  asset_type TEXT,
  is_etf INTEGER DEFAULT 0,
  is_active INTEGER DEFAULT 1,
  first_seen DATE,
  last_seen DATE,
  updated_at TEXT
);

CREATE TABLE IF NOT EXISTS latest_quotes (
  symbol TEXT PRIMARY KEY,
  market TEXT NOT NULL,
  date TEXT NOT NULL,
  open REAL,
  high REAL,
  low REAL,
  close REAL,
  adj_close REAL,
  volume INTEGER,
  change REAL,
  change_pct REAL,
  source TEXT,
  updated_at TEXT
);

CREATE TABLE IF NOT EXISTS ingestion_runs (
  id TEXT PRIMARY KEY,
  market TEXT NOT NULL,
  date TEXT NOT NULL,
  status TEXT NOT NULL,
  source TEXT,
  records_total INTEGER,
  records_success INTEGER,
  records_failed INTEGER,
  started_at TEXT,
  finished_at TEXT,
  error_message TEXT
);

CREATE TABLE IF NOT EXISTS symbol_aliases (
  alias TEXT PRIMARY KEY,
  symbol TEXT NOT NULL,
  source TEXT,
  updated_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_latest_quotes_market_date ON latest_quotes (market, date);
CREATE INDEX IF NOT EXISTS idx_symbols_market_exchange ON symbols (market, exchange);
CREATE INDEX IF NOT EXISTS idx_ingestion_runs_date ON ingestion_runs (date);
