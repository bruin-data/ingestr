package pipeline

import (
	"context"
	"fmt"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDestination struct {
	destination.Destination
	tableSchema *schema.TableSchema
	scheme      string
}

func (m *mockDestination) GetTableSchema(_ context.Context, _ string) (*schema.TableSchema, error) {
	return m.tableSchema, nil
}

func (m *mockDestination) GetScheme() string {
	return m.scheme
}

func TestSetupNamingConvention(t *testing.T) {
	camelCaseSource := schema.TableSchema{
		Columns: []schema.Column{
			{Name: "date", DataType: schema.TypeDate},
			{Name: "currencyCode", DataType: schema.TypeString},
			{Name: "baseCurrency", DataType: schema.TypeString},
			{Name: "rate", DataType: schema.TypeFloat64},
		},
		PrimaryKeys:    []string{"date", "currencyCode", "baseCurrency"},
		IncrementalKey: "currencyCode",
	}

	ingestrDestSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "date", DataType: schema.TypeDate},
			{Name: "currency_code", DataType: schema.TypeString},
			{Name: "base_currency", DataType: schema.TypeString},
			{Name: "rate", DataType: schema.TypeFloat64},
			{Name: "_dlt_load_id", DataType: schema.TypeString},
			{Name: "_dlt_id", DataType: schema.TypeString},
		},
	}

	tests := []struct {
		name               string
		schemaNaming       string
		destSchema         *schema.TableSchema
		wantColumns        []string
		wantPrimaryKeys    []string
		wantIncrementalKey string
		wantRenamer        bool
	}{
		// SchemaNaming="" defaults to snake_case
		{
			name:               "default naming converts to snake_case",
			schemaNaming:       "",
			destSchema:         ingestrDestSchema,
			wantColumns:        []string{"date", "currency_code", "base_currency", "rate"},
			wantPrimaryKeys:    []string{"date", "currency_code", "base_currency"},
			wantIncrementalKey: "currency_code",
			wantRenamer:        true,
		},
		// SchemaNaming="auto" with ingestr dest → detects snake_case
		{
			name:               "auto with ingestr dest detects snake_case",
			schemaNaming:       "auto",
			destSchema:         ingestrDestSchema,
			wantColumns:        []string{"date", "currency_code", "base_currency", "rate"},
			wantPrimaryKeys:    []string{"date", "currency_code", "base_currency"},
			wantIncrementalKey: "currency_code",
			wantRenamer:        true,
		},
		// Dest table doesn't exist yet → default still uses snake_case
		{
			name:               "default naming with no dest table uses snake_case",
			schemaNaming:       "",
			destSchema:         nil,
			wantColumns:        []string{"date", "currency_code", "base_currency", "rate"},
			wantPrimaryKeys:    []string{"date", "currency_code", "base_currency"},
			wantIncrementalKey: "currency_code",
			wantRenamer:        true,
		},
		// Explicit "direct" with ingestr dest → respects user choice, no renaming
		{
			name:               "explicit direct with ingestr dest stays direct",
			schemaNaming:       "direct",
			destSchema:         ingestrDestSchema,
			wantColumns:        []string{"date", "currencyCode", "baseCurrency", "rate"},
			wantPrimaryKeys:    []string{"date", "currencyCode", "baseCurrency"},
			wantIncrementalKey: "currencyCode",
			wantRenamer:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Copy source schema so each test starts fresh
			src := camelCaseSource
			src.Columns = make([]schema.Column, len(camelCaseSource.Columns))
			copy(src.Columns, camelCaseSource.Columns)
			src.PrimaryKeys = make([]string, len(camelCaseSource.PrimaryKeys))
			copy(src.PrimaryKeys, camelCaseSource.PrimaryKeys)

			p := &Pipeline{
				config: &config.IngestConfig{
					DestTable:    "exchange_rates",
					SchemaNaming: tt.schemaNaming,
				},
				dest: &mockDestination{tableSchema: tt.destSchema},
			}

			err := p.setupNamingConvention(context.Background(), &src)
			if err != nil {
				t.Fatalf("setupNamingConvention() error = %v", err)
			}

			gotColumns := src.ColumnNames()
			if len(gotColumns) != len(tt.wantColumns) {
				t.Fatalf("columns length = %d, want %d", len(gotColumns), len(tt.wantColumns))
			}
			for i, want := range tt.wantColumns {
				if gotColumns[i] != want {
					t.Errorf("column[%d] = %q, want %q", i, gotColumns[i], want)
				}
			}

			if len(src.PrimaryKeys) != len(tt.wantPrimaryKeys) {
				t.Fatalf("primary keys length = %d, want %d", len(src.PrimaryKeys), len(tt.wantPrimaryKeys))
			}
			for i, want := range tt.wantPrimaryKeys {
				if src.PrimaryKeys[i] != want {
					t.Errorf("primary key[%d] = %q, want %q", i, src.PrimaryKeys[i], want)
				}
			}

			if src.IncrementalKey != tt.wantIncrementalKey {
				t.Errorf("incremental key = %q, want %q", src.IncrementalKey, tt.wantIncrementalKey)
			}

			hasRenamer := p.columnRenamer != nil && p.columnRenamer.HasRenames()
			if hasRenamer != tt.wantRenamer {
				t.Errorf("has renamer = %v, want %v", hasRenamer, tt.wantRenamer)
			}
		})
	}

	// Test with mostly single-word columns (team_members scenario):
	// only 1 camelCase column vs 3 single-word columns, ingestr dest must still convert
	t.Run("ingestr dest with mostly single-word columns still converts to snake_case", func(t *testing.T) {
		src := schema.TableSchema{
			Columns: []schema.Column{
				{Name: "isRemoved", DataType: schema.TypeBoolean},
				{Name: "name", DataType: schema.TypeString},
				{Name: "role", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "is_removed", DataType: schema.TypeBoolean},
				{Name: "name", DataType: schema.TypeString},
				{Name: "role", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "_dlt_load_id", DataType: schema.TypeString},
				{Name: "_dlt_id", DataType: schema.TypeString},
			},
		}

		p := &Pipeline{
			config: &config.IngestConfig{
				DestTable: "team_members",
			},
			dest: &mockDestination{tableSchema: destSchema},
		}

		err := p.setupNamingConvention(context.Background(), &src)
		if err != nil {
			t.Fatalf("setupNamingConvention() error = %v", err)
		}

		wantColumns := []string{"is_removed", "name", "role", "email"}
		gotColumns := src.ColumnNames()
		if len(gotColumns) != len(wantColumns) {
			t.Fatalf("columns length = %d, want %d", len(gotColumns), len(wantColumns))
		}
		for i, want := range wantColumns {
			if gotColumns[i] != want {
				t.Errorf("column[%d] = %q, want %q", i, gotColumns[i], want)
			}
		}

		hasRenamer := p.columnRenamer != nil && p.columnRenamer.HasRenames()
		if !hasRenamer {
			t.Error("expected column renamer to be set")
		}
	})
}

func TestApplyColumnOverrides(t *testing.T) {
	tests := []struct {
		name      string
		columns   string
		schema    schema.TableSchema
		wantTypes map[string]schema.DataType
		wantErr   bool
	}{
		{
			name:    "no overrides",
			columns: "",
			schema: schema.TableSchema{
				Columns: []schema.Column{
					{Name: "id", DataType: schema.TypeString},
				},
			},
			wantTypes: map[string]schema.DataType{"id": schema.TypeString},
		},
		{
			name:    "override inferred string to timestamptz",
			columns: "LastViewedDate:timestamptz",
			schema: schema.TableSchema{
				Columns: []schema.Column{
					{Name: "Id", DataType: schema.TypeString},
					{Name: "LastViewedDate", DataType: schema.TypeString},
					{Name: "Name", DataType: schema.TypeString},
				},
			},
			wantTypes: map[string]schema.DataType{
				"Id":             schema.TypeString,
				"LastViewedDate": schema.TypeTimestampTZ,
				"Name":           schema.TypeString,
			},
		},
		{
			name:    "multiple overrides",
			columns: "score:float64,count:bigint,created_at:timestamp",
			schema: schema.TableSchema{
				Columns: []schema.Column{
					{Name: "score", DataType: schema.TypeString},
					{Name: "count", DataType: schema.TypeString},
					{Name: "created_at", DataType: schema.TypeString},
					{Name: "name", DataType: schema.TypeString},
				},
			},
			wantTypes: map[string]schema.DataType{
				"score":      schema.TypeFloat64,
				"count":      schema.TypeInt64,
				"created_at": schema.TypeTimestampTZ,
				"name":       schema.TypeString,
			},
		},
		{
			name:    "override column not in schema is ignored",
			columns: "nonexistent:bigint",
			schema: schema.TableSchema{
				Columns: []schema.Column{
					{Name: "id", DataType: schema.TypeString},
				},
			},
			wantTypes: map[string]schema.DataType{"id": schema.TypeString},
		},
		{
			name:    "invalid override format returns error",
			columns: "badformat",
			schema: schema.TableSchema{
				Columns: []schema.Column{
					{Name: "id", DataType: schema.TypeString},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := tt.schema
			src.Columns = make([]schema.Column, len(tt.schema.Columns))
			copy(src.Columns, tt.schema.Columns)

			p := &Pipeline{
				config: &config.IngestConfig{
					Columns: tt.columns,
				},
			}

			err := p.applyColumnOverrides(&src)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("applyColumnOverrides() error = %v", err)
			}

			for _, col := range src.Columns {
				wantType, ok := tt.wantTypes[col.Name]
				if !ok {
					t.Errorf("unexpected column %q", col.Name)
					continue
				}
				if col.DataType != wantType {
					t.Errorf("column %q: type = %v, want %v", col.Name, col.DataType, wantType)
				}
			}
		})
	}
}

// mutableMockDestination simulates a destination whose table schema changes between runs.
type mutableMockDestination struct {
	mockDestination
	schemas []*schema.TableSchema
	callIdx int
}

func (m *mutableMockDestination) GetTableSchema(_ context.Context, _ string) (*schema.TableSchema, error) {
	if m.callIdx >= len(m.schemas) {
		return m.schemas[len(m.schemas)-1], nil
	}
	s := m.schemas[m.callIdx]
	m.callIdx++
	return s, nil
}

func simulateRun(t *testing.T, p *Pipeline, sourceSchema *schema.TableSchema) []string {
	t.Helper()
	src := *sourceSchema
	src.Columns = make([]schema.Column, len(sourceSchema.Columns))
	copy(src.Columns, sourceSchema.Columns)
	src.PrimaryKeys = make([]string, len(sourceSchema.PrimaryKeys))
	copy(src.PrimaryKeys, sourceSchema.PrimaryKeys)

	p.columnRenamer = nil

	err := p.setupNamingConvention(context.Background(), &src)
	if err != nil {
		t.Fatalf("setupNamingConvention() error = %v", err)
	}
	return src.ColumnNames()
}

func assertColumns(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("[%s] column count = %d, want %d\n  got:  %v\n  want: %v", label, len(got), len(want), got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%s] column[%d] = %q, want %q\n  got:  %v\n  want: %v", label, i, got[i], want[i], got, want)
			return
		}
	}
}

func runLabel(i int) string {
	return fmt.Sprintf("run%d", i)
}

func TestNamingConsistency(t *testing.T) {
	camelCaseSourceSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "Id", DataType: schema.TypeString},
			{Name: "CreatedDate", DataType: schema.TypeTimestampTZ},
			{Name: "LastModifiedDate", DataType: schema.TypeTimestampTZ},
			{Name: "OpportunityId", DataType: schema.TypeString},
		},
		PrimaryKeys: []string{"Id"},
	}

	snakeCaseSourceSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeString},
			{Name: "created_date", DataType: schema.TypeTimestampTZ},
			{Name: "last_modified_date", DataType: schema.TypeTimestampTZ},
			{Name: "opportunity_id", DataType: schema.TypeString},
		},
		PrimaryKeys: []string{"id"},
	}

	ingestrSnakeCaseDest := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeString},
			{Name: "created_date", DataType: schema.TypeTimestampTZ},
			{Name: "last_modified_date", DataType: schema.TypeTimestampTZ},
			{Name: "opportunity_id", DataType: schema.TypeString},
			{Name: "_dlt_load_id", DataType: schema.TypeString},
			{Name: "_dlt_id", DataType: schema.TypeString},
		},
	}

	snakeCaseDest := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeString},
			{Name: "created_date", DataType: schema.TypeTimestampTZ},
			{Name: "last_modified_date", DataType: schema.TypeTimestampTZ},
			{Name: "opportunity_id", DataType: schema.TypeString},
		},
	}

	camelCaseDest := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "Id", DataType: schema.TypeString},
			{Name: "CreatedDate", DataType: schema.TypeTimestampTZ},
			{Name: "LastModifiedDate", DataType: schema.TypeTimestampTZ},
			{Name: "OpportunityId", DataType: schema.TypeString},
		},
	}

	snakeCaseExpected := []string{"id", "created_date", "last_modified_date", "opportunity_id"}
	camelCaseExpected := []string{"Id", "CreatedDate", "LastModifiedDate", "OpportunityId"}

	// Replace: ingestr table exists, then replaced — should keep snake_case across runs
	t.Run("replace/ingestr then default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Replace: ingestr table exists, auto naming — should detect snake_case both runs
	t.Run("replace/ingestr then auto naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "auto"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Replace: explicit direct should ignore ingestr dest and keep original column names
	t.Run("replace/explicit direct ignores ingestr dest", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, camelCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "direct"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), camelCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), camelCaseExpected)
	})

	// Replace: no table exists on first run, default naming uses snake_case
	t.Run("replace/no existing table default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{nil, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Replace: no table exists on first run with auto, falls back to snake_case
	t.Run("replace/no existing table auto naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{nil, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "auto"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Replace: explicit snake_case always converts regardless of dest
	t.Run("replace/explicit snake_case", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{nil, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "snake_case"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Replace: 3 consecutive runs after ingestr — naming must stay consistent
	t.Run("replace/three consecutive runs after ingestr", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, snakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		for i := 1; i <= 3; i++ {
			assertColumns(t, runLabel(i), simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		}
	})

	// Merge: ingestr table, then metadata columns removed — should still detect snake_case
	t.Run("merge/ingestr then default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run3", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Merge: ingestr table with auto naming — detects snake_case consistently
	t.Run("merge/ingestr then auto naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "auto"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	t.Run("merge/default naming honors destination convention", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{camelCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), camelCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), camelCaseExpected)
	})

	// Merge: 3 consecutive runs with auto naming after ingestr
	t.Run("merge/three consecutive runs auto naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "auto"},
			dest:   dest,
		}
		for i := 1; i <= 3; i++ {
			assertColumns(t, runLabel(i), simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		}
	})

	// Append: ingestr table, then metadata columns removed — should still detect snake_case
	t.Run("append/ingestr then default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run3", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Append: no table on first run, default naming uses snake_case
	t.Run("append/no existing table default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{nil, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Source already snake_case with ingestr dest — no renaming needed
	t.Run("snake_case source/ingestr dest default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, snakeCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, snakeCaseSourceSchema), snakeCaseExpected)
	})

	// Source already snake_case, no dest, auto naming — stays snake_case
	t.Run("snake_case source/no dest auto naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{nil, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "auto"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, snakeCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, snakeCaseSourceSchema), snakeCaseExpected)
	})

	// camelCase dest without ingestr metadata, default naming — converts to snake_case
	t.Run("camelCase dest no ingestr columns/default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// camelCase dest without ingestr metadata, auto naming — should detect direct
	t.Run("camelCase dest no ingestr columns/auto naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{camelCaseDest, camelCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "auto"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), camelCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), camelCaseExpected)
	})
}

// TestSourceSchemaPreservesOriginalColumnNames verifies that SourceSchema
// (used by strategies to read from the source) retains the original source
// column names even when a naming convention renames columns for the
// destination. The ColumnRenamer handles the rename on Arrow batches after
// reading; the source SELECT query must use the original names.
func TestSourceSchemaPreservesOriginalColumnNames(t *testing.T) {
	sourceSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "Id", DataType: schema.TypeString},
			{Name: "FirstName", DataType: schema.TypeString},
			{Name: "LastName", DataType: schema.TypeString},
		},
		PrimaryKeys: []string{"Id"},
	}

	// Deep copy, same as Run() gets from source.GetTable
	tableSchema := *sourceSchema
	tableSchema.Columns = make([]schema.Column, len(sourceSchema.Columns))
	copy(tableSchema.Columns, sourceSchema.Columns)
	tableSchema.PrimaryKeys = make([]string, len(sourceSchema.PrimaryKeys))
	copy(tableSchema.PrimaryKeys, sourceSchema.PrimaryKeys)

	p := &Pipeline{
		config: &config.IngestConfig{
			DestTable:    "users",
			SchemaNaming: "", // defaults to snake_case
		},
		dest: &mockDestination{tableSchema: nil}, // no existing dest table
	}

	// Snapshot original column names before naming convention renames them.
	// This is what Run() must do to preserve original names for SourceSchema.
	originalSourceSchema := schema.TableSchema{
		Name:           tableSchema.Name,
		Schema:         tableSchema.Schema,
		Columns:        make([]schema.Column, len(tableSchema.Columns)),
		PrimaryKeys:    make([]string, len(tableSchema.PrimaryKeys)),
		IncrementalKey: tableSchema.IncrementalKey,
	}
	copy(originalSourceSchema.Columns, tableSchema.Columns)
	copy(originalSourceSchema.PrimaryKeys, tableSchema.PrimaryKeys)

	// Run() applies naming convention which renames tableSchema columns in-place.
	err := p.setupNamingConvention(context.Background(), &tableSchema)
	if err != nil {
		t.Fatalf("setupNamingConvention() error = %v", err)
	}

	// Verify the destination schema was renamed (sanity check)
	destNames := tableSchema.ColumnNames()
	wantDestNames := []string{"id", "first_name", "last_name"}
	for i, want := range wantDestNames {
		if destNames[i] != want {
			t.Errorf("dest column[%d] = %q, want %q", i, destNames[i], want)
		}
	}

	// SourceSchema must have the ORIGINAL column names because the source
	// table has those names, not the renamed ones. Using renamed names causes:
	//   ERROR: column "first_name" does not exist (SQLSTATE 42703)
	originalNames := []string{"Id", "FirstName", "LastName"}
	sourceSchemaNames := originalSourceSchema.ColumnNames()

	for i, want := range originalNames {
		if sourceSchemaNames[i] != want {
			t.Errorf("SourceSchema column[%d] = %q, want original name %q (source uses these for SELECT queries)",
				i, sourceSchemaNames[i], want)
		}
	}
}

func resolveIncrementality(
	handlesIncrementality bool,
	cfg *config.IngestConfig,
	table *mockSourceTable,
	tableSchema *schema.TableSchema,
) config.IncrementalStrategy {
	// Resolve PKs: user always wins, then table, then schema
	if len(cfg.PrimaryKeys) > 0 {
		tableSchema.PrimaryKeys = cfg.PrimaryKeys
	} else if len(tableSchema.PrimaryKeys) == 0 {
		tableSchema.PrimaryKeys = table.pks
	}

	// Track 1 vs Track 2
	var resolvedStrategy config.IncrementalStrategy
	if handlesIncrementality {
		tableSchema.IncrementalKey = table.incrementalKey
		resolvedStrategy = table.strategy
	} else {
		if cfg.IncrementalKey != "" {
			tableSchema.IncrementalKey = cfg.IncrementalKey
		} else if tableSchema.IncrementalKey == "" {
			tableSchema.IncrementalKey = table.incrementalKey
		}
		resolvedStrategy = cfg.IncrementalStrategy
	}

	if cfg.FullRefresh {
		resolvedStrategy = config.StrategyReplace
	}

	return resolvedStrategy
}

type mockSourceTable struct {
	pks            []string
	incrementalKey string
	strategy       config.IncrementalStrategy
}

func TestResolveIncrementality(t *testing.T) {
	tests := []struct {
		name                  string
		handlesIncrementality bool
		cfg                   *config.IngestConfig
		table                 *mockSourceTable
		schemaIncrementalKey  string
		schemaPKs             []string
		wantStrategy          config.IncrementalStrategy
		wantPKs               []string
		wantIncrementalKey    string
	}{
		// === Track 1: source handles incrementality ===
		{
			name:                  "track1: source strategy and incremental key win",
			handlesIncrementality: true,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyReplace,
				IncrementalKey:      "user_key",
			},
			table: &mockSourceTable{
				pks:            []string{"source_pk"},
				incrementalKey: "updated_at",
				strategy:       config.StrategyMerge,
			},
			wantStrategy:       config.StrategyMerge,
			wantPKs:            []string{"source_pk"},
			wantIncrementalKey: "updated_at",
		},
		{
			name:                  "track1: user PKs override source PKs",
			handlesIncrementality: true,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyReplace,
				PrimaryKeys:         []string{"user_pk"},
			},
			table: &mockSourceTable{
				pks:            []string{"source_pk"},
				incrementalKey: "updated_at",
				strategy:       config.StrategyMerge,
			},
			wantStrategy:       config.StrategyMerge,
			wantPKs:            []string{"user_pk"},
			wantIncrementalKey: "updated_at",
		},
		{
			name:                  "track1: full refresh overrides source strategy",
			handlesIncrementality: true,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyReplace,
				FullRefresh:         true,
			},
			table: &mockSourceTable{
				pks:            []string{"id"},
				incrementalKey: "updated_at",
				strategy:       config.StrategyMerge,
			},
			wantStrategy:       config.StrategyReplace,
			wantPKs:            []string{"id"},
			wantIncrementalKey: "updated_at",
		},

		// === Track 2: framework handles incrementality ===
		{
			name:                  "track2: user strategy wins over table",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyMerge,
				IncrementalKey:      "created_at",
				PrimaryKeys:         []string{"user_pk"},
			},
			table: &mockSourceTable{
				pks:            []string{"table_pk"},
				incrementalKey: "table_key",
				strategy:       config.StrategyAppend,
			},
			wantStrategy:       config.StrategyMerge,
			wantPKs:            []string{"user_pk"},
			wantIncrementalKey: "created_at",
		},
		{
			name:                  "track2: falls back to table PKs when user provides none",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyReplace,
			},
			table: &mockSourceTable{
				pks:      []string{"auto_pk"},
				strategy: config.StrategyReplace,
			},
			wantStrategy:       config.StrategyReplace,
			wantPKs:            []string{"auto_pk"},
			wantIncrementalKey: "",
		},
		{
			name:                  "track2: falls back to table incremental key when user provides none",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyDeleteInsert,
			},
			table: &mockSourceTable{
				pks:            []string{"id"},
				incrementalKey: "modified_at",
				strategy:       config.StrategyReplace,
			},
			wantStrategy:       config.StrategyDeleteInsert,
			wantPKs:            []string{"id"},
			wantIncrementalKey: "modified_at",
		},
		{
			name:                  "track2: schema PKs used when table has none",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyReplace,
			},
			table: &mockSourceTable{
				strategy: config.StrategyReplace,
			},
			schemaPKs:          []string{"schema_pk"},
			wantStrategy:       config.StrategyReplace,
			wantPKs:            []string{"schema_pk"},
			wantIncrementalKey: "",
		},
		{
			name:                  "track2: user PKs override schema PKs",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"user_pk"},
			},
			table: &mockSourceTable{
				pks:      []string{"table_pk"},
				strategy: config.StrategyReplace,
			},
			schemaPKs:          []string{"schema_pk"},
			wantStrategy:       config.StrategyMerge,
			wantPKs:            []string{"user_pk"},
			wantIncrementalKey: "",
		},
		{
			name:                  "track2: schema incremental key used when table has none",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyReplace,
			},
			table: &mockSourceTable{
				strategy: config.StrategyReplace,
			},
			schemaIncrementalKey: "schema_inc_key",
			wantStrategy:         config.StrategyReplace,
			wantPKs:              nil,
			wantIncrementalKey:   "schema_inc_key",
		},
		{
			name:                  "track2: full refresh overrides user strategy",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},
				FullRefresh:         true,
			},
			table: &mockSourceTable{
				pks:      []string{"table_pk"},
				strategy: config.StrategyAppend,
			},
			wantStrategy:       config.StrategyReplace,
			wantPKs:            []string{"id"},
			wantIncrementalKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tableSchema := &schema.TableSchema{
				PrimaryKeys:    tt.schemaPKs,
				IncrementalKey: tt.schemaIncrementalKey,
			}

			gotStrategy := resolveIncrementality(tt.handlesIncrementality, tt.cfg, tt.table, tableSchema)

			if gotStrategy != tt.wantStrategy {
				t.Errorf("strategy = %q, want %q", gotStrategy, tt.wantStrategy)
			}

			if len(tableSchema.PrimaryKeys) != len(tt.wantPKs) {
				t.Errorf("PKs = %v, want %v", tableSchema.PrimaryKeys, tt.wantPKs)
			} else {
				for i, want := range tt.wantPKs {
					if tableSchema.PrimaryKeys[i] != want {
						t.Errorf("PK[%d] = %q, want %q", i, tableSchema.PrimaryKeys[i], want)
					}
				}
			}

			if tableSchema.IncrementalKey != tt.wantIncrementalKey {
				t.Errorf("incrementalKey = %q, want %q", tableSchema.IncrementalKey, tt.wantIncrementalKey)
			}
		})
	}
}

func TestCDCSlotSuffix(t *testing.T) {
	// Deterministic: same input always produces same output
	s1 := cdcSlotSuffix("sqlite:///tmp/a.db")
	s2 := cdcSlotSuffix("sqlite:///tmp/a.db")
	if s1 != s2 {
		t.Errorf("cdcSlotSuffix not deterministic: %q != %q", s1, s2)
	}

	// 6 hex characters
	if len(s1) != 6 {
		t.Errorf("cdcSlotSuffix length = %d, want 6", len(s1))
	}

	// Different URIs produce different suffixes
	s3 := cdcSlotSuffix("sqlite:///tmp/b.db")
	if s1 == s3 {
		t.Errorf("cdcSlotSuffix(%q) == cdcSlotSuffix(%q), want different", "sqlite:///tmp/a.db", "sqlite:///tmp/b.db")
	}
}

func TestDroppedColumnsPKFiltering(t *testing.T) {
	tests := []struct {
		name           string
		primaryKeys    []string
		droppedColumns map[string]bool
		wantPKs        []string
	}{
		{
			name:           "no dropped columns",
			primaryKeys:    []string{"id", "name"},
			droppedColumns: nil,
			wantPKs:        []string{"id", "name"},
		},
		{
			name:           "PK references dropped column",
			primaryKeys:    []string{"campaign_id", "adsquad_id", "start_time"},
			droppedColumns: map[string]bool{"adsquad_id": true},
			wantPKs:        []string{"campaign_id", "start_time"},
		},
		{
			name:           "all PKs dropped",
			primaryKeys:    []string{"a", "b"},
			droppedColumns: map[string]bool{"a": true, "b": true},
			wantPKs:        nil,
		},
		{
			name:           "no PKs defined",
			primaryKeys:    nil,
			droppedColumns: map[string]bool{"a": true},
			wantPKs:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Pipeline{
				droppedColumns: tt.droppedColumns,
			}

			pks := p.filterDroppedPKs(tt.primaryKeys)

			if len(pks) != len(tt.wantPKs) {
				t.Fatalf("PKs = %v, want %v", pks, tt.wantPKs)
			}
			for i, want := range tt.wantPKs {
				if pks[i] != want {
					t.Errorf("PK[%d] = %q, want %q", i, pks[i], want)
				}
			}
		})
	}
}

func tcol(name string, dt schema.DataType) schema.Column {
	return schema.Column{Name: name, DataType: dt, Nullable: true}
}

func tschema(name string, cols ...schema.Column) *schema.TableSchema {
	return &schema.TableSchema{Name: name, Columns: cols}
}

func arrowFieldNames(s *arrow.Schema) []string {
	out := make([]string, s.NumFields())
	for i, f := range s.Fields() {
		out[i] = f.Name
	}
	return out
}

// Case 1: identical source and dest schemas, target equals dest's order,
// types come from dest.
func TestBuildBufferReaderTarget_NoDriftIdenticalOrder(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("age", schema.TypeInt32),
	)
	dest := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("age", schema.TypeInt32),
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"id", "name", "age"})
	if got.Field(0).Type.ID() != arrow.PrimitiveTypes.Int64.ID() {
		t.Errorf("field 0 type = %s, want int64", got.Field(0).Type)
	}
	if got.Field(1).Type.ID() != arrow.BinaryTypes.String.ID() {
		t.Errorf("field 1 type = %s, want string", got.Field(1).Type)
	}
	if got.Field(2).Type.ID() != arrow.PrimitiveTypes.Int32.ID() {
		t.Errorf("field 2 type = %s, want int32", got.Field(2).Type)
	}
}

// source-only columns reach destSchema via the evolve phase (ChangeAddColumn);
// when destSchema doesn't carry them, this function drops them.
func TestBuildBufferReaderTarget_SourceOnlyColumnIsDropped(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("email", schema.TypeString),
	)
	dest := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"id", "name"})
}

func TestBuildBufferReaderTarget_OrderFollowsDest(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"users",
		tcol("email", schema.TypeString),
		tcol("age", schema.TypeInt32),
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
	)
	dest := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("age", schema.TypeInt32),
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"id", "name", "age"})
}

// Case 4a: dest has an ingestr metadata column NOT in source. It must be
// SKIPPED in the target. IngestrColumnFiller adds it downstream; including
// it here would cause a duplicate.
func TestBuildBufferReaderTarget_SkipsIngestrMetadataColumn(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
	)
	dest := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("_dlt_load_id", schema.TypeString),
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"id", "name"})
}

// Case 4b: dest has a NON-ingestr column NOT in source (soft-removed under
// evolve mode). It MUST be included in the target so the buffer reader
// null-fills it; staging then gets the column with NULLs, MERGE inserts NULL
// into the dest column for new rows.
func TestBuildBufferReaderTarget_IncludesSoftRemovedColumn(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
	)
	dest := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("email", schema.TypeString), // soft-removed from source
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"id", "name", "email"})
	if !got.Field(2).Nullable {
		t.Errorf("soft-removed column must be nullable")
	}
}

// Case 5: dest type differs from source type. Target uses dest's type so the
// buffer reader casts batches to the staging-table column type.
func TestBuildBufferReaderTarget_UsesDestTypes(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"events",
		tcol("id", schema.TypeInt64),
		tcol("created_at", schema.TypeTimestamp), // source: TIMESTAMP
	)
	dest := tschema(
		"events",
		tcol("id", schema.TypeInt64),
		tcol("created_at", schema.TypeString), // dest: STRING
	)

	got := p.buildBufferReaderTarget(src, dest)

	if got.Field(1).Name != "created_at" {
		t.Errorf("field 1 name = %q, want created_at", got.Field(1).Name)
	}
	if got.Field(1).Type.ID() != arrow.BinaryTypes.String.ID() {
		t.Errorf("field 1 type = %s, want string", got.Field(1).Type)
	}
}

// Case 6: a ColumnRenamer bridges source camelCase names to dest snake_case.
// Field names in the target stay as SOURCE names (to match buffer files), but
// type lookup goes through the rename map to find the dest column.
func TestBuildBufferReaderTarget_HonorsRenamer(t *testing.T) {
	p := &Pipeline{
		columnRenamer: transformer.NewColumnRenamer(map[string]string{
			"userId":    "user_id",
			"createdAt": "created_at",
		}),
	}
	src := tschema(
		"users",
		tcol("userId", schema.TypeInt64),
		tcol("createdAt", schema.TypeTimestamp),
	)
	dest := tschema(
		"users",
		tcol("user_id", schema.TypeInt64),
		tcol("created_at", schema.TypeString), // wider dest type
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"userId", "createdAt"})
	if got.Field(1).Type.ID() != arrow.BinaryTypes.String.ID() {
		t.Errorf("field 1 type = %s, want string", got.Field(1).Type)
	}
}

// Case 7: realistic evolve scenario.
func TestBuildBufferReaderTarget_EvolveScenario(t *testing.T) {
	p := &Pipeline{}
	// Source order can be anything — we only care about names.
	src := tschema(
		"orders",
		tcol("age", schema.TypeInt64),
		tcol("email", schema.TypeString), // new
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("score", schema.TypeInt64),
	)

	dest := tschema(
		"orders",
		tcol("age", schema.TypeInt64),
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("score", schema.TypeInt64),
		tcol("email", schema.TypeString), // added by Compare.ChangeAddColumn
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"age", "id", "name", "score", "email"})
}

func TestApplyColumnMapping(t *testing.T) {
	t.Run("RenamesOnly", func(t *testing.T) {
		p := &Pipeline{}
		s := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "userId"},
				{Name: "createdAt"},
			},
			PrimaryKeys:    []string{"userId"},
			IncrementalKey: "createdAt",
			PartitionBy:    "createdAt",
		}
		renames := map[string]string{
			"userId":    "user_id",
			"createdAt": "created_at",
		}

		p.applyColumnMapping(s, renames, nil)

		got := make([]string, len(s.Columns))
		for i, c := range s.Columns {
			got[i] = c.Name
		}
		assert.Equal(t, []string{"id", "user_id", "created_at"}, got)
		assert.Equal(t, []string{"user_id"}, s.PrimaryKeys)
		assert.Equal(t, "created_at", s.IncrementalKey)
		assert.Equal(t, "created_at", s.PartitionBy)
		assert.True(t, p.columnRenamer.HasRenames())
		assert.Equal(t, renames, p.columnRenamer.Mapping())
	})

	t.Run("DropOnlyTrulyRemovesColumn", func(t *testing.T) {
		// No rename reclaims the dropped name → column truly disappears.
		p := &Pipeline{}
		s := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "ssn"},
				{Name: "email"},
			},
		}
		drops := map[string]bool{"ssn": true}

		p.applyColumnMapping(s, nil, drops)

		got := make([]string, len(s.Columns))
		for i, c := range s.Columns {
			got[i] = c.Name
		}
		assert.Equal(t, []string{"id", "email"}, got)
		assert.True(t, p.columnRenamer.Drops()["ssn"])
	})

	t.Run("DropThenRenameRestoresName_KeyReferencesSurvive", func(t *testing.T) {
		// `created_at` is dropped (collision loser) but the next step renames
		// `createdAt → created_at`, restoring the name. The PK / incremental
		// key / partition key all point at "created_at" and must keep working.
		p := &Pipeline{}
		s := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id"},
				{Name: "created_at"},
				{Name: "createdAt"},
			},
			PrimaryKeys:    []string{"created_at"},
			IncrementalKey: "created_at",
			PartitionBy:    "created_at",
		}
		renames := map[string]string{"createdAt": "created_at"}
		drops := map[string]bool{"created_at": true}

		p.applyColumnMapping(s, renames, drops)

		got := make([]string, len(s.Columns))
		for i, c := range s.Columns {
			got[i] = c.Name
		}
		assert.Equal(t, []string{"id", "created_at"}, got)
		assert.Equal(t, []string{"created_at"}, s.PrimaryKeys)
		assert.Equal(t, "created_at", s.IncrementalKey)
		assert.Equal(t, "created_at", s.PartitionBy)
	})

	t.Run("EmptyMapsAreNoop", func(t *testing.T) {
		p := &Pipeline{}
		s := &schema.TableSchema{
			Columns: []schema.Column{{Name: "id"}, {Name: "name"}},
		}
		p.applyColumnMapping(s, nil, nil)

		got := make([]string, len(s.Columns))
		for i, c := range s.Columns {
			got[i] = c.Name
		}
		assert.Equal(t, []string{"id", "name"}, got)
		assert.False(t, p.columnRenamer.HasRenames())
	})
}

func TestApplyColumnMappingComposition(t *testing.T) {
	t.Run("OldDropPreservedAcrossSecondStep", func(t *testing.T) {
		// Step 1: drop D.
		// Step 2: rename X -> Y (no drops).
		// Composed renamer: D still dropped, X still renamed.
		// Mirrors the real flow: setupNamingConvention may drop columns,
		// shortenLongIdentifiers only renames — composition must carry old
		// drops forward across a rename-only second step.
		p := &Pipeline{}
		s := &schema.TableSchema{
			Columns: []schema.Column{{Name: "X"}, {Name: "D"}, {Name: "Z"}},
		}

		p.applyColumnMapping(s, nil, map[string]bool{"D": true})
		p.applyColumnMapping(s, map[string]string{"X": "Y"}, nil)

		assert.True(t, p.columnRenamer.Drops()["D"])
		assert.Equal(t, "Y", p.columnRenamer.Mapping()["X"])
		got := make([]string, len(s.Columns))
		for i, c := range s.Columns {
			got[i] = c.Name
		}
		assert.Equal(t, []string{"Y", "Z"}, got)
	})

	t.Run("ChainedRenameThroughIntermediateName", func(t *testing.T) {
		// Step 1: A -> B.
		// Step 2: B -> C (no drops).
		// Composed: A -> C.
		p := &Pipeline{}
		s := &schema.TableSchema{
			Columns: []schema.Column{{Name: "A"}, {Name: "other"}},
		}

		p.applyColumnMapping(s, map[string]string{"A": "B"}, nil)
		p.applyColumnMapping(s, map[string]string{"B": "C"}, nil)

		assert.Equal(t, "C", p.columnRenamer.Mapping()["A"])
	})
}

// TestEndToEndRenamingWithCollisions exercises the full naming pipeline on a
// realistic Arrow batch:
//  1. setupNamingConvention with snake_case + collisions → renames last,
//     drops earlier.
//  2. shortenLongIdentifiers if any name exceeds the destination's limit →
//     composed with naming's renamer.
//  3. Renamer.Transform applied to a batch carrying ORIGINAL source column
//     names → produces the expected output schema and data.
//
// This is the real downstream consumer's view: whatever the destination
// receives is whatever this batch ends up looking like.
func TestEndToEndRenamingWithCollisions(t *testing.T) {
	pool := memory.NewGoAllocator()

	t.Run("CollisionDropsEarlierAndRenamesLast", func(t *testing.T) {
		// Source has 3 columns that all normalize to "user_id":
		//   userID, userId, user_ID
		// Under snake_case, the LAST (user_ID) wins, earlier two are dropped.
		src := schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "userID", DataType: schema.TypeString},
				{Name: "userId", DataType: schema.TypeString},
				{Name: "user_ID", DataType: schema.TypeString},
				{Name: "createdAt", DataType: schema.TypeString},
			},
		}

		p := &Pipeline{
			config: &config.IngestConfig{
				DestTable:    "users",
				SchemaNaming: "snake_case",
			},
			dest: &mockDestination{scheme: "postgres"}, // 63-char limit
		}

		if err := p.setupNamingConvention(context.Background(), &src); err != nil {
			t.Fatalf("setupNamingConvention: %v", err)
		}
		p.shortenLongIdentifiers(&src)

		// Schema after naming: collision losers gone, winner renamed.
		gotCols := make([]string, len(src.Columns))
		for i, c := range src.Columns {
			gotCols[i] = c.Name
		}
		assert.Equal(t, []string{"id", "user_id", "created_at"}, gotCols)

		// Build an incoming batch with the ORIGINAL source column names + values.
		// id=[1,2], userID="A1"/"A2", userId="B1"/"B2", user_ID="C1"/"C2", createdAt=t1/t2.
		fields := []arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
			{Name: "userID", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "userId", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "user_ID", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "createdAt", Type: arrow.BinaryTypes.String, Nullable: true},
		}
		inputSchema := arrow.NewSchema(fields, nil)

		idB := array.NewInt64Builder(pool)
		defer idB.Release()
		idB.AppendValues([]int64{1, 2}, nil)

		mkStr := func(vals ...string) arrow.Array {
			b := array.NewStringBuilder(pool)
			defer b.Release()
			b.AppendValues(vals, nil)
			return b.NewArray()
		}
		cols := []arrow.Array{
			idB.NewArray(),
			mkStr("A1", "A2"), // userID — should be dropped
			mkStr("B1", "B2"), // userId — should be dropped
			mkStr("C1", "C2"), // user_ID — should be renamed to user_id, data preserved
			mkStr("t1", "t2"), // createdAt — should be renamed to created_at
		}
		batch := array.NewRecordBatch(inputSchema, cols, 2)
		for _, c := range cols {
			c.Release()
		}
		defer batch.Release()

		out, err := p.columnRenamer.Transform(batch)
		require.NoError(t, err)
		defer out.Release()

		// Output schema: id, user_id, created_at (in that order).
		require.Equal(t, int64(3), out.NumCols())
		assert.Equal(t, "id", out.Schema().Field(0).Name)
		assert.Equal(t, "user_id", out.Schema().Field(1).Name)
		assert.Equal(t, "created_at", out.Schema().Field(2).Name)

		// user_id carries user_ID's data (C1/C2), NOT userID/userId.
		userIDOut, ok := out.Column(1).(*array.String)
		require.True(t, ok)
		assert.Equal(t, "C1", userIDOut.Value(0))
		assert.Equal(t, "C2", userIDOut.Value(1))

		// created_at carries createdAt's data.
		createdAtOut, ok := out.Column(2).(*array.String)
		require.True(t, ok)
		assert.Equal(t, "t1", createdAtOut.Value(0))
		assert.Equal(t, "t2", createdAtOut.Value(1))
	})

	t.Run("ShorteningComposesWithNaming", func(t *testing.T) {
		// Pick a column whose snake_case form exceeds Postgres's 63-char limit
		// so the second-stage shortening fires AND has to compose with the
		// naming renamer from the first stage.
		longCamel := "thisIsAReallyVeryLongCamelCaseColumnNameThatExceedsTheSixtyThreeCharacterLimit"
		// (snake_case form is even longer — guaranteed over 63.)

		src := schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: longCamel, DataType: schema.TypeString},
			},
		}

		p := &Pipeline{
			config: &config.IngestConfig{
				DestTable:    "things",
				SchemaNaming: "snake_case",
			},
			dest: &mockDestination{scheme: "postgres"},
		}

		require.NoError(t, p.setupNamingConvention(context.Background(), &src))
		p.shortenLongIdentifiers(&src)

		// Confirm shortening kicked in for the long column.
		require.Equal(t, 2, len(src.Columns))
		assert.Equal(t, "id", src.Columns[0].Name)
		shortened := src.Columns[1].Name
		assert.LessOrEqual(t, len(shortened), 63, "shortened name must fit Postgres limit")
		assert.NotEqual(t, longCamel, shortened, "must differ from original")

		// Run a batch through the composed renamer. Incoming batch uses the
		// ORIGINAL source name (longCamel) — composition must rewrite that
		// directly to the shortened name in one pass.
		fields := []arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
			{Name: longCamel, Type: arrow.BinaryTypes.String, Nullable: true},
		}
		idB := array.NewInt64Builder(pool)
		defer idB.Release()
		idB.AppendValues([]int64{1}, nil)
		valB := array.NewStringBuilder(pool)
		defer valB.Release()
		valB.AppendValues([]string{"hello"}, nil)

		cols := []arrow.Array{idB.NewArray(), valB.NewArray()}
		batch := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, 1)
		for _, c := range cols {
			c.Release()
		}
		defer batch.Release()

		out, err := p.columnRenamer.Transform(batch)
		require.NoError(t, err)
		defer out.Release()

		require.Equal(t, int64(2), out.NumCols())
		assert.Equal(t, "id", out.Schema().Field(0).Name)
		assert.Equal(t, shortened, out.Schema().Field(1).Name,
			"composed renamer must rewrite original source name directly to shortened name")

		// Data preserved.
		valOut, ok := out.Column(1).(*array.String)
		require.True(t, ok)
		assert.Equal(t, "hello", valOut.Value(0))
	})

	t.Run("PrimaryKeyAndIncrementalKeySurviveCollision", func(t *testing.T) {
		// Source columns collide (created_at + createdAt → created_at), AND
		// the dropped name is referenced as both PK and incremental key.
		// The rename step restores the name with the winner's data, so the
		// key references must remain intact.
		src := schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "created_at", DataType: schema.TypeString},
				{Name: "createdAt", DataType: schema.TypeString},
			},
			PrimaryKeys:    []string{"id", "created_at"},
			IncrementalKey: "created_at",
			PartitionBy:    "created_at",
		}

		p := &Pipeline{
			config: &config.IngestConfig{
				DestTable:    "events",
				SchemaNaming: "snake_case",
			},
			dest: &mockDestination{scheme: "postgres"},
		}

		require.NoError(t, p.setupNamingConvention(context.Background(), &src))

		// Key references still resolve — the dropped column's name was
		// reclaimed by the rename.
		assert.Equal(t, []string{"id", "created_at"}, src.PrimaryKeys)
		assert.Equal(t, "created_at", src.IncrementalKey)
		assert.Equal(t, "created_at", src.PartitionBy)

		// Schema has the survivors.
		gotCols := make([]string, len(src.Columns))
		for i, c := range src.Columns {
			gotCols[i] = c.Name
		}
		assert.Equal(t, []string{"id", "created_at"}, gotCols)

		// Renamer correctly drops original `created_at` and renames `createdAt`.
		assert.True(t, p.columnRenamer.Drops()["created_at"])
		assert.Equal(t, "created_at", p.columnRenamer.Mapping()["createdAt"])
	})

	t.Run("KeyReferencesRewrittenWhenWinnerIsAlreadySnakeCase", func(t *testing.T) {
		src := schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "userId", DataType: schema.TypeString},
				{Name: "user_id", DataType: schema.TypeString},
			},
			PrimaryKeys:    []string{"userId"},
			IncrementalKey: "userId",
			PartitionBy:    "userId",
		}

		p := &Pipeline{
			config: &config.IngestConfig{
				DestTable:    "users",
				SchemaNaming: "snake_case",
			},
			dest: &mockDestination{scheme: "postgres"},
		}

		require.NoError(t, p.setupNamingConvention(context.Background(), &src))

		// Schema: id + the surviving user_id (winner needed no rename).
		gotCols := make([]string, len(src.Columns))
		for i, c := range src.Columns {
			gotCols[i] = c.Name
		}
		assert.Equal(t, []string{"id", "user_id"}, gotCols)

		// All three key references rewritten from "userId" to "user_id" so
		// they keep pointing at a column that actually exists.
		assert.Equal(t, []string{"user_id"}, src.PrimaryKeys)
		assert.Equal(t, "user_id", src.IncrementalKey)
		assert.Equal(t, "user_id", src.PartitionBy)

		// userId is dropped from batches; the rename entry is present so the
		// PK/IK rewrite above can resolve it
		assert.True(t, p.columnRenamer.Drops()["userId"])
		assert.Equal(t, "user_id", p.columnRenamer.Mapping()["userId"])
	})
}
