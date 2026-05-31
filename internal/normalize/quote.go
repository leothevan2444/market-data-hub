package normalize

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"market-data-hub/internal/schema"
)

type ValidationResult struct {
	Valid    bool
	Flags    []string
	Problems []string
}

func ValidateQuote(q schema.DailyQuoteRecord, universe map[string]schema.SymbolInfo) ValidationResult {
	var res ValidationResult
	if strings.TrimSpace(q.Symbol) == "" {
		res.Problems = append(res.Problems, "symbol_empty")
	}
	if _, err := time.Parse("2006-01-02", q.Date); err != nil {
		res.Problems = append(res.Problems, "date_invalid")
	}
	if q.Open <= 0 || q.High <= 0 || q.Low <= 0 || q.Close <= 0 {
		res.Problems = append(res.Problems, "price_non_positive")
	}
	if q.High < q.Low {
		res.Problems = append(res.Problems, "high_below_low")
	}
	if q.Close > 0 && q.High > 0 && q.Low > 0 && (q.Close > q.High || q.Close < q.Low) {
		res.Problems = append(res.Problems, "close_out_of_range")
	}
	if q.Volume < 0 {
		res.Problems = append(res.Problems, "volume_negative")
	}
	if q.Source == "" {
		res.Problems = append(res.Problems, "source_empty")
	}
	if q.Volume == 0 {
		res.Flags = append(res.Flags, "low_quality")
	}
	if len(universe) > 0 {
		if _, ok := universe[strings.ToUpper(q.Symbol)]; !ok {
			res.Flags = append(res.Flags, "unknown_symbol")
		}
	}
	res.Valid = len(res.Problems) == 0
	return res
}

func BuildLatest(market, date string, records []schema.DailyQuoteRecord, previous schema.LatestFull) schema.LatestFull {
	quotes := make(map[string]schema.LatestQuote, len(records))
	for _, r := range records {
		prevClose := 0.0
		if previous.Quotes != nil {
			prevClose = previous.Quotes[strings.ToUpper(r.Symbol)].Price
		}
		change := 0.0
		changePct := 0.0
		if prevClose > 0 {
			change = round(r.Close - prevClose)
			changePct = round((change / prevClose) * 100)
		}
		flags := slices.Clone(r.Flags)
		if math.Abs(changePct) > 50 {
			flags = append(flags, "abnormal")
		}
		quotes[strings.ToUpper(r.Symbol)] = schema.LatestQuote{
			Price:     r.Close,
			Change:    change,
			ChangePct: changePct,
			Volume:    r.Volume,
			Open:      r.Open,
			High:      r.High,
			Low:       r.Low,
			PrevClose: prevClose,
			Date:      r.Date,
			Source:    r.Source,
			Flags:     flags,
		}
	}
	return schema.LatestFull{Market: strings.ToUpper(market), AsOf: date, SchemaVersion: 1, Quotes: quotes}
}

func BuildLatestMin(full schema.LatestFull) schema.LatestMinified {
	out := schema.LatestMinified{
		Market: full.Market,
		AsOf:   full.AsOf,
		Schema: []string{"price", "changePct", "volume"},
		Quotes: map[string][]float64{},
	}
	for sym, q := range full.Quotes {
		out.Quotes[sym] = []float64{q.Price, q.ChangePct, float64(q.Volume)}
	}
	return out
}

func UniverseMap(symbols []schema.SymbolInfo) map[string]schema.SymbolInfo {
	out := make(map[string]schema.SymbolInfo, len(symbols))
	for _, s := range symbols {
		out[strings.ToUpper(s.Symbol)] = s
	}
	return out
}

func RequireDate(date string) error {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return fmt.Errorf("date must be YYYY-MM-DD: %w", err)
	}
	return nil
}

func round(v float64) float64 {
	return math.Round(v*10000) / 10000
}
