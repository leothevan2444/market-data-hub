package normalize

import (
	"testing"

	"market-data-hub/internal/schema"
)

func TestValidateQuote(t *testing.T) {
	record := schema.DailyQuoteRecord{Symbol: "NVDA", Date: "2026-05-31", Open: 10, High: 12, Low: 9, Close: 11, AdjClose: 11, Volume: 0, Source: "stooq"}
	result := ValidateQuote(record, map[string]schema.SymbolInfo{"NVDA": {Symbol: "NVDA"}})
	if !result.Valid {
		t.Fatalf("expected valid quote, got problems %v", result.Problems)
	}
	if len(result.Flags) != 1 || result.Flags[0] != "low_quality" {
		t.Fatalf("expected low_quality flag, got %v", result.Flags)
	}
}

func TestBuildLatestComputesChange(t *testing.T) {
	previous := schema.LatestFull{Quotes: map[string]schema.LatestQuote{"NVDA": {Price: 100}}}
	latest := BuildLatest("us", "2026-05-31", []schema.DailyQuoteRecord{
		{Symbol: "NVDA", Date: "2026-05-31", Open: 100, High: 111, Low: 99, Close: 110, AdjClose: 110, Volume: 10, Source: "stooq"},
	}, previous)
	q := latest.Quotes["NVDA"]
	if q.Change != 10 || q.ChangePct != 10 {
		t.Fatalf("unexpected change values: %+v", q)
	}
}
