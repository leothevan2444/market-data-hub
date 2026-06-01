package stooq

import (
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

func TestParseQuoteCSV(t *testing.T) {
	input := "Symbol,Date,Time,Open,High,Low,Close,Volume\nNVDA.US,2026-05-29,22:00:20,214.575,217.8599,211.13,211.14,289410624\nAAPL.US,2026-05-29,22:00:19,311.775,315,309.53,312.06,70026752\n"
	records, failures, err := ParseQuoteCSV(strings.NewReader(input), "2026-05-29")
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Fatalf("expected no failures, got %v", failures)
	}
	if len(records) != 2 {
		t.Fatalf("expected two records, got %d", len(records))
	}
	if records["NVDA"].Close != 211.14 || records["AAPL"].Volume != 70026752 {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestParseQuoteCSVDateMismatch(t *testing.T) {
	input := "Symbol,Date,Time,Open,High,Low,Close,Volume\nNVDA.US,2026-05-29,22:00:20,214.575,217.8599,211.13,211.14,289410624\n"
	records, failures, err := ParseQuoteCSV(strings.NewReader(input), "2026-06-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("expected no records, got %v", records)
	}
	if failures["NVDA"] == nil {
		t.Fatalf("expected date mismatch failure")
	}
}
