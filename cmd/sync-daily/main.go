package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"market-data-hub/internal/service"
	"market-data-hub/internal/storage/d1"
)

func main() {
	market := flag.String("market", "us", "market code")
	date := flag.String("date", "", "sync date YYYY-MM-DD")
	storageName := flag.String("storage", "local", "storage backend: local or r2")
	root := flag.String("root", "data", "local storage root")
	watchlist := flag.String("watchlist", "", "optional watchlist path; defaults to all active symbols")
	r2Concurrency := flag.Int("r2-concurrency", 16, "parallel workers for R2/object-store history updates")
	flag.Parse()

	store, err := service.StorageFor(*storageName, *root)
	if err != nil {
		fatal(err)
	}
	syncer := service.NewSyncer(store, d1.FromEnv())
	err = syncer.Run(context.Background(), service.SyncOptions{
		Market:        *market,
		Date:          *date,
		WatchlistPath: *watchlist,
		R2Workers:     *r2Concurrency,
		Log: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "[sync-daily] "+format+"\n", args...)
		},
	})
	if err != nil {
		fatal(err)
	}
	fmt.Println("sync completed")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
