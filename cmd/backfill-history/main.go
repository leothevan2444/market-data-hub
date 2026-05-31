package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"market-data-hub/internal/service"
	"market-data-hub/internal/storage/d1"
)

func main() {
	market := flag.String("market", "us", "market code")
	from := flag.String("from", "", "from date YYYY-MM-DD")
	to := flag.String("to", "", "to date YYYY-MM-DD")
	symbols := flag.String("symbols", "", "comma-separated symbols")
	watchlist := flag.String("watchlist", "config/watchlist.yaml", "watchlist path")
	all := flag.Bool("all", false, "backfill all active universe symbols")
	replace := flag.Bool("replace", false, "replace target symbol records in the requested date range instead of merging")
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
		WatchlistPath: *watchlist,
		All:           *all,
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
