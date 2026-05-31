package storage

import "testing"

func TestKeys(t *testing.T) {
	if got := DailyKey("US", "2026-05-31"); got != "daily/us/2026/2026-05-31.json.gz" {
		t.Fatalf("daily key mismatch: %s", got)
	}
	if got := HistoryKey("US", "nvda"); got != "history/us/NVDA/daily.json.gz" {
		t.Fatalf("history key mismatch: %s", got)
	}
}
