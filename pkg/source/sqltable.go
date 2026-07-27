package source

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// DynamicSourceTable is a SourceTable implementation for sources
// where metadata is queried from the database at runtime or inferred from data.
type DynamicSourceTable struct {
	TableName                        string
	TablePrimaryKeys                 []string
	TablePrimaryKeysUnique           bool
	TableIncrementalKey              string
	TableStrategy                    config.IncrementalStrategy
	TablePartitionBy                 string
	TableSupportsExtractPartitioning bool
	KnownSchema                      bool
	SchemaFn                         func(ctx context.Context) (*schema.TableSchema, error)
	ReadSchemaFn                     func(ctx context.Context, opts ReadOptions) (*schema.TableSchema, error)
	ReadFn                           func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error)
}

func (t *DynamicSourceTable) Name() string {
	return t.TableName
}

func (t *DynamicSourceTable) PrimaryKeys() []string {
	return t.TablePrimaryKeys
}

func (t *DynamicSourceTable) PrimaryKeysUnique() bool {
	return t.TablePrimaryKeysUnique
}

func (t *DynamicSourceTable) IncrementalKey() string {
	return t.TableIncrementalKey
}

func (t *DynamicSourceTable) Strategy() config.IncrementalStrategy {
	return t.TableStrategy
}

func (t *DynamicSourceTable) PartitionBy() string {
	return t.TablePartitionBy
}

func (t *DynamicSourceTable) SupportsExtractPartitioning() bool {
	return t.TableSupportsExtractPartitioning
}

func (t *DynamicSourceTable) HasKnownSchema() bool {
	return t.KnownSchema
}

func (t *DynamicSourceTable) GetSchema(ctx context.Context) (*schema.TableSchema, error) {
	return t.SchemaFn(ctx)
}

func (t *DynamicSourceTable) GetReadSchema(ctx context.Context, opts ReadOptions) (*schema.TableSchema, error) {
	if t.ReadSchemaFn != nil {
		return t.ReadSchemaFn(ctx, opts)
	}
	return t.GetSchema(ctx)
}

func (t *DynamicSourceTable) SupportsReadSchema() bool {
	return t.ReadSchemaFn != nil
}

func (t *DynamicSourceTable) Read(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
	return t.ReadFn(ctx, opts)
}

var (
	_ SourceTable        = (*DynamicSourceTable)(nil)
	_ ReadSchemaProvider = (*DynamicSourceTable)(nil)
)

// IsCustomQuery checks if a table name has the "query:" prefix indicating a custom SQL query.
func IsCustomQuery(tableName string) (string, bool) {
	query, ok := strings.CutPrefix(tableName, "query:")
	return query, ok
}

const CustomQueryTableName = "__custom_query__"

// CustomQueryTable builds a DynamicSourceTable for a custom SQL query.
func CustomQueryTable(
	req TableRequest,
	executeFn func(ctx context.Context, query string, opts ReadOptions) (<-chan RecordBatchResult, error),
) (*DynamicSourceTable, error) {
	rawQuery, ok := IsCustomQuery(req.Name)
	if !ok {
		return nil, fmt.Errorf("not a custom query: %s", req.Name)
	}
	if rawQuery == "" {
		return nil, fmt.Errorf("custom query cannot be empty (nothing after \"query:\")")
	}

	return &DynamicSourceTable{
		TableName:           CustomQueryTableName,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       req.Strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("schema is not available for custom queries; use schema inference")
		},
		ReadFn: func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
			query := SubstituteIntervalParams(rawQuery, opts.IntervalStart, opts.IntervalEnd)
			if strings.Contains(query, ":interval_start") || strings.Contains(query, ":interval_end") {
				return nil, fmt.Errorf("custom query contains unresolved interval placeholders; provide --interval-start and --interval-end")
			}
			return executeFn(ctx, query, opts)
		},
	}, nil
}

type PartitionedCustomQueryOptions struct {
	QuoteIdentifier func(string) string
	FormatTime      func(time.Time) string
	GetSchema       func(ctx context.Context, query string) (*schema.TableSchema, error)
	DiscoverBounds  func(ctx context.Context, query string, opts ReadOptions) (ExtractPartitionBounds, error)
}

// PartitionedCustomQueryTable builds a custom query table whose result set can
// be split into extract partition windows.
func PartitionedCustomQueryTable(
	req TableRequest,
	executeFn func(ctx context.Context, query string, opts ReadOptions) (<-chan RecordBatchResult, error),
	partitioning PartitionedCustomQueryOptions,
) (*DynamicSourceTable, error) {
	rawQuery, ok := IsCustomQuery(req.Name)
	if !ok {
		return nil, fmt.Errorf("not a custom query: %s", req.Name)
	}
	if strings.TrimSpace(rawQuery) == "" {
		return nil, fmt.Errorf("custom query cannot be empty (nothing after \"query:\")")
	}
	if partitioning.QuoteIdentifier == nil || partitioning.FormatTime == nil || partitioning.GetSchema == nil || partitioning.DiscoverBounds == nil {
		return nil, fmt.Errorf("partitioned custom query requires quoting, time formatting, schema, and bounds callbacks")
	}

	resolveQuery := func(opts ReadOptions) (string, error) {
		query := SubstituteIntervalParams(rawQuery, opts.IntervalStart, opts.IntervalEnd)
		if strings.Contains(query, ":interval_start") || strings.Contains(query, ":interval_end") {
			return "", fmt.Errorf("custom query contains unresolved interval placeholders; provide --interval-start and --interval-end")
		}
		return query, nil
	}

	return &DynamicSourceTable{
		TableName:                        CustomQueryTableName,
		TablePrimaryKeys:                 req.PrimaryKeys,
		TableIncrementalKey:              req.IncrementalKey,
		TableStrategy:                    req.Strategy,
		TableSupportsExtractPartitioning: true,
		KnownSchema:                      false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("schema is not available for custom queries without read options; use schema inference")
		},
		ReadSchemaFn: func(ctx context.Context, opts ReadOptions) (*schema.TableSchema, error) {
			query, err := resolveQuery(opts)
			if err != nil {
				return nil, err
			}
			return partitioning.GetSchema(ctx, SQLCustomQuerySchemaQuery(query, partitioning.QuoteIdentifier))
		},
		ReadFn: func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
			query, err := resolveQuery(opts)
			if err != nil {
				return nil, err
			}
			if !opts.ExtractPartitioningEnabled() {
				return executeFn(ctx, query, opts)
			}

			read := func(ctx context.Context, readOpts ReadOptions) (<-chan RecordBatchResult, error) {
				windowQuery := SQLCustomQuerySelectQuery(query, readOpts, partitioning.QuoteIdentifier, partitioning.FormatTime)
				return executeFn(ctx, windowQuery, readOpts)
			}
			discover := func(ctx context.Context, readOpts ReadOptions) (ExtractPartitionBounds, error) {
				boundsQuery := SQLCustomQueryBoundsQuery(query, readOpts.ExtractPartitionBy, partitioning.QuoteIdentifier)
				return partitioning.DiscoverBounds(ctx, boundsQuery, readOpts)
			}
			partitionSchema := opts.ExtractPartitionSchema
			if partitionSchema == nil {
				partitionSchema = opts.Schema
			}
			return ReadExtractPartitions(ctx, opts, partitionSchema, read, discover)
		},
	}, nil
}

func SubstituteIntervalParams(query string, intervalStart, intervalEnd *time.Time) string {
	if intervalStart != nil {
		query = strings.ReplaceAll(query, ":interval_start",
			fmt.Sprintf("'%s'", intervalStart.Format("2006-01-02 15:04:05-07:00")))
	}
	if intervalEnd != nil {
		query = strings.ReplaceAll(query, ":interval_end",
			fmt.Sprintf("'%s'", intervalEnd.Format("2006-01-02 15:04:05-07:00")))
	}
	return query
}
