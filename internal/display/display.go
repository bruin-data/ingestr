package display

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/internal/uri"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/fatih/color"
)

// sourceTableSummary describes the tables the run will read. The confirmation
// prompt is the user's last chance to catch a mistyped table list, so a subset
// has to be visible here rather than showing an empty source table.
func sourceTableSummary(cfg *config.IngestConfig) string {
	if len(cfg.SourceTables) == 0 {
		return cfg.SourceTable
	}
	const maxNamed = 5
	if len(cfg.SourceTables) <= maxNamed {
		return fmt.Sprintf("%d tables (%s)", len(cfg.SourceTables), strings.Join(cfg.SourceTables, ", "))
	}
	return fmt.Sprintf("%d tables (%s, ...)", len(cfg.SourceTables), strings.Join(cfg.SourceTables[:maxNamed], ", "))
}

func PrintSummary(cfg *config.IngestConfig) {
	if output.IsJSON() {
		output.EventStart(output.StartInfo{
			SourceType:     displayFromURI(cfg.SourceURI)[0],
			DestType:       displayFromURI(cfg.DestURI)[0],
			SourceTable:    sourceTableSummary(cfg),
			DestTable:      cfg.DestTable,
			Strategy:       string(cfg.IncrementalStrategy),
			IncrementalKey: cfg.IncrementalKey,
			PrimaryKey:     cfg.PrimaryKeys,
			SchemaNaming:   cfg.SchemaNaming,
		})
		return
	}

	green := color.New(color.FgGreen, color.Bold).SprintFunc()
	yellow := color.New(color.FgYellow, color.Bold).SprintFunc()
	magenta := color.New(color.FgMagenta).SprintFunc()

	output.Statusf("\n\n")
	output.Statusf("%s\n", green("Initiated the pipeline with the following:"))

	printConnectionLine := func(label string, display []string, suffix string) {
		value := "unknown"
		if len(display) > 0 {
			value = display[0]
		}
		if suffix != "" {
			value = fmt.Sprintf("%s / %s", value, suffix)
		}

		output.Statusf("%s %s\n", yellow(label), value)
		for _, extra := range display[1:] {
			output.Statusf("  %s\n", extra)
		}
	}

	printConnectionLine("Source:", displayFromURI(cfg.SourceURI), sourceTableSummary(cfg))
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

	output.Statusf("%s %s\n", yellow("Incremental Strategy:"), strategyValue)
	output.Statusf("%s %s\n", yellow("Incremental Key:"), keyValue)
	output.Statusf("%s %s\n", yellow("Primary Key:"), pkValue)
	if cfg.SchemaNaming != string(naming.Default) && cfg.SchemaNaming != "" {
		output.Statusf("%s %s\n", yellow("Schema naming:"), cfg.SchemaNaming)
	}

	output.Statusf("\n\n")
}

func displayFromURI(rawURI string) []string {
	scheme, err := uri.ExtractScheme(rawURI)
	if err != nil {
		return []string{"unknown"}
	}
	return []string{uri.NormalizeScheme(scheme)}
}
