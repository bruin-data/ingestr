package source

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestIsCustomQuery(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		wantQuery string
		wantOK    bool
	}{
		{
			name:      "valid custom query",
			tableName: "query:SELECT * FROM orders",
			wantQuery: "SELECT * FROM orders",
			wantOK:    true,
		},
		{
			name:      "not a custom query",
			tableName: "orders",
			wantQuery: "orders",
			wantOK:    false,
		},
		{
			name:      "empty after prefix",
			tableName: "query:",
			wantQuery: "",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, ok := IsCustomQuery(tt.tableName)
			if query != tt.wantQuery || ok != tt.wantOK {
				t.Errorf("IsCustomQuery(%q) = (%q, %v), want (%q, %v)", tt.tableName, query, ok, tt.wantQuery, tt.wantOK)
			}
		})
	}
}

func TestCustomQueryTable(t *testing.T) {
	t.Run("returns table for valid custom query", func(t *testing.T) {
		var capturedQuery string
		executeFn := func(ctx context.Context, query string, opts ReadOptions) (<-chan RecordBatchResult, error) {
			capturedQuery = query
			ch := make(chan RecordBatchResult)
			close(ch)
			return ch, nil
		}

		req := TableRequest{
			Name:           "query:SELECT * FROM orders",
			PrimaryKeys:    []string{"id"},
			IncrementalKey: "updated_at",
			Strategy:       config.StrategyMerge,
		}

		table, err := CustomQueryTable(req, executeFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if table.Name() != CustomQueryTableName {
			t.Errorf("Name() = %q, want %q", table.Name(), CustomQueryTableName)
		}
		if table.HasKnownSchema() {
			t.Error("HasKnownSchema() = true, want false")
		}
		if table.Strategy() != config.StrategyMerge {
			t.Errorf("Strategy() = %v, want %v", table.Strategy(), config.StrategyMerge)
		}

		// Verify the query is passed through to executeFn
		_, _ = table.Read(context.Background(), ReadOptions{})
		if capturedQuery != "SELECT * FROM orders" {
			t.Errorf("executeFn received query %q, want %q", capturedQuery, "SELECT * FROM orders")
		}
	})

	t.Run("substitutes interval params at read time", func(t *testing.T) {
		var capturedQuery string
		executeFn := func(ctx context.Context, query string, opts ReadOptions) (<-chan RecordBatchResult, error) {
			capturedQuery = query
			ch := make(chan RecordBatchResult)
			close(ch)
			return ch, nil
		}

		req := TableRequest{
			Name: "query:SELECT * FROM orders WHERE created_at >= :interval_start",
		}

		table, err := CustomQueryTable(req, executeFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		start := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		_, _ = table.Read(context.Background(), ReadOptions{IntervalStart: &start})
		want := "SELECT * FROM orders WHERE created_at >= '2024-01-15 10:30:00+00:00'"
		if capturedQuery != want {
			t.Errorf("executeFn received query %q, want %q", capturedQuery, want)
		}
	})

	t.Run("errors on unresolved interval placeholders at read time", func(t *testing.T) {
		executeFn := func(ctx context.Context, query string, opts ReadOptions) (<-chan RecordBatchResult, error) {
			ch := make(chan RecordBatchResult)
			close(ch)
			return ch, nil
		}

		req := TableRequest{
			Name: "query:SELECT * FROM orders WHERE created_at >= :interval_start",
		}

		table, err := CustomQueryTable(req, executeFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Read without providing interval params — should error
		_, err = table.Read(context.Background(), ReadOptions{})
		if err == nil {
			t.Fatal("expected error for unresolved interval placeholders")
		}
	})

	t.Run("errors when not a custom query", func(t *testing.T) {
		executeFn := func(ctx context.Context, query string, opts ReadOptions) (<-chan RecordBatchResult, error) {
			ch := make(chan RecordBatchResult)
			close(ch)
			return ch, nil
		}

		req := TableRequest{Name: "orders"}
		_, err := CustomQueryTable(req, executeFn)
		if err == nil {
			t.Fatal("expected error for non-custom-query name")
		}
	})
}

func TestPartitionedCustomQueryTable(t *testing.T) {
	var schemaQuery string
	var executedQueries []string
	quote := func(name string) string { return `"` + name + `"` }
	tableSchema := &schema.TableSchema{
		Name: CustomQueryTableName,
		Columns: []schema.Column{
			{Name: "created_at", DataType: schema.TypeTimestamp},
			{Name: "id", DataType: schema.TypeInt64},
		},
	}

	executeFn := func(ctx context.Context, query string, opts ReadOptions) (<-chan RecordBatchResult, error) {
		executedQueries = append(executedQueries, query)
		ch := make(chan RecordBatchResult)
		close(ch)
		return ch, nil
	}
	table, err := PartitionedCustomQueryTable(TableRequest{
		Name:           "query:SELECT id, created_at FROM orders WHERE updated_at >= :interval_start;",
		IncrementalKey: "created_at",
	}, executeFn, PartitionedCustomQueryOptions{
		QuoteIdentifier: quote,
		FormatTime:      DefaultSQLTimeFormat,
		GetSchema: func(ctx context.Context, query string) (*schema.TableSchema, error) {
			schemaQuery = query
			return tableSchema, nil
		},
		DiscoverBounds: func(ctx context.Context, query string, opts ReadOptions) (ExtractPartitionBounds, error) {
			t.Fatal("bounds discovery should not run when the time partition column matches the incremental key")
			return ExtractPartitionBounds{}, nil
		},
	})
	if err != nil {
		t.Fatalf("PartitionedCustomQueryTable() error = %v", err)
	}
	if !table.SupportsExtractPartitioning() {
		t.Fatal("expected custom query table to support extract partitioning")
	}

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	readOpts := ReadOptions{
		IncrementalKey:           "created_at",
		IntervalStart:            &start,
		IntervalEnd:              &end,
		ExtractPartitionBy:       "created_at",
		ExtractPartitionInterval: 24 * time.Hour,
		Parallelism:              1,
	}
	resolvedSchema, err := table.GetReadSchema(context.Background(), readOpts)
	if err != nil {
		t.Fatalf("GetReadSchema() error = %v", err)
	}
	if resolvedSchema != tableSchema {
		t.Fatal("GetReadSchema() returned an unexpected schema")
	}
	if strings.Contains(schemaQuery, ":interval_start") || strings.Contains(schemaQuery, ";)") {
		t.Fatalf("schema query was not normalized: %s", schemaQuery)
	}
	if !strings.Contains(schemaQuery, "updated_at >= '2026-01-01 00:00:00+00:00'") {
		t.Fatalf("schema query did not substitute the interval: %s", schemaQuery)
	}

	readOpts.Schema = resolvedSchema
	records, err := table.Read(context.Background(), readOpts)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	for result := range records {
		if result.Err != nil {
			t.Fatalf("Read() result error = %v", result.Err)
		}
	}

	if len(executedQueries) != 2 {
		t.Fatalf("executed query count = %d, want 2: %v", len(executedQueries), executedQueries)
	}
	if !strings.Contains(executedQueries[0], `"__ingestr_query"."created_at" >= '2026-01-01 00:00:00'`) ||
		!strings.Contains(executedQueries[0], `"__ingestr_query"."created_at" < '2026-01-02 00:00:00'`) {
		t.Fatalf("first window query = %s", executedQueries[0])
	}
	if !strings.Contains(executedQueries[1], `"__ingestr_query"."created_at" <= '2026-01-03 00:00:00'`) {
		t.Fatalf("final window query does not use an inclusive end: %s", executedQueries[1])
	}
}

func TestPartitionedCustomQueryTableUsesExtractPartitionSchemaNames(t *testing.T) {
	var executedQuery string
	table, err := PartitionedCustomQueryTable(
		TableRequest{Name: "query:SELECT created_at AS partition_ts FROM events", IncrementalKey: "partition_ts"},
		func(ctx context.Context, query string, opts ReadOptions) (<-chan RecordBatchResult, error) {
			executedQuery = query
			return closedRecordBatchResults(), nil
		},
		PartitionedCustomQueryOptions{
			QuoteIdentifier: func(name string) string { return `"` + name + `"` },
			FormatTime:      DefaultSQLTimeFormat,
			GetSchema: func(ctx context.Context, query string) (*schema.TableSchema, error) {
				return nil, nil
			},
			DiscoverBounds: func(ctx context.Context, query string, opts ReadOptions) (ExtractPartitionBounds, error) {
				return ExtractPartitionBounds{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("PartitionedCustomQueryTable() error = %v", err)
	}

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	records, err := table.Read(context.Background(), ReadOptions{
		IncrementalKey:           "partition_ts",
		IntervalStart:            &start,
		IntervalEnd:              &end,
		ExtractPartitionBy:       "PARTITION_TS",
		ExtractPartitionInterval: time.Hour,
		Schema:                   &schema.TableSchema{Columns: []schema.Column{{Name: "partition_ts", DataType: schema.TypeTimestamp}}},
		ExtractPartitionSchema:   &schema.TableSchema{Columns: []schema.Column{{Name: "PARTITION_TS", DataType: schema.TypeTimestamp}}},
	})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	for range records {
	}

	if !strings.Contains(executedQuery, `"__ingestr_query"."PARTITION_TS"`) {
		t.Fatalf("partition query did not preserve metadata casing: %s", executedQuery)
	}
}

func TestSubstituteIntervalParams(t *testing.T) {
	start := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	end := time.Date(2024, 6, 20, 23, 59, 59, 0, time.UTC)

	tests := []struct {
		name  string
		query string
		start *time.Time
		end   *time.Time
		want  string
	}{
		{
			name:  "both placeholders replaced",
			query: "SELECT * FROM orders WHERE created_at >= :interval_start AND created_at <= :interval_end",
			start: &start,
			end:   &end,
			want:  "SELECT * FROM orders WHERE created_at >= '2024-01-15 10:30:00+00:00' AND created_at <= '2024-06-20 23:59:59+00:00'",
		},
		{
			name:  "only start replaced",
			query: "SELECT * FROM orders WHERE created_at >= :interval_start",
			start: &start,
			end:   nil,
			want:  "SELECT * FROM orders WHERE created_at >= '2024-01-15 10:30:00+00:00'",
		},
		{
			name:  "only end replaced",
			query: "SELECT * FROM orders WHERE created_at <= :interval_end",
			start: nil,
			end:   &end,
			want:  "SELECT * FROM orders WHERE created_at <= '2024-06-20 23:59:59+00:00'",
		},
		{
			name:  "no placeholders",
			query: "SELECT * FROM orders WHERE status = 'active'",
			start: &start,
			end:   &end,
			want:  "SELECT * FROM orders WHERE status = 'active'",
		},
		{
			name:  "both nil",
			query: "SELECT * FROM orders WHERE created_at >= :interval_start AND created_at <= :interval_end",
			start: nil,
			end:   nil,
			want:  "SELECT * FROM orders WHERE created_at >= :interval_start AND created_at <= :interval_end",
		},
		{
			name:  "multiple occurrences replaced",
			query: "SELECT * FROM t WHERE a >= :interval_start AND b >= :interval_start",
			start: &start,
			end:   nil,
			want:  "SELECT * FROM t WHERE a >= '2024-01-15 10:30:00+00:00' AND b >= '2024-01-15 10:30:00+00:00'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SubstituteIntervalParams(tt.query, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
