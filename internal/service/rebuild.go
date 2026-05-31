package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"market-data-hub/internal/normalize"
	"market-data-hub/internal/schema"
	"market-data-hub/internal/storage"
)

type RebuildOptions struct {
	Market  string
	From    string
	To      string
	Symbols []string
}

func RebuildHistory(ctx context.Context, store storage.ObjectStore, opt RebuildOptions) error {
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
	filter := map[string]bool{}
	for _, sym := range opt.Symbols {
		if strings.TrimSpace(sym) != "" {
			filter[strings.ToUpper(strings.TrimSpace(sym))] = true
		}
	}
	bySymbol := map[string][]schema.DailyQuoteRecord{}
	var all []schema.DailyQuoteRecord
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		raw, err := store.Get(ctx, storage.DailyKey(opt.Market, date))
		if err != nil {
			continue
		}
		var daily schema.DailyMarketFile
		if err := storage.UnmarshalMaybeGzip(raw, &daily); err != nil {
			return fmt.Errorf("read %s: %w", date, err)
		}
		for _, r := range daily.Records {
			sym := strings.ToUpper(r.Symbol)
			if len(filter) > 0 && !filter[sym] {
				continue
			}
			bySymbol[sym] = append(bySymbol[sym], r)
			all = append(all, r)
		}
	}
	if len(all) == 0 {
		return fmt.Errorf("no daily records found in range")
	}
	for sym, records := range bySymbol {
		slices.SortFunc(records, func(a, b schema.DailyQuoteRecord) int { return strings.Compare(a.Date, b.Date) })
		history := schema.SymbolHistory{Symbol: sym, Market: strings.ToUpper(opt.Market), SchemaVersion: 1, Records: records}
		raw, err := storage.MarshalGzipJSON(history)
		if err != nil {
			return err
		}
		if err := store.Put(ctx, storage.HistoryKey(opt.Market, sym), raw, "application/json"); err != nil {
			return err
		}
	}
	latestDate := ""
	for _, r := range all {
		if r.Date > latestDate {
			latestDate = r.Date
		}
	}
	var latestRecords []schema.DailyQuoteRecord
	for _, r := range all {
		if r.Date == latestDate {
			latestRecords = append(latestRecords, r)
		}
	}
	latest := normalize.BuildLatest(opt.Market, latestDate, latestRecords, schema.LatestFull{})
	raw, err := storage.MarshalJSON(latest)
	if err != nil {
		return err
	}
	if err := store.Put(ctx, storage.LatestKey(opt.Market), raw, "application/json"); err != nil {
		return err
	}
	raw, err = storage.MarshalJSON(normalize.BuildLatestMin(latest))
	if err != nil {
		return err
	}
	if err := store.Put(ctx, storage.LatestMinKey(opt.Market), raw, "application/json"); err != nil {
		return err
	}
	run := schema.IngestionRun{
		ID:             opt.Market + "-rebuild-" + time.Now().UTC().Format("20060102T150405Z"),
		Market:         strings.ToUpper(opt.Market),
		Date:           latestDate,
		Status:         "success",
		Source:         "rebuild",
		RecordsTotal:   len(all),
		RecordsSuccess: len(all),
		StartedAt:      time.Now().UTC().Format(time.RFC3339),
		FinishedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	raw, err = storage.MarshalJSON(run)
	if err != nil {
		return err
	}
	return store.Put(ctx, storage.RunKey(latestDate), raw, "application/json")
}
