package stooq

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestParseCSV(t *testing.T) {
	input := "Date,Open,High,Low,Close,Volume\n2026-05-30,10,12,9,11,100\n2026-05-31,11,13,10,12,200.75\n"
	records, err := ParseCSV(strings.NewReader(input), "NVDA", "2026-05-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}
	if records[0].Symbol != "NVDA" || records[0].Close != 12 || records[0].Volume != 200 {
		t.Fatalf("unexpected record: %+v", records[0])
	}
}

func TestArchiveURLUsesAPIKeyPath(t *testing.T) {
	client := Client{APIKey: "abc123"}
	if got, want := client.archiveURL(), "https://static.stooq.com/db/h/abc123/d_us_txt.zip"; got != want {
		t.Fatalf("archive url mismatch: got %s want %s", got, want)
	}
}

func TestParseArchiveFilesFiltersSymbolsAndStooqTXTFormat(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeZipFile(t, zw, "data/daily/us/nasdaq stocks/1/nvda.us.txt", "<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\nNVDA.US,D,20240603,000000,10,12,9,11,100,0\n")
	writeZipFile(t, zw, "data/daily/us/nyse stocks/1/ibm.us.txt", "<TICKER>,<PER>,<DATE>,<TIME>,<OPEN>,<HIGH>,<LOW>,<CLOSE>,<VOL>,<OPENINT>\nIBM.US,D,20240603,000000,20,22,19,21,200,0\n")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	records, err := ParseArchiveFiles(zr.File, []string{"NVDA"})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || len(records["NVDA"]) != 1 {
		t.Fatalf("expected only NVDA records, got %+v", records)
	}
	if got := records["NVDA"][0]; got.Date != "2024-06-03" || got.Close != 11 || got.Volume != 100 {
		t.Fatalf("unexpected archive record: %+v", got)
	}
}

func writeZipFile(t *testing.T, zw *zip.Writer, name, body string) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
}
