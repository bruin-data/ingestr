package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pglogrepl"
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
	StrategyReplace      IncrementalStrategy = "replace"
	StrategyAppend       IncrementalStrategy = "append"
	StrategyDeleteInsert IncrementalStrategy = "delete+insert"
	StrategyMerge        IncrementalStrategy = "merge"
	StrategySCD2         IncrementalStrategy = "scd2"
	StrategyNone         IncrementalStrategy = "none"
	// Reverse-ETL-only strategies. Gated to destinations that implement
	// destination.ReverseETLDestination.
	StrategyUpdate IncrementalStrategy = "update"
	StrategyDelete IncrementalStrategy = "delete"
)

// RejectMode governs how a reverse-ETL write handles a row that cannot be
// applied (not found, or rejected by the API). Writes are not transactional,
// so valid rows already sent stay written in every mode.
type RejectMode string

const (
	// RejectFailFast stops at the first bad row; rows already sent stay written.
	RejectFailFast RejectMode = "fail_fast"
	// RejectFail sends everything (valid rows land), then fails with the reject list.
	RejectFail RejectMode = "fail"
	// RejectSkip sends everything, succeeds, and reports the rejects at the end.
	RejectSkip RejectMode = "skip"
)

type ProgressMode string

const (
	ProgressInteractive ProgressMode = "interactive"
	ProgressLog         ProgressMode = "log"
	ProgressJSON        ProgressMode = "json"
)

// TableNaming selects how a multi-table run maps a schema-qualified source
// table to its destination table when dest_schema funnels every table into one
// destination schema.
type TableNaming string

const (
	// TableNamingSchemaTable flattens the source-schema qualifier into the
	// table name: foo.A -> <dest_schema>.foo_A. Default.
	TableNamingSchemaTable TableNaming = "schema_table"
	// TableNamingTable keeps only the table component: foo.A -> <dest_schema>.A.
	TableNamingTable TableNaming = "table"
)

// CDCDeleteBehavior selects how a CDC merge applies source deletes to the
// destination table.
type CDCDeleteBehavior string

const (
	// CDCDeleteSoft keeps deleted rows as tombstones with _cdc_deleted = true.
	// Default.
	CDCDeleteSoft CDCDeleteBehavior = "soft"
	// CDCDeleteHard removes deleted rows from the destination table.
	CDCDeleteHard CDCDeleteBehavior = "hard"
)

type IngestConfig struct {
	SourceURI   string
	DestURI     string
	SourceTable string
	DestTable   string

	// SourceTables restricts a multi-table CDC run to the named tables. It is
	// populated from a comma-separated --source-table and is mutually
	// exclusive with SourceTable: a single name stays in SourceTable and takes
	// the single-table path, two or more move here and leave SourceTable empty
	// so the multi-table path runs against just this set.
	SourceTables []string

	IncrementalStrategy         IncrementalStrategy
	IncrementalStrategyExplicit bool
	IncrementalKey              string

	// RejectMode and WriteNulls apply only to reverse-ETL destinations.
	RejectMode RejectMode // fail_fast | fail | skip (default fail)
	// WriteNulls writes source NULLs through to clear the field. Default (unset) is
	// clear for reverse-ETL destinations; --write-nulls=false opts into omitting.
	WriteNulls bool
	// WriteNullsSet reports whether --write-nulls was passed explicitly, so the
	// clear-by-default can be applied only when the user left it unset.
	WriteNullsSet bool
	// ReverseETLDestination is set by the pipeline (not a flag) when the
	// destination is reverse-ETL, so strategy validation can relax rules the API
	// path handles itself (e.g. merge matching on id_property without a PK).
	ReverseETLDestination bool
	IncrementalPredicate  string
	IntervalStart         *time.Time
	IntervalEnd           *time.Time

	PrimaryKeys []string

	PartitionBy string
	ClusterBy   []string

	FullRefresh       bool
	RefreshID         string
	SchemaContract    string            // Schema contract mode: evolve, freeze, discard_row, discard_value
	SchemaNaming      string            // Schema naming convention: direct, snake_case, auto
	CDCTableNaming    TableNaming       // Multi-table CDC destination naming: schema_table, table
	CDCDeleteBehavior CDCDeleteBehavior // How CDC merges apply source deletes: soft, hard
	Yes               bool
	Progress          ProgressMode
	Debug             bool

	PageSize                        int
	MaxBatchBytes                   int64
	LoaderFileSize                  int
	LoaderFileFormat                string
	ExtractParallelism              int
	DestinationParallelism          int
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
	NoRunID           bool

	// RunID is stamped into _ingestr_run_id and embedded in managed staging
	// table names. Runtime-populated; empty when run id tracking is disabled.
	RunID string

	PipelinesDir   string
	StagingBucket  string
	StagingDataset string
	KeepStaging    bool // testing only: skip final DropTable so tests can inspect staging

	CDCSnapshotMode            string // Empty inherits the durable policy, or defaults to auto
	CDCChunkedSnapshot         bool
	CDCSnapshotChunkSize       int
	CDCSnapshotChunkTimeout    time.Duration
	CDCWALStaging              string // inmemory or destination
	CDCWALBufferBytes          int64
	CDCStartLSN                string // Bootstrap boundary for externally loaded PostgreSQL CDC targets
	CDCResumeLSN               string // For CDC sources: resume from this LSN (auto-detected from destination)
	CDCResumeIncarnation       string
	CDCResumeSchemaFingerprint string
	CDCSlotSuffix              string // For CDC sources: suffix appended to auto-generated slot names (derived from connector identity)
	CDCLegacySlotSuffix        string // For CDC upgrades: legacy 6-hex suffix derived from the raw destination URI

	Stream        bool          // Continuous ingestion: flush buffered records on an interval or record-count trigger
	FlushInterval time.Duration // Streaming mode: flush at least this often
	FlushRecords  int           // Streaming mode: flush when this many records are buffered

	// QueryAnnotations is a JSON object of external annotation keys (e.g. asset,
	// pipeline) supplied by the caller. When set, ingestr prepends a
	// "-- @bruin.config: {...}" comment to destination queries (QUERY_TAG on
	// Snowflake) for warehouse cost attribution. Empty disables annotations.
	QueryAnnotations string
}

// DefaultExtractParallelism is the default of the --extract-parallelism CLI
// flag. EffectiveDestinationParallelism compares against it to tell "user left
// the default" apart from an explicit choice, which cmd records by setting
// DestinationParallelism.
const DefaultExtractParallelism = 5

const (
	DefaultCDCSnapshotChunkSize    = 10000
	DefaultCDCSnapshotChunkTimeout = 30 * time.Second
	DefaultCDCWALBufferBytes       = int64(64 << 20)
)

const defaultPostgresWriteParallelism = 8

func (c *IngestConfig) EffectiveDestinationParallelism() int {
	if c.DestinationParallelism > 0 {
		return c.DestinationParallelism
	}
	parallelism := c.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}
	if parallelism == DefaultExtractParallelism && isPostgresURI(c.DestURI) {
		return defaultPostgresWriteParallelism
	}
	return parallelism
}

func isPostgresURI(uri string) bool {
	scheme, _, _ := strings.Cut(strings.ToLower(uri), "://")
	return scheme == "postgres" || scheme == "postgresql" || scheme == "postgresql+psycopg2"
}

func DefaultConfig() *IngestConfig {
	return &IngestConfig{
		IncrementalStrategy:     StrategyReplace,
		SchemaContract:          "evolve",
		SchemaNaming:            "",
		CDCTableNaming:          TableNamingSchemaTable,
		CDCDeleteBehavior:       CDCDeleteSoft,
		CDCSnapshotChunkSize:    DefaultCDCSnapshotChunkSize,
		CDCSnapshotChunkTimeout: DefaultCDCSnapshotChunkTimeout,
		CDCWALStaging:           "inmemory",
		CDCWALBufferBytes:       DefaultCDCWALBufferBytes,
		Progress:                ProgressInteractive,
		PageSize:                25000,
		MaxBatchBytes:           512 << 20, // 512 MiB
		LoaderFileSize:          25000,
		ExtractParallelism:      5,
		FlushInterval:           30 * time.Second,
		FlushRecords:            50000,
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
	if c.SourceTable == "" && len(c.SourceTables) == 0 && !c.IsCDCSource() {
		return &ValidationError{Field: "source-table", Message: "is required"}
	}
	if err := c.validateSourceTables(); err != nil {
		return err
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
	if c.IncrementalStrategy == IncrementalStrategy("truncate+insert") {
		return &ValidationError{
			Field:   "incremental-strategy",
			Message: `"truncate+insert" has been removed; use "replace"`,
		}
	}
	if err := c.validateCDCTableNaming(); err != nil {
		return err
	}
	if err := c.ValidateCDCSnapshotMode(); err != nil {
		return err
	}
	if err := c.validateCDCDeleteBehavior(); err != nil {
		return err
	}
	if err := c.validateExtractPartitioning(); err != nil {
		return err
	}
	if c.NoInference && strings.TrimSpace(c.Columns) == "" {
		return &ValidationError{Field: "columns", Message: "is required when no-inference is enabled"}
	}
	if strings.TrimSpace(c.IncrementalPredicate) != "" {
		if c.FullRefresh {
			return &ValidationError{Field: "incremental-predicate", Message: "cannot be combined with --full-refresh"}
		}
	}
	if c.RefreshID != "" && (!c.FullRefresh || !c.Stream) {
		return &ValidationError{Field: "refresh-id", Message: "requires --full-refresh and --stream"}
	}
	if c.Stream {
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
	if c.IsCDCSource() {
		if err := c.validateCDCMode(); err != nil {
			return err
		}
	}
	if c.IsChangeTrackingSource() && c.SQLLimit > 0 {
		return &ValidationError{Field: "sql-limit", Message: "is not supported for SQL Server Change Tracking sources because partial snapshots cannot safely advance the resume cursor"}
	}
	if c.IsChangeTrackingSource() && (!c.FullRefresh || c.Stream) && c.IncrementalStrategyExplicit && c.IncrementalStrategy != StrategyMerge {
		return &ValidationError{Field: "incremental-strategy", Message: fmt.Sprintf("must be %q for SQL Server Change Tracking sources unless batch full-refresh is enabled", StrategyMerge)}
	}
	return nil
}

// ValidateCDCSnapshotMode also runs after loading the destination's saved policy.
func (c *IngestConfig) ValidateCDCSnapshotMode() error {
	if err := c.validateCDCChunkedSnapshot(); err != nil {
		return err
	}
	if c.CDCSnapshotMode == "" && c.CDCStartLSN == "" {
		return nil
	}
	u, err := url.Parse(c.SourceURI)
	if err != nil || (u.Scheme != "postgres+cdc" && u.Scheme != "postgresql+cdc") {
		return &ValidationError{Field: "cdc-snapshot-mode", Message: "is only supported for postgres+cdc sources"}
	}
	if c.CDCSnapshotMode != "" && c.CDCSnapshotMode != "auto" && c.CDCSnapshotMode != "never" {
		return &ValidationError{Field: "cdc-snapshot-mode", Message: "must be auto or never"}
	}
	if c.CDCStartLSN != "" {
		if c.CDCSnapshotMode != "never" {
			return &ValidationError{Field: "cdc-start-lsn", Message: "requires --cdc-snapshot-mode never"}
		}
		lsn, err := pglogrepl.ParseLSN(c.CDCStartLSN)
		if err != nil || lsn == 0 {
			return &ValidationError{Field: "cdc-start-lsn", Message: "must be a nonzero PostgreSQL LSN"}
		}
	}
	if c.CDCSnapshotMode != "never" {
		return nil
	}
	if c.FullRefresh {
		return &ValidationError{Field: "full-refresh", Message: "cannot be combined with CDC snapshot mode never"}
	}
	if c.SourceTable == "" && len(c.SourceTables) == 0 {
		return &ValidationError{Field: "source-table", Message: "must explicitly select tables with CDC snapshot mode never"}
	}
	if u.Query().Get("slot") == "" {
		return &ValidationError{Field: "source-uri", Message: "CDC snapshot mode never requires an existing explicit slot= replication slot"}
	}
	if c.IncrementalStrategy != "" && c.IncrementalStrategy != StrategyMerge &&
		(c.IncrementalStrategy != StrategyReplace || c.IncrementalStrategyExplicit) {
		return &ValidationError{Field: "incremental-strategy", Message: "CDC snapshot mode never requires merge"}
	}
	return nil
}

// ApplyCDCChunkedSnapshotDefaults fills options omitted by programmatic callers.
func (c *IngestConfig) ApplyCDCChunkedSnapshotDefaults() {
	if !c.CDCChunkedSnapshot {
		return
	}
	if c.CDCSnapshotChunkSize == 0 {
		c.CDCSnapshotChunkSize = DefaultCDCSnapshotChunkSize
	}
	if c.CDCSnapshotChunkTimeout == 0 {
		c.CDCSnapshotChunkTimeout = DefaultCDCSnapshotChunkTimeout
	}
	if c.CDCWALStaging == "" {
		c.CDCWALStaging = "inmemory"
	}
	if c.CDCWALBufferBytes == 0 {
		c.CDCWALBufferBytes = DefaultCDCWALBufferBytes
	}
	if c.FlushInterval == 0 {
		c.FlushInterval = 30 * time.Second
	}
	if c.FlushRecords == 0 {
		c.FlushRecords = 50000
	}
}

func (c *IngestConfig) validateCDCChunkedSnapshot() error {
	if c.CDCWALStaging != "" && c.CDCWALStaging != "inmemory" && c.CDCWALStaging != "destination" {
		return &ValidationError{Field: "cdc-wal-staging", Message: "must be inmemory or destination"}
	}
	if !c.CDCChunkedSnapshot {
		return nil
	}
	c.ApplyCDCChunkedSnapshotDefaults()
	u, err := url.Parse(c.SourceURI)
	if err != nil || (u.Scheme != "postgres+cdc" && u.Scheme != "postgresql+cdc") {
		return &ValidationError{Field: "cdc-snapshot-chunked", Message: "is only supported for postgres+cdc sources"}
	}
	if c.CDCSnapshotMode == "never" {
		return &ValidationError{Field: "cdc-snapshot-chunked", Message: "cannot be combined with CDC snapshot mode never"}
	}
	if c.CDCStartLSN != "" {
		return &ValidationError{Field: "cdc-snapshot-chunked", Message: "cannot be combined with --cdc-start-lsn"}
	}
	if c.IncrementalStrategy != "" && c.IncrementalStrategy != StrategyMerge &&
		(c.IncrementalStrategy != StrategyReplace || c.IncrementalStrategyExplicit) {
		return &ValidationError{Field: "incremental-strategy", Message: "chunked CDC snapshots require merge"}
	}
	for _, option := range []struct {
		field string
		set   bool
	}{
		{"sql-limit", c.SQLLimit != 0},
		{"sql-exclude-columns", len(c.SQLExcludeColumns) != 0},
		{"incremental-key", c.IncrementalKey != ""},
		{"incremental-predicate", strings.TrimSpace(c.IncrementalPredicate) != ""},
		{"interval-start", c.IntervalStart != nil},
		{"interval-end", c.IntervalEnd != nil},
		{"extract-partition-by", c.ExtractPartitionBy != ""},
	} {
		if option.set {
			return &ValidationError{Field: option.field, Message: "cannot be combined with chunked CDC snapshots"}
		}
	}
	if c.CDCSnapshotChunkSize <= 0 {
		return &ValidationError{Field: "cdc-snapshot-chunk-size", Message: "must be positive"}
	}
	if c.CDCSnapshotChunkTimeout <= 0 {
		return &ValidationError{Field: "cdc-snapshot-chunk-timeout", Message: "must be positive"}
	}
	if c.CDCWALBufferBytes <= 0 {
		return &ValidationError{Field: "cdc-wal-buffer-bytes", Message: "must be positive"}
	}
	if c.FlushInterval <= 0 {
		return &ValidationError{Field: "flush-interval", Message: "must be positive"}
	}
	if c.FlushRecords <= 0 {
		return &ValidationError{Field: "flush-records", Message: "must be positive"}
	}
	return nil
}

// ParsePartitionExpression checks a partition_by expression against the destination's syntax.
func (c *IngestConfig) ParsePartitionExpression(value string) error {
	if !schema.IsPartitionExpression(value) {
		return nil
	}
	var err error
	switch scheme, _, _ := strings.Cut(strings.ToLower(c.DestURI), "://"); {
	case scheme == "bigquery":
		_, err = schema.ParseBigQueryPartitionBy(value)
	case scheme == "iceberg" || strings.HasPrefix(scheme, "iceberg+"):
		_, err = schema.ParseIcebergPartitionBy(value)
	default:
		err = errors.New("partition expressions are not supported for this destination")
	}
	return err
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
	if c.IntervalStart == nil && c.IntervalEnd != nil {
		return &ValidationError{Field: "interval-start", Message: "is required when interval-end is set"}
	}
	if c.IntervalEnd == nil && c.IntervalStart != nil {
		return &ValidationError{Field: "interval-end", Message: "is required when interval-start is set"}
	}
	if c.IncrementalKey != "" && c.IntervalStart == nil {
		return &ValidationError{Field: "interval-start", Message: "is required when extract partitioning is enabled"}
	}
	if c.IncrementalKey != "" && c.IntervalEnd == nil {
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
	return nil
}

// IsCDCSource returns true if the source URI is a CDC source.
func (c *IngestConfig) IsCDCSource() bool {
	return IsCDCSourceURI(c.SourceURI)
}

// IsCDCSourceURI reports whether a source URI names a CDC connector. It is
// exported so the CLI can decide how to parse --source-table before a config
// exists.
func IsCDCSourceURI(sourceURI string) bool {
	schemeEnd := strings.Index(sourceURI, "://")
	if schemeEnd == -1 {
		return false
	}
	return strings.Contains(strings.ToLower(sourceURI[:schemeEnd]), "+cdc")
}

// validateCDCTableNaming rejects unknown --cdc-table-naming values and the "table"
// mode outside multi-table ingestion, where it would silently do nothing.
func (c *IngestConfig) validateCDCTableNaming() error {
	switch c.CDCTableNaming {
	case "", TableNamingSchemaTable:
		return nil
	case TableNamingTable:
		if c.SourceTable != "" {
			return &ValidationError{
				Field:   "cdc-table-naming",
				Message: fmt.Sprintf("%q applies to multi-table ingestion only; use --dest-table to choose a single table's destination", TableNamingTable),
			}
		}
		return nil
	default:
		return &ValidationError{
			Field:   "cdc-table-naming",
			Message: fmt.Sprintf("invalid value %q (must be %q or %q)", c.CDCTableNaming, TableNamingSchemaTable, TableNamingTable),
		}
	}
}

// HardDeletesCDC reports whether CDC merges remove deleted rows instead of
// keeping tombstones.
func (c *IngestConfig) HardDeletesCDC() bool {
	return c.CDCDeleteBehavior == CDCDeleteHard
}

func (c *IngestConfig) validateCDCDeleteBehavior() error {
	switch c.CDCDeleteBehavior {
	case "", CDCDeleteSoft:
		return nil
	case CDCDeleteHard:
		if !c.IsCDCSource() && !c.IsChangeTrackingSource() {
			return &ValidationError{
				Field:   "cdc-delete-behavior",
				Message: fmt.Sprintf("%q applies to CDC and Change Tracking sources only", CDCDeleteHard),
			}
		}
		if c.IncrementalStrategyExplicit && c.IncrementalStrategy != StrategyMerge && (!c.FullRefresh || c.Stream) {
			return &ValidationError{
				Field:   "cdc-delete-behavior",
				Message: fmt.Sprintf("%q requires the %q incremental strategy or a non-streaming full refresh", CDCDeleteHard, StrategyMerge),
			}
		}
		return nil
	default:
		return &ValidationError{
			Field:   "cdc-delete-behavior",
			Message: fmt.Sprintf("invalid value %q (must be %q or %q)", c.CDCDeleteBehavior, CDCDeleteSoft, CDCDeleteHard),
		}
	}
}

// validateSourceTables checks the multi-table subset produced by a
// comma-separated --source-table.
func (c *IngestConfig) validateSourceTables() error {
	if len(c.SourceTables) == 0 {
		return nil
	}
	if c.SourceTable != "" {
		return &ValidationError{
			Field:   "source-table",
			Message: "cannot name both a single table and a table list",
		}
	}
	if !c.IsCDCSource() {
		return &ValidationError{
			Field:   "source-table",
			Message: "accepts a comma-separated list of tables for CDC sources only",
		}
	}
	if len(c.SourceTables) < 2 {
		return &ValidationError{
			Field:   "source-table",
			Message: "table list needs at least two tables; name one table directly to ingest it alone",
		}
	}
	seen := make(map[string]struct{}, len(c.SourceTables))
	for _, table := range c.SourceTables {
		if strings.TrimSpace(table) == "" {
			return &ValidationError{Field: "source-table", Message: "table list has an empty entry"}
		}
		if _, ok := seen[table]; ok {
			return &ValidationError{
				Field:   "source-table",
				Message: fmt.Sprintf("table list repeats %s", table),
			}
		}
		seen[table] = struct{}{}
	}
	return nil
}

// validateCDCMode enforces the deprecation of the ?mode= CDC URI parameter.
// Continuous ingestion is selected by --stream alone; mode= no longer has any
// effect. mode=stream on its own used to leave the source reading forever while
// the batch write path waited for a read that never ended, so it is rejected
// rather than silently ignored.
func (c *IngestConfig) validateCDCMode() error {
	parsed, err := url.Parse(c.SourceURI)
	if err != nil {
		// Leave malformed URIs to the source's own parser, which reports better.
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(parsed.Query().Get("mode")))
	switch mode {
	case "":
		return nil
	case "batch":
	case "stream":
		if !c.Stream {
			return &ValidationError{
				Field:   "source-uri",
				Message: "mode=stream is no longer supported; remove it and use --stream on sources that support continuous ingestion",
			}
		}
	default:
		return &ValidationError{
			Field:   "source-uri",
			Message: fmt.Sprintf("invalid mode: %s (must be 'batch' or 'stream')", mode),
		}
	}
	output.Warnf("Warning: the ?mode= URI parameter is deprecated and ignored; continuous ingestion is controlled by --stream\n")
	return nil
}

func (c *IngestConfig) IsChangeTrackingSource() bool {
	schemeEnd := strings.Index(c.SourceURI, "://")
	if schemeEnd == -1 {
		return false
	}
	return strings.Contains(strings.ToLower(c.SourceURI[:schemeEnd]), "+ct")
}

// ResolveWriteNulls decides whether a reverse-ETL run writes source NULLs through
// to clear the field. Reverse-ETL clears by default; only an explicit --write-nulls
// (writeNullsSet) keeps the caller's value. Non-reverse-ETL passes the value as-is.
func ResolveWriteNulls(reverseETL, writeNullsSet, writeNulls bool) bool {
	if reverseETL && !writeNullsSet {
		return true
	}
	return writeNulls
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + " " + e.Message
}
