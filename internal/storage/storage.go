package storage

import (
	"context"
	"fmt"
	"strings"
)

type ObjectStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Exists(ctx context.Context, key string) (bool, error)
}

func DailyKey(market, date string) string {
	year := date
	if len(date) >= 4 {
		year = date[:4]
	}
	return fmt.Sprintf("daily/%s/%s/%s.json.gz", strings.ToLower(market), year, date)
}

func LatestKey(market string) string {
	return fmt.Sprintf("latest/%s/latest.json", strings.ToLower(market))
}

func LatestMinKey(market string) string {
	return fmt.Sprintf("latest/%s/latest.min.json", strings.ToLower(market))
}

func HistoryKey(market, symbol string) string {
	return fmt.Sprintf("history/%s/%s/daily.json.gz", strings.ToLower(market), strings.ToUpper(symbol))
}

func SymbolsLatestKey(market string) string {
	return fmt.Sprintf("symbols/%s/latest.json", strings.ToLower(market))
}

func SymbolsDatedKey(market, date string) string {
	year := date
	if len(date) >= 4 {
		year = date[:4]
	}
	return fmt.Sprintf("symbols/%s/%s/%s.json", strings.ToLower(market), year, date)
}

func RunKey(date string) string {
	year := date
	if len(date) >= 4 {
		year = date[:4]
	}
	return fmt.Sprintf("runs/%s/%s.json", year, date)
}

func BackfillRunKey(date, id string) string {
	year := date
	if len(date) >= 4 {
		year = date[:4]
	}
	return fmt.Sprintf("runs/%s/%s.json", year, id)
}
