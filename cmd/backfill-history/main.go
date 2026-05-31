package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"market-data-hub/internal/service"
	"market-data-hub/internal/storage/d1"
)

func main() {
	market := flag.String("market", "us", "market code")
	from := flag.String("from", "", "from date YYYY-MM-DD")
	to := flag.String("to", "", "to date YYYY-MM-DD")
	symbols := flag.String("symbols", "", "comma-separated symbols")
	symbolsFile := flag.String("symbols-file", "", "newline or comma separated symbols file")
	watchlist := flag.String("watchlist", "config/watchlist.yaml", "watchlist path")
	all := flag.Bool("all", false, "backfill all active universe symbols")
	replace := flag.Bool("replace", false, "replace target symbol records in the requested date range instead of merging")
	offset := flag.Int("offset", 0, "number of resolved symbols to skip")
	batchSize := flag.Int("batch-size", 0, "maximum number of resolved symbols to process; 0 means all")
	sleepMS := flag.Int("sleep-ms", 1000, "delay between Stooq symbol requests in milliseconds")
	maxRetries := flag.Int("max-retries", 3, "retry attempts per symbol after the first request")
	retryBackoffMS := flag.Int("retry-backoff-ms", 2000, "linear retry backoff base in milliseconds")
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
	backfiller := service.NewBackfiller(store, d1.FromEnv())
	err = backfiller.Run(context.Background(), service.BackfillOptions{
		Market:        *market,
		From:          *from,
		To:            *to,
		Symbols:       list,
		SymbolsFile:   *symbolsFile,
		WatchlistPath: *watchlist,
		All:           *all,
		Replace:       *replace,
		Offset:        *offset,
		BatchSize:     *batchSize,
		Sleep:         time.Duration(*sleepMS) * time.Millisecond,
		MaxRetries:    *maxRetries,
		RetryBackoff:  time.Duration(*retryBackoffMS) * time.Millisecond,
	})
	if err != nil {
		fatal(err)
	}
	fmt.Println("backfill completed")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
