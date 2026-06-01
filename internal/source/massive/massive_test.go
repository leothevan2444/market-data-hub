package massive

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestParseDailyMarket(t *testing.T) {
	input := `{"status":"OK","adjusted":true,"results":[{"T":"NVDA","o":214.575,"h":217.8599,"l":211.13,"c":211.14,"v":289410624.4,"t":1780099200000,"vw":213.2}]}`
	records, err := ParseDailyMarket(strings.NewReader(input), "2026-05-29", time.Date(2026, 5, 30, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	record := records["NVDA"]
	if record.Symbol != "NVDA" || record.Date != "2026-05-29" || record.Close != 211.14 || record.Volume != 289410624 || record.Source != "massive" {
		t.Fatalf("unexpected record: %+v", record)
	}
}

func TestFetchDailyBulk(t *testing.T) {
	var gotPath, gotAuth string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Query().Get("adjusted") != "true" || r.URL.Query().Get("include_otc") != "false" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"status":"OK","results":[{"T":"NVDA","o":10,"h":12,"l":9,"c":11,"v":100},{"T":"MSFT","o":20,"h":22,"l":19,"c":21,"v":200}]}`)),
			Request:    r,
		}, nil
	})}

	client := Client{
		BaseURL: "https://api.test",
		APIKey:  "secret",
		HTTP:    httpClient,
		Clock:   func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) },
	}
	records, failures, err := client.FetchDailyBulk(context.Background(), []string{"NVDA", "AAPL"}, "2026-05-29")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v2/aggs/grouped/locale/us/market/stocks/2026-05-29" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("unexpected authorization header: %s", gotAuth)
	}
	if records["NVDA"].Close != 11 {
		t.Fatalf("unexpected records: %+v", records)
	}
	if failures["AAPL"] == nil {
		t.Fatalf("expected AAPL failure")
	}
}
