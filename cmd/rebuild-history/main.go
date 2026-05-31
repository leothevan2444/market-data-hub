package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"market-data-hub/internal/service"
)

func main() {
	market := flag.String("market", "us", "market code")
	from := flag.String("from", "", "from date YYYY-MM-DD")
	to := flag.String("to", "", "to date YYYY-MM-DD")
	symbols := flag.String("symbols", "", "comma-separated symbols")
	storageName := flag.String("storage", "local", "storage backend: local or r2")
	root := flag.String("root", "data", "local storage root")
	flag.Parse()

	store, err := service.StorageFor(*storageName, *root)
	if err != nil {
		fatal(err)
	}
	var list []string
	if *symbols != "" {
		list = strings.Split(*symbols, ",")
	}
	if err := service.RebuildHistory(context.Background(), store, service.RebuildOptions{Market: *market, From: *from, To: *to, Symbols: list}); err != nil {
		fatal(err)
	}
	fmt.Println("rebuild completed")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
