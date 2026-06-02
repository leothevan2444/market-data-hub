package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"market-data-hub/internal/service"
	"market-data-hub/internal/source/stooq"
	"market-data-hub/internal/storage/d1"
)

func main() {
	market := flag.String("market", "us", "market code")
	from := flag.String("from", "", "optional from date YYYY-MM-DD; defaults to earliest archive record")
	to := flag.String("to", "", "optional to date YYYY-MM-DD; defaults to latest archive record")
	watchlist := flag.String("watchlist", "", "watchlist path; when omitted, all active universe symbols are backfilled")
	replace := flag.Bool("replace", false, "replace target symbol records in the requested date range instead of merging")
	stooqArchiveFile := flag.String("stooq-archive-file", "", "local Stooq d_us_txt.zip archive file to use instead of downloading")
	storageName := flag.String("storage", "local", "storage backend: local or r2")
	root := flag.String("root", "data", "local storage root")
	flag.Parse()

	store, err := service.StorageFor(*storageName, *root)
	if err != nil {
		fatal(err)
	}
	logf := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
	}
	stooqClient := stooq.New()
	stooqClient.Logf = logf
	if *stooqArchiveFile != "" {
		stooqClient.ArchiveFile = *stooqArchiveFile
	}
	backfiller := service.NewBackfiller(store, d1.FromEnv())
	backfiller.Quotes = stooqClient
	backfiller.Logf = logf
	err = backfiller.Run(context.Background(), service.BackfillOptions{
		Market:        *market,
		From:          *from,
		To:            *to,
		WatchlistPath: *watchlist,
		Replace:       *replace,
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
