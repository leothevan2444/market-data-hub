package massive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"market-data-hub/internal/schema"
)

const sourceName = "massive"

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	Clock   func() time.Time
}

type groupedDailyResponse struct {
	Status       string               `json:"status"`
	Adjusted     bool                 `json:"adjusted"`
	QueryCount   int                  `json:"queryCount"`
	ResultsCount int                  `json:"resultsCount"`
	RequestID    string               `json:"request_id"`
	Results      []groupedDailyResult `json:"results"`
	Error        string               `json:"error"`
	Message      string               `json:"message"`
}

type groupedDailyResult struct {
	Symbol       string  `json:"T"`
	Close        float64 `json:"c"`
	High         float64 `json:"h"`
	Low          float64 `json:"l"`
	Open         float64 `json:"o"`
	TimestampMS  int64   `json:"t"`
	Volume       float64 `json:"v"`
	Transactions int64   `json:"n"`
	VWAP         float64 `json:"vw"`
	OTC          bool    `json:"otc"`
}

func New() Client {
	return Client{
		BaseURL: "https://api.massive.com",
		APIKey:  os.Getenv("MASSIVE_API_KEY"),
		HTTP:    http.DefaultClient,
		Clock:   func() time.Time { return time.Now().UTC() },
	}
}

func (c Client) FetchDaily(ctx context.Context, symbol, date string) (schema.DailyQuoteRecord, error) {
	records, failures, err := c.FetchDailyBulk(ctx, []string{symbol}, date)
	if err != nil {
		return schema.DailyQuoteRecord{}, err
	}
	symbol = strings.ToUpper(symbol)
	if err := failures[symbol]; err != nil {
		return schema.DailyQuoteRecord{}, err
	}
	record, ok := records[symbol]
	if !ok {
		return schema.DailyQuoteRecord{}, fmt.Errorf("no massive quote for %s", symbol)
	}
	return record, nil
}

func (c Client) FetchDailyBulk(ctx context.Context, symbols []string, date string) (map[string]schema.DailyQuoteRecord, map[string]error, error) {
	if len(symbols) == 0 {
		return map[string]schema.DailyQuoteRecord{}, map[string]error{}, nil
	}
	allRecords, err := c.FetchDailyMarket(ctx, date)
	if err != nil {
		return nil, nil, err
	}
	records := make(map[string]schema.DailyQuoteRecord, len(symbols))
	failures := map[string]error{}
	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			continue
		}
		record, ok := allRecords[symbol]
		if !ok {
			failures[symbol] = fmt.Errorf("massive quote unavailable")
			continue
		}
		records[symbol] = record
	}
	return records, failures, nil
}

func (c Client) FetchDailyMarket(ctx context.Context, date string) (map[string]schema.DailyQuoteRecord, error) {
	u, err := url.Parse(strings.TrimRight(c.baseURL(), "/") + "/v2/aggs/grouped/locale/us/market/stocks/" + url.PathEscape(date))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("adjusted", "true")
	q.Set("include_otc", "false")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	res, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("massive daily market summary %s: %s", date, res.Status)
	}
	return ParseDailyMarket(res.Body, date, c.now())
}

func ParseDailyMarket(r io.Reader, date string, updatedAt time.Time) (map[string]schema.DailyQuoteRecord, error) {
	var payload groupedDailyResponse
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Status != "" && !strings.EqualFold(payload.Status, "OK") {
		msg := payload.Error
		if msg == "" {
			msg = payload.Message
		}
		if msg == "" {
			msg = payload.Status
		}
		return nil, fmt.Errorf("massive daily market summary: %s", msg)
	}
	records := make(map[string]schema.DailyQuoteRecord, len(payload.Results))
	for _, result := range payload.Results {
		symbol := strings.ToUpper(strings.TrimSpace(result.Symbol))
		if symbol == "" {
			continue
		}
		records[symbol] = schema.DailyQuoteRecord{
			Symbol:    symbol,
			Date:      date,
			Open:      result.Open,
			High:      result.High,
			Low:       result.Low,
			Close:     result.Close,
			AdjClose:  result.Close,
			Volume:    int64(math.Round(result.Volume)),
			Currency:  "USD",
			Source:    sourceName,
			UpdatedAt: updatedAt.UTC().Format(time.RFC3339),
		}
	}
	return records, nil
}

func (c Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://api.massive.com"
}

func (c Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c Client) now() time.Time {
	if c.Clock != nil {
		return c.Clock()
	}
	return time.Now().UTC()
}
