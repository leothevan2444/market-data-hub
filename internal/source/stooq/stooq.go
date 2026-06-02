package stooq

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"market-data-hub/internal/schema"
)

type Client struct {
	ArchiveURL  string
	ArchiveFile string
	APIKey      string
	HTTP        *http.Client
}

func New() Client {
	apiKey := os.Getenv("STOOQ_API_KEY")
	return Client{
		ArchiveURL:  archiveURLFromEnv(),
		ArchiveFile: os.Getenv("STOOQ_ARCHIVE_FILE"),
		APIKey:      apiKey,
		HTTP:        http.DefaultClient,
	}
}

func (c Client) FetchMarketHistory(ctx context.Context, symbols []string) (map[string][]schema.DailyQuoteRecord, error) {
	if c.ArchiveFile != "" {
		return c.fetchMarketHistoryFile(symbols)
	}

	tmp, err := os.CreateTemp("", "stooq-d-us-*.zip")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.archiveURL(), nil)
	if err != nil {
		tmp.Close()
		return nil, err
	}
	res, err := c.client().Do(req)
	if err != nil {
		tmp.Close()
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		tmp.Close()
		return nil, archiveInputError(fmt.Errorf("stooq archive: %s", res.Status))
	}
	if _, err := io.Copy(tmp, res.Body); err != nil {
		tmp.Close()
		return nil, archiveInputError(err)
	}
	if err := tmp.Close(); err != nil {
		return nil, archiveInputError(err)
	}

	zr, err := zip.OpenReader(tmpName)
	if err != nil {
		return nil, archiveInputError(err)
	}
	defer zr.Close()
	return ParseArchiveFiles(zr.File, symbols)
}

func (c Client) fetchMarketHistoryFile(symbols []string) (map[string][]schema.DailyQuoteRecord, error) {
	zr, err := zip.OpenReader(c.ArchiveFile)
	if err != nil {
		return nil, fmt.Errorf("open stooq archive file %s: %w", c.ArchiveFile, err)
	}
	defer zr.Close()
	return ParseArchiveFiles(zr.File, symbols)
}

func ParseArchiveFiles(files []*zip.File, symbols []string) (map[string][]schema.DailyQuoteRecord, error) {
	targets := map[string]bool{}
	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol != "" {
			targets[symbol] = true
		}
	}
	out := map[string][]schema.DailyQuoteRecord{}
	for _, file := range files {
		if file.FileInfo().IsDir() || !strings.EqualFold(filepath.Ext(file.Name), ".txt") {
			continue
		}
		symbol := archiveSymbol(file.Name)
		if symbol == "" || (len(targets) > 0 && !targets[symbol]) {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.Name, err)
		}
		records, parseErr := ParseCSV(rc, symbol, "")
		closeErr := rc.Close()
		if parseErr != nil {
			return nil, fmt.Errorf("%s: %w", file.Name, parseErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("%s: %w", file.Name, closeErr)
		}
		if len(records) > 0 {
			out[symbol] = append(out[symbol], records...)
		}
	}
	for symbol := range out {
		slices.SortFunc(out[symbol], func(a, b schema.DailyQuoteRecord) int { return strings.Compare(a.Date, b.Date) })
	}
	return out, nil
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
		idx[normalizeHeader(h)] = i
	}
	volumeKey := "volume"
	if _, ok := idx[volumeKey]; !ok {
		volumeKey = "vol"
	}
	for _, required := range []string{"date", "open", "high", "low", "close", volumeKey} {
		if _, ok := idx[required]; !ok {
			return nil, fmt.Errorf("unexpected stooq csv header %q; if the response asks for an apikey, set STOOQ_API_KEY", strings.Join(rows[0], ","))
		}
	}
	var out []schema.DailyQuoteRecord
	for _, row := range rows[1:] {
		if len(row) < len(rows[0]) {
			continue
		}
		d := normalizeDate(row[idx["date"]])
		if targetDate != "" && d != targetDate {
			continue
		}
		recordSymbol := strings.ToUpper(symbol)
		if recordSymbol == "" {
			recordSymbol = symbolFromRow(row, idx)
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
		volume, err := parseInt(row, idx, volumeKey)
		if err != nil {
			return nil, err
		}
		out = append(out, schema.DailyQuoteRecord{
			Symbol:    recordSymbol,
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

func archiveSymbol(path string) string {
	name := strings.ToLower(filepath.Base(path))
	name = strings.TrimSuffix(name, ".txt")
	name = strings.TrimSuffix(name, ".us")
	return strings.ToUpper(strings.TrimSpace(name))
}

func symbolFromRow(row []string, idx map[string]int) string {
	i, ok := idx["ticker"]
	if !ok || i >= len(row) {
		return ""
	}
	return archiveSymbol(row[i] + ".txt")
}

func normalizeHeader(h string) string {
	h = strings.TrimSpace(strings.ToLower(h))
	return strings.Trim(h, "<>")
}

func normalizeDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 8 {
		if _, err := time.Parse("20060102", value); err == nil {
			return value[:4] + "-" + value[4:6] + "-" + value[6:]
		}
	}
	return value
}

func (c Client) archiveURL() string {
	if c.ArchiveURL != "" {
		return c.ArchiveURL
	}
	if c.APIKey != "" {
		return fmt.Sprintf("https://static.stooq.com/db/h/%s/d_us_txt.zip", c.APIKey)
	}
	return defaultArchiveURL
}

func archiveURLFromEnv() string {
	if v := os.Getenv("STOOQ_ARCHIVE_URL"); v != "" {
		return v
	}
	return ""
}

func (c Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func archiveInputError(err error) error {
	return fmt.Errorf("%w; download the Stooq US daily archive manually and rerun with --stooq-archive-file PATH or STOOQ_ARCHIVE_FILE=PATH", err)
}

const defaultArchiveURL = "https://static.stooq.com/db/h/d_us_txt.zip"
