package config

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/fatih/color"
	"github.com/muesli/termenv"
)

var errorPrinter = color.New(color.FgRed)

func highlightSQL(query string) string {
	o, err := os.Stderr.Stat()
	if err != nil {
		return query
	}

	if (o.Mode() & os.ModeCharDevice) != os.ModeCharDevice {
		return query
	}

	style := "monokai"
	if !termenv.HasDarkBackground() {
		style = "github"
	}

	b := new(strings.Builder)
	err = quick.Highlight(b, query, "sql", "terminal16m", style)
	if err != nil {
		return query
	}

	return b.String()
}

var (
	failedQueryMu sync.Mutex
	failedQuery   *string
)

func LogFailedQuery(query string, err error) {
	if err == nil {
		return
	}
	failedQueryMu.Lock()
	failedQuery = &query
	failedQueryMu.Unlock()
}

func PrintFailedQuery() {
	failedQueryMu.Lock()
	defer failedQueryMu.Unlock()
	if failedQuery == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "\n")
	errorPrinter.Fprintf(os.Stderr, "Failed SQL query:\n")
	fmt.Fprintf(os.Stderr, "%s\n", highlightSQL(*failedQuery))
	failedQuery = nil
}
