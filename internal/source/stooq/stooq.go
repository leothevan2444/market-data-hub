package stooq

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"market-data-hub/internal/schema"
)

type Client struct {
	BaseURL  string
	QuoteURL string
	APIKey   string
	HTTP     *http.Client
}

func New() Client {
	return Client{BaseURL: "https://stooq.com/q/d/l/", QuoteURL: "https://stooq.com/q/l/", APIKey: os.Getenv("STOOQ_API_KEY"), HTTP: http.DefaultClient}
}

func (c Client) FetchDaily(ctx context.Context, symbol, date string) (schema.DailyQuoteRecord, error) {
	records, failures, err := c.FetchDailyBatch(ctx, []string{symbol}, date)
	if err != nil {
		return schema.DailyQuoteRecord{}, err
	}
	symbol = strings.ToUpper(symbol)
	if err := failures[symbol]; err != nil {
		return schema.DailyQuoteRecord{}, err
	}
	record, ok := records[symbol]
	if !ok {
		return schema.DailyQuoteRecord{}, fmt.Errorf("no stooq quote for %s", symbol)
	}
	return record, nil
}

func (c Client) FetchDailyBatch(ctx context.Context, symbols []string, date string) (map[string]schema.DailyQuoteRecord, map[string]error, error) {
	if len(symbols) == 0 {
		return map[string]schema.DailyQuoteRecord{}, map[string]error{}, nil
	}
	u, err := url.Parse(c.quoteURL())
	if err != nil {
		return nil, nil, err
	}
	stooqSymbols := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		stooqSymbols = append(stooqSymbols, strings.ToLower(strings.TrimSpace(symbol))+".us")
	}
	q := u.Query()
	q.Set("s", strings.Join(stooqSymbols, " "))
	q.Set("f", "sd2t2ohlcv")
	q.Set("h", "")
	q.Set("e", "csv")
	if c.APIKey != "" {
		q.Set("apikey", c.APIKey)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	res, err := c.client().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("stooq quote batch: %s", res.Status)
	}
	return ParseQuoteCSV(res.Body, date)
}

func (c Client) FetchHistory(ctx context.Context, symbol string) ([]schema.DailyQuoteRecord, error) {
	u, err := url.Parse(c.baseURL())
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("s", strings.ToLower(symbol)+".us")
	q.Set("i", "d")
	if c.APIKey != "" {
		q.Set("apikey", c.APIKey)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("stooq %s: %s", symbol, res.Status)
	}
	records, err := ParseCSV(res.Body, strings.ToUpper(symbol), "")
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("no stooq records for %s", symbol)
	}
	return records, nil
}

func ParseCSV(r io.Reader, symbol, targetDate string) ([]schema.DailyQuoteRecord, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, nil
	}
	idx := map[string]int{}
	for i, h := range rows[0] {
		idx[strings.ToLower(h)] = i
	}
	for _, required := range []string{"date", "open", "high", "low", "close", "volume"} {
		if _, ok := idx[required]; !ok {
			return nil, fmt.Errorf("unexpected stooq csv header %q; if the response asks for an apikey, set STOOQ_API_KEY", strings.Join(rows[0], ","))
		}
	}
	var out []schema.DailyQuoteRecord
	for _, row := range rows[1:] {
		if len(row) < len(rows[0]) {
			continue
		}
		d := row[idx["date"]]
		if targetDate != "" && d != targetDate {
			continue
		}
		open, err := parseFloat(row, idx, "open")
		if err != nil {
			return nil, err
		}
		high, err := parseFloat(row, idx, "high")
		if err != nil {
			return nil, err
		}
		low, err := parseFloat(row, idx, "low")
		if err != nil {
			return nil, err
		}
		closePrice, err := parseFloat(row, idx, "close")
		if err != nil {
			return nil, err
		}
		volume, err := parseInt(row, idx, "volume")
		if err != nil {
			return nil, err
		}
		out = append(out, schema.DailyQuoteRecord{
			Symbol:    strings.ToUpper(symbol),
			Date:      d,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			AdjClose:  closePrice,
			Volume:    volume,
			Currency:  "USD",
			Source:    "stooq",
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

func ParseQuoteCSV(r io.Reader, targetDate string) (map[string]schema.DailyQuoteRecord, map[string]error, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(rows) < 2 {
		return nil, nil, fmt.Errorf("empty stooq quote csv")
	}
	idx := map[string]int{}
	for i, h := range rows[0] {
		idx[strings.ToLower(h)] = i
	}
	for _, required := range []string{"symbol", "date", "open", "high", "low", "close", "volume"} {
		if _, ok := idx[required]; !ok {
			return nil, nil, fmt.Errorf("unexpected stooq quote csv header %q", strings.Join(rows[0], ","))
		}
	}
	records := map[string]schema.DailyQuoteRecord{}
	failures := map[string]error{}
	for _, row := range rows[1:] {
		if len(row) < len(rows[0]) {
			continue
		}
		rawSymbol := row[idx["symbol"]]
		symbol := strings.ToUpper(strings.TrimSuffix(rawSymbol, ".US"))
		if symbol == "" {
			continue
		}
		if strings.EqualFold(row[idx["date"]], "N/D") {
			failures[symbol] = fmt.Errorf("stooq quote unavailable")
			continue
		}
		if targetDate != "" && row[idx["date"]] != targetDate {
			failures[symbol] = fmt.Errorf("stooq quote date %s does not match target date %s", row[idx["date"]], targetDate)
			continue
		}
		open, err := parseFloat(row, idx, "open")
		if err != nil {
			failures[symbol] = err
			continue
		}
		high, err := parseFloat(row, idx, "high")
		if err != nil {
			failures[symbol] = err
			continue
		}
		low, err := parseFloat(row, idx, "low")
		if err != nil {
			failures[symbol] = err
			continue
		}
		closePrice, err := parseFloat(row, idx, "close")
		if err != nil {
			failures[symbol] = err
			continue
		}
		volume, err := parseInt(row, idx, "volume")
		if err != nil {
			failures[symbol] = err
			continue
		}
		records[symbol] = schema.DailyQuoteRecord{
			Symbol:    symbol,
			Date:      row[idx["date"]],
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			AdjClose:  closePrice,
			Volume:    volume,
			Currency:  "USD",
			Source:    "stooq",
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
	}
	return records, failures, nil
}

func parseFloat(row []string, idx map[string]int, name string) (float64, error) {
	i, ok := idx[name]
	if !ok {
		return 0, fmt.Errorf("missing %s column", name)
	}
	return strconv.ParseFloat(row[i], 64)
}

func parseInt(row []string, idx map[string]int, name string) (int64, error) {
	i, ok := idx[name]
	if !ok {
		return 0, fmt.Errorf("missing %s column", name)
	}
	if value, err := strconv.ParseInt(row[i], 10, 64); err == nil {
		return value, nil
	}
	value, err := strconv.ParseFloat(row[i], 64)
	if err != nil {
		return 0, err
	}
	return int64(value), nil
}

func (c Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://stooq.com/q/d/l/"
}

func (c Client) quoteURL() string {
	if c.QuoteURL != "" {
		return c.QuoteURL
	}
	return "https://stooq.com/q/l/"
}

func (c Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}
