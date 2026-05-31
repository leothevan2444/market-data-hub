package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"market-data-hub/internal/schema"
	"market-data-hub/internal/storage"
	"market-data-hub/internal/storage/local"
)

type fakeBackfillSymbols struct{}

func (fakeBackfillSymbols) FetchUniverse(context.Context) ([]schema.SymbolInfo, error) {
	return []schema.SymbolInfo{
		{Symbol: "NVDA", Name: "NVIDIA Corporation", Exchange: "NASDAQ", AssetType: "stock", IsActive: true},
		{Symbol: "AAPL", Name: "Apple Inc.", Exchange: "NASDAQ", AssetType: "stock", IsActive: true},
		{Symbol: "MSFT", Name: "Microsoft Corporation", Exchange: "NASDAQ", AssetType: "stock", IsActive: true},
	}, nil
}

type fakeBackfillQuotes struct{}

func (fakeBackfillQuotes) FetchHistory(_ context.Context, symbol string) ([]schema.DailyQuoteRecord, error) {
	if symbol == "MSFT" {
		return nil, errors.New("source unavailable")
	}
	return []schema.DailyQuoteRecord{
		quote(symbol, "2024-06-03", 10),
		quote(symbol, "2024-06-04", 11),
	}, nil
}

func TestBackfillWritesDailyHistoryLatestAndPartialRun(t *testing.T) {
	root := t.TempDir()
	watchlist := filepath.Join(root, "watchlist.yaml")
	if err := os.WriteFile(watchlist, []byte("watchlist:\n  - NVDA\n  - AAPL\n  - MSFT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := local.New(root)
	backfiller := Backfiller{
		Store:   store,
		Symbols: fakeBackfillSymbols{},
		Quotes:  fakeBackfillQuotes{},
		Clock:   func() time.Time { return time.Date(2026, 6, 1, 1, 2, 3, 0, time.UTC) },
	}
	err := backfiller.Run(context.Background(), BackfillOptions{Market: "us", From: "2024-06-03", To: "2024-06-04", WatchlistPath: watchlist})
	if err != nil {
		t.Fatal(err)
	}

	var daily schema.DailyMarketFile
	readObject(t, store, storage.DailyKey("us", "2024-06-04"), &daily)
	if daily.Count != 2 {
		t.Fatalf("expected 2 daily records, got %d", daily.Count)
	}
	var history schema.SymbolHistory
	readObject(t, store, storage.HistoryKey("us", "NVDA"), &history)
	if len(history.Records) != 2 {
		t.Fatalf("expected 2 history records, got %d", len(history.Records))
	}
	var latest schema.LatestFull
	readObject(t, store, storage.LatestKey("us"), &latest)
	if latest.AsOf != "2024-06-04" || latest.Quotes["NVDA"].Price != 11 {
		t.Fatalf("unexpected latest: %+v", latest)
	}
	var run schema.IngestionRun
	readObject(t, store, "runs/2024/us-backfill-2024-06-03-2024-06-04-010203.json", &run)
	if run.Status != "partial" || run.RecordsSuccess != 2 || run.RecordsFailed != 1 {
		t.Fatalf("unexpected run: %+v", run)
	}
}

func TestMergeRecordsReplaceOnlyTargetRange(t *testing.T) {
	existing := []schema.DailyQuoteRecord{
		quote("NVDA", "2024-06-02", 8),
		quote("NVDA", "2024-06-03", 9),
		quote("AAPL", "2024-06-03", 99),
		quote("NVDA", "2024-06-05", 12),
	}
	incoming := []schema.DailyQuoteRecord{quote("NVDA", "2024-06-04", 11)}
	merged := mergeRecords(existing, incoming, true, "2024-06-03", "2024-06-04", map[string]bool{"NVDA": true})
	if len(merged) != 4 {
		t.Fatalf("expected 4 records, got %d: %+v", len(merged), merged)
	}
	for _, record := range merged {
		if record.Symbol == "NVDA" && record.Date == "2024-06-03" {
			t.Fatalf("replace should remove target symbol inside range when missing from incoming")
		}
		if record.Symbol == "AAPL" && record.Date == "2024-06-03" && record.Close != 99 {
			t.Fatalf("replace should keep other symbols: %+v", record)
		}
	}
}

func readObject(t *testing.T, store local.Store, key string, out any) {
	t.Helper()
	raw, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.UnmarshalMaybeGzip(raw, out); err != nil {
		t.Fatal(err)
	}
}

func quote(symbol, date string, close float64) schema.DailyQuoteRecord {
	return schema.DailyQuoteRecord{
		Symbol:   symbol,
		Date:     date,
		Open:     close - 1,
		High:     close + 1,
		Low:      close - 2,
		Close:    close,
		AdjClose: close,
		Volume:   100,
		Currency: "USD",
		Source:   "stooq",
	}
}
