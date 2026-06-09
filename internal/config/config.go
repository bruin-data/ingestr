package config

import (
	"fmt"
	"strings"
	"time"
)

var DebugMode bool

func Debug(format string, args ...interface{}) {
	if DebugMode {
		fmt.Printf("%s\tDEBUG\t%s\n", time.Now().Format("2006-01-02T15:04:05.000Z0700"), fmt.Sprintf(format, args...))
	}
}

type IncrementalStrategy string

const (
	StrategyReplace        IncrementalStrategy = "replace"
	StrategyTruncateInsert IncrementalStrategy = "truncate+insert"
	StrategyAppend         IncrementalStrategy = "append"
	StrategyDeleteInsert   IncrementalStrategy = "delete+insert"
	StrategyMerge          IncrementalStrategy = "merge"
	StrategySCD2           IncrementalStrategy = "scd2"
	StrategyNone           IncrementalStrategy = "none"
)

type ProgressMode string

const (
	ProgressInteractive ProgressMode = "interactive"
	ProgressLog         ProgressMode = "log"
)

type IngestConfig struct {
	SourceURI   string
	DestURI     string
	SourceTable string
	DestTable   string

	IncrementalStrategy IncrementalStrategy
	IncrementalKey      string
	IntervalStart       *time.Time
	IntervalEnd         *time.Time

	PrimaryKeys []string

	PartitionBy string
	ClusterBy   []string

	FullRefresh    bool
	SchemaContract string // Schema contract mode: evolve, freeze, discard_row, discard_value
	SchemaNaming   string // Schema naming convention: direct, snake_case, auto
	Yes            bool
	Progress       ProgressMode
	Debug          bool

	PageSize           int
	LoaderFileSize     int
	LoaderFileFormat   string
	ExtractParallelism int

	SQLLimit          int
	SQLExcludeColumns []string
	Columns           string // Raw column overrides string (parsed by pipeline)
	NoInference       bool   // Skip schema inference for unknown-schema sources and use Columns as the schema
	Mask              []string

	PipelinesDir   string
	StagingBucket  string
	StagingDataset string
	KeepStaging    bool // testing only: skip final DropTable so tests can inspect staging

	CDCResumeLSN  string // For CDC sources: resume from this LSN (auto-detected from destination)
	CDCSlotSuffix string // For CDC sources: suffix appended to auto-generated slot names (derived from dest URI)

	// QueryAnnotations is a JSON object of external annotation keys (e.g. asset,
	// pipeline) supplied by the caller. When set, ingestr prepends a
	// "-- @bruin.config: {...}" comment to destination queries (QUERY_TAG on
	// Snowflake) for warehouse cost attribution. Empty disables annotations.
	QueryAnnotations string
}

func DefaultConfig() *IngestConfig {
	return &IngestConfig{
		IncrementalStrategy: StrategyReplace,
		SchemaContract:      "evolve",
		SchemaNaming:        "",
		Progress:            ProgressInteractive,
		PageSize:            25000,
		LoaderFileSize:      25000,
		ExtractParallelism:  5,
	}
}

func (c *IngestConfig) Validate() error {
	if c.SourceURI == "" {
		return &ValidationError{Field: "source-uri", Message: "is required"}
	}
	if c.DestURI == "" {
		return &ValidationError{Field: "dest-uri", Message: "is required"}
	}
	// Source table is required unless this is a CDC source (multi-table mode)
	if c.SourceTable == "" && !c.IsCDCSource() {
		return &ValidationError{Field: "source-table", Message: "is required"}
	}
	if c.DestTable == "" {
		c.DestTable = c.SourceTable
	}
	if c.IntervalStart != nil && c.IntervalEnd != nil && !c.IntervalStart.Before(*c.IntervalEnd) {
		return &ValidationError{
			Field:   "interval-start",
			Message: fmt.Sprintf("must be earlier than interval-end (got start=%s, end=%s)", c.IntervalStart.Format(time.RFC3339), c.IntervalEnd.Format(time.RFC3339)),
		}
	}
	if c.NoInference && strings.TrimSpace(c.Columns) == "" {
		return &ValidationError{Field: "columns", Message: "is required when no-inference is enabled"}
	}
	return nil
}

// IsCDCSource returns true if the source URI is a CDC source.
func (c *IngestConfig) IsCDCSource() bool {
	schemeEnd := strings.Index(c.SourceURI, "://")
	if schemeEnd == -1 {
		return false
	}
	return strings.Contains(strings.ToLower(c.SourceURI[:schemeEnd]), "+cdc")
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + " " + e.Message
}
