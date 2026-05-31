package nasdaqtrader

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"market-data-hub/internal/schema"
)

type Client struct {
	HTTP *http.Client
}

func New() Client {
	return Client{HTTP: http.DefaultClient}
}

func (c Client) FetchUniverse(ctx context.Context) ([]schema.SymbolInfo, error) {
	urls := []struct {
		url      string
		exchange string
	}{
		{"https://www.nasdaqtrader.com/dynamic/SymDir/nasdaqlisted.txt", "NASDAQ"},
		{"https://www.nasdaqtrader.com/dynamic/SymDir/otherlisted.txt", ""},
	}
	var out []schema.SymbolInfo
	for _, src := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.url, nil)
		if err != nil {
			return nil, err
		}
		res, err := c.client().Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			res.Body.Close()
			return nil, fmt.Errorf("nasdaq trader %s: %s", src.url, res.Status)
		}
		symbols, err := ParsePipe(res.Body, src.exchange)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, symbols...)
	}
	return out, nil
}

func ParsePipe(r io.Reader, defaultExchange string) ([]schema.SymbolInfo, error) {
	scanner := bufio.NewScanner(r)
	var headers []string
	var out []schema.SymbolInfo
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "File Creation Time:") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if headers == nil {
			headers = parts
			continue
		}
		if len(parts) != len(headers) {
			continue
		}
		row := map[string]string{}
		for i, h := range headers {
			row[h] = parts[i]
		}
		symbol := first(row, "Symbol", "ACT Symbol")
		if symbol == "" || strings.Contains(symbol, " ") {
			continue
		}
		exchange := defaultExchange
		if row["Exchange"] != "" {
			exchange = exchangeName(row["Exchange"])
		}
		isETF := strings.EqualFold(first(row, "ETF"), "Y")
		testIssue := strings.EqualFold(first(row, "Test Issue"), "Y")
		out = append(out, schema.SymbolInfo{
			Symbol:    strings.ToUpper(symbol),
			Name:      first(row, "Security Name", "Security Name"),
			Exchange:  exchange,
			AssetType: assetType(isETF),
			IsEtf:     isETF,
			IsActive:  !testIssue,
		})
	}
	return out, scanner.Err()
}

func first(row map[string]string, keys ...string) string {
	for _, k := range keys {
		if row[k] != "" {
			return row[k]
		}
	}
	return ""
}

func assetType(isETF bool) string {
	if isETF {
		return "etf"
	}
	return "stock"
}

func exchangeName(code string) string {
	switch code {
	case "A":
		return "NYSE American"
	case "N":
		return "NYSE"
	case "P":
		return "NYSE Arca"
	case "Q":
		return "NASDAQ"
	case "Z":
		return "Cboe BZX"
	default:
		return code
	}
}

func (c Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}
