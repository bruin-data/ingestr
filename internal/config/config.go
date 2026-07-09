package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/output"
)

var DebugMode bool

func Debug(format string, args ...any) {
	if DebugMode {
		line := fmt.Sprintf("%s\tDEBUG\t%s\n", time.Now().Format("2006-01-02T15:04:05.000Z0700"), fmt.Sprintf(format, args...))
		output.WriteDebug(line)
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
	ProgressJSON        ProgressMode = "json"
)

type IngestConfig struct {
	SourceURI   string
	DestURI     string
	SourceTable string
	DestTable   string

	IncrementalStrategy         IncrementalStrategy
	IncrementalStrategyExplicit bool
	IncrementalKey              string
	IntervalStart               *time.Time
	IntervalEnd                 *time.Time

	PrimaryKeys []string

	PartitionBy string
	ClusterBy   []string

	FullRefresh    bool
	SchemaContract string // Schema contract mode: evolve, freeze, discard_row, discard_value
	SchemaNaming   string // Schema naming convention: direct, snake_case, auto
	Yes            bool
	Progress       ProgressMode
	Debug          bool

	PageSize                        int
	LoaderFileSize                  int
	LoaderFileFormat                string
	ExtractParallelism              int
	ExtractPartitionBy              string
	ExtractPartitionInterval        time.Duration
	ExtractPartitionNumericInterval int64
	ExtractPartitionAuto            bool
	DisablePreStaging               bool // Skip extract-time load-file staging for schema-inferred sources

	SQLLimit          int
	SQLExcludeColumns []string
	Columns           string // Raw column overrides string (parsed by pipeline)
	NoInference       bool   // Skip schema inference for unknown-schema sources and use Columns as the schema
	Mask              []string
	TrimWhitespace    bool
	NoLoadTimestamp   bool

	PipelinesDir   string
	StagingBucket  string
	StagingDataset string
	KeepStaging    bool // testing only: skip final DropTable so tests can inspect staging

	CDCResumeLSN  string // For CDC sources: resume from this LSN (auto-detected from destination)
	CDCSlotSuffix string // For CDC sources: suffix appended to auto-generated slot names (derived from dest URI)

	Stream        bool          // Continuous ingestion: flush buffered records on an interval or record-count trigger
	FlushInterval time.Duration // Streaming mode: flush at least this often
	FlushRecords  int           // Streaming mode: flush when this many records are buffered

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
		FlushInterval:       30 * time.Second,
		FlushRecords:        50000,
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
	if err := c.validateExtractPartitioning(); err != nil {
		return err
	}
	if c.NoInference && strings.TrimSpace(c.Columns) == "" {
		return &ValidationError{Field: "columns", Message: "is required when no-inference is enabled"}
	}
	if c.Stream {
		if c.FullRefresh {
			return &ValidationError{Field: "full-refresh", Message: "cannot be combined with --stream"}
		}
		if c.IntervalEnd != nil {
			return &ValidationError{Field: "interval-end", Message: "cannot be combined with --stream (a bounded end contradicts continuous ingestion)"}
		}
		if c.SQLLimit > 0 {
			return &ValidationError{Field: "sql-limit", Message: "cannot be combined with --stream"}
		}
		if c.FlushInterval <= 0 {
			return &ValidationError{Field: "flush-interval", Message: "must be positive"}
		}
		if c.FlushRecords <= 0 {
			return &ValidationError{Field: "flush-records", Message: "must be positive"}
		}
		switch c.IncrementalStrategy {
		case "", StrategyMerge, StrategyAppend:
		default:
			return &ValidationError{Field: "incremental-strategy", Message: fmt.Sprintf("%q is not supported with --stream (only merge and append)", c.IncrementalStrategy)}
		}
	}
	if c.IsChangeTrackingSource() && c.SQLLimit > 0 {
		return &ValidationError{Field: "sql-limit", Message: "is not supported for SQL Server Change Tracking sources because partial snapshots cannot safely advance the resume cursor"}
	}
	if c.IsChangeTrackingSource() && !c.FullRefresh && c.IncrementalStrategyExplicit && c.IncrementalStrategy != StrategyMerge {
		return &ValidationError{Field: "incremental-strategy", Message: fmt.Sprintf("must be %q for SQL Server Change Tracking sources unless full-refresh is enabled", StrategyMerge)}
	}
	return nil
}

func (c *IngestConfig) validateExtractPartitioning() error {
	hasColumn := strings.TrimSpace(c.ExtractPartitionBy) != ""
	hasInterval := c.ExtractPartitionInterval != 0 || c.ExtractPartitionNumericInterval != 0 || c.ExtractPartitionAuto
	if !hasColumn && !hasInterval {
		return nil
	}
	if !hasColumn {
		return &ValidationError{Field: "extract-partition-by", Message: "is required when extract-partition-interval is set"}
	}
	if !hasInterval {
		return &ValidationError{Field: "extract-partition-interval", Message: "is required when extract-partition-by is set"}
	}
	modeCount := 0
	if c.ExtractPartitionInterval != 0 {
		modeCount++
	}
	if c.ExtractPartitionNumericInterval != 0 {
		modeCount++
	}
	if c.ExtractPartitionAuto {
		modeCount++
	}
	if modeCount > 1 {
		return &ValidationError{Field: "extract-partition-interval", Message: "must be one of auto, a duration, or an integer step"}
	}
	if c.ExtractPartitionInterval < 0 || c.ExtractPartitionNumericInterval < 0 {
		return &ValidationError{Field: "extract-partition-interval", Message: "must be positive"}
	}
	if c.IntervalStart == nil {
		return &ValidationError{Field: "interval-start", Message: "is required when extract partitioning is enabled"}
	}
	if c.IntervalEnd == nil {
		return &ValidationError{Field: "interval-end", Message: "is required when extract partitioning is enabled"}
	}
	if c.SQLLimit > 0 {
		return &ValidationError{Field: "sql-limit", Message: "cannot be combined with extract partitioning"}
	}
	if c.Stream {
		return &ValidationError{Field: "stream", Message: "cannot be combined with extract partitioning"}
	}
	if c.IsCDCSource() {
		return &ValidationError{Field: "source-uri", Message: "CDC sources do not support extract partitioning"}
	}
	if c.IsChangeTrackingSource() {
		return &ValidationError{Field: "source-uri", Message: "change tracking sources do not support extract partitioning"}
	}
	if c.FullRefresh {
		return &ValidationError{Field: "full-refresh", Message: "cannot be combined with extract partitioning"}
	}
	switch c.IncrementalStrategy {
	case StrategyReplace, StrategyTruncateInsert:
		return &ValidationError{Field: "incremental-strategy", Message: fmt.Sprintf("%q cannot be combined with extract partitioning because it rewrites the whole destination table from a bounded source read", c.IncrementalStrategy)}
	}
	if rawQuery, ok := strings.CutPrefix(c.SourceTable, "query:"); ok {
		if strings.TrimSpace(rawQuery) == "" {
			return nil
		}
		return &ValidationError{Field: "source-table", Message: "custom queries do not support extract partitioning"}
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

func (c *IngestConfig) IsChangeTrackingSource() bool {
	schemeEnd := strings.Index(c.SourceURI, "://")
	if schemeEnd == -1 {
		return false
	}
	return strings.Contains(strings.ToLower(c.SourceURI[:schemeEnd]), "+ct")
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + " " + e.Message
}
