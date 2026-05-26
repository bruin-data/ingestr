package pipeline

import (
	"context"
	"fmt"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/transformer"
)

type mockDestination struct {
	destination.Destination
	tableSchema *schema.TableSchema
}

func (m *mockDestination) GetTableSchema(_ context.Context, _ string) (*schema.TableSchema, error) {
	return m.tableSchema, nil
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

func TestSetupNamingConventionCollisionMerge(t *testing.T) {
	src := schema.TableSchema{
		Columns: []schema.Column{
			{Name: "_id", DataType: schema.TypeInt64},
			{Name: "userId", DataType: schema.TypeInt64},
			{Name: "user_id", DataType: schema.TypeInt64},
			{Name: "UserID", DataType: schema.TypeInt64},
		},
	}

	p := &Pipeline{
		config: &config.IngestConfig{
			DestTable:    "users",
			SchemaNaming: "snake_case",
		},
		dest: &mockDestination{tableSchema: nil},
	}

	if err := p.setupNamingConvention(context.Background(), &src); err != nil {
		t.Fatalf("setupNamingConvention() error = %v", err)
	}

	// Schema collapses to two columns: _id and user_id.
	gotCols := src.ColumnNames()
	wantCols := []string{"_id", "user_id"}
	if len(gotCols) != len(wantCols) {
		t.Fatalf("columns = %v, want %v", gotCols, wantCols)
	}
	for i, w := range wantCols {
		if gotCols[i] != w {
			t.Errorf("column[%d] = %q, want %q", i, gotCols[i], w)
		}
	}

	// Renamer holds a merge group with all three variants in source order.
	if p.columnRenamer == nil || !p.columnRenamer.HasRenames() {
		t.Fatalf("expected column renamer with merges")
	}
	got := p.columnRenamer.Merges()["user_id"]
	want := []string{"userId", "user_id", "UserID"}
	if len(got) != len(want) {
		t.Fatalf("merges[user_id] = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("merges[user_id][%d] = %q, want %q", i, got[i], w)
		}
	}
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
