package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"market-data-hub/internal/schema"
	"market-data-hub/internal/storage"
	"market-data-hub/internal/storage/local"
)

type fakeSymbols struct{}

func (fakeSymbols) FetchUniverse(context.Context) ([]schema.SymbolInfo, error) {
	return []schema.SymbolInfo{{Symbol: "NVDA", Name: "NVIDIA Corporation", Exchange: "NASDAQ", AssetType: "stock", IsActive: true}}, nil
}

type fakeQuotes struct{}

func (fakeQuotes) FetchDaily(_ context.Context, symbol, date string) (schema.DailyQuoteRecord, error) {
	return schema.DailyQuoteRecord{Symbol: symbol, Date: date, Open: 10, High: 12, Low: 9, Close: 11, AdjClose: 11, Volume: 100, Source: "stooq", UpdatedAt: "2026-06-01T00:00:00Z"}, nil
}

type partialQuotes struct{}

func (partialQuotes) FetchDaily(_ context.Context, symbol, date string) (schema.DailyQuoteRecord, error) {
	if symbol == "AAPL" {
		return schema.DailyQuoteRecord{}, os.ErrNotExist
	}
	return schema.DailyQuoteRecord{Symbol: symbol, Date: date, Open: 10, High: 12, Low: 9, Close: 11, AdjClose: 11, Volume: 100, Source: "stooq", UpdatedAt: "2026-06-01T00:00:00Z"}, nil
}

func TestSyncerRunLocal(t *testing.T) {
	root := t.TempDir()
	watchlist := filepath.Join(root, "watchlist.yaml")
	if err := os.WriteFile(watchlist, []byte("watchlist:\n  - NVDA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	syncer := Syncer{
		Store:   local.New(root),
		Symbols: fakeSymbols{},
		Quotes:  fakeQuotes{},
		Clock:   func() time.Time { return time.Date(2026, 5, 31, 1, 2, 3, 0, time.UTC) },
	}
	if err := syncer.Run(context.Background(), SyncOptions{Market: "us", Date: "2026-05-31", WatchlistPath: watchlist}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{storage.DailyKey("us", "2026-05-31"), storage.LatestKey("us"), storage.HistoryKey("us", "NVDA"), storage.RunKey("2026-05-31")} {
		exists, err := syncer.Store.Exists(context.Background(), key)
		if err != nil || !exists {
			t.Fatalf("expected %s to exist, exists=%v err=%v", key, exists, err)
		}
	}
}

func TestSyncerRunKeepsStaleLatestOnPartialFailure(t *testing.T) {
	root := t.TempDir()
	watchlist := filepath.Join(root, "watchlist.yaml")
	if err := os.WriteFile(watchlist, []byte("watchlist:\n  - NVDA\n  - AAPL\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := local.New(root)
	previous := schema.LatestFull{Market: "US", AsOf: "2026-05-30", SchemaVersion: 1, Quotes: map[string]schema.LatestQuote{"AAPL": {Price: 100, Date: "2026-05-30"}}}
	raw, err := storage.MarshalJSON(previous)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), storage.LatestKey("us"), raw, "application/json"); err != nil {
		t.Fatal(err)
	}
	syncer := Syncer{
		Store:   store,
		Symbols: fakeSymbols{},
		Quotes:  partialQuotes{},
		Clock:   func() time.Time { return time.Date(2026, 5, 31, 1, 2, 3, 0, time.UTC) },
	}
	if err := syncer.Run(context.Background(), SyncOptions{Market: "us", Date: "2026-05-31", WatchlistPath: watchlist}); err != nil {
		t.Fatal(err)
	}
	raw, err = store.Get(context.Background(), storage.LatestKey("us"))
	if err != nil {
		t.Fatal(err)
	}
	var latest schema.LatestFull
	if err := storage.UnmarshalMaybeGzip(raw, &latest); err != nil {
		t.Fatal(err)
	}
	if !latest.Quotes["AAPL"].Stale {
		t.Fatalf("expected AAPL to be marked stale: %+v", latest.Quotes["AAPL"])
	}
}

func TestInferDailyDate(t *testing.T) {
	date, ok := inferDailyDate(map[string]schema.DailyQuoteRecord{
		"NVDA": {Date: "2026-05-29"},
		"AAPL": {Date: "2026-05-29"},
		"MSFT": {Date: "2026-05-28"},
	})
	if !ok || date != "2026-05-29" {
		t.Fatalf("unexpected inferred date: %s ok=%v", date, ok)
	}
}
