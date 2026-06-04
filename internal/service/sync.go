package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"market-data-hub/internal/normalize"
	"market-data-hub/internal/schema"
	"market-data-hub/internal/source/massive"
	"market-data-hub/internal/source/nasdaqtrader"
	"market-data-hub/internal/storage"
	"market-data-hub/internal/storage/d1"
	"market-data-hub/internal/storage/local"
	"market-data-hub/internal/storage/r2"
)

type SyncOptions struct {
	Market        string
	Date          string
	WatchlistPath string
	R2Workers     int
	Log           func(string, ...any)
}

type dailyBatchSource interface {
	FetchDailyBatch(context.Context, []string, string) (map[string]schema.DailyQuoteRecord, map[string]error, error)
}

type dailyBulkSource interface {
	FetchDailyBulk(context.Context, []string, string) (map[string]schema.DailyQuoteRecord, map[string]error, error)
}

type Syncer struct {
	Store   storage.ObjectStore
	D1      d1.Client
	Symbols interface {
		FetchUniverse(context.Context) ([]schema.SymbolInfo, error)
	}
	Quotes interface {
		FetchDaily(context.Context, string, string) (schema.DailyQuoteRecord, error)
	}
	Clock func() time.Time
}

func NewSyncer(store storage.ObjectStore, d1Client d1.Client) Syncer {
	return Syncer{
		Store:   store,
		D1:      d1Client,
		Symbols: nasdaqtrader.New(),
		Quotes:  massive.New(),
		Clock:   func() time.Time { return time.Now().UTC() },
	}
}

func (s Syncer) Run(ctx context.Context, opt SyncOptions) error {
	logf := func(format string, args ...any) {
		if opt.Log != nil {
			opt.Log(format, args...)
		}
	}
	started := time.Now()
	if opt.Market == "" {
		opt.Market = "us"
	}
	dateExplicit := opt.Date != ""
	if opt.Date == "" {
		opt.Date = defaultMarketDate(s.now())
	}
	if err := normalize.RequireDate(opt.Date); err != nil {
		return err
	}
	logf("start market=%s date=%s watchlist=%s", strings.ToUpper(opt.Market), opt.Date, displayWatchlist(opt.WatchlistPath))
	run := schema.IngestionRun{
		ID:        opt.Market + "-" + opt.Date + "-" + s.now().Format("150405"),
		Market:    strings.ToUpper(opt.Market),
		Date:      opt.Date,
		Status:    "running",
		Source:    "massive",
		StartedAt: s.now().Format(time.RFC3339),
	}
	finish := func(status string, err error) error {
		run.Status = status
		run.FinishedAt = s.now().Format(time.RFC3339)
		if err != nil {
			run.ErrorMessage = err.Error()
		}
		logf("finish status=%s total=%d success=%d failed=%d elapsed=%s", status, run.RecordsTotal, run.RecordsSuccess, run.RecordsFailed, time.Since(started).Round(time.Second))
		_ = s.writeRun(ctx, opt.Date, run)
		_ = s.writeRunD1(ctx, run)
		return err
	}

	logf("fetching symbol universe")
	symbols, err := s.Symbols.FetchUniverse(ctx)
	if err != nil {
		return finish("failed", fmt.Errorf("fetch universe: %w", err))
	}
	logf("fetched symbol universe symbols=%d", len(symbols))
	if s.D1.Enabled() {
		logf("updating D1 symbols rows=%d", len(symbols))
	}
	if err := s.writeSymbolsD1(ctx, opt.Market, symbols); err != nil {
		logf("warning: update D1 symbols failed: %v", err)
	}

	var targets []string
	if opt.WatchlistPath != "" {
		logf("loading watchlist path=%s", opt.WatchlistPath)
		var err error
		targets, err = LoadWatchlist(opt.WatchlistPath)
		if err != nil {
			return finish("failed", err)
		}
		if len(targets) == 0 {
			return finish("failed", fmt.Errorf("watchlist %s contains no symbols or does not exist", opt.WatchlistPath))
		}
	} else {
		for _, sym := range symbols {
			if sym.IsActive {
				targets = append(targets, sym.Symbol)
			}
		}
	}
	run.RecordsTotal = len(targets)
	logf("resolved targets count=%d", len(targets))

	logf("reading previous latest snapshot")
	previous := s.readLatest(ctx, opt.Market)
	universeMap := normalize.UniverseMap(symbols)
	var records []schema.DailyQuoteRecord
	var failedTargets []string
	logf("fetching daily quotes targets=%d date=%s", len(targets), opt.Date)
	fetched, fetchFailures, err := s.fetchDailyRecords(ctx, targets, opt.Date, dateExplicit)
	if err != nil {
		return finish("failed", err)
	}
	logf("fetched daily quotes records=%d fetchFailures=%d", len(fetched), len(fetchFailures))
	if !dateExplicit {
		if inferred, ok := inferDailyDate(fetched); ok {
			opt.Date = inferred
			run.Date = inferred
			logf("inferred daily date=%s", opt.Date)
		}
	}
	universe := schema.SymbolUniverse{Date: opt.Date, Market: strings.ToUpper(opt.Market), Source: "nasdaqtrader", Symbols: symbols}
	logf("writing symbol snapshots")
	if err := s.writeJSON(ctx, storage.SymbolsLatestKey(opt.Market), universe); err != nil {
		return finish("failed", err)
	}
	if err := s.writeJSON(ctx, storage.SymbolsDatedKey(opt.Market, opt.Date), universe); err != nil {
		return finish("failed", err)
	}
	logf("validating target records")
	for _, symbol := range targets {
		symbol = strings.ToUpper(symbol)
		record, ok := fetched[symbol]
		if !ok {
			run.RecordsFailed++
			if err := fetchFailures[symbol]; err != nil {
				run.FailedSymbols = append(run.FailedSymbols, symbol+":"+err.Error())
			} else {
				run.FailedSymbols = append(run.FailedSymbols, symbol+":missing quote")
			}
			failedTargets = append(failedTargets, symbol)
			continue
		}
		result := normalize.ValidateQuote(record, universeMap)
		record.Flags = append(record.Flags, result.Flags...)
		if !result.Valid {
			run.RecordsFailed++
			run.FailedSymbols = append(run.FailedSymbols, symbol+":"+strings.Join(result.Problems, ","))
			failedTargets = append(failedTargets, strings.ToUpper(symbol))
			continue
		}
		records = append(records, record)
	}
	run.RecordsSuccess = len(records)
	logf("validated records success=%d failed=%d", run.RecordsSuccess, run.RecordsFailed)
	if len(records) == 0 {
		return finish("failed", fmt.Errorf("no valid records synced"))
	}
	slices.SortFunc(records, func(a, b schema.DailyQuoteRecord) int { return strings.Compare(a.Symbol, b.Symbol) })
	daily := schema.DailyMarketFile{Market: strings.ToUpper(opt.Market), Date: opt.Date, Type: "eod", Source: []string{"massive"}, Count: len(records), SchemaVersion: 1, Records: records}
	dailyKey := storage.DailyKey(opt.Market, opt.Date)
	logf("checking daily object key=%s", dailyKey)
	if exists, _ := s.Store.Exists(ctx, dailyKey); exists {
		logf("overwriting existing daily object key=%s", dailyKey)
	} else {
		logf("creating daily object key=%s", dailyKey)
	}
	logf("writing daily object key=%s records=%d", dailyKey, len(records))
	if err := s.writeGzipJSON(ctx, dailyKey, daily); err != nil {
		return finish("failed", err)
	}

	logf("building latest snapshot")
	latest := normalize.BuildLatest(opt.Market, opt.Date, records, previous)
	for _, symbol := range failedTargets {
		if previous.Quotes == nil {
			continue
		}
		if quote, ok := previous.Quotes[symbol]; ok {
			quote.Stale = true
			latest.Quotes[symbol] = quote
		}
	}
	logf("writing latest snapshots quotes=%d staleCandidates=%d", len(latest.Quotes), len(failedTargets))
	if err := s.writeJSON(ctx, storage.LatestKey(opt.Market), latest); err != nil {
		return finish("failed", err)
	}
	if err := s.writeJSON(ctx, storage.LatestMinKey(opt.Market), normalize.BuildLatestMin(latest)); err != nil {
		return finish("failed", err)
	}
	r2Workers := r2WorkerCount(opt.R2Workers)
	logf("updating symbol histories records=%d r2Workers=%d", len(records), r2Workers)
	if err := s.updateHistories(ctx, opt.Market, records, r2Workers, logf); err != nil {
		return finish("failed", err)
	}
	if s.D1.Enabled() {
		logf("updating D1 latest rows=%d", len(latest.Quotes))
	}
	if err := s.writeLatestD1(ctx, opt.Market, latest); err != nil {
		logf("warning: update D1 latest failed: %v", err)
	}
	if run.RecordsFailed > 0 {
		return finish("partial", nil)
	}
	return finish("success", nil)
}

func (s Syncer) fetchDailyRecords(ctx context.Context, targets []string, date string, dateExplicit bool) (map[string]schema.DailyQuoteRecord, map[string]error, error) {
	fetched := map[string]schema.DailyQuoteRecord{}
	failures := map[string]error{}
	if bulk, ok := s.Quotes.(dailyBulkSource); ok {
		return bulk.FetchDailyBulk(ctx, targets, date)
	}
	if batcher, ok := s.Quotes.(dailyBatchSource); ok {
		const batchSize = 200
		for start := 0; start < len(targets); start += batchSize {
			end := start + batchSize
			if end > len(targets) {
				end = len(targets)
			}
			targetDate := date
			if !dateExplicit {
				targetDate = ""
			}
			records, batchFailures, err := batcher.FetchDailyBatch(ctx, targets[start:end], targetDate)
			if err != nil {
				return nil, nil, err
			}
			for symbol, record := range records {
				fetched[strings.ToUpper(symbol)] = record
			}
			for symbol, err := range batchFailures {
				failures[strings.ToUpper(symbol)] = err
			}
		}
		return fetched, failures, nil
	}
	for _, symbol := range targets {
		record, err := s.Quotes.FetchDaily(ctx, symbol, date)
		if err != nil {
			failures[strings.ToUpper(symbol)] = err
			continue
		}
		fetched[strings.ToUpper(symbol)] = record
	}
	return fetched, failures, nil
}

func inferDailyDate(records map[string]schema.DailyQuoteRecord) (string, bool) {
	counts := map[string]int{}
	bestDate := ""
	bestCount := 0
	for _, record := range records {
		counts[record.Date]++
		if counts[record.Date] > bestCount || (counts[record.Date] == bestCount && record.Date > bestDate) {
			bestDate = record.Date
			bestCount = counts[record.Date]
		}
	}
	return bestDate, bestDate != ""
}

func defaultMarketDate(now time.Time) string {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return now.UTC().Format("2006-01-02")
	}
	return now.In(loc).Format("2006-01-02")
}

func (s Syncer) updateHistories(ctx context.Context, market string, records []schema.DailyQuoteRecord, workers int, logf func(string, ...any)) error {
	total := len(records)
	if total == 0 {
		return nil
	}
	if workers < 1 {
		workers = 1
	}
	if workers > total {
		workers = total
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan schema.DailyQuoteRecord)
	errCh := make(chan error, 1)
	var completed int64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case r, ok := <-jobs:
					if !ok {
						return
					}
					if err := s.updateHistory(ctx, market, r); err != nil {
						select {
						case errCh <- err:
						default:
						}
						cancel()
						return
					}
					done := int(atomic.AddInt64(&completed, 1))
					if logf != nil && (done == 1 || done%1000 == 0 || done == total) {
						logf("history progress %d/%d current=%s", done, total, r.Symbol)
					}
				}
			}
		}()
	}

	var sendErr error
send:
	for _, r := range records {
		select {
		case <-ctx.Done():
			sendErr = ctx.Err()
			break send
		case jobs <- r:
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
	}
	return sendErr
}

func (s Syncer) updateHistory(ctx context.Context, market string, r schema.DailyQuoteRecord) error {
	key := storage.HistoryKey(market, r.Symbol)
	history := schema.SymbolHistory{Symbol: r.Symbol, Market: strings.ToUpper(market), SchemaVersion: 1}
	if raw, err := s.Store.Get(ctx, key); err == nil {
		_ = storage.UnmarshalMaybeGzip(raw, &history)
	}
	replaced := false
	for i := range history.Records {
		if history.Records[i].Date == r.Date {
			history.Records[i] = r
			replaced = true
			break
		}
	}
	if !replaced {
		history.Records = append(history.Records, r)
	}
	slices.SortFunc(history.Records, func(a, b schema.DailyQuoteRecord) int { return strings.Compare(a.Date, b.Date) })
	if err := s.writeGzipJSON(ctx, key, history); err != nil {
		return err
	}
	return nil
}

func (s Syncer) readLatest(ctx context.Context, market string) schema.LatestFull {
	var latest schema.LatestFull
	if raw, err := s.Store.Get(ctx, storage.LatestKey(market)); err == nil {
		_ = json.Unmarshal(raw, &latest)
	}
	return latest
}

func (s Syncer) writeJSON(ctx context.Context, key string, v any) error {
	raw, err := storage.MarshalJSON(v)
	if err != nil {
		return err
	}
	return s.Store.Put(ctx, key, raw, "application/json")
}

func (s Syncer) writeGzipJSON(ctx context.Context, key string, v any) error {
	raw, err := storage.MarshalGzipJSON(v)
	if err != nil {
		return err
	}
	return s.Store.Put(ctx, key, raw, "application/json")
}

func (s Syncer) writeRun(ctx context.Context, date string, run schema.IngestionRun) error {
	raw, err := storage.MarshalJSON(run)
	if err != nil {
		return err
	}
	return s.Store.Put(ctx, storage.RunKey(date), raw, "application/json")
}

func (s Syncer) writeSymbolsD1(ctx context.Context, market string, symbols []schema.SymbolInfo) error {
	const chunkSize = 100
	statements := make([]d1.Statement, 0, chunkSize)
	for _, sym := range symbols {
		statements = append(statements, d1.Statement{
			SQL:    `INSERT OR REPLACE INTO symbols (symbol, name, market, exchange, asset_type, is_etf, is_active, first_seen, last_seen, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, COALESCE((SELECT first_seen FROM symbols WHERE symbol = ?), date('now')), date('now'), datetime('now'))`,
			Params: []any{sym.Symbol, sym.Name, strings.ToUpper(market), sym.Exchange, sym.AssetType, boolInt(sym.IsEtf), boolInt(sym.IsActive), sym.Symbol},
		})
		if len(statements) == chunkSize {
			if err := s.D1.ExecBatch(ctx, statements); err != nil {
				return err
			}
			statements = statements[:0]
		}
	}
	return s.D1.ExecBatch(ctx, statements)
}

func (s Syncer) writeLatestD1(ctx context.Context, market string, latest schema.LatestFull) error {
	statements := make([]d1.Statement, 0, len(latest.Quotes))
	for symbol, q := range latest.Quotes {
		statements = append(statements, d1.Statement{
			SQL:    `INSERT OR REPLACE INTO latest_quotes (symbol, market, date, open, high, low, close, adj_close, volume, change, change_pct, source, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
			Params: []any{symbol, strings.ToUpper(market), latest.AsOf, q.Open, q.High, q.Low, q.Price, q.Price, q.Volume, q.Change, q.ChangePct, q.Source},
		})
	}
	return s.D1.ExecBatch(ctx, statements)
}

func (s Syncer) writeRunD1(ctx context.Context, run schema.IngestionRun) error {
	return s.D1.Exec(ctx, `INSERT OR REPLACE INTO ingestion_runs (id, market, date, status, source, records_total, records_success, records_failed, started_at, finished_at, error_message) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.Market, run.Date, run.Status, run.Source, run.RecordsTotal, run.RecordsSuccess, run.RecordsFailed, run.StartedAt, run.FinishedAt, run.ErrorMessage)
}

func (s Syncer) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now().UTC()
}

func displayWatchlist(path string) string {
	if path == "" {
		return "<all-active-symbols>"
	}
	return path
}

func r2WorkerCount(workers int) int {
	if workers > 0 {
		return workers
	}
	return 16
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func StorageFor(name, root string) (storage.ObjectStore, error) {
	switch name {
	case "", "local":
		return localStore(root), nil
	case "r2":
		return r2Store(), nil
	default:
		return nil, fmt.Errorf("unknown storage %q", name)
	}
}

var localStore = func(root string) storage.ObjectStore {
	return local.New(root)
}

var r2Store = func() storage.ObjectStore {
	return r2.FromEnv()
}
