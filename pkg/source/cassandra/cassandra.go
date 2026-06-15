package cassandra

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/bruin-data/ingestr/internal/cassandrautil"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/connredact"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"gopkg.in/inf.v0"
)

const defaultBatchSize = 10000

type CassandraSource struct {
	session  *gocql.Session
	keyspace string
	pageSize int
}

func NewCassandraSource() *CassandraSource {
	return &CassandraSource{}
}

func (s *CassandraSource) Schemes() []string {
	return []string{"cassandra"}
}

func (s *CassandraSource) Connect(ctx context.Context, uri string) error {
	cfg, err := cassandrautil.ParseURI(uri)
	if err != nil {
		return err
	}

	session, err := cassandrautil.NewCluster(cfg).CreateSession()
	if err != nil {
		return fmt.Errorf("failed to open Cassandra connection: %w", connredact.Redact(uri, err))
	}

	var version string
	if err := session.Query("SELECT release_version FROM system.local").ScanContext(ctx, &version); err != nil {
		session.Close()
		return fmt.Errorf("failed to ping Cassandra: %w", connredact.Redact(uri, err))
	}

	s.session = session
	s.keyspace = cfg.Keyspace
	s.pageSize = cfg.PageSize
	config.Debug("[CASSANDRA] Connected to cluster, release_version=%s", version)
	return nil
}

func (s *CassandraSource) Close(_ context.Context) error {
	if s.session != nil {
		s.session.Close()
	}
	return nil
}

func (s *CassandraSource) HandlesIncrementality() bool {
	return false
}

func (s *CassandraSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if _, ok := source.IsCustomQuery(req.Name); ok {
		return source.CustomQueryTable(req, s.ExecuteCustomQuery)
	}

	tableSchema, err := cassandrautil.GetTableSchema(ctx, s.session, s.keyspace, req.Name)
	if err != nil {
		return nil, err
	}
	if tableSchema == nil {
		return nil, fmt.Errorf("cassandra table %q not found", req.Name)
	}

	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}

	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	tableName := req.Name
	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    pks,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			cols := tableSchema.Columns
			if opts.Schema != nil {
				cols = opts.Schema.Columns
			}
			return s.readTable(ctx, tableName, cols, opts)
		},
	}, nil
}

func (s *CassandraSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	query = strings.TrimSpace(strings.TrimSuffix(query, ";"))
	if query == "" {
		return nil, fmt.Errorf("custom query cannot be empty")
	}
	return s.readQuery(ctx, query, nil, nil, opts)
}

func (s *CassandraSource) readTable(ctx context.Context, table string, columns []schema.Column, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	query, args, filteredColumns, err := buildSelectQuery(s.keyspace, table, columns, opts)
	if err != nil {
		return nil, err
	}
	return s.readQuery(ctx, query, args, filteredColumns, opts)
}

func (s *CassandraSource) readQuery(ctx context.Context, query string, args []interface{}, columns []schema.Column, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = s.pageSize
	}
	if pageSize <= 0 {
		pageSize = defaultBatchSize
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)

		config.Debug("[CASSANDRA] Executing query: %s", query)
		iter := s.session.Query(query, args...).PageSize(pageSize).IterContext(ctx)
		defer func() {
			if err := iter.Close(); err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to close Cassandra iterator: %w", err)}
			}
		}()

		var items []map[string]interface{}
		batchSize := pageSize
		if batchSize <= 0 {
			batchSize = defaultBatchSize
		}
		batchNum := 0
		totalRows := int64(0)
		colByName := columnsByName(columns)

		for {
			row := make(map[string]interface{})
			if !iter.MapScan(row) {
				break
			}

			normalized := make(map[string]interface{}, len(row))
			for key, value := range row {
				normalized[key] = normalizeValue(value, colByName[key])
			}
			items = append(items, normalized)

			if len(items) >= batchSize {
				record, err := arrowconv.ItemsToArrowRecordWithSchema(items, columns, opts.ExcludeColumns)
				if err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert Cassandra rows to Arrow: %w", err)}
					return
				}
				batchNum++
				totalRows += int64(len(items))
				config.Debug("[CASSANDRA] Batch %d: %d rows read (total: %d)", batchNum, len(items), totalRows)
				results <- source.RecordBatchResult{Batch: record}
				items = nil
			}
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, columns, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert Cassandra rows to Arrow: %w", err)}
				return
			}
			batchNum++
			totalRows += int64(len(items))
			config.Debug("[CASSANDRA] Batch %d: %d rows read (total: %d)", batchNum, len(items), totalRows)
			results <- source.RecordBatchResult{Batch: record}
		}

		config.Debug("[CASSANDRA] Total: %d rows in %d batches", totalRows, batchNum)
	}()

	return results, nil
}

func buildSelectQuery(defaultKeyspace, table string, columns []schema.Column, opts source.ReadOptions) (string, []interface{}, []schema.Column, error) {
	quotedTable, err := cassandrautil.QuoteTable(defaultKeyspace, table)
	if err != nil {
		return "", nil, nil, err
	}

	filteredColumns := filterColumns(columns, opts.ExcludeColumns)
	if len(filteredColumns) == 0 {
		return "", nil, nil, fmt.Errorf("no Cassandra columns selected from table %q", table)
	}

	colNames := make([]string, len(filteredColumns))
	for i, col := range filteredColumns {
		colNames[i] = cassandrautil.QuoteIdentifier(col.Name)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(colNames, ", "), quotedTable)
	var args []interface{}
	var conditions []string
	if opts.IncrementalKey != "" {
		if opts.IntervalStart != nil {
			conditions = append(conditions, cassandrautil.QuoteIdentifier(opts.IncrementalKey)+" >= ?")
			args = append(args, *opts.IntervalStart)
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, cassandrautil.QuoteIdentifier(opts.IncrementalKey)+" <= ?")
			args = append(args, *opts.IntervalEnd)
		}
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	if len(conditions) > 0 {
		query += " ALLOW FILTERING"
	}

	return query, args, filteredColumns, nil
}

func filterColumns(columns []schema.Column, exclude []string) []schema.Column {
	if len(exclude) == 0 {
		return columns
	}
	excluded := make(map[string]bool, len(exclude))
	for _, col := range exclude {
		excluded[strings.ToLower(col)] = true
	}

	filtered := make([]schema.Column, 0, len(columns))
	for _, col := range columns {
		if !excluded[strings.ToLower(col.Name)] {
			filtered = append(filtered, col)
		}
	}
	return filtered
}

func columnsByName(columns []schema.Column) map[string]*schema.Column {
	out := make(map[string]*schema.Column, len(columns))
	for i := range columns {
		out[columns[i].Name] = &columns[i]
	}
	return out
}

func normalizeValue(value interface{}, col *schema.Column) interface{} {
	if value == nil {
		return nil
	}

	if col != nil && col.DataType == schema.TypeTime {
		switch v := value.(type) {
		case time.Duration:
			return timeFromDuration(v)
		case int64:
			return timeFromDuration(time.Duration(v))
		}
	}

	switch v := value.(type) {
	case gocql.UUID:
		return v.String()
	case *inf.Dec:
		if v == nil {
			return nil
		}
		return v.String()
	case inf.Dec:
		return v.String()
	case net.IP:
		return v.String()
	case gocql.Duration:
		return fmt.Sprintf("%dmo%dd%dns", v.Months, v.Days, v.Nanoseconds)
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = normalizeValue(item, nil)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, item := range v {
			out[key] = normalizeValue(item, nil)
		}
		return out
	default:
		if col != nil && col.DataType == schema.TypeJSON {
			return ensureJSONValue(v)
		}
		return v
	}
}

func ensureJSONValue(v interface{}) interface{} {
	switch v.(type) {
	case string, map[string]interface{}, []interface{}:
		return v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	}
}

func timeFromDuration(d time.Duration) time.Time {
	return time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC).Add(d)
}

var _ source.Source = (*CassandraSource)(nil)
