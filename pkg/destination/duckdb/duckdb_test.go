package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc" // Register ADBC driver
	"github.com/bruin-data/ingestr/pkg/tablename"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func connectTestDuckDB(t *testing.T, ctx context.Context) (*DuckDBDestination, string) {
	t.Helper()

	dest := NewDuckDBDestination()
	path := filepath.Join(t.TempDir(), "test.duckdb")
	err := dest.Connect(ctx, fmt.Sprintf("duckdb:///%s", path))
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = dest.Close(ctx)
	})

	return dest, dest.filePath
}

func openDuckDB(t *testing.T, ctx context.Context, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestNewDuckDBDestination(t *testing.T) {
	dest := NewDuckDBDestination()
	if dest == nil {
		t.Fatal("NewDuckDBDestination returned nil")
	}
}

func TestSchemes(t *testing.T) {
	dest := NewDuckDBDestination()
	schemes := dest.Schemes()

	expected := []string{"duckdb", "motherduck", "md"}
	if len(schemes) != len(expected) {
		t.Errorf("expected %d schemes, got %d", len(expected), len(schemes))
	}
	for i, scheme := range schemes {
		if scheme != expected[i] {
			t.Errorf("expected scheme '%s', got '%s'", expected[i], scheme)
		}
	}
}

func TestWriteCancellationReleasesQueuedRecords(t *testing.T) {
	t.Setenv("INGESTR_DUCKDB_CHECKPOINT_ROWS", "-1")
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	newBatch := func(value int64) arrow.RecordBatch {
		builder := array.NewInt64Builder(mem)
		builder.Append(value)
		values := builder.NewArray()
		builder.Release()
		batch := array.NewRecordBatch(
			arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil),
			[]arrow.Array{values},
			1,
		)
		values.Release()
		return batch
	}

	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Batch: newBatch(1), Err: context.Canceled}
	records <- source.RecordBatchResult{Batch: newBatch(2)}
	close(records)

	err := (&DuckDBDestination{}).WriteParallel(t.Context(), records, destination.WriteOptions{Table: "state"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestClaimCDCTargetCanonicalizesCurrentCatalog(t *testing.T) {
	dest, _ := connectTestDuckDB(t, t.Context())
	claimSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "destination_table", DataType: schema.TypeString, MaxLength: 512, Nullable: false},
		{Name: "connector_id", DataType: schema.TypeString, MaxLength: 64, Nullable: false},
		{Name: "claimed_at", DataType: schema.TypeTimestampTZ, Nullable: false},
	}}
	require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table:       "_bruin_staging.cdc_targets",
		Schema:      claimSchema,
		PrimaryKeys: []string{"destination_table"},
	}))
	claim := func(table, connector string) destination.CDCTargetClaim {
		return destination.CDCTargetClaim{DestinationTable: table, ConnectorID: connector, SourceTable: "public.orders"}
	}
	require.NoError(t, dest.ClaimCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim("orders", "connector-a")))

	qualifiedTarget := dest.catalog + ".main.orders"
	err := dest.ClaimCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim(qualifiedTarget, "connector-b"))
	require.ErrorContains(t, err, "already claimed")
	require.NoError(t, dest.ClaimCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim(qualifiedTarget, "connector-a")))
	require.NoError(t, dest.ClaimCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim("customers", "connector-b")))
}

func TestCDCTargetIncarnationStableAcrossDMLAndChangesOnRecreate(t *testing.T) {
	dest, _ := connectTestDuckDB(t, t.Context())
	if err := dest.Exec(t.Context(), `CREATE TABLE events (id BIGINT)`); err != nil {
		t.Fatal(err)
	}

	first, exists, err := dest.CDCTargetIncarnation(t.Context(), "events")
	require.NoError(t, err)
	require.True(t, exists)
	require.NotEmpty(t, first)
	require.NoError(t, dest.Exec(t.Context(), `INSERT INTO events VALUES (1)`))
	stable, exists, err := dest.CDCTargetIncarnation(t.Context(), "main.events")
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, first, stable)
	require.NoError(t, dest.Exec(t.Context(), `DROP TABLE events`))
	_, exists, err = dest.CDCTargetIncarnation(t.Context(), "events")
	require.NoError(t, err)
	require.False(t, exists)
	require.NoError(t, dest.Exec(t.Context(), `CREATE TABLE events (id BIGINT)`))
	recreated, exists, err := dest.CDCTargetIncarnation(t.Context(), "events")
	require.NoError(t, err)
	require.True(t, exists)
	require.NotEqual(t, first, recreated)
}

func TestParseDuckDBPath(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		expected    string
		expectError bool
	}{
		{
			name:     "memory_short",
			uri:      "duckdb://:memory:",
			expected: ":memory:",
		},
		{
			name:     "memory_with_slash",
			uri:      "duckdb:///:memory:",
			expected: ":memory:",
		},
		{
			name:     "absolute_path",
			uri:      "duckdb:///absolute/path/to/db.db",
			expected: "/absolute/path/to/db.db",
		},
		{
			name:     "relative_path_single_file",
			uri:      "duckdb:///mydb.db",
			expected: "./mydb.db",
		},
		{
			name:     "empty_defaults_to_memory",
			uri:      "duckdb://",
			expected: ":memory:",
		},
		{
			name:        "invalid_scheme",
			uri:         "sqlite://test.db",
			expectError: true,
		},
		{
			name:     "motherduck_with_database",
			uri:      "motherduck://mydb?token=test123",
			expected: "md:mydb?motherduck_token=test123",
		},
		{
			name:     "motherduck_without_database",
			uri:      "motherduck://?token=test123",
			expected: "md:?motherduck_token=test123",
		},
		{
			name:     "md_scheme_with_database",
			uri:      "md://mydb?token=test123",
			expected: "md:mydb?motherduck_token=test123",
		},
		{
			name:        "motherduck_without_token",
			uri:         "motherduck://mydb",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDuckDBPath(tt.uri)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("parseDuckDBPath(%s) = %s, want %s", tt.uri, result, tt.expected)
			}
		})
	}
}

func TestParseSchemaTable(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedSchema string
		expectedTable  string
	}{
		{
			name:           "schema_and_table",
			input:          "myschema.mytable",
			expectedSchema: "myschema",
			expectedTable:  "mytable",
		},
		{
			name:           "table_only",
			input:          "mytable",
			expectedSchema: "",
			expectedTable:  "mytable",
		},
		{
			name:           "main_schema",
			input:          "main.users",
			expectedSchema: "main",
			expectedTable:  "users",
		},
		{
			name:           "empty",
			input:          "",
			expectedSchema: "",
			expectedTable:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, table := parseSchemaTable(tt.input)
			if schema != tt.expectedSchema {
				t.Errorf("schema = %s, want %s", schema, tt.expectedSchema)
			}
			if table != tt.expectedTable {
				t.Errorf("table = %s, want %s", table, tt.expectedTable)
			}
		})
	}
}

func TestReplaceStagingPolicy(t *testing.T) {
	dest := NewDuckDBDestination()

	policy := dest.ReplaceStagingPolicy()
	if policy.DefaultPlacement != destination.ReplaceStagingTargetSchema {
		t.Fatalf("DefaultPlacement = %q, want %q", policy.DefaultPlacement, destination.ReplaceStagingTargetSchema)
	}
	if policy.DefaultTargetSchema != "main" {
		t.Fatalf("DefaultTargetSchema = %q, want main", policy.DefaultTargetSchema)
	}
}

func TestDuckDBSwapTable_MainSchemaStagingToUnqualifiedTargetUsesRename(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	require.NoError(t, dest.Exec(ctx, `CREATE TABLE users (id BIGINT)`))
	require.NoError(t, dest.Exec(ctx, `INSERT INTO users VALUES (1)`))
	require.NoError(t, dest.Exec(ctx, `CREATE TABLE main.users_staging (id BIGINT)`))
	require.NoError(t, dest.Exec(ctx, `INSERT INTO main.users_staging VALUES (2)`))

	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		StagingTable: "main.users_staging",
		TargetTable:  "users",
	}))

	db := openDuckDB(t, ctx, path)
	var got int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT id FROM users`).Scan(&got))
	assert.Equal(t, int64(2), got)

	var stagingCount int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'main' AND table_name = 'users_staging'`).Scan(&stagingCount))
	assert.Equal(t, int64(0), stagingCount)
}

func TestQuoteColumns(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "single_column",
			input:    []string{"id"},
			expected: []string{`"id"`},
		},
		{
			name:     "multiple_columns",
			input:    []string{"id", "name", "email"},
			expected: []string{`"id"`, `"name"`, `"email"`},
		},
		{
			name:     "empty",
			input:    []string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := quoteColumns(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("length = %d, want %d", len(result), len(tt.expected))
				return
			}
			for i, col := range result {
				if col != tt.expected[i] {
					t.Errorf("result[%d] = %s, want %s", i, col, tt.expected[i])
				}
			}
		})
	}
}

func TestFilterColumns(t *testing.T) {
	tests := []struct {
		name     string
		columns  []string
		exclude  []string
		expected []string
	}{
		{
			name:     "filter_one",
			columns:  []string{"id", "name", "email"},
			exclude:  []string{"id"},
			expected: []string{"name", "email"},
		},
		{
			name:     "filter_multiple",
			columns:  []string{"id", "name", "email", "age"},
			exclude:  []string{"id", "age"},
			expected: []string{"name", "email"},
		},
		{
			name:     "case_insensitive",
			columns:  []string{"ID", "Name", "EMAIL"},
			exclude:  []string{"id", "email"},
			expected: []string{"Name"},
		},
		{
			name:     "no_filter",
			columns:  []string{"id", "name"},
			exclude:  []string{},
			expected: []string{"id", "name"},
		},
		{
			name:     "filter_all",
			columns:  []string{"id"},
			exclude:  []string{"id"},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterColumns(tt.columns, tt.exclude)
			if len(result) != len(tt.expected) {
				t.Errorf("length = %d, want %d", len(result), len(tt.expected))
				return
			}
			for i, col := range result {
				if col != tt.expected[i] {
					t.Errorf("result[%d] = %s, want %s", i, col, tt.expected[i])
				}
			}
		})
	}
}

func TestBuildJoinCondition(t *testing.T) {
	tests := []struct {
		name        string
		keys        []string
		targetAlias string
		sourceAlias string
		expected    string
	}{
		{
			name:        "single_key",
			keys:        []string{"id"},
			targetAlias: "target",
			sourceAlias: "source",
			expected:    `target."id" = source."id"`,
		},
		{
			name:        "multiple_keys",
			keys:        []string{"id", "tenant_id"},
			targetAlias: "t",
			sourceAlias: "s",
			expected:    `t."id" = s."id" AND t."tenant_id" = s."tenant_id"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildJoinCondition(tt.keys, tt.targetAlias, tt.sourceAlias)
			if result != tt.expected {
				t.Errorf("buildJoinCondition = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestBuildUpdateSet(t *testing.T) {
	tests := []struct {
		name        string
		columns     []string
		targetAlias string
		sourceAlias string
		cdcMerge    bool
		expected    string
	}{
		{
			name:        "single_column",
			columns:     []string{"name"},
			targetAlias: "target",
			sourceAlias: "source",
			expected:    `"name" = source."name"`,
		},
		{
			name:        "multiple_columns",
			columns:     []string{"name", "email", "age"},
			targetAlias: "target",
			sourceAlias: "s",
			expected:    `"name" = s."name", "email" = s."email", "age" = s."age"`,
		},
		{
			name:        "cdc_unchanged_cols",
			columns:     []string{"config_data"},
			targetAlias: "target",
			sourceAlias: "source",
			cdcMerge:    true,
			expected:    `"config_data" = CASE WHEN json_contains(source."_cdc_unchanged_cols", '["config_data"]') THEN target."config_data" ELSE source."config_data" END`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildUpdateSet(tt.columns, tt.targetAlias, tt.sourceAlias, tt.cdcMerge)
			if result != tt.expected {
				t.Errorf("buildUpdateSet = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestBuildCreateTableSQL(t *testing.T) {
	tests := []struct {
		name        string
		table       string
		columns     []schema.Column
		primaryKeys []string
		validate    func(*testing.T, string)
	}{
		{
			name:  "simple_table",
			table: "users",
			columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "name", DataType: schema.TypeString, Nullable: true},
			},
			primaryKeys: nil,
			validate: func(t *testing.T, sql string) {
				assert.Contains(t, sql, "CREATE TABLE IF NOT EXISTS users")
				assert.Contains(t, sql, `"id" BIGINT`)
				assert.NotContains(t, sql, `NOT NULL`)
				assert.Contains(t, sql, `"name" VARCHAR`)
				assert.NotContains(t, sql, "PRIMARY KEY")
			},
		},
		{
			name:  "table_with_primary_key",
			table: "orders",
			columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "customer_id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "amount", DataType: schema.TypeDecimal, Precision: 10, Scale: 2, Nullable: true},
			},
			primaryKeys: []string{"id"},
			validate: func(t *testing.T, sql string) {
				assert.Contains(t, sql, "CREATE TABLE IF NOT EXISTS orders")
				assert.Contains(t, sql, `"id" BIGINT`)
				assert.Contains(t, sql, `"amount" DECIMAL(10,2)`)
				assert.Contains(t, sql, `PRIMARY KEY ("id")`)
			},
		},
		{
			name:  "table_with_composite_primary_key",
			table: "order_items",
			columns: []schema.Column{
				{Name: "order_id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "item_id", DataType: schema.TypeInt64, Nullable: false},
				{Name: "quantity", DataType: schema.TypeInt32, Nullable: true},
			},
			primaryKeys: []string{"order_id", "item_id"},
			validate: func(t *testing.T, sql string) {
				assert.Contains(t, sql, `PRIMARY KEY ("order_id", "item_id")`)
			},
		},
		{
			name:  "table_with_schema",
			table: "myschema.users",
			columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
			},
			primaryKeys: nil,
			validate: func(t *testing.T, sql string) {
				assert.Contains(t, sql, "CREATE TABLE IF NOT EXISTS myschema.users")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql := buildCreateTableSQL(tt.table, tt.columns, tt.primaryKeys)
			tt.validate(t, sql)
		})
	}
}

func TestStrategySupport(t *testing.T) {
	dest := NewDuckDBDestination()

	assert.True(t, dest.SupportsReplaceStrategy(), "should support replace strategy")
	assert.True(t, dest.SupportsAppendStrategy(), "should support append strategy")
	assert.True(t, dest.SupportsMergeStrategy(), "should support merge strategy")
	assert.True(t, dest.SupportsDeleteInsertStrategy(), "should support delete+insert strategy")
}

// Integration tests using actual DuckDB

func TestConnect_InMemory(t *testing.T) {
	ctx := context.Background()
	dest := NewDuckDBDestination()

	err := dest.Connect(ctx, "duckdb://:memory:")
	require.NoError(t, err)

	err = dest.Close(ctx)
	require.NoError(t, err)
}

func TestConnect_FileDatabase(t *testing.T) {
	ctx := context.Background()
	dest := NewDuckDBDestination()

	tmpFile := fmt.Sprintf("/tmp/test_duckdb_%d.db", time.Now().UnixNano())
	defer func() { _ = os.Remove(tmpFile) }()

	err := dest.Connect(ctx, fmt.Sprintf("duckdb:///%s", tmpFile))
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(tmpFile)
	require.NoError(t, err, "database file should exist")

	err = dest.Close(ctx)
	require.NoError(t, err)
}

func TestPrepareTable_CreateTable(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "created_at", DataType: schema.TypeTimestamp, Nullable: true},
		},
	}

	err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       "test_table",
		Schema:      tableSchema,
		DropFirst:   false,
		PrimaryKeys: []string{"id"},
	})
	require.NoError(t, err)

	// Verify table was created
	db := openDuckDB(t, ctx, path)
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'test_table'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "table should exist")
}

func TestPrepareTable_DropFirst(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		},
	}

	// Create table first time
	err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     "test_table",
		Schema:    tableSchema,
		DropFirst: false,
	})
	require.NoError(t, err)

	// Insert some data
	err = dest.Exec(ctx, "INSERT INTO test_table VALUES (1)")
	require.NoError(t, err)

	// Recreate with DropFirst
	err = dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     "test_table",
		Schema:    tableSchema,
		DropFirst: true,
	})
	require.NoError(t, err)

	// Verify table is empty
	db := openDuckDB(t, ctx, path)
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_table").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "table should be empty after drop")
}

func TestPrepareTable_WithSchema(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		},
	}

	err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     "myschema.test_table",
		Schema:    tableSchema,
		DropFirst: false,
	})
	require.NoError(t, err)

	// Verify schema and table exist
	db := openDuckDB(t, ctx, path)
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'myschema' AND table_name = 'test_table'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "table should exist in myschema")
}

func TestWrite_BasicData(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	// Prepare table
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString},
		},
	}

	err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     "test_table",
		Schema:    tableSchema,
		DropFirst: true,
	})
	require.NoError(t, err)

	// Create Arrow record batch
	mem := memory.NewGoAllocator()
	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	idBuilder := array.NewInt64Builder(mem)
	nameBuilder := array.NewStringBuilder(mem)

	idBuilder.AppendValues([]int64{1, 2, 3}, nil)
	nameBuilder.AppendValues([]string{"Alice", "Bob", "Charlie"}, nil)

	idArray := idBuilder.NewArray()
	nameArray := nameBuilder.NewArray()

	record := array.NewRecordBatch(arrowSchema, []arrow.Array{idArray, nameArray}, 3)

	// Create channel with record
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: record}
	close(records)

	// Write data
	err = dest.Write(ctx, records, destination.WriteOptions{
		Table: "test_table",
	})
	require.NoError(t, err)

	// Verify data
	db := openDuckDB(t, ctx, path)
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_table").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Verify specific values
	rows, err := db.QueryContext(ctx, "SELECT id, name FROM test_table ORDER BY id")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	expected := []struct {
		id   int64
		name string
	}{
		{1, "Alice"},
		{2, "Bob"},
		{3, "Charlie"},
	}

	i := 0
	for rows.Next() {
		var id int64
		var nameRaw []byte
		err = rows.Scan(&id, &nameRaw)
		require.NoError(t, err)
		assert.Equal(t, expected[i].id, id)
		assert.Equal(t, expected[i].name, string(append([]byte(nil), nameRaw...)))
		i++
	}
}

func TestIngestConnections_UsePreparedTableConstraints(t *testing.T) {
	t.Setenv("INGESTR_DUCKDB_INGEST_CONNS", "4")

	ctx := context.Background()
	dest, _ := connectTestDuckDB(t, ctx)
	tableSchema := &schema.TableSchema{
		Columns:     []schema.Column{{Name: "id", DataType: schema.TypeInt64}},
		PrimaryKeys: []string{"id"},
	}

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       "target",
		Schema:      tableSchema,
		DropFirst:   true,
		PrimaryKeys: []string{"id"},
	}))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:  "target",
		Schema: tableSchema,
	}))
	assert.Equal(t, 1, dest.ingestConnections(destination.WriteOptions{Table: "target"}))

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     "staging",
		Schema:    tableSchema,
		DropFirst: true,
	}))
	assert.Equal(t, 4, dest.ingestConnections(destination.WriteOptions{Table: "staging"}))
}

func TestMergeTable(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	// Create target table with initial data
	err := dest.Exec(ctx, `
		CREATE TABLE target_table (
			id BIGINT PRIMARY KEY,
			name VARCHAR,
			value INTEGER
		)
	`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `
		INSERT INTO target_table VALUES
			(1, 'Alice', 100),
			(2, 'Bob', 200)
	`)
	require.NoError(t, err)

	// Create staging table with updated/new data
	err = dest.Exec(ctx, `
		CREATE TABLE staging_table (
			id BIGINT,
			name VARCHAR,
			value INTEGER
		)
	`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `
		INSERT INTO staging_table VALUES
			(1, 'Alice Updated', 150),
			(3, 'Charlie', 300)
	`)
	require.NoError(t, err)

	// Perform merge
	err = dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable:         "staging_table",
		TargetTable:          "target_table",
		PrimaryKeys:          []string{"id"},
		Columns:              []string{"id", "name", "value"},
		IncrementalPredicate: "target.value >= 100",
	})
	require.NoError(t, err)

	// Verify results
	db := openDuckDB(t, ctx, path)
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM target_table").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "should have 3 rows after merge")

	// Verify Alice was updated
	var nameRaw []byte
	var value int
	err = db.QueryRowContext(ctx, "SELECT name, value FROM target_table WHERE id = 1").Scan(&nameRaw, &value)
	require.NoError(t, err)
	assert.Equal(t, "Alice Updated", string(append([]byte(nil), nameRaw...)))
	assert.Equal(t, 150, value)

	// Verify Bob unchanged
	err = db.QueryRowContext(ctx, "SELECT name, value FROM target_table WHERE id = 2").Scan(&nameRaw, &value)
	require.NoError(t, err)
	assert.Equal(t, "Bob", string(append([]byte(nil), nameRaw...)))
	assert.Equal(t, 200, value)

	// Verify Charlie was inserted
	err = db.QueryRowContext(ctx, "SELECT name, value FROM target_table WHERE id = 3").Scan(&nameRaw, &value)
	require.NoError(t, err)
	assert.Equal(t, "Charlie", string(append([]byte(nil), nameRaw...)))
	assert.Equal(t, 300, value)
}

func TestMergeTable_EmptyTargetDedupesStagingByPK(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	err := dest.Exec(ctx, `
		CREATE TABLE target_table (
			id BIGINT PRIMARY KEY,
			name VARCHAR,
			updated_at BIGINT
		)
	`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `
		CREATE TABLE staging_table (
			id BIGINT,
			name VARCHAR,
			updated_at BIGINT
		)
	`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `
		INSERT INTO staging_table VALUES
			(1, 'Alice Old', 10),
			(1, 'Alice New', 20),
			(2, 'Bob', 15)
	`)
	require.NoError(t, err)

	err = dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable:   "staging_table",
		TargetTable:    "target_table",
		PrimaryKeys:    []string{"id"},
		Columns:        []string{"id", "name", "updated_at"},
		IncrementalKey: "updated_at",
	})
	require.NoError(t, err)

	db := openDuckDB(t, ctx, path)
	var total, id1Count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM target_table").Scan(&total))
	assert.Equal(t, 2, total)
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM target_table WHERE id = 1").Scan(&id1Count))
	assert.Equal(t, 1, id1Count)

	var nameRaw []byte
	var updatedAt int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT name, updated_at FROM target_table WHERE id = 1").Scan(&nameRaw, &updatedAt))
	assert.Equal(t, "Alice New", string(append([]byte(nil), nameRaw...)))
	assert.Equal(t, 20, updatedAt)
}

func TestMergeTable_EmptyTargetUniqueStagingInsertsRows(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	err := dest.Exec(ctx, `
		CREATE TABLE target_table (
			id BIGINT PRIMARY KEY,
			name VARCHAR
		)
	`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `
		CREATE TABLE staging_table (
			id BIGINT,
			name VARCHAR
		)
	`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `
		INSERT INTO staging_table VALUES
			(1, 'Alice'),
			(2, 'Bob')
	`)
	require.NoError(t, err)

	err = dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: "staging_table",
		TargetTable:  "target_table",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name"},
	})
	require.NoError(t, err)

	db := openDuckDB(t, ctx, path)
	var total int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM target_table").Scan(&total))
	assert.Equal(t, 2, total)

	var nameRaw []byte
	require.NoError(t, db.QueryRowContext(ctx, "SELECT name FROM target_table WHERE id = 2").Scan(&nameRaw))
	assert.Equal(t, "Bob", string(append([]byte(nil), nameRaw...)))
}

func TestStagingPrimaryKeysUniqueLocked(t *testing.T) {
	ctx := context.Background()
	dest, _ := connectTestDuckDB(t, ctx)

	err := dest.Exec(ctx, `
		CREATE TABLE staging_table (
			id BIGINT,
			part BIGINT,
			name VARCHAR
		)
	`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `
		INSERT INTO staging_table VALUES
			(1, 10, 'Alice'),
			(2, 10, 'Bob')
	`)
	require.NoError(t, err)

	unique, err := dest.stagingPrimaryKeysUniqueLocked(ctx, destination.MergeOptions{
		StagingTable: "staging_table",
		PrimaryKeys:  []string{"id"},
	})
	require.NoError(t, err)
	assert.True(t, unique)

	err = dest.Exec(ctx, `INSERT INTO staging_table VALUES (2, 20, 'Bob duplicate')`)
	require.NoError(t, err)

	unique, err = dest.stagingPrimaryKeysUniqueLocked(ctx, destination.MergeOptions{
		StagingTable: "staging_table",
		PrimaryKeys:  []string{"id"},
	})
	require.NoError(t, err)
	assert.False(t, unique)

	unique, err = dest.stagingPrimaryKeysUniqueLocked(ctx, destination.MergeOptions{
		StagingTable: "staging_table",
		PrimaryKeys:  []string{"id", "part"},
	})
	require.NoError(t, err)
	assert.True(t, unique)

	err = dest.Exec(ctx, `INSERT INTO staging_table VALUES (NULL, 30, 'No id')`)
	require.NoError(t, err)

	unique, err = dest.stagingPrimaryKeysUniqueLocked(ctx, destination.MergeOptions{
		StagingTable: "staging_table",
		PrimaryKeys:  []string{"id", "part"},
	})
	require.NoError(t, err)
	assert.False(t, unique)
}

func TestTableHasRowsLocked(t *testing.T) {
	ctx := context.Background()
	dest, _ := connectTestDuckDB(t, ctx)

	err := dest.Exec(ctx, `CREATE TABLE target_table (id BIGINT)`)
	require.NoError(t, err)

	hasRows, err := dest.tableHasRowsLocked(ctx, destination.QuoteTableName("target_table"))
	require.NoError(t, err)
	assert.False(t, hasRows)

	err = dest.Exec(ctx, `INSERT INTO target_table VALUES (1)`)
	require.NoError(t, err)

	hasRows, err = dest.tableHasRowsLocked(ctx, destination.QuoteTableName("target_table"))
	require.NoError(t, err)
	assert.True(t, hasRows)
}

func TestChannelRecordReaderSkipsEmptyBatches(t *testing.T) {
	ctx := context.Background()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	first := makeDuckDBRecordBatch(t, schema, []int64{1})
	empty := makeDuckDBRecordBatch(t, schema, nil)
	second := makeDuckDBRecordBatch(t, schema, []int64{2, 3})

	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Batch: empty}
	records <- source.RecordBatchResult{Batch: second}
	close(records)

	reader := newChannelRecordReader(ctx, records, first)
	defer reader.Release()

	require.True(t, reader.Next())
	assert.Equal(t, int64(1), reader.RecordBatch().NumRows())
	require.True(t, reader.Next())
	assert.Equal(t, int64(2), reader.RecordBatch().NumRows())
	assert.False(t, reader.Next())
	assert.NoError(t, reader.Err())
	assert.Equal(t, int64(3), reader.rowsWritten())
}

func TestChannelRecordReaderRewrapsCompatibleSchema(t *testing.T) {
	ctx := context.Background()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false}}, nil)
	nullableSchema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, nil)
	first := makeDuckDBRecordBatch(t, schema, []int64{1})
	second := makeDuckDBRecordBatch(t, nullableSchema, []int64{2})

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: second}
	close(records)

	reader := newChannelRecordReader(ctx, records, first)
	defer reader.Release()

	require.True(t, reader.Next())
	assert.True(t, reader.RecordBatch().Schema().Equal(schema))
	require.True(t, reader.Next())
	assert.True(t, reader.RecordBatch().Schema().Equal(schema))
	assert.Equal(t, int64(2), reader.rowsWritten())
	assert.False(t, reader.Next())
	assert.NoError(t, reader.Err())
}

func TestChannelRecordReaderReportsSchemaChange(t *testing.T) {
	ctx := context.Background()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	otherSchema := arrow.NewSchema([]arrow.Field{{Name: "value", Type: arrow.PrimitiveTypes.Int64}}, nil)
	first := makeDuckDBRecordBatch(t, schema, []int64{1})
	second := makeDuckDBRecordBatch(t, otherSchema, []int64{2})

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: second}
	close(records)

	reader := newChannelRecordReader(ctx, records, first)
	defer reader.Release()

	require.True(t, reader.Next())
	assert.False(t, reader.Next())
	assert.ErrorContains(t, reader.Err(), "schema changed")
}

func makeDuckDBRecordBatch(t *testing.T, schema *arrow.Schema, values []int64) arrow.RecordBatch {
	t.Helper()

	builder := array.NewInt64Builder(memory.DefaultAllocator)
	defer builder.Release()
	for _, value := range values {
		builder.Append(value)
	}
	arr := builder.NewArray()
	defer arr.Release()

	return array.NewRecordBatch(schema, []arrow.Array{arr}, int64(len(values)))
}

func TestMergeTable_CDCMaterializesDeleteOnlyTombstone(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	err := dest.Exec(ctx, `
		CREATE TABLE target_table (
			id BIGINT PRIMARY KEY,
			name VARCHAR,
			"_cdc_lsn" VARCHAR,
			"_cdc_deleted" BOOLEAN,
			"_cdc_synced_at" TIMESTAMP
		)
	`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `
		CREATE TABLE staging_table (
			id BIGINT,
			name VARCHAR,
			"_cdc_lsn" VARCHAR,
			"_cdc_deleted" BOOLEAN,
			"_cdc_synced_at" TIMESTAMP
		)
	`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `
		INSERT INTO staging_table VALUES
			(-9223372036854775808, NULL, '00000000000000000042', true, CURRENT_TIMESTAMP)
	`)
	require.NoError(t, err)

	err = dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: "staging_table",
		TargetTable:  "target_table",
		PrimaryKeys:  []string{"id"},
		Columns: []string{
			"id",
			"name",
			destination.CDCLSNColumn,
			destination.CDCDeletedColumn,
			destination.CDCSyncedAtColumn,
		},
	})
	require.NoError(t, err)

	db := openDuckDB(t, ctx, path)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM target_table WHERE "_cdc_deleted" = true AND "_cdc_lsn" = '00000000000000000042'`).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestMergeTable_CDCDoesNotRegressTargetLSN(t *testing.T) {
	ctx := t.Context()
	dest, path := connectTestDuckDB(t, ctx)
	require.NoError(t, dest.Exec(ctx, `
		CREATE TABLE target_table (
			id BIGINT PRIMARY KEY,
			name VARCHAR,
			"_cdc_lsn" VARCHAR,
			"_cdc_deleted" BOOLEAN,
			"_cdc_synced_at" TIMESTAMP
		);
		CREATE TABLE staging_table (
			id BIGINT,
			name VARCHAR,
			"_cdc_lsn" VARCHAR,
			"_cdc_deleted" BOOLEAN,
			"_cdc_synced_at" TIMESTAMP,
			"_cdc_unchanged_cols" VARCHAR
		);
		INSERT INTO target_table VALUES
			(1, 'newer-active', '00000000000000000030', false, '2026-01-03'),
			(2, 'newer-deleted', '00000000000000000030', true, '2026-01-03'),
			(3, 'legacy', NULL, false, '2026-01-01'),
			(6, 'same-active', '00000000000000000010', false, '2026-01-01'),
			(7, 'same-deleted', '00000000000000000010', true, '2026-01-01'),
			(8, 'tie-delete', '00000000000000000010', false, '2026-01-01'),
			(9, 'toast-newer', '00000000000000000030', false, '2026-01-03'),
			(10, 'older-row-image', '00000000000000000010', false, '2026-01-01');
		INSERT INTO staging_table VALUES
			(1, 'stale-active', '00000000000000000020', false, '2026-01-02', '[]'),
			(1, NULL, '00000000000000000025', true, '2026-01-02', '[]'),
			(2, 'stale-resurrection', '00000000000000000020', false, '2026-01-02', '[]'),
			(3, 'first-cdc-update', '00000000000000000010', false, '2026-01-02', '[]'),
			(4, 'first-insert', '00000000000000000010', false, '2026-01-02', '[]'),
			(5, NULL, '00000000000000000010', true, '2026-01-02', '[]'),
			(6, 'same-replay', '00000000000000000010', false, '2026-01-02', '[]'),
			(7, 'same-resurrection', '00000000000000000010', false, '2026-01-02', '[]'),
			(8, NULL, '00000000000000000010', true, '2026-01-02', '[]'),
			(9, NULL, '00000000000000000020', false, '2026-01-02', '["name"]'),
			(10, 'latest-row-image', '00000000000000000010', false, '2026-01-02', '[]'),
			(10, NULL, '00000000000000000010', true, '2026-01-02', '[]')
	`))

	opts := destination.MergeOptions{
		StagingTable: "staging_table",
		TargetTable:  "target_table",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn},
	}
	require.NoError(t, dest.MergeTable(ctx, opts))

	expected := map[int64]struct {
		name    string
		lsn     string
		deleted bool
		synced  string
	}{
		1:  {"newer-active", "00000000000000000030", false, "2026-01-03"},
		2:  {"newer-deleted", "00000000000000000030", true, "2026-01-03"},
		3:  {"first-cdc-update", "00000000000000000010", false, "2026-01-02"},
		4:  {"first-insert", "00000000000000000010", false, "2026-01-02"},
		5:  {"<null>", "00000000000000000010", true, "2026-01-02"},
		6:  {"same-active", "00000000000000000010", false, "2026-01-01"},
		7:  {"same-deleted", "00000000000000000010", true, "2026-01-01"},
		8:  {"tie-delete", "00000000000000000010", true, "2026-01-02"},
		9:  {"toast-newer", "00000000000000000030", false, "2026-01-03"},
		10: {"latest-row-image", "00000000000000000010", true, "2026-01-02"},
	}
	for id, want := range expected {
		name, lsn, deleted, synced := readDuckDBCDCRow(t, ctx, dest, id)
		assert.Equal(t, want.name, name, "id %d name", id)
		assert.Equal(t, want.lsn, lsn, "id %d LSN", id)
		assert.Equal(t, want.deleted, deleted, "id %d deleted", id)
		assert.Equal(t, want.synced, synced, "id %d synced timestamp", id)
	}

	require.NoError(t, dest.Exec(ctx, `UPDATE target_table SET name = 'replay-sentinel' WHERE id = 10`))
	require.NoError(t, dest.MergeTable(ctx, opts))
	name, lsn, deleted, _ := readDuckDBCDCRow(t, ctx, dest, 10)
	assert.Equal(t, "replay-sentinel", name)
	assert.Equal(t, "00000000000000000010", lsn)
	assert.True(t, deleted)

	require.NoError(t, dest.Exec(ctx, `DELETE FROM staging_table`))
	require.NoError(t, dest.Exec(ctx, `INSERT INTO staging_table VALUES (1, 'newest', '00000000000000000040', false, '2026-01-04', '[]')`))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertDuckDBCDCState(t, ctx, dest, "newest", "00000000000000000040", false)

	require.NoError(t, dest.Exec(ctx, `DELETE FROM staging_table`))
	require.NoError(t, dest.Exec(ctx, `INSERT INTO staging_table VALUES (1, NULL, '00000000000000000040', true, '2026-01-04', '[]')`))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertDuckDBCDCState(t, ctx, dest, "newest", "00000000000000000040", true)

	require.NoError(t, dest.Exec(ctx, `DELETE FROM staging_table`))
	require.NoError(t, dest.Exec(ctx, `INSERT INTO staging_table VALUES (1, 'stale-outside-predicate', '00000000000000000020', false, '2026-01-02', '[]')`))
	opts.IncrementalPredicate = "target.id > 100"
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertDuckDBCDCState(t, ctx, dest, "newest", "00000000000000000040", true)
	require.NoError(t, dest.Close(ctx))
	assertDuckDBCDCRow(t, ctx, path, "newest", "00000000000000000040", true)
}

func readDuckDBCDCRow(t *testing.T, ctx context.Context, dest *DuckDBDestination, id int64) (string, string, bool, string) {
	t.Helper()
	stmt, err := dest.conn.NewStatement()
	require.NoError(t, err)
	defer func() { _ = stmt.Close() }()
	require.NoError(t, stmt.SetSqlQuery(fmt.Sprintf(`
		SELECT COALESCE(name, '<null>'), COALESCE("_cdc_lsn", ''), "_cdc_deleted", strftime("_cdc_synced_at", '%%Y-%%m-%%d')
		FROM target_table WHERE id = %d
	`, id)))
	reader, _, err := stmt.ExecuteQuery(ctx)
	require.NoError(t, err)
	defer reader.Release()
	require.True(t, reader.Next())
	record := reader.RecordBatch()
	return strings.Clone(record.Column(0).(*array.String).Value(0)),
		strings.Clone(record.Column(1).(*array.String).Value(0)),
		record.Column(2).(*array.Boolean).Value(0),
		strings.Clone(record.Column(3).(*array.String).Value(0))
}

func assertDuckDBCDCState(t *testing.T, ctx context.Context, dest *DuckDBDestination, wantName, wantLSN string, wantDeleted bool) {
	t.Helper()
	name, lsn, deleted, _ := readDuckDBCDCRow(t, ctx, dest, 1)
	assert.Equal(t, wantName, name)
	assert.Equal(t, wantLSN, lsn)
	assert.Equal(t, wantDeleted, deleted)
}

func assertDuckDBCDCRow(t *testing.T, ctx context.Context, path, wantName, wantLSN string, wantDeleted bool) {
	t.Helper()
	db := openDuckDB(t, ctx, path)
	defer func() { _ = db.Close() }()
	var nameRaw, lsnRaw []byte
	var deleted bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT name, "_cdc_lsn", "_cdc_deleted" FROM target_table WHERE id = 1`).Scan(&nameRaw, &lsnRaw, &deleted))
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM target_table WHERE id = 1`).Scan(&count))
	assert.Equal(t, wantName, string(nameRaw))
	assert.Equal(t, wantLSN, string(lsnRaw))
	assert.Equal(t, wantDeleted, deleted)
	assert.Equal(t, 1, count)
}

func TestMergeTable_CDCWithIncrementalPredicateInsertsBeforeUpdate(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	err := dest.Exec(ctx, `
		CREATE TABLE target_table (
			id BIGINT PRIMARY KEY,
			name VARCHAR,
			"_cdc_lsn" VARCHAR,
			"_cdc_deleted" BOOLEAN,
			"_cdc_synced_at" TIMESTAMP
		);
		CREATE TABLE staging_table (
			id BIGINT,
			name VARCHAR,
			"_cdc_lsn" VARCHAR,
			"_cdc_deleted" BOOLEAN,
			"_cdc_synced_at" TIMESTAMP
		);
		INSERT INTO target_table VALUES (1, 'before', '00000000000000000001', false, CURRENT_TIMESTAMP);
		INSERT INTO staging_table VALUES (1, 'after', '00000000000000000002', false, CURRENT_TIMESTAMP)
	`)
	require.NoError(t, err)

	err = dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable:         "staging_table",
		TargetTable:          "target_table",
		PrimaryKeys:          []string{"id"},
		Columns:              []string{"id", "name", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
		IncrementalPredicate: "target.name = 'before'",
	})
	require.NoError(t, err)

	db := openDuckDB(t, ctx, path)
	var count int
	var nameRaw []byte
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*), MAX(name) FROM target_table WHERE id = 1").Scan(&count, &nameRaw))
	assert.Equal(t, 1, count)
	assert.Equal(t, "after", string(nameRaw))
}

func TestDeleteInsertTable_DedupesStagingByPK(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	err := dest.Exec(ctx, `CREATE TABLE target_table (id BIGINT, name VARCHAR, ts BIGINT)`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `CREATE TABLE staging_table (id BIGINT, name VARCHAR, ts BIGINT)`)
	require.NoError(t, err)

	// Staging holds duplicate primary keys for id=1.
	err = dest.Exec(ctx, `
		INSERT INTO staging_table VALUES
			(1, 'Alice', 10),
			(1, 'Alice Dup', 11),
			(2, 'Bob', 20)
	`)
	require.NoError(t, err)

	err = dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
		StagingTable:   "staging_table",
		TargetTable:    "target_table",
		IncrementalKey: "ts",
		IntervalStart:  int64(0),
		IntervalEnd:    int64(100),
		Columns:        []string{"id", "name", "ts"},
		PrimaryKeys:    []string{"id"},
	})
	require.NoError(t, err)

	db := openDuckDB(t, ctx, path)
	var total, id1 int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM target_table").Scan(&total))
	assert.Equal(t, 2, total, "duplicate PK in staging should collapse to one row")
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM target_table WHERE id = 1").Scan(&id1))
	assert.Equal(t, 1, id1, "id=1 should appear exactly once")
}

func TestSwapTable(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	// Create target table with initial data
	err := dest.Exec(ctx, `
		CREATE TABLE target_table (id BIGINT, name VARCHAR)
	`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `INSERT INTO target_table VALUES (1, 'Old')`)
	require.NoError(t, err)

	// Create staging table with new data
	err = dest.Exec(ctx, `
		CREATE TABLE staging_table (id BIGINT, name VARCHAR)
	`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `INSERT INTO staging_table VALUES (2, 'New')`)
	require.NoError(t, err)

	// Swap tables
	err = dest.SwapTable(ctx, destination.SwapOptions{StagingTable: "staging_table", TargetTable: "target_table"})
	require.NoError(t, err)

	// Verify target now has staging data
	db := openDuckDB(t, ctx, path)
	var id int64
	var nameRaw []byte
	err = db.QueryRowContext(ctx, "SELECT id, name FROM target_table").Scan(&id, &nameRaw)
	require.NoError(t, err)
	assert.Equal(t, int64(2), id)
	assert.Equal(t, "New", string(append([]byte(nil), nameRaw...)))
}

// TestSwapTable_CrossSchema_TargetSchemaMissing reproduces the regression
// reported against gong v0.1.101: on a fresh DuckDB the replace strategy only
// PrepareTables the staging side (in `_bruin_staging`), so the target schema
// (e.g. `public`) may not exist yet when SwapTable runs. The cross-schema
// branch must auto-create the target schema before recreating the target
// table, otherwise it fails with "Schema with name public does not exist!".
func TestSwapTable_CrossSchema_TargetSchemaMissing(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString},
		},
	}

	// Stage only — never touch the target schema. This mirrors what the
	// replace strategy does on a fresh deployment.
	stagingTable := "_bruin_staging.public__bootstrap_ingestr_staging_1"
	targetTable := "public.bootstrap_ingestr"

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     stagingTable,
		Schema:    tableSchema,
		DropFirst: true,
	}))

	require.NoError(t, dest.Exec(ctx,
		`INSERT INTO "_bruin_staging"."public__bootstrap_ingestr_staging_1" VALUES (1, 'a'), (2, 'b')`))

	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		StagingTable: stagingTable,
		TargetTable:  targetTable,
	}))

	// Re-open via the same dest path to read post-swap state. We use the
	// dest's own connection so we see committed state without a second
	// reader-cache snapshot.
	_ = path

	var rowCount int64
	rows, err := dest.conn.NewStatement()
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.NoError(t, rows.SetSqlQuery(`SELECT COUNT(*) FROM "public"."bootstrap_ingestr"`))
	reader, _, err := rows.ExecuteQuery(ctx)
	require.NoError(t, err)
	defer reader.Release()
	require.True(t, reader.Next())
	rec := reader.RecordBatch()
	rowCount = rec.Column(0).(*array.Int64).Value(0)
	assert.Equal(t, int64(2), rowCount)
}

// TestSwapTableCleansUpOldTables verifies that SwapTable properly cleans up
// temporary _old_ tables after swapping. This test reproduces a bug where
// tables without schema prefix (e.g., "users" instead of "main.users") would
// leave orphaned _old_ tables because the DROP statement was malformed.
func TestSwapTableCleansUpOldTables(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	// Create target table with initial data (no schema prefix to trigger the bug)
	err := dest.Exec(ctx, `CREATE TABLE swap_cleanup_target (id BIGINT, name VARCHAR)`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `INSERT INTO swap_cleanup_target VALUES (1, 'Old')`)
	require.NoError(t, err)

	// Create staging table with new data (no schema prefix)
	err = dest.Exec(ctx, `CREATE TABLE swap_cleanup_staging (id BIGINT, name VARCHAR)`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `INSERT INTO swap_cleanup_staging VALUES (2, 'New')`)
	require.NoError(t, err)

	// Swap tables - this should rename target to _old_ and then drop the _old_ table
	err = dest.SwapTable(ctx, destination.SwapOptions{StagingTable: "swap_cleanup_staging", TargetTable: "swap_cleanup_target"})
	require.NoError(t, err)

	// Verify no _old_ tables are left behind
	db := openDuckDB(t, ctx, path)
	var oldTableCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_name LIKE 'swap_cleanup_target_old_%'
	`).Scan(&oldTableCount)
	require.NoError(t, err)
	assert.Equal(t, 0, oldTableCount, "expected no _old_ tables to remain after swap")

	// Verify data was swapped correctly
	var id int64
	var nameRaw []byte
	err = db.QueryRowContext(ctx, "SELECT id, name FROM swap_cleanup_target").Scan(&id, &nameRaw)
	require.NoError(t, err)
	assert.Equal(t, int64(2), id)
	assert.Equal(t, "New", string(append([]byte(nil), nameRaw...)))
}

func TestSwapTableQuotedDotTargetPreservesSiblingBoundaries(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	require.NoError(t, dest.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS "order"`))
	require.NoError(t, dest.Exec(ctx, `CREATE TABLE "order"."sentinel" (id BIGINT)`))
	require.NoError(t, dest.Exec(ctx, `INSERT INTO "order"."sentinel" VALUES (99)`))
	require.NoError(t, dest.Exec(ctx, `CREATE TABLE "main"."order.events" (id BIGINT)`))
	require.NoError(t, dest.Exec(ctx, `INSERT INTO "main"."order.events" VALUES (1)`))
	require.NoError(t, dest.Exec(ctx, `CREATE TABLE "main"."staging_for_order_events" (id BIGINT)`))
	require.NoError(t, dest.Exec(ctx, `INSERT INTO "main"."staging_for_order_events" VALUES (2)`))

	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		StagingTable: `"main"."staging_for_order_events"`,
		TargetTable:  `"main"."order.events"`,
	}))

	require.NoError(t, dest.Close(ctx))
	db := openDuckDB(t, ctx, path)
	defer func() { _ = db.Close() }()
	var targetID, sentinelID int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT id FROM "main"."order.events"`).Scan(&targetID))
	require.Equal(t, int64(2), targetID)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT id FROM "order"."sentinel"`).Scan(&sentinelID))
	require.Equal(t, int64(99), sentinelID)
	var oldCount int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'main' AND table_name LIKE 'order.events_old_%'`).Scan(&oldCount))
	require.Zero(t, oldCount)
}

func TestDuckDBSwapRejectsOverQualifiedNames(t *testing.T) {
	dest := NewDuckDBDestination()
	err := dest.SwapTable(t.Context(), destination.SwapOptions{StagingTable: "main.staging", TargetTable: "server.catalog.schema.orders"})
	require.ErrorContains(t, err, "duckdb table name")
}

func TestDropTable(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	// Create table
	err := dest.Exec(ctx, `CREATE TABLE drop_test (id BIGINT)`)
	require.NoError(t, err)

	// Drop table
	err = dest.DropTable(ctx, "drop_test")
	require.NoError(t, err)

	// Verify table doesn't exist
	db := openDuckDB(t, ctx, path)
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'drop_test'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "table should not exist")
}

func TestExec(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	// Execute arbitrary SQL
	err := dest.Exec(ctx, "CREATE TABLE exec_test (id BIGINT)")
	require.NoError(t, err)

	// Verify it worked
	db := openDuckDB(t, ctx, path)
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'exec_test'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestTransaction(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	// Create table
	err := dest.Exec(ctx, `CREATE TABLE tx_test (id BIGINT)`)
	require.NoError(t, err)

	// Test commit
	tx, err := dest.BeginTransaction(ctx)
	require.NoError(t, err)

	err = tx.Exec(ctx, "INSERT INTO tx_test VALUES (1)")
	require.NoError(t, err)

	err = tx.Commit(ctx)
	require.NoError(t, err)

	db := openDuckDB(t, ctx, path)
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tx_test").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Test rollback
	tx2, err := dest.BeginTransaction(ctx)
	require.NoError(t, err)

	err = tx2.Exec(ctx, "INSERT INTO tx_test VALUES (2)")
	require.NoError(t, err)

	err = tx2.Rollback(ctx)
	require.NoError(t, err)

	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tx_test").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "rollback should undo insert")
}

func TestWrite_MultipleBatches(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	// Prepare table
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "value", DataType: schema.TypeFloat64},
		},
	}

	err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     "batch_test",
		Schema:    tableSchema,
		DropFirst: true,
	})
	require.NoError(t, err)

	// Create multiple batches
	mem := memory.NewGoAllocator()
	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "value", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	}, nil)

	records := make(chan source.RecordBatchResult, 3)

	for batch := 0; batch < 3; batch++ {
		idBuilder := array.NewInt64Builder(mem)
		valueBuilder := array.NewFloat64Builder(mem)

		for i := 0; i < 100; i++ {
			idBuilder.Append(int64(batch*100 + i))
			valueBuilder.Append(float64(batch*100+i) * 1.5)
		}

		record := array.NewRecordBatch(arrowSchema, []arrow.Array{idBuilder.NewArray(), valueBuilder.NewArray()}, 100)
		records <- source.RecordBatchResult{Batch: record}
	}
	close(records)

	// Write data
	err = dest.Write(ctx, records, destination.WriteOptions{
		Table: "batch_test",
	})
	require.NoError(t, err)

	// Verify total count
	db := openDuckDB(t, ctx, path)
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM batch_test").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 300, count)
}

func TestWrite_WithNullValues(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	// Prepare table
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
	}

	err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     "null_test",
		Schema:    tableSchema,
		DropFirst: true,
	})
	require.NoError(t, err)

	// Create record with null values
	mem := memory.NewGoAllocator()
	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	idBuilder := array.NewInt64Builder(mem)
	nameBuilder := array.NewStringBuilder(mem)

	idBuilder.Append(1)
	nameBuilder.Append("Alice")

	idBuilder.Append(2)
	nameBuilder.AppendNull()

	idBuilder.Append(3)
	nameBuilder.Append("Charlie")

	record := array.NewRecordBatch(arrowSchema, []arrow.Array{idBuilder.NewArray(), nameBuilder.NewArray()}, 3)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: record}
	close(records)

	err = dest.Write(ctx, records, destination.WriteOptions{
		Table: "null_test",
	})
	require.NoError(t, err)

	// Verify null value
	db := openDuckDB(t, ctx, path)
	var nameRaw []byte
	err = db.QueryRowContext(ctx, "SELECT name FROM null_test WHERE id = 2").Scan(&nameRaw)
	require.NoError(t, err)
	assert.Nil(t, nameRaw, "name should be null for id=2")
}

func TestEnsureSchemaExists(t *testing.T) {
	ctx := context.Background()
	dest, path := connectTestDuckDB(t, ctx)

	// Test that main schema is skipped
	err := dest.ensureSchemaExists(ctx, tablename.TableName{Schema: "main"})
	require.NoError(t, err)

	// Test that empty schema is skipped
	err = dest.ensureSchemaExists(ctx, tablename.TableName{})
	require.NoError(t, err)

	// Test that custom schema is created
	err = dest.ensureSchemaExists(ctx, tablename.TableName{Schema: "custom_schema"})
	require.NoError(t, err)

	// Verify schema exists
	db := openDuckDB(t, ctx, path)
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = 'custom_schema'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "custom_schema should exist")
}

func TestGetTableSchema_Basic(t *testing.T) {
	ctx := context.Background()
	dest, _ := connectTestDuckDB(t, ctx)

	// Create a table with various column types
	err := dest.Exec(ctx, `
		CREATE TABLE schema_test (
			id BIGINT PRIMARY KEY,
			name VARCHAR,
			age INTEGER,
			score DOUBLE,
			active BOOLEAN
		)
	`)
	require.NoError(t, err)

	// Get the schema
	tableSchema, err := dest.GetTableSchema(ctx, "schema_test")
	require.NoError(t, err)
	require.NotNil(t, tableSchema)

	// Verify column count
	assert.Len(t, tableSchema.Columns, 5)

	// Create a map for easier lookup
	colMap := make(map[string]schema.Column)
	for _, col := range tableSchema.Columns {
		colMap[col.Name] = col
	}

	// Verify each column
	assert.Equal(t, schema.TypeInt64, colMap["id"].DataType)
	assert.Equal(t, schema.TypeString, colMap["name"].DataType)
	assert.Equal(t, schema.TypeInt32, colMap["age"].DataType)
	assert.Equal(t, schema.TypeFloat64, colMap["score"].DataType)
	assert.Equal(t, schema.TypeBoolean, colMap["active"].DataType)
}

func TestGetTableSchema_NonExistentTable(t *testing.T) {
	ctx := context.Background()
	dest, _ := connectTestDuckDB(t, ctx)

	// Get schema for non-existent table
	tableSchema, err := dest.GetTableSchema(ctx, "nonexistent_table")
	require.NoError(t, err)
	assert.Nil(t, tableSchema, "should return nil for non-existent table")
}

func TestGetTableSchema_ColumnNamesPreserved(t *testing.T) {
	// This test verifies that column names are properly copied from Arrow buffers
	// and not corrupted after the reader is released. This was a bug where
	// Arrow string values (which are slices into internal buffers) were stored
	// directly without copying, causing garbage values after buffer release.
	ctx := context.Background()
	dest, _ := connectTestDuckDB(t, ctx)

	// Create table with specific column names that we'll verify
	err := dest.Exec(ctx, `
		CREATE TABLE column_name_test (
			user_id BIGINT,
			first_name VARCHAR,
			last_name VARCHAR,
			email_address VARCHAR,
			created_at TIMESTAMP
		)
	`)
	require.NoError(t, err)

	// Get the schema - this is where the bug would manifest
	tableSchema, err := dest.GetTableSchema(ctx, "column_name_test")
	require.NoError(t, err)
	require.NotNil(t, tableSchema)

	// Verify all column names are correct and not garbage
	expectedNames := []string{"user_id", "first_name", "last_name", "email_address", "created_at"}
	actualNames := make([]string, len(tableSchema.Columns))
	for i, col := range tableSchema.Columns {
		actualNames[i] = col.Name
	}

	// Sort both for comparison (DESCRIBE may return in different order)
	assert.ElementsMatch(t, expectedNames, actualNames,
		"column names should match exactly and not be corrupted")

	// Additional check: verify column names are valid UTF-8 strings
	for _, col := range tableSchema.Columns {
		assert.True(t, len(col.Name) > 0, "column name should not be empty")
		// Check that name contains only expected characters (not garbage bytes)
		for _, r := range col.Name {
			assert.True(t, r >= 'a' && r <= 'z' || r == '_',
				"column name %q contains unexpected character %q", col.Name, r)
		}
	}
}

func TestGetTableSchema_ColumnNamesStableAfterMultipleCalls(t *testing.T) {
	// This test ensures column names remain stable across multiple GetTableSchema calls.
	// If strings weren't properly copied, subsequent operations might corrupt previously
	// returned schema data.
	ctx := context.Background()
	dest, _ := connectTestDuckDB(t, ctx)

	// Create two tables
	err := dest.Exec(ctx, `CREATE TABLE table_one (col_a BIGINT, col_b VARCHAR)`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `CREATE TABLE table_two (col_x BIGINT, col_y VARCHAR, col_z INTEGER)`)
	require.NoError(t, err)

	// Get schema for first table
	schema1, err := dest.GetTableSchema(ctx, "table_one")
	require.NoError(t, err)
	require.NotNil(t, schema1)

	// Store the column names from first schema
	names1 := make([]string, len(schema1.Columns))
	for i, col := range schema1.Columns {
		names1[i] = col.Name
	}

	// Get schema for second table - this could potentially corrupt first schema
	// if strings weren't properly copied
	schema2, err := dest.GetTableSchema(ctx, "table_two")
	require.NoError(t, err)
	require.NotNil(t, schema2)

	// Verify first schema's column names are still valid
	for i, col := range schema1.Columns {
		assert.Equal(t, names1[i], col.Name,
			"column name at index %d should remain stable after getting another table's schema", i)
	}

	// Verify column names match expected values
	assert.ElementsMatch(t, []string{"col_a", "col_b"}, names1)

	names2 := make([]string, len(schema2.Columns))
	for i, col := range schema2.Columns {
		names2[i] = col.Name
	}
	assert.ElementsMatch(t, []string{"col_x", "col_y", "col_z"}, names2)
}

func TestGetTableSchema_WithSchema(t *testing.T) {
	ctx := context.Background()
	dest, _ := connectTestDuckDB(t, ctx)

	// Create a schema and table
	err := dest.Exec(ctx, `CREATE SCHEMA test_schema`)
	require.NoError(t, err)

	err = dest.Exec(ctx, `CREATE TABLE test_schema.my_table (id BIGINT, data VARCHAR)`)
	require.NoError(t, err)

	// Get the schema using qualified name
	tableSchema, err := dest.GetTableSchema(ctx, "test_schema.my_table")
	require.NoError(t, err)
	require.NotNil(t, tableSchema)

	assert.Len(t, tableSchema.Columns, 2)
	assert.Equal(t, "test_schema", tableSchema.Schema)
	assert.Equal(t, "my_table", tableSchema.Name)
}

func TestGetTableSchema_ManyColumns(t *testing.T) {
	// Test with many columns to stress the Arrow buffer handling.
	// More columns means more string data in the Arrow buffer.
	ctx := context.Background()
	dest, _ := connectTestDuckDB(t, ctx)

	// Build a table with 50 columns
	var columnDefs []string
	var expectedNames []string
	for i := 0; i < 50; i++ {
		colName := fmt.Sprintf("column_%03d", i)
		columnDefs = append(columnDefs, fmt.Sprintf("%s VARCHAR", colName))
		expectedNames = append(expectedNames, colName)
	}

	createSQL := fmt.Sprintf("CREATE TABLE many_columns (%s)", strings.Join(columnDefs, ", "))
	err := dest.Exec(ctx, createSQL)
	require.NoError(t, err)

	// Get the schema
	tableSchema, err := dest.GetTableSchema(ctx, "many_columns")
	require.NoError(t, err)
	require.NotNil(t, tableSchema)

	assert.Len(t, tableSchema.Columns, 50)

	// Verify all column names are correct
	actualNames := make([]string, len(tableSchema.Columns))
	for i, col := range tableSchema.Columns {
		actualNames[i] = col.Name
	}
	assert.ElementsMatch(t, expectedNames, actualNames)
}

func TestGetTableSchema_SpecialCharacterColumnNames(t *testing.T) {
	// Test that column names with special characters are handled correctly
	ctx := context.Background()
	dest, _ := connectTestDuckDB(t, ctx)

	// Create table with column names that need quoting
	err := dest.Exec(ctx, `
		CREATE TABLE special_cols (
			"Column With Spaces" BIGINT,
			"UPPERCASE" VARCHAR,
			"MixedCase" VARCHAR,
			"with_underscore" INTEGER
		)
	`)
	require.NoError(t, err)

	tableSchema, err := dest.GetTableSchema(ctx, "special_cols")
	require.NoError(t, err)
	require.NotNil(t, tableSchema)

	assert.Len(t, tableSchema.Columns, 4)

	// Verify column names are preserved correctly
	colMap := make(map[string]bool)
	for _, col := range tableSchema.Columns {
		colMap[col.Name] = true
	}

	assert.True(t, colMap["Column With Spaces"], "should preserve spaces in column name")
	assert.True(t, colMap["UPPERCASE"], "should preserve uppercase column name")
	assert.True(t, colMap["MixedCase"], "should preserve mixed case column name")
	assert.True(t, colMap["with_underscore"], "should preserve underscore column name")
}
