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
	TableName              string
	TablePrimaryKeys       []string
	TablePrimaryKeysUnique bool
	TableIncrementalKey    string
	TableStrategy          config.IncrementalStrategy
	TablePartitionBy       string
	KnownSchema            bool
	SchemaFn               func(ctx context.Context) (*schema.TableSchema, error)
	ReadFn                 func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error)
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

func (t *DynamicSourceTable) HasKnownSchema() bool {
	return t.KnownSchema
}

func (t *DynamicSourceTable) GetSchema(ctx context.Context) (*schema.TableSchema, error) {
	return t.SchemaFn(ctx)
}

func (t *DynamicSourceTable) Read(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
	return t.ReadFn(ctx, opts)
}

var _ SourceTable = (*DynamicSourceTable)(nil)

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
