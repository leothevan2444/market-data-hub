package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"market-data-hub/internal/normalize"
	"market-data-hub/internal/schema"
	"market-data-hub/internal/source/nasdaqtrader"
	"market-data-hub/internal/source/stooq"
	"market-data-hub/internal/storage"
	"market-data-hub/internal/storage/d1"
)

type BackfillOptions struct {
	Market        string
	From          string
	To            string
	Symbols       []string
	SymbolsFile   string
	WatchlistPath string
	All           bool
	Replace       bool
	Offset        int
	BatchSize     int
	Sleep         time.Duration
	MaxRetries    int
	RetryBackoff  time.Duration
}

type Backfiller struct {
	Store   storage.ObjectStore
	D1      d1.Client
	Symbols interface {
		FetchUniverse(context.Context) ([]schema.SymbolInfo, error)
	}
	Quotes interface {
		FetchHistory(context.Context, string) ([]schema.DailyQuoteRecord, error)
	}
	Clock func() time.Time
}

func NewBackfiller(store storage.ObjectStore, d1Client d1.Client) Backfiller {
	return Backfiller{
		Store:   store,
		D1:      d1Client,
		Symbols: nasdaqtrader.New(),
		Quotes:  stooq.New(),
		Clock:   func() time.Time { return time.Now().UTC() },
	}
}

func (b Backfiller) Run(ctx context.Context, opt BackfillOptions) error {
	if opt.Market == "" {
		opt.Market = "us"
	}
	if err := normalize.RequireDate(opt.From); err != nil {
		return err
	}
	if err := normalize.RequireDate(opt.To); err != nil {
		return err
	}
	start, _ := time.Parse("2006-01-02", opt.From)
	end, _ := time.Parse("2006-01-02", opt.To)
	if end.Before(start) {
		return fmt.Errorf("to date must be on or after from date")
	}

	run := schema.IngestionRun{
		ID:        fmt.Sprintf("%s-backfill-%s-%s-%s", strings.ToLower(opt.Market), opt.From, opt.To, b.now().Format("150405")),
		Market:    strings.ToUpper(opt.Market),
		Date:      opt.To,
		Status:    "running",
		Source:    "stooq-backfill",
		StartedAt: b.now().Format(time.RFC3339),
	}
	finish := func(status string, err error) error {
		run.Status = status
		run.FinishedAt = b.now().Format(time.RFC3339)
		if err != nil {
			run.ErrorMessage = err.Error()
		}
		_ = b.writeRun(ctx, opt.To, run)
		_ = b.writeRunD1(ctx, run)
		return err
	}

	universe, err := b.Symbols.FetchUniverse(ctx)
	if err != nil {
		return finish("failed", fmt.Errorf("fetch universe: %w", err))
	}
	if err := b.writeUniverse(ctx, opt, universe); err != nil {
		return finish("failed", err)
	}
	_ = b.writeSymbolsD1(ctx, opt.Market, universe)

	targets, err := resolveBackfillTargets(opt, universe)
	if err != nil {
		return finish("failed", err)
	}
	targets = sliceBackfillTargets(targets, opt.Offset, opt.BatchSize)
	run.RecordsTotal = len(targets)
	if len(targets) == 0 {
		return finish("failed", fmt.Errorf("no backfill targets resolved"))
	}

	universeMap := normalize.UniverseMap(universe)
	bySymbol := map[string][]schema.DailyQuoteRecord{}
	byDate := map[string][]schema.DailyQuoteRecord{}
	for i, symbol := range targets {
		if opt.Sleep > 0 && i > 0 {
			if err := sleepContext(ctx, opt.Sleep); err != nil {
				return finish("failed", err)
			}
		}
		records, err := b.fetchHistoryWithRetry(ctx, symbol, opt.MaxRetries, opt.RetryBackoff)
		if err != nil {
			run.RecordsFailed++
			run.FailedSymbols = append(run.FailedSymbols, symbol+":"+err.Error())
			continue
		}
		valid, problems := filterBackfillRecords(records, start, end, universeMap)
		if len(problems) > 0 {
			run.FailedSymbols = append(run.FailedSymbols, symbol+":"+strings.Join(problems, ","))
		}
		if len(valid) == 0 {
			run.RecordsFailed++
			if len(problems) == 0 {
				run.FailedSymbols = append(run.FailedSymbols, symbol+":no valid records in range")
			}
			continue
		}
		run.RecordsSuccess++
		bySymbol[strings.ToUpper(symbol)] = valid
		for _, record := range valid {
			byDate[record.Date] = append(byDate[record.Date], record)
		}
	}
	if len(bySymbol) == 0 {
		return finish("failed", fmt.Errorf("no symbols produced valid backfill records"))
	}

	if err := b.writeHistories(ctx, opt.Market, bySymbol, opt.Replace, opt.From, opt.To); err != nil {
		return finish("failed", fmt.Errorf("write histories: %w", err))
	}
	if err := b.writeDailyFiles(ctx, opt.Market, byDate, opt.Replace); err != nil {
		return finish("failed", fmt.Errorf("write daily files: %w", err))
	}
	latest, err := b.writeLatest(ctx, opt.Market, bySymbol)
	if err != nil {
		return finish("failed", fmt.Errorf("write latest: %w", err))
	}
	if err := b.writeLatestD1(ctx, opt.Market, latest); err != nil {
		return finish("failed", fmt.Errorf("write latest D1: %w", err))
	}

	if run.RecordsFailed > 0 {
		return finish("partial", nil)
	}
	return finish("success", nil)
}

func resolveBackfillTargets(opt BackfillOptions, universe []schema.SymbolInfo) ([]string, error) {
	seen := map[string]bool{}
	add := func(symbol string, out *[]string) {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" || seen[symbol] {
			return
		}
		seen[symbol] = true
		*out = append(*out, symbol)
	}
	var targets []string
	for _, symbol := range opt.Symbols {
		add(symbol, &targets)
	}
	if len(targets) == 0 && opt.SymbolsFile != "" {
		fileSymbols, err := LoadSymbolList(opt.SymbolsFile)
		if err != nil {
			return nil, err
		}
		for _, symbol := range fileSymbols {
			add(symbol, &targets)
		}
	}
	if len(targets) == 0 && opt.All {
		for _, symbol := range universe {
			if symbol.IsActive {
				add(symbol.Symbol, &targets)
			}
		}
	}
	if len(targets) == 0 && opt.WatchlistPath != "" {
		watchlist, err := LoadWatchlist(opt.WatchlistPath)
		if err != nil {
			return nil, err
		}
		for _, symbol := range watchlist {
			add(symbol, &targets)
		}
	}
	return targets, nil
}

func LoadSymbolList(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var symbols []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, part := range strings.Split(line, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				symbols = append(symbols, strings.ToUpper(part))
			}
		}
	}
	return symbols, scanner.Err()
}

func sliceBackfillTargets(targets []string, offset, batchSize int) []string {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(targets) {
		return nil
	}
	targets = targets[offset:]
	if batchSize > 0 && batchSize < len(targets) {
		return targets[:batchSize]
	}
	return targets
}

func (b Backfiller) fetchHistoryWithRetry(ctx context.Context, symbol string, maxRetries int, backoff time.Duration) ([]schema.DailyQuoteRecord, error) {
	if maxRetries < 0 {
		maxRetries = 0
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		records, err := b.Quotes.FetchHistory(ctx, symbol)
		if err == nil {
			return records, nil
		}
		lastErr = err
		if attempt == maxRetries {
			break
		}
		delay := backoff
		if delay <= 0 {
			delay = time.Second
		}
		delay *= time.Duration(attempt + 1)
		if err := sleepContext(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func filterBackfillRecords(records []schema.DailyQuoteRecord, start, end time.Time, universe map[string]schema.SymbolInfo) ([]schema.DailyQuoteRecord, []string) {
	var valid []schema.DailyQuoteRecord
	var problems []string
	for _, record := range records {
		date, err := time.Parse("2006-01-02", record.Date)
		if err != nil || date.Before(start) || date.After(end) {
			continue
		}
		res := normalize.ValidateQuote(record, universe)
		record.Flags = append(record.Flags, res.Flags...)
		if !res.Valid {
			problems = append(problems, record.Date+":"+strings.Join(res.Problems, "|"))
			continue
		}
		valid = append(valid, record)
	}
	slices.SortFunc(valid, func(a, b schema.DailyQuoteRecord) int { return strings.Compare(a.Date, b.Date) })
	return valid, problems
}

func (b Backfiller) writeHistories(ctx context.Context, market string, bySymbol map[string][]schema.DailyQuoteRecord, replace bool, from, to string) error {
	for symbol, records := range bySymbol {
		key := storage.HistoryKey(market, symbol)
		history := schema.SymbolHistory{Symbol: symbol, Market: strings.ToUpper(market), SchemaVersion: 1}
		if raw, err := b.Store.Get(ctx, key); err == nil {
			if err := storage.UnmarshalMaybeGzip(raw, &history); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s: %w", key, err)
		}
		merged := mergeRecords(history.Records, records, replace, from, to, map[string]bool{symbol: true})
		history = schema.SymbolHistory{Symbol: symbol, Market: strings.ToUpper(market), SchemaVersion: 1, Records: merged}
		if err := b.writeGzipJSON(ctx, key, history); err != nil {
			return err
		}
	}
	return nil
}

func (b Backfiller) writeDailyFiles(ctx context.Context, market string, byDate map[string][]schema.DailyQuoteRecord, replace bool) error {
	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	slices.Sort(dates)
	for _, date := range dates {
		key := storage.DailyKey(market, date)
		daily := schema.DailyMarketFile{Market: strings.ToUpper(market), Date: date, Type: "eod", Source: []string{"stooq"}, SchemaVersion: 1}
		if raw, err := b.Store.Get(ctx, key); err == nil {
			if err := storage.UnmarshalMaybeGzip(raw, &daily); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s: %w", key, err)
		}
		targetSymbols := map[string]bool{}
		for _, record := range byDate[date] {
			targetSymbols[strings.ToUpper(record.Symbol)] = true
		}
		daily.Records = mergeRecords(daily.Records, byDate[date], replace, date, date, targetSymbols)
		daily.Count = len(daily.Records)
		daily.Market = strings.ToUpper(market)
		daily.Date = date
		daily.Type = "eod"
		daily.SchemaVersion = 1
		if len(daily.Source) == 0 {
			daily.Source = []string{"stooq"}
		}
		if err := b.writeGzipJSON(ctx, key, daily); err != nil {
			return err
		}
	}
	return nil
}

func (b Backfiller) writeLatest(ctx context.Context, market string, bySymbol map[string][]schema.DailyQuoteRecord) (schema.LatestFull, error) {
	latest := b.readLatest(ctx, market)
	if latest.Quotes == nil {
		latest.Quotes = map[string]schema.LatestQuote{}
	}
	latest.Market = strings.ToUpper(market)
	latest.SchemaVersion = 1
	for symbol := range bySymbol {
		history, err := b.readHistory(ctx, market, symbol)
		if err != nil {
			return schema.LatestFull{}, err
		}
		if len(history.Records) == 0 {
			continue
		}
		last := history.Records[len(history.Records)-1]
		prevClose := 0.0
		if len(history.Records) > 1 {
			prevClose = history.Records[len(history.Records)-2].Close
		}
		change := 0.0
		changePct := 0.0
		if prevClose > 0 {
			change = round4(last.Close - prevClose)
			changePct = round4((change / prevClose) * 100)
		}
		latest.Quotes[strings.ToUpper(symbol)] = schema.LatestQuote{
			Price:     last.Close,
			Change:    change,
			ChangePct: changePct,
			Volume:    last.Volume,
			Open:      last.Open,
			High:      last.High,
			Low:       last.Low,
			PrevClose: prevClose,
			Date:      last.Date,
			Source:    last.Source,
			Flags:     last.Flags,
		}
		if last.Date > latest.AsOf {
			latest.AsOf = last.Date
		}
	}
	if latest.AsOf == "" {
		return schema.LatestFull{}, fmt.Errorf("no latest records produced")
	}
	if err := b.writeJSON(ctx, storage.LatestKey(market), latest); err != nil {
		return schema.LatestFull{}, err
	}
	if err := b.writeJSON(ctx, storage.LatestMinKey(market), normalize.BuildLatestMin(latest)); err != nil {
		return schema.LatestFull{}, err
	}
	return latest, nil
}

func mergeRecords(existing, incoming []schema.DailyQuoteRecord, replace bool, from, to string, targetSymbols map[string]bool) []schema.DailyQuoteRecord {
	out := map[string]schema.DailyQuoteRecord{}
	for _, record := range existing {
		symbol := strings.ToUpper(record.Symbol)
		inRange := record.Date >= from && record.Date <= to
		if replace && inRange && targetSymbols[symbol] {
			continue
		}
		out[symbol+"|"+record.Date] = record
	}
	for _, record := range incoming {
		record.Symbol = strings.ToUpper(record.Symbol)
		out[record.Symbol+"|"+record.Date] = record
	}
	records := make([]schema.DailyQuoteRecord, 0, len(out))
	for _, record := range out {
		records = append(records, record)
	}
	slices.SortFunc(records, func(a, b schema.DailyQuoteRecord) int {
		if a.Date == b.Date {
			return strings.Compare(a.Symbol, b.Symbol)
		}
		return strings.Compare(a.Date, b.Date)
	})
	return records
}

func (b Backfiller) readHistory(ctx context.Context, market, symbol string) (schema.SymbolHistory, error) {
	history := schema.SymbolHistory{Symbol: strings.ToUpper(symbol), Market: strings.ToUpper(market), SchemaVersion: 1}
	raw, err := b.Store.Get(ctx, storage.HistoryKey(market, symbol))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return history, nil
		}
		return history, err
	}
	if err := storage.UnmarshalMaybeGzip(raw, &history); err != nil {
		return history, err
	}
	slices.SortFunc(history.Records, func(a, b schema.DailyQuoteRecord) int { return strings.Compare(a.Date, b.Date) })
	return history, nil
}

func (b Backfiller) readLatest(ctx context.Context, market string) schema.LatestFull {
	var latest schema.LatestFull
	if raw, err := b.Store.Get(ctx, storage.LatestKey(market)); err == nil {
		_ = json.Unmarshal(raw, &latest)
	}
	return latest
}

func (b Backfiller) writeUniverse(ctx context.Context, opt BackfillOptions, symbols []schema.SymbolInfo) error {
	universe := schema.SymbolUniverse{Date: opt.To, Market: strings.ToUpper(opt.Market), Source: "nasdaqtrader", Symbols: symbols}
	if err := b.writeJSON(ctx, storage.SymbolsLatestKey(opt.Market), universe); err != nil {
		return err
	}
	return b.writeJSON(ctx, storage.SymbolsDatedKey(opt.Market, opt.To), universe)
}

func (b Backfiller) writeJSON(ctx context.Context, key string, v any) error {
	raw, err := storage.MarshalJSON(v)
	if err != nil {
		return err
	}
	return b.Store.Put(ctx, key, raw, "application/json")
}

func (b Backfiller) writeGzipJSON(ctx context.Context, key string, v any) error {
	raw, err := storage.MarshalGzipJSON(v)
	if err != nil {
		return err
	}
	return b.Store.Put(ctx, key, raw, "application/json")
}

func (b Backfiller) writeRun(ctx context.Context, date string, run schema.IngestionRun) error {
	raw, err := storage.MarshalJSON(run)
	if err != nil {
		return err
	}
	return b.Store.Put(ctx, storage.BackfillRunKey(date, run.ID), raw, "application/json")
}

func (b Backfiller) writeSymbolsD1(ctx context.Context, market string, symbols []schema.SymbolInfo) error {
	syncer := Syncer{D1: b.D1}
	return syncer.writeSymbolsD1(ctx, market, symbols)
}

func (b Backfiller) writeLatestD1(ctx context.Context, market string, latest schema.LatestFull) error {
	syncer := Syncer{D1: b.D1}
	return syncer.writeLatestD1(ctx, market, latest)
}

func (b Backfiller) writeRunD1(ctx context.Context, run schema.IngestionRun) error {
	syncer := Syncer{D1: b.D1}
	return syncer.writeRunD1(ctx, run)
}

func (b Backfiller) now() time.Time {
	if b.Clock != nil {
		return b.Clock()
	}
	return time.Now().UTC()
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
