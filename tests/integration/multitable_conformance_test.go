package integration

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/internal/registry"
	"github.com/bruin-data/gong/pkg/pipeline"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Constants for multi-table tests
const (
	multiTableUsersRows    = 5
	multiTableOrdersRows   = 4
	multiTableProductsRows = 3
)

// mockMultiTableSource is a test source that emits data from multiple JSONL files
type mockMultiTableSource struct {
	tables    map[string]string // table name -> file path
	schemas   map[string]*schema.TableSchema
	connected bool
}

func newMockMultiTableSource() *mockMultiTableSource {
	return &mockMultiTableSource{
		tables:  make(map[string]string),
		schemas: make(map[string]*schema.TableSchema),
	}
}

func (s *mockMultiTableSource) Schemes() []string {
	return []string{"multitable-test"}
}

func (s *mockMultiTableSource) Connect(ctx context.Context, uri string) error {
	// URI format: multitable-test://table1=path1,table2=path2,...
	uri = strings.TrimPrefix(uri, "multitable-test://")
	parts := strings.Split(uri, ",")

	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return fmt.Errorf("invalid table spec: %s", part)
		}
		tableName := kv[0]
		filePath := kv[1]

		if _, err := os.Stat(filePath); err != nil {
			return fmt.Errorf("file not found for table %s: %w", tableName, err)
		}

		s.tables[tableName] = filePath
	}

	s.connected = true
	return nil
}

func (s *mockMultiTableSource) Close(ctx context.Context) error {
	s.connected = false
	return nil
}

func (s *mockMultiTableSource) HandlesIncrementality() bool {
	return true
}

func (s *mockMultiTableSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	return nil, fmt.Errorf("multi-table source does not support single-table GetTable")
}

func (s *mockMultiTableSource) IsMultiTable() bool {
	return true
}

func (s *mockMultiTableSource) GetTables(ctx context.Context) ([]source.SourceTableInfo, error) {
	var tables []source.SourceTableInfo

	// Sort table names for deterministic order
	tableNames := make([]string, 0, len(s.tables))
	for name := range s.tables {
		tableNames = append(tableNames, name)
	}
	sort.Strings(tableNames)

	for _, name := range tableNames {
		filePath := s.tables[name]
		tblSchema, err := s.inferSchemaFromFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to infer schema for %s: %w", name, err)
		}

		// Look for id as primary key
		var pks []string
		for _, col := range tblSchema.Columns {
			if col.Name == "id" {
				pks = []string{"id"}
				break
			}
		}

		s.schemas[name] = tblSchema

		tables = append(tables, source.SourceTableInfo{
			Name:        name,
			Schema:      tblSchema,
			PrimaryKeys: pks,
		})
	}

	return tables, nil
}

func (s *mockMultiTableSource) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 16)

	go func() {
		defer close(results)

		// Read from each table and emit batches with TableName set
		for tableName, filePath := range s.tables {
			if ctx.Err() != nil {
				return
			}

			batches, err := s.readJSONLFile(filePath, tableName)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("table %s: %w", tableName, err)}
				return
			}

			for _, batch := range batches {
				select {
				case results <- batch:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return results, nil
}

func (s *mockMultiTableSource) readJSONLFile(filePath, tableName string) ([]source.RecordBatchResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	var items []map[string]interface{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var item map[string]interface{}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return nil, nil
	}

	record, err := itemsToArrowRecordMT(items)
	if err != nil {
		return nil, err
	}

	return []source.RecordBatchResult{
		{Batch: record, TableName: tableName},
	}, nil
}

func (s *mockMultiTableSource) inferSchemaFromFile(filePath string) (*schema.TableSchema, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	var items []map[string]interface{}
	count := 0

	for scanner.Scan() && count < 100 {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var item map[string]interface{}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
		count++
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("empty file")
	}

	// Infer schema from items
	fieldOrder := make([]string, 0)
	fieldTypes := make(map[string]schema.DataType)
	fieldSeen := make(map[string]bool)

	for _, item := range items {
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			val := item[key]
			if !fieldSeen[key] {
				fieldSeen[key] = true
				fieldOrder = append(fieldOrder, key)
				fieldTypes[key] = inferSchemaType(val)
			}
		}
	}

	columns := make([]schema.Column, len(fieldOrder))
	for i, name := range fieldOrder {
		columns[i] = schema.Column{
			Name:     name,
			DataType: fieldTypes[name],
			Nullable: true,
		}
	}

	return &schema.TableSchema{
		Columns: columns,
	}, nil
}

func inferSchemaType(val interface{}) schema.DataType {
	switch v := val.(type) {
	case bool:
		return schema.TypeBoolean
	case float64:
		if v == float64(int64(v)) {
			return schema.TypeInt64
		}
		return schema.TypeFloat64
	case string:
		return schema.TypeString
	default:
		return schema.TypeString
	}
}

func itemsToArrowRecordMT(items []map[string]interface{}) (arrow.RecordBatch, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("no items to convert")
	}

	fieldOrder := make([]string, 0)
	fieldTypes := make(map[string]arrow.DataType)
	fieldSeen := make(map[string]bool)

	for _, item := range items {
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			val := item[key]
			if !fieldSeen[key] {
				fieldSeen[key] = true
				fieldOrder = append(fieldOrder, key)
				fieldTypes[key] = inferArrowTypeMT(val)
			}
		}
	}

	fields := make([]arrow.Field, len(fieldOrder))
	for i, name := range fieldOrder {
		fields[i] = arrow.Field{
			Name:     name,
			Type:     fieldTypes[name],
			Nullable: true,
		}
	}
	arrowSchema := arrow.NewSchema(fields, nil)

	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(fieldOrder))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	for _, item := range items {
		for i, fieldName := range fieldOrder {
			val, exists := item[fieldName]
			if !exists || val == nil {
				builders[i].AppendNull()
			} else {
				appendJSONValueMT(builders[i], val)
			}
		}
	}

	arrays := make([]arrow.Array, len(builders))
	for i, b := range builders {
		arrays[i] = b.NewArray()
	}

	record := array.NewRecordBatch(arrowSchema, arrays, int64(len(items)))

	for _, arr := range arrays {
		arr.Release()
	}

	return record, nil
}

func inferArrowTypeMT(val interface{}) arrow.DataType {
	switch v := val.(type) {
	case bool:
		return arrow.FixedWidthTypes.Boolean
	case float64:
		if v == float64(int64(v)) {
			return arrow.PrimitiveTypes.Int64
		}
		return arrow.PrimitiveTypes.Float64
	case string:
		return arrow.BinaryTypes.String
	default:
		return arrow.BinaryTypes.String
	}
}

func appendJSONValueMT(builder array.Builder, val interface{}) {
	if val == nil {
		builder.AppendNull()
		return
	}

	switch b := builder.(type) {
	case *array.BooleanBuilder:
		if v, ok := val.(bool); ok {
			b.Append(v)
		} else {
			b.AppendNull()
		}

	case *array.Int64Builder:
		switch v := val.(type) {
		case float64:
			b.Append(int64(v))
		case int64:
			b.Append(v)
		case int:
			b.Append(int64(v))
		default:
			b.AppendNull()
		}

	case *array.Float64Builder:
		switch v := val.(type) {
		case float64:
			b.Append(v)
		case int64:
			b.Append(float64(v))
		case int:
			b.Append(float64(v))
		default:
			b.AppendNull()
		}

	case *array.StringBuilder:
		if v, ok := val.(string); ok {
			b.Append(v)
		} else {
			b.Append(fmt.Sprintf("%v", val))
		}

	default:
		builder.AppendNull()
	}
}

// Register the mock source for testing
func init() {
	registry.RegisterSource([]string{"multitable-test"}, func() interface{} {
		return newMockMultiTableSource()
	})
}

// multiTableDestCase extends destCase for multi-table tests
type multiTableDestCase struct {
	name       string
	setup      func(t *testing.T, ctx context.Context) (destURI string, destPrefix string, cleanup func())
	sqlBackend *sqlBackend
}

func multiTableDestinationCases() []multiTableDestCase {
	return []multiTableDestCase{
		{
			name: "postgres",
			setup: func(t *testing.T, ctx context.Context) (string, string, func()) {
				if pgDest.uri == "" {
					t.Skip("shared postgres destination container not available")
				}

				// Multi-table sources use source table names directly in the public schema
				// We add unique suffixes to the source table names to avoid conflicts
				cleanup := func() {
					db, err := sql.Open("pgx", pgDest.uri)
					if err == nil {
						_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS public.users")
						_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS public.orders")
						_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS public.products")
						_ = db.Close()
					}
				}
				return pgDest.uri, "public", cleanup
			},
			sqlBackend: postgresBackend(),
		},
		{
			name: "duckdb",
			setup: func(t *testing.T, _ context.Context) (string, string, func()) {
				// Each test gets its own DuckDB file using t.TempDir for cross-platform compatibility
				tmpDir := t.TempDir()
				path := filepath.Join(tmpDir, fmt.Sprintf("multitable_%d.duckdb", time.Now().UnixNano()))
				uri := fmt.Sprintf("duckdb:///%s", path)
				return uri, "main", func() {}
			},
			sqlBackend: duckdbBackend(),
		},
	}
}

// TestDestinations_MultiTable_Replace validates multi-table replace strategy.
// This tests that multiple tables from a multi-table source are all properly
// written to corresponding destination tables.
func TestDestinations_MultiTable_Replace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create test data files
	usersFile := createMultiTableTestFile(t, "users", []map[string]interface{}{
		{"id": 1, "name": "alice", "active": true},
		{"id": 2, "name": "bob", "active": false},
		{"id": 3, "name": "charlie", "active": true},
		{"id": 4, "name": "david", "active": true},
		{"id": 5, "name": "eve", "active": false},
	})
	defer func() { _ = os.Remove(usersFile) }()

	ordersFile := createMultiTableTestFile(t, "orders", []map[string]interface{}{
		{"id": 1, "user_id": 1, "amount": 100.50},
		{"id": 2, "user_id": 2, "amount": 200.00},
		{"id": 3, "user_id": 1, "amount": 50.25},
		{"id": 4, "user_id": 3, "amount": 75.00},
	})
	defer func() { _ = os.Remove(ordersFile) }()

	productsFile := createMultiTableTestFile(t, "products", []map[string]interface{}{
		{"id": 1, "name": "Widget", "price": 9.99},
		{"id": 2, "name": "Gadget", "price": 19.99},
		{"id": 3, "name": "Gizmo", "price": 14.99},
	})
	defer func() { _ = os.Remove(productsFile) }()

	sourceURI := fmt.Sprintf("multitable-test://users=%s,orders=%s,products=%s", usersFile, ordersFile, productsFile)

	for _, tc := range multiTableDestinationCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			destURI, destPrefix, cleanup := tc.setup(t, ctx)
			defer cleanup()

			cfg := &config.IngestConfig{
				SourceURI:           sourceURI,
				SourceTable:         "",
				DestURI:             destURI,
				DestTable:           destPrefix,
				IncrementalStrategy: config.StrategyReplace,
			}

			p := pipeline.New(cfg)
			require.NoError(t, p.Run(ctx), "Multi-table replace should succeed")

			// Verify each table has correct row counts
			db, err := tc.sqlBackend.openDB(destURI)
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			// Multi-table sources use source table names directly (schema.tablename format)
			usersTable := fmt.Sprintf("%s.users", destPrefix)
			ordersTable := fmt.Sprintf("%s.orders", destPrefix)
			productsTable := fmt.Sprintf("%s.products", destPrefix)

			// Check users table
			var usersCount int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(usersTable)).Scan(&usersCount))
			assert.Equal(t, multiTableUsersRows, usersCount, "users table should have correct row count")

			// Check orders table
			var ordersCount int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(ordersTable)).Scan(&ordersCount))
			assert.Equal(t, multiTableOrdersRows, ordersCount, "orders table should have correct row count")

			// Check products table
			var productsCount int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(productsTable)).Scan(&productsCount))
			assert.Equal(t, multiTableProductsRows, productsCount, "products table should have correct row count")
		})
	}
}

// TestDestinations_MultiTable_Append validates multi-table append strategy.
func TestDestinations_MultiTable_Append(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Initial data files
	usersFile1 := createMultiTableTestFile(t, "users_initial", []map[string]interface{}{
		{"id": 1, "name": "alice", "active": true},
		{"id": 2, "name": "bob", "active": false},
	})
	defer func() { _ = os.Remove(usersFile1) }()

	ordersFile1 := createMultiTableTestFile(t, "orders_initial", []map[string]interface{}{
		{"id": 1, "user_id": 1, "amount": 100.50},
	})
	defer func() { _ = os.Remove(ordersFile1) }()

	// Additional data files for append
	usersFile2 := createMultiTableTestFile(t, "users_append", []map[string]interface{}{
		{"id": 3, "name": "charlie", "active": true},
		{"id": 4, "name": "david", "active": true},
	})
	defer func() { _ = os.Remove(usersFile2) }()

	ordersFile2 := createMultiTableTestFile(t, "orders_append", []map[string]interface{}{
		{"id": 2, "user_id": 2, "amount": 200.00},
		{"id": 3, "user_id": 3, "amount": 50.25},
	})
	defer func() { _ = os.Remove(ordersFile2) }()

	sourceURI1 := fmt.Sprintf("multitable-test://users=%s,orders=%s", usersFile1, ordersFile1)
	sourceURI2 := fmt.Sprintf("multitable-test://users=%s,orders=%s", usersFile2, ordersFile2)

	for _, tc := range multiTableDestinationCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			destURI, destPrefix, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// First run: initial load with replace to create tables
			cfg := &config.IngestConfig{
				SourceURI:           sourceURI1,
				SourceTable:         "",
				DestURI:             destURI,
				DestTable:           destPrefix,
				IncrementalStrategy: config.StrategyReplace,
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "Initial load should succeed")

			// Second run: append more data
			cfg.SourceURI = sourceURI2
			cfg.IncrementalStrategy = config.StrategyAppend

			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx), "Append should succeed")

			// Verify row counts after append
			db, err := tc.sqlBackend.openDB(destURI)
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			usersTable := fmt.Sprintf("%s.users", destPrefix)
			ordersTable := fmt.Sprintf("%s.orders", destPrefix)

			// Check users table (2 initial + 2 appended = 4)
			var usersCount int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(usersTable)).Scan(&usersCount))
			assert.Equal(t, 4, usersCount, "users table should have 4 rows after append")

			// Check orders table (1 initial + 2 appended = 3)
			var ordersCount int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(ordersTable)).Scan(&ordersCount))
			assert.Equal(t, 3, ordersCount, "orders table should have 3 rows after append")
		})
	}
}

// TestDestinations_MultiTable_Merge validates multi-table merge strategy.
func TestDestinations_MultiTable_Merge(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Initial data files
	usersFile1 := createMultiTableTestFile(t, "users_merge_initial", []map[string]interface{}{
		{"id": 1, "name": "alice", "active": true},
		{"id": 2, "name": "bob", "active": false},
		{"id": 3, "name": "charlie", "active": true},
	})
	defer func() { _ = os.Remove(usersFile1) }()

	ordersFile1 := createMultiTableTestFile(t, "orders_merge_initial", []map[string]interface{}{
		{"id": 1, "user_id": 1, "amount": 100.50},
		{"id": 2, "user_id": 2, "amount": 200.00},
	})
	defer func() { _ = os.Remove(ordersFile1) }()

	// Update data files for merge (update existing + add new)
	usersFile2 := createMultiTableTestFile(t, "users_merge_update", []map[string]interface{}{
		{"id": 2, "name": "bob_updated", "active": true}, // update existing
		{"id": 4, "name": "david", "active": true},       // new record
	})
	defer func() { _ = os.Remove(usersFile2) }()

	ordersFile2 := createMultiTableTestFile(t, "orders_merge_update", []map[string]interface{}{
		{"id": 1, "user_id": 1, "amount": 150.00}, // update existing
		{"id": 3, "user_id": 3, "amount": 75.00},  // new record
	})
	defer func() { _ = os.Remove(ordersFile2) }()

	sourceURI1 := fmt.Sprintf("multitable-test://users=%s,orders=%s", usersFile1, ordersFile1)
	sourceURI2 := fmt.Sprintf("multitable-test://users=%s,orders=%s", usersFile2, ordersFile2)

	for _, tc := range multiTableDestinationCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			destURI, destPrefix, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// First run: initial load with replace
			cfg := &config.IngestConfig{
				SourceURI:           sourceURI1,
				SourceTable:         "",
				DestURI:             destURI,
				DestTable:           destPrefix,
				IncrementalStrategy: config.StrategyReplace,
				PrimaryKeys:         []string{"id"},
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "Initial load should succeed")

			// Second run: merge updates
			cfg.SourceURI = sourceURI2
			cfg.IncrementalStrategy = config.StrategyMerge

			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx), "Merge should succeed")

			// Verify row counts after merge
			db, err := tc.sqlBackend.openDB(destURI)
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			usersTable := fmt.Sprintf("%s.users", destPrefix)
			ordersTable := fmt.Sprintf("%s.orders", destPrefix)

			// Check users table (3 initial + 1 new = 4, with 1 updated in place)
			var usersCount int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(usersTable)).Scan(&usersCount))
			assert.Equal(t, 4, usersCount, "users table should have 4 rows after merge")

			// Verify the merge updated the existing record
			var bobName string
			require.NoError(t, db.QueryRow(tc.sqlBackend.nameByIDQuery(usersTable, 2)).Scan(&bobName))
			assert.Equal(t, "bob_updated", bobName, "bob's name should be updated")

			// Check orders table (2 initial + 1 new = 3, with 1 updated in place)
			var ordersCount int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(ordersTable)).Scan(&ordersCount))
			assert.Equal(t, 3, ordersCount, "orders table should have 3 rows after merge")
		})
	}
}

func createMultiTableTestFile(t *testing.T, name string, items []map[string]interface{}) string {
	t.Helper()

	tmpFile, err := os.CreateTemp("", fmt.Sprintf("multitable_%s_*.jsonl", name))
	require.NoError(t, err)

	for _, item := range items {
		data, err := json.Marshal(item)
		require.NoError(t, err)
		_, err = tmpFile.Write(append(data, '\n'))
		require.NoError(t, err)
	}

	require.NoError(t, tmpFile.Close())
	return tmpFile.Name()
}

var (
	_ source.Source           = (*mockMultiTableSource)(nil)
	_ source.MultiTableSource = (*mockMultiTableSource)(nil)
)
