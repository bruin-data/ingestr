package config

import (
	"fmt"
	"strings"
	"time"
)

var DebugMode bool

func Debug(format string, args ...interface{}) {
	if DebugMode {
		fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
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
	Mask              []string

	PipelinesDir   string
	StagingBucket  string
	StagingDataset string

	CDCResumeLSN  string // For CDC sources: resume from this LSN (auto-detected from destination)
	CDCSlotSuffix string // For CDC sources: suffix appended to auto-generated slot names (derived from dest URI)
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
	return nil
}

// IsCDCSource returns true if the source URI is a CDC source.
func (c *IngestConfig) IsCDCSource() bool {
	return strings.HasPrefix(c.SourceURI, "postgres+cdc://") || strings.HasPrefix(c.SourceURI, "postgresql+cdc://")
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + " " + e.Message
}
