package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/internal/uri"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/fatih/color"
	"github.com/urfave/cli/v3"
)

func IngestCommand() *cli.Command {
	return &cli.Command{
		Name:  "ingest",
		Usage: "Ingest data from source to destination",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "source-uri",
				Usage:    "The URI of the source",
				Required: true,
				Sources:  cli.EnvVars("SOURCE_URI", "INGESTR_SOURCE_URI"),
			},
			&cli.StringFlag{
				Name:     "dest-uri",
				Usage:    "The URI of the destination",
				Required: true,
				Sources:  cli.EnvVars("DESTINATION_URI", "INGESTR_DESTINATION_URI"),
			},
			&cli.StringFlag{
				Name:    "source-table",
				Usage:   "The table name in the source to fetch (optional for CDC multi-table mode)",
				Sources: cli.EnvVars("SOURCE_TABLE", "INGESTR_SOURCE_TABLE"),
			},
			&cli.StringFlag{
				Name:    "dest-table",
				Usage:   "The table in the destination to save the data into",
				Sources: cli.EnvVars("DESTINATION_TABLE", "INGESTR_DESTINATION_TABLE"),
			},
			&cli.StringFlag{
				Name:    "incremental-key",
				Usage:   "The incremental key from the table to be used for incremental strategies",
				Sources: cli.EnvVars("INCREMENTAL_KEY", "INGESTR_INCREMENTAL_KEY"),
			},
			&cli.StringFlag{
				Name:    "incremental-strategy",
				Usage:   "The incremental strategy to use (replace, truncate+insert, append, delete+insert, merge, scd2, none)",
				Value:   "replace",
				Sources: cli.EnvVars("INCREMENTAL_STRATEGY", "INGESTR_INCREMENTAL_STRATEGY"),
			},
			&cli.StringFlag{
				Name:    "interval-start",
				Usage:   "The start of the interval the incremental key will cover",
				Sources: cli.EnvVars("INTERVAL_START", "INGESTR_INTERVAL_START"),
			},
			&cli.StringFlag{
				Name:    "interval-end",
				Usage:   "The end of the interval the incremental key will cover",
				Sources: cli.EnvVars("INTERVAL_END", "INGESTR_INTERVAL_END"),
			},
			&cli.StringSliceFlag{
				Name:    "primary-key",
				Usage:   "The key that will be used to deduplicate the resulting table",
				Sources: cli.EnvVars("PRIMARY_KEY", "INGESTR_PRIMARY_KEY"),
			},
			&cli.StringFlag{
				Name:    "partition-by",
				Usage:   "The partition key to be used for partitioning the destination table",
				Sources: cli.EnvVars("PARTITION_BY", "INGESTR_PARTITION_BY"),
			},
			&cli.StringFlag{
				Name:    "cluster-by",
				Usage:   "The clustering key to be used for clustering the destination table",
				Sources: cli.EnvVars("CLUSTER_BY", "INGESTR_CLUSTER_BY"),
			},
			&cli.BoolFlag{
				Name:    "yes",
				Usage:   "Skip the confirmation prompt and ingest right away",
				Sources: cli.EnvVars("SKIP_CONFIRMATION", "INGESTR_SKIP_CONFIRMATION"),
			},
			&cli.BoolFlag{
				Name:    "full-refresh",
				Usage:   "Ignore the state and refresh the destination table completely",
				Sources: cli.EnvVars("FULL_REFRESH", "INGESTR_FULL_REFRESH"),
			},
			&cli.StringFlag{
				Name:    "schema-contract",
				Usage:   "Schema contract mode: evolve (auto-apply changes), freeze (reject changes), discard_row (drop non-conforming rows), discard_value (NULL non-conforming values)",
				Value:   "evolve",
				Sources: cli.EnvVars("SCHEMA_CONTRACT", "INGESTR_SCHEMA_CONTRACT"),
			},
			&cli.StringFlag{
				Name:    "schema-naming",
				Usage:   "Schema naming convention: direct (preserve original names), snake_case (ingestr-compatible snake_case), auto (detect from destination)",
				Value:   string(naming.Default),
				Sources: cli.EnvVars("SCHEMA_NAMING", "INGESTR_SCHEMA_NAMING"),
			},
			&cli.StringFlag{
				Name:    "progress",
				Usage:   "The progress display type (interactive, log, json)",
				Value:   "interactive",
				Sources: cli.EnvVars("PROGRESS", "INGESTR_PROGRESS"),
			},
			&cli.IntFlag{
				Name:    "page-size",
				Usage:   "The page size to be used when fetching data from SQL sources",
				Value:   25000,
				Sources: cli.EnvVars("PAGE_SIZE", "INGESTR_PAGE_SIZE"),
			},
			&cli.IntFlag{
				Name:    "loader-file-size",
				Usage:   "The file size to be used by the loader to split the data into multiple files",
				Value:   25000,
				Sources: cli.EnvVars("LOADER_FILE_SIZE", "INGESTR_LOADER_FILE_SIZE"),
			},
			&cli.StringFlag{
				Name:    "loader-file-format",
				Usage:   "The file format to be used by the loader",
				Hidden:  true,
				Sources: cli.EnvVars("LOADER_FILE_FORMAT", "INGESTR_LOADER_FILE_FORMAT"),
			},
			&cli.IntFlag{
				Name:    "extract-parallelism",
				Usage:   "The number of parallel jobs to run for extracting data from the source",
				Value:   5,
				Sources: cli.EnvVars("EXTRACT_PARALLELISM", "INGESTR_EXTRACT_PARALLELISM"),
			},
			&cli.StringFlag{
				Name:    "extract-partition-by",
				Usage:   "The source date/time or integer column used to split bounded extraction into parallel windows",
				Sources: cli.EnvVars("EXTRACT_PARTITION_BY", "INGESTR_EXTRACT_PARTITION_BY"),
			},
			&cli.StringFlag{
				Name:    "extract-partition-interval",
				Usage:   "The width for each extract partition window: duration (1h, 7d), integer step (10000), or auto. Defaults to auto when extract-partition-by is set",
				Sources: cli.EnvVars("EXTRACT_PARTITION_INTERVAL", "INGESTR_EXTRACT_PARTITION_INTERVAL"),
			},
			&cli.IntFlag{
				Name:    "sql-limit",
				Usage:   "The limit to use when fetching data from the source",
				Sources: cli.EnvVars("SQL_LIMIT", "INGESTR_SQL_LIMIT"),
			},
			&cli.StringSliceFlag{
				Name:    "sql-exclude-columns",
				Usage:   "The columns to exclude from the source table",
				Sources: cli.EnvVars("SQL_EXCLUDE_COLUMNS", "INGESTR_SQL_EXCLUDE_COLUMNS"),
			},
			&cli.StringSliceFlag{
				Name:    "sql-backend",
				Usage:   "The SQL backend to use",
				Hidden:  true,
				Sources: cli.EnvVars("SQL_BACKEND", "INGESTR_SQL_BACKEND"),
			},
			&cli.StringFlag{
				Name:    "columns",
				Usage:   "Override column types and/or rename columns. Per-column format: 'name:type' (type override), 'name:type:source' (rename + type), or 'name::source' (rename only). Multiple entries comma-separated, e.g. 'id:bigint,first_name:varchar(50):fname,email::eml'. Types: bigint, int, smallint, tinyint, float, double, decimal(p,s), string, text, varchar(n), boolean, date, timestamp (with tz), timestamp_ntz (no tz), json, uuid, binary",
				Sources: cli.EnvVars("INGESTR_COLUMNS"),
			},
			&cli.BoolFlag{
				Name:    "no-inference",
				Usage:   "Skip schema inference for schema-less sources and use --columns as the source schema",
				Sources: cli.EnvVars("NO_INFERENCE", "INGESTR_NO_INFERENCE"),
			},
			&cli.StringSliceFlag{
				Name:    "mask",
				Usage:   "Column masking configuration in format 'column:algorithm[:param]'. Algorithms: hash, sha256, md5, hmac, email, phone, credit_card, ssn, redact, stars, fixed, random, partial, first_letter, uuid, sequential, round, range, noise, date_shift, year_only, month_year.",
				Sources: cli.EnvVars("MASK", "INGESTR_MASK"),
			},
			&cli.BoolFlag{
				Name:    "trim-whitespace",
				Usage:   "Trim leading and trailing whitespace from all string column values",
				Sources: cli.EnvVars("TRIM_WHITESPACE", "INGESTR_TRIM_WHITESPACE"),
			},
			&cli.BoolFlag{
				Name:    "no-load-timestamp",
				Usage:   "Disable adding the _ingestr_loaded_at load timestamp column",
				Sources: cli.EnvVars("NO_LOAD_TIMESTAMP", "INGESTR_NO_LOAD_TIMESTAMP"),
			},
			&cli.StringFlag{
				Name:    "pipelines-dir",
				Usage:   "The path to store pipeline metadata",
				Sources: cli.EnvVars("PIPELINES_DIR", "INGESTR_PIPELINES_DIR"),
			},
			&cli.StringFlag{
				Name:    "staging-bucket",
				Usage:   "The staging bucket to be used for the ingestion (gs:// or s3://)",
				Sources: cli.EnvVars("STAGING_BUCKET", "INGESTR_STAGING_BUCKET"),
			},
			&cli.StringFlag{
				Name:    "staging-dataset",
				Usage:   "The dataset/schema to use for staging tables (defaults to the destination table's dataset/schema)",
				Sources: cli.EnvVars("STAGING_DATASET", "INGESTR_STAGING_DATASET"),
			},
			&cli.BoolFlag{
				Name:    "debug",
				Usage:   "Enable debug logging",
				Sources: cli.EnvVars("DEBUG", "INGESTR_DEBUG"),
			},
			&cli.BoolFlag{
				Name:    "stream",
				Usage:   "Continuously ingest from the source, flushing to the destination on an interval or record-count trigger (CDC and message-broker sources only)",
				Sources: cli.EnvVars("INGESTR_STREAM"),
			},
			&cli.DurationFlag{
				Name:    "flush-interval",
				Usage:   "How often to flush buffered records to the destination in streaming mode",
				Value:   30 * time.Second,
				Sources: cli.EnvVars("INGESTR_FLUSH_INTERVAL"),
			},
			&cli.IntFlag{
				Name:    "flush-records",
				Usage:   "Flush to the destination when this many records are buffered in streaming mode",
				Value:   50000,
				Sources: cli.EnvVars("INGESTR_FLUSH_RECORDS"),
			},
			&cli.StringFlag{
				Name:    "query-annotations",
				Usage:   "JSON object of caller annotation keys (e.g. {\"pipeline\":\"p\",\"asset\":\"a\"}) merged into the '-- @bruin.config' comment on destination queries (QUERY_TAG on Snowflake) for cost attribution. ingestr always annotates with its own keys (type, ingestr_step); this flag adds the caller's keys on top.",
				Sources: cli.EnvVars("INGESTR_QUERY_ANNOTATIONS"),
			},
		},
		Action: runIngest,
	}
}

func runIngest(ctx context.Context, c *cli.Command) (err error) {
	trackCommandTriggered(ctx, "ingest")
	var finishedProperties map[string]any
	defer func() {
		output.EnsureTerminal(err)
		trackCommandFinished(ctx, "ingest", err, finishedProperties)
	}()

	config.DebugMode = c.Bool("debug")
	outputMode := output.ModeText
	if config.ProgressMode(c.String("progress")) == config.ProgressJSON {
		outputMode = output.ModeJSON
	}
	output.Init(os.Stdout, os.Stderr, outputMode)
	cfg := config.DefaultConfig()

	cfg.SourceURI = c.String("source-uri")
	cfg.DestURI = c.String("dest-uri")
	cfg.SourceTable = c.String("source-table")
	cfg.DestTable = c.String("dest-table")
	cfg.IncrementalKey = c.String("incremental-key")
	cfg.IncrementalStrategy = config.IncrementalStrategy(c.String("incremental-strategy"))
	cfg.IncrementalStrategyExplicit = c.IsSet("incremental-strategy")
	cfg.PrimaryKeys = c.StringSlice("primary-key")
	cfg.PartitionBy = c.String("partition-by")
	cfg.Yes = c.Bool("yes")
	cfg.FullRefresh = c.Bool("full-refresh")
	cfg.SchemaContract = c.String("schema-contract")
	cfg.SchemaNaming = c.String("schema-naming")
	cfg.Progress = config.ProgressMode(c.String("progress"))
	cfg.PageSize = int(c.Int("page-size"))
	cfg.LoaderFileSize = int(c.Int("loader-file-size"))
	cfg.LoaderFileFormat = c.String("loader-file-format")
	cfg.ExtractParallelism = int(c.Int("extract-parallelism"))
	cfg.ExtractPartitionBy = c.String("extract-partition-by")
	cfg.SQLLimit = int(c.Int("sql-limit"))
	cfg.SQLExcludeColumns = c.StringSlice("sql-exclude-columns")
	cfg.Columns = c.String("columns")
	cfg.NoInference = c.Bool("no-inference")
	cfg.Mask = c.StringSlice("mask")
	cfg.TrimWhitespace = c.Bool("trim-whitespace")
	cfg.NoLoadTimestamp = c.Bool("no-load-timestamp")
	cfg.PipelinesDir = c.String("pipelines-dir")
	cfg.StagingBucket = c.String("staging-bucket")
	cfg.StagingDataset = c.String("staging-dataset")
	cfg.QueryAnnotations = c.String("query-annotations")
	cfg.Stream = c.Bool("stream")
	cfg.FlushInterval = c.Duration("flush-interval")
	cfg.FlushRecords = int(c.Int("flush-records"))
	finishedProperties = ingestTelemetryProperties(cfg)

	if !cfg.Stream && (c.IsSet("flush-interval") || c.IsSet("flush-records")) {
		return fmt.Errorf("--flush-interval and --flush-records are only valid together with --stream")
	}
	// In streaming mode the source decides the default strategy (merge for CDC,
	// append for brokers); only treat the strategy as a user override when the
	// flag was explicitly set, since it defaults to "replace".
	if cfg.Stream && !c.IsSet("incremental-strategy") {
		cfg.IncrementalStrategy = ""
	}

	if clusterBy := c.String("cluster-by"); clusterBy != "" {
		// Split by comma to support multiple clustering columns
		parts := strings.Split(clusterBy, ",")
		cfg.ClusterBy = make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				cfg.ClusterBy = append(cfg.ClusterBy, trimmed)
			}
		}
	}

	if intervalStart := c.String("interval-start"); intervalStart != "" {
		t, err := parseDateTime(intervalStart)
		if err != nil {
			return fmt.Errorf("invalid interval-start: %w", err)
		}
		cfg.IntervalStart = &t
	}

	if intervalEnd := c.String("interval-end"); intervalEnd != "" {
		t, err := parseDateTime(intervalEnd)
		if err != nil {
			return fmt.Errorf("invalid interval-end: %w", err)
		}
		cfg.IntervalEnd = &t
	}

	if err := applyExtractPartitionInterval(cfg, c.String("extract-partition-interval")); err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	if cfg.IncrementalStrategy != "" {
		if _, err := strategy.Get(cfg.IncrementalStrategy); err != nil {
			return err
		}
	}

	p := pipeline.New(cfg)
	if err := p.Run(ctx); err != nil {
		return err
	}

	if ctx.Err() != nil {
		// In streaming mode, cancellation (SIGINT/SIGTERM) is the normal way to
		// stop; the pipeline has already flushed pending data.
		if cfg.Stream {
			if !output.IsJSON() {
				color.Green("Streaming ingestion stopped.")
			}
			return nil
		}
		return ctx.Err()
	}

	if !output.IsJSON() {
		color.Green("Ingestion completed successfully!")
	}
	return nil
}

func ingestTelemetryProperties(cfg *config.IngestConfig) map[string]any {
	properties := map[string]any{}
	if sourceType := telemetryScheme(cfg.SourceURI); sourceType != "" {
		properties["source_platform"] = sourceType
	}
	if destinationType := telemetryScheme(cfg.DestURI); destinationType != "" {
		properties["destination_platform"] = destinationType
	}
	return properties
}

func telemetryScheme(rawURI string) string {
	parsed, err := uri.Parse(rawURI)
	if err != nil {
		return ""
	}
	return parsed.Scheme
}

func parseDateTime(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.000000",
		"2006-01-02T15:04:05.000000Z07:00",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse date: %s", s)
}

func parseExtractPartitionInterval(s string) (time.Duration, int64, bool, error) {
	value := strings.TrimSpace(strings.ToLower(s))
	if value == "" {
		return 0, 0, false, fmt.Errorf("cannot be empty")
	}
	if value == "auto" {
		return 0, 0, true, nil
	}

	if numericInterval, err := strconv.ParseInt(value, 10, 64); err == nil {
		if numericInterval <= 0 {
			return 0, 0, false, fmt.Errorf("must be positive")
		}
		return 0, numericInterval, false, nil
	}

	var duration time.Duration
	switch {
	case strings.HasSuffix(value, "d"):
		parsed, err := parseBoundedDurationMultiple(strings.TrimSuffix(value, "d"), 24*time.Hour)
		if err != nil {
			return 0, 0, false, err
		}
		duration = parsed
	case strings.HasSuffix(value, "w"):
		parsed, err := parseBoundedDurationMultiple(strings.TrimSuffix(value, "w"), 7*24*time.Hour)
		if err != nil {
			return 0, 0, false, err
		}
		duration = parsed
	default:
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return 0, 0, false, err
		}
		duration = parsed
	}

	if duration <= 0 {
		return 0, 0, false, fmt.Errorf("must be positive")
	}
	return duration, 0, false, nil
}

func parseBoundedDurationMultiple(raw string, unit time.Duration) (time.Duration, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, err
	}
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("must be positive")
	}

	const maxDuration = time.Duration(1<<63 - 1)
	if value > float64(maxDuration)/float64(unit) {
		return 0, fmt.Errorf("duration overflows time.Duration")
	}
	duration := time.Duration(value * float64(unit))
	if duration <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return duration, nil
}

func applyExtractPartitionInterval(cfg *config.IngestConfig, raw string) error {
	if raw == "" {
		if strings.TrimSpace(cfg.ExtractPartitionBy) != "" {
			cfg.ExtractPartitionAuto = true
		}
		return nil
	}

	duration, numericInterval, auto, err := parseExtractPartitionInterval(raw)
	if err != nil {
		return fmt.Errorf("invalid extract-partition-interval: %w", err)
	}
	cfg.ExtractPartitionInterval = duration
	cfg.ExtractPartitionNumericInterval = numericInterval
	cfg.ExtractPartitionAuto = auto
	return nil
}
