package service

import (
	"fmt"
	"os"

	"market-data-hub/internal/normalize"
	"market-data-hub/internal/schema"
	"market-data-hub/internal/storage"
)

type ValidationSummary struct {
	Kind     string
	Total    int
	Valid    int
	Invalid  int
	Warnings int
}

func ValidateFile(path string) (ValidationSummary, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ValidationSummary{}, err
	}
	var daily schema.DailyMarketFile
	if err := storage.UnmarshalMaybeGzip(raw, &daily); err == nil && daily.Type == "eod" {
		return validateRecords("daily", daily.Records), nil
	}
	var history schema.SymbolHistory
	if err := storage.UnmarshalMaybeGzip(raw, &history); err == nil && history.Symbol != "" {
		return validateRecords("history", history.Records), nil
	}
	var latest schema.LatestFull
	if err := storage.UnmarshalMaybeGzip(raw, &latest); err == nil && latest.Quotes != nil {
		return ValidationSummary{Kind: "latest", Total: len(latest.Quotes), Valid: len(latest.Quotes)}, nil
	}
	return ValidationSummary{}, fmt.Errorf("unsupported or invalid market-data file")
}

func validateRecords(kind string, records []schema.DailyQuoteRecord) ValidationSummary {
	summary := ValidationSummary{Kind: kind, Total: len(records)}
	for _, r := range records {
		res := normalize.ValidateQuote(r, nil)
		if res.Valid {
			summary.Valid++
		} else {
			summary.Invalid++
		}
		summary.Warnings += len(res.Flags)
	}
	return summary
}
