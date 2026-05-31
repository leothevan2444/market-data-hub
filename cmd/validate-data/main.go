package main

import (
	"flag"
	"fmt"
	"os"

	"market-data-hub/internal/service"
)

func main() {
	file := flag.String("file", "", "file to validate")
	flag.Parse()
	if *file == "" {
		fatal(fmt.Errorf("--file is required"))
	}
	summary, err := service.ValidateFile(*file)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("kind=%s total=%d valid=%d invalid=%d warnings=%d\n", summary.Kind, summary.Total, summary.Valid, summary.Invalid, summary.Warnings)
	if summary.Invalid > 0 {
		os.Exit(2)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
