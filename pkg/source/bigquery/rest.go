package bigquery

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"github.com/bruin-data/ingestr/internal/annotation"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"google.golang.org/api/iterator"
)

const restDefaultBatchSize = 100000

// restSource reads BigQuery through the REST API only: tabledata.list for
// whole-table reads and query jobs (jobs.query/getQueryResults) for everything
// else. It never creates a Storage Read API client and needs no ADBC driver.
type restSource struct {
	dialect *Dialect
	client  *bigquery.Client
}

func newRESTSource(dialect *Dialect) *restSource {
	return &restSource{dialect: dialect}
}

func (s *restSource) Schemes() []string {
	return s.dialect.Schemes()
}

func (s *restSource) Connect(ctx context.Context, uri string) error {
	if _, err := s.dialect.BuildConnectionString(uri); err != nil {
		return fmt.Errorf("failed to parse BigQuery URI: %w", err)
	}
	client, err := s.dialect.newClient(ctx)
	if err != nil {
		return err
	}
	if s.dialect.location != "" {
		client.Location = s.dialect.location
	}
	s.client = client
	return nil
}

func (s *restSource) Close(ctx context.Context) error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *restSource) HandlesIncrementality() bool {
	return false
}

func (s *restSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if _, ok := source.IsCustomQuery(req.Name); ok {
		return source.PartitionedCustomQueryTable(req, s.executeCustomQuery, source.PartitionedCustomQueryOptions{
			QuoteIdentifier: s.dialect.QuoteIdentifier,
			FormatTime:      source.DefaultSQLTimeFormat,
			GetSchema:       s.customQuerySchema,
			DiscoverBounds:  s.discoverCustomQueryBounds,
		})
	}

	if err := s.dialect.ValidateTableName(req.Name); err != nil {
		return nil, err
	}
	project, dataset, tableName := s.dialect.ParseTableNameWithCatalog(req.Name)
	ref := s.client.DatasetInProject(project, dataset).Table(tableName)
	metadata, err := ref.Metadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get table metadata: %w", err)
	}
	tableSchema := s.dialect.tableSchemaFromMetadata(dataset, tableName, metadata)
	config.Debug("[BIGQUERY] Detected primary keys: %v", tableSchema.PrimaryKeys)

	pks, pksUnique := source.ResolvePrimaryKeys(req.PrimaryKeys, tableSchema.PrimaryKeys, false)
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	table := &restTable{
		source:   s,
		ref:      ref,
		metadata: metadata,
		schema:   tableSchema,
		sqlRef:   s.dialect.QuoteIdentifier(project) + "." + s.dialect.QuoteIdentifier(dataset) + "." + s.dialect.QuoteIdentifier(tableName),
	}
	return &source.DynamicSourceTable{
		TableName:                        req.Name,
		TablePrimaryKeys:                 pks,
		TablePrimaryKeysUnique:           pksUnique,
		TableIncrementalKey:              req.IncrementalKey,
		TableStrategy:                    strategy,
		TableSupportsExtractPartitioning: true,
		KnownSchema:                      true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return source.ReadExtractPartitions(ctx, opts, tableSchema, table.read, table.discoverBounds)
		},
	}, nil
}

type restTable struct {
	source   *restSource
	ref      *bigquery.Table
	metadata *bigquery.TableMetadata
	schema   *schema.TableSchema
	sqlRef   string
}

func (t *restTable) read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	fields, indices := selectRESTFields(t.metadata.Schema, opts.ExcludeColumns)
	if len(fields) == 0 {
		return nil, fmt.Errorf("no columns left to read from %s after exclusions", t.sqlRef)
	}

	rowFilter := buildRowFilter(opts, t.schema)
	if rowFilter == "" && t.metadata.Type == bigquery.RegularTable {
		config.Debug("[BIGQUERY] REST API read of %s via tabledata.list", t.sqlRef)
		it := t.ref.Read(ctx)
		it.Schema = t.metadata.Schema
		return t.source.streamRows(ctx, it, fields, indices, opts), nil
	}

	columns := make([]string, len(fields))
	for i, field := range fields {
		columns[i] = t.source.dialect.QuoteIdentifier(field.Name)
	}
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), t.sqlRef)
	if rowFilter != "" {
		query += " WHERE " + rowFilter
	}
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	config.Debug("[BIGQUERY] REST API read of %s via query job: %s", t.sqlRef, query)

	it, err := t.source.runQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query %s: %w", t.sqlRef, err)
	}
	return t.source.streamRows(ctx, it, fields, nil, opts), nil
}

func (t *restTable) discoverBounds(ctx context.Context, opts source.ReadOptions) (source.ExtractPartitionBounds, error) {
	query := source.SQLExtractPartitionBoundsQuery(t.sqlRef, opts.ExtractPartitionBy, opts.IncrementalKey, opts.IncrementalKeyDataType, opts.IntervalStart, opts.IntervalEnd, t.source.dialect.QuoteIdentifier, func(name string) string {
		return name
	}, source.DefaultSQLTimeFormat)
	bounds, err := t.source.queryExtractPartitionBounds(ctx, query, opts.ExtractPartitionKind)
	if err != nil {
		return source.ExtractPartitionBounds{}, fmt.Errorf("failed to discover extract partition bounds: %w", err)
	}
	return bounds, nil
}

// selectRESTFields drops excluded columns. indices maps the kept fields back
// to their position in a full row; nil means nothing was dropped.
func selectRESTFields(fields bigquery.Schema, exclude []string) (bigquery.Schema, []int) {
	if len(exclude) == 0 {
		return fields, nil
	}
	selected := make(bigquery.Schema, 0, len(fields))
	indices := make([]int, 0, len(fields))
	for i, field := range fields {
		excluded := false
		for _, name := range exclude {
			if strings.EqualFold(field.Name, name) {
				excluded = true
				break
			}
		}
		if !excluded {
			selected = append(selected, field)
			indices = append(indices, i)
		}
	}
	return selected, indices
}

func (s *restSource) executeCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	config.Debug("[BIGQUERY] REST API custom query: %s", query)
	it, err := s.runQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute custom query: %w", err)
	}
	return s.streamRows(ctx, it, nil, nil, opts), nil
}

// customQuerySchema resolves the result schema with a dry run, which is free
// and does not execute the query.
func (s *restSource) customQuerySchema(ctx context.Context, query string) (*schema.TableSchema, error) {
	q := s.client.Query(annotation.Prepend(annotation.WithStep(ctx, annotation.StepExtract), query))
	q.DryRun = true
	job, err := q.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect custom query schema: %w", err)
	}
	status := job.LastStatus()
	if status == nil || status.Statistics == nil {
		return nil, errors.New("failed to inspect custom query schema: dry run returned no statistics")
	}
	if err := status.Err(); err != nil {
		return nil, fmt.Errorf("failed to inspect custom query schema: %w", err)
	}
	stats, ok := status.Statistics.Details.(*bigquery.QueryStatistics)
	if !ok || stats.Schema == nil {
		return nil, errors.New("failed to inspect custom query schema: dry run returned no schema")
	}

	columns := make([]schema.Column, len(stats.Schema))
	for i, field := range stats.Schema {
		dt, precision, scale, arrayType := mapBigQueryFieldToDataType(field)
		columns[i] = schema.Column{
			Name:      field.Name,
			DataType:  dt,
			Nullable:  !field.Required,
			Precision: precision,
			Scale:     scale,
			ArrayType: arrayType,
		}
	}
	return &schema.TableSchema{Name: source.CustomQueryTableName, Columns: columns}, nil
}

func (s *restSource) discoverCustomQueryBounds(ctx context.Context, query string, opts source.ReadOptions) (source.ExtractPartitionBounds, error) {
	bounds, err := s.queryExtractPartitionBounds(ctx, query, opts.ExtractPartitionKind)
	if err != nil {
		return source.ExtractPartitionBounds{}, fmt.Errorf("failed to discover custom query extract partition bounds: %w", err)
	}
	return bounds, nil
}

func (s *restSource) queryExtractPartitionBounds(ctx context.Context, query string, kind source.ExtractPartitionKind) (source.ExtractPartitionBounds, error) {
	it, err := s.runQuery(ctx, query)
	if err != nil {
		return source.ExtractPartitionBounds{}, err
	}
	var row []bigquery.Value
	if err := it.Next(&row); err != nil {
		return source.ExtractPartitionBounds{}, err
	}
	if it.IsAccelerated() {
		return source.ExtractPartitionBounds{}, errStorageReadUsed
	}
	if len(row) != 4 {
		return source.ExtractPartitionBounds{}, fmt.Errorf("bounds query returned %d columns, expected 4", len(row))
	}
	total, err := source.SQLIntValue(row[2])
	if err != nil {
		return source.ExtractPartitionBounds{}, fmt.Errorf("failed to parse row count: %w", err)
	}
	nonNull, err := source.SQLIntValue(row[3])
	if err != nil {
		return source.ExtractPartitionBounds{}, fmt.Errorf("failed to parse non-null count: %w", err)
	}
	return source.ExtractPartitionBoundsFromValues(kind, boundValue(row[0]), boundValue(row[1]), total, nonNull)
}

// boundValue converts REST MIN/MAX values into the types
// source.ExtractPartitionBoundsFromValues understands.
func boundValue(value bigquery.Value) any {
	switch v := value.(type) {
	case civil.Date:
		return v.In(time.UTC)
	case civil.DateTime:
		return v.In(time.UTC)
	case *big.Rat:
		if v.IsInt() {
			return v.Num()
		}
		return v.FloatString(storageBigNumericScale)
	default:
		return v
	}
}

var errStorageReadUsed = errors.New("BigQuery client switched to the Storage Read API although read_api=rest")

func (s *restSource) runQuery(ctx context.Context, query string) (*bigquery.RowIterator, error) {
	q := s.client.Query(annotation.Prepend(annotation.WithStep(ctx, annotation.StepExtract), query))
	return q.Read(ctx)
}

// streamRows pages through a REST row iterator into Arrow batches. fields nil
// means "use the iterator's result schema" (custom queries); indices projects
// full rows down to fields.
func (s *restSource) streamRows(ctx context.Context, it *bigquery.RowIterator, fields bigquery.Schema, indices []int, opts source.ReadOptions) <-chan source.RecordBatchResult {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = restDefaultBatchSize
	}
	pageSize := batchSize
	if opts.Limit > 0 && opts.Limit < pageSize {
		pageSize = opts.Limit
	}
	it.PageInfo().MaxSize = pageSize

	results := make(chan source.RecordBatchResult, source.RecordBatchBufferSize(opts, 8))
	go func() {
		defer close(results)
		startTime := time.Now()

		send := func(result source.RecordBatchResult) bool {
			select {
			case results <- result:
				return true
			case <-ctx.Done():
				if result.Batch != nil {
					result.Batch.Release()
				}
				return false
			}
		}
		fail := func(err error) {
			send(source.RecordBatchResult{Err: err})
		}

		var builder *restBatchBuilder
		defer func() {
			if builder != nil {
				builder.release()
			}
		}()

		var total, batches int64
		projected := make([]bigquery.Value, len(indices))
		for opts.Limit <= 0 || total < int64(opts.Limit) {
			var row []bigquery.Value
			err := it.Next(&row)
			if errors.Is(err, iterator.Done) {
				break
			}
			if err != nil {
				fail(fmt.Errorf("failed to read BigQuery rows: %w", err))
				return
			}
			if builder == nil {
				if it.IsAccelerated() {
					fail(errStorageReadUsed)
					return
				}
				if fields == nil {
					fields = it.Schema
				}
				builder, err = newRESTBatchBuilder(fields)
				if err != nil {
					fail(err)
					return
				}
			}

			values := row
			if indices != nil {
				for i, idx := range indices {
					projected[i] = row[idx]
				}
				values = projected
			}
			if err := builder.appendRow(values); err != nil {
				fail(err)
				return
			}
			total++

			if builder.full(batchSize, opts.MaxBatchBytes) {
				batches++
				if !send(source.RecordBatchResult{Batch: builder.flush()}) {
					return
				}
			}
		}

		if builder != nil {
			if batch := builder.flush(); batch != nil {
				batches++
				if !send(source.RecordBatchResult{Batch: batch}) {
					return
				}
			}
		}
		config.Debug("[BIGQUERY] REST API read completed: %d rows in %d batches in %v", total, batches, time.Since(startTime))
	}()
	return results
}
