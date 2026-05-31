package nasdaqtrader

import (
	"strings"
	"testing"
)

func TestParsePipe(t *testing.T) {
	input := "Symbol|Security Name|Market Category|Test Issue|Financial Status|Round Lot Size|ETF|NextShares\nNVDA|NVIDIA Corporation|Q|N|N|100|N|N\nQQQ|Invesco QQQ|G|N|N|100|Y|N\nFile Creation Time: 0531202620:00|||||||\n"
	symbols, err := ParsePipe(strings.NewReader(input), "NASDAQ")
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 2 {
		t.Fatalf("expected two symbols, got %d", len(symbols))
	}
	if symbols[1].AssetType != "etf" || !symbols[1].IsEtf {
		t.Fatalf("expected ETF symbol, got %+v", symbols[1])
	}
}
