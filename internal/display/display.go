package display

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/uri"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/fatih/color"
)

func PrintSummary(cfg *config.IngestConfig) {
	green := color.New(color.FgGreen, color.Bold).SprintFunc()
	yellow := color.New(color.FgYellow, color.Bold).SprintFunc()
	magenta := color.New(color.FgMagenta).SprintFunc()

	fmt.Print("\n\n")
	fmt.Println(green("Initiated the pipeline with the following:"))

	printConnectionLine := func(label string, display []string, suffix string) {
		value := "unknown"
		if len(display) > 0 {
			value = display[0]
		}
		if suffix != "" {
			value = fmt.Sprintf("%s / %s", value, suffix)
		}

		fmt.Printf("%s %s\n", yellow(label), value)
		for _, extra := range display[1:] {
			fmt.Printf("  %s\n", extra)
		}
	}

	printConnectionLine("Source:", displayFromURI(cfg.SourceURI), cfg.SourceTable)
	printConnectionLine("Destination:", displayFromURI(cfg.DestURI), cfg.DestTable)

	strategyValue := string(cfg.IncrementalStrategy)
	keyValue := cfg.IncrementalKey
	if keyValue == "" {
		keyValue = magenta("None")
	}

	pkValue := "None"
	if len(cfg.PrimaryKeys) > 0 {
		pkValue = strings.Join(cfg.PrimaryKeys, ", ")
	} else {
		pkValue = magenta(pkValue)
	}

	fmt.Printf("%s %s\n", yellow("Incremental Strategy:"), strategyValue)
	fmt.Printf("%s %s\n", yellow("Incremental Key:"), keyValue)
	fmt.Printf("%s %s\n", yellow("Primary Key:"), pkValue)
	if cfg.SchemaNaming != string(naming.Default) && cfg.SchemaNaming != "" {
		fmt.Printf("%s %s\n", yellow("Schema naming:"), cfg.SchemaNaming)
	}

	fmt.Print("\n\n")
}

func displayFromURI(rawURI string) []string {
	scheme, err := uri.ExtractScheme(rawURI)
	if err != nil {
		return []string{"unknown"}
	}
	return []string{uri.NormalizeScheme(scheme)}
}
