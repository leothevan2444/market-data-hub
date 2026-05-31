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
