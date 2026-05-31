package service

import (
	"bufio"
	"os"
	"strings"
)

type MarketConfig struct {
	Market        string
	Timezone      string
	Currency      string
	DefaultSource string
	Enabled       bool
}

func DefaultMarket(market string) MarketConfig {
	return MarketConfig{Market: strings.ToLower(market), Timezone: "America/New_York", Currency: "USD", DefaultSource: "stooq", Enabled: true}
}

func LoadWatchlist(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var symbols []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "- ")
		if line == "" || strings.HasPrefix(line, "#") || line == "watchlist:" {
			continue
		}
		symbols = append(symbols, strings.ToUpper(strings.Trim(line, `"'`)))
	}
	return symbols, scanner.Err()
}
