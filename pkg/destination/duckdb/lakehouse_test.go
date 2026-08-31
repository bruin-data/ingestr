package duckdb

import (
	"context"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestDuckLakeMemStageName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{
			name:   "schema-qualified staging table",
			target: "main.orders_staging_1784713433980251000",
			want:   "orders_staging_1784713433980251000__ducklake_memstage",
		},
		{
			name:   "catalog-qualified target uses only the table component",
			target: "ducklake_catalog.main.orders",
			want:   "orders__ducklake_memstage",
		},
		{
			name:   "unsafe characters are sanitized",
			target: `weird-name.with space`,
			want:   "with_space__ducklake_memstage",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := duckLakeMemStageName(tt.target)
			if got != tt.want {
				t.Errorf("duckLakeMemStageName(%q) = %q, want %q", tt.target, got, tt.want)
			}
			// The staged name must be a valid single SQL identifier body.
			if strings.ContainsAny(got, ". -") {
				t.Errorf("staged name %q contains characters that need quoting", got)
			}
		})
	}
}

func TestBuildDuckLakeTableLayout(t *testing.T) {
	t.Parallel()
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "tenant_id", DataType: schema.TypeInt64},
		{Name: "created_at", DataType: schema.TypeTimestamp},
	}}
	layout, err := buildDuckLakeTableLayout(destination.PrepareOptions{
		Schema:      tableSchema,
		PartitionBy: "created_at",
		ClusterBy:   []string{"tenant_id", "created_at"},
	})
	require.NoError(t, err)
	require.True(t, layout.partitionSet)
	require.Equal(t, "created_at", layout.partitionColumn)
	require.True(t, layout.partitionByDay)
	require.True(t, layout.sortSet)
	require.Equal(t, []string{"tenant_id", "created_at"}, layout.sortColumns)
	require.Equal(t,
		`ALTER TABLE "analytics"."events" SET PARTITIONED BY (year("created_at"), month("created_at"), day("created_at"))`,
		buildDuckLakePartitionSQL("analytics.events", layout.partitionColumn, layout.partitionByDay),
	)
	require.Equal(t,
		`ALTER TABLE "analytics"."events" SET SORTED BY ("tenant_id" ASC, "created_at" ASC)`,
		buildDuckLakeSortSQL("analytics.events", layout.sortColumns),
	)
}

func TestBuildDuckLakeTableLayoutUsesIdentityForDate(t *testing.T) {
	t.Parallel()
	layout, err := buildDuckLakeTableLayout(destination.PrepareOptions{
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "event_date", DataType: schema.TypeDate},
		}},
		PartitionBy: "event_date",
	})
	require.NoError(t, err)
	require.False(t, layout.partitionByDay)
	require.Equal(t,
		`ALTER TABLE "events" SET PARTITIONED BY ("event_date")`,
		buildDuckLakePartitionSQL("events", layout.partitionColumn, layout.partitionByDay),
	)
}

func TestBuildDuckLakeTableLayoutValidatesColumns(t *testing.T) {
	t.Parallel()
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "created_at", DataType: schema.TypeTimestamp},
		{Name: "label", DataType: schema.TypeString},
	}}
	tests := []struct {
		name string
		opts destination.PrepareOptions
		want string
	}{
		{
			name: "missing partition column",
			opts: destination.PrepareOptions{Schema: tableSchema, PartitionBy: "missing"},
			want: `layout column "missing" does not exist`,
		},
		{
			name: "partition requires temporal column",
			opts: destination.PrepareOptions{Schema: tableSchema, PartitionBy: "label"},
			want: `partition column "label" must be a date or timestamp, got string`,
		},
		{
			name: "missing sort column",
			opts: destination.PrepareOptions{Schema: tableSchema, ClusterBy: []string{"missing"}},
			want: `layout column "missing" does not exist`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := buildDuckLakeTableLayout(tt.opts)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestBuildDuckLakeTableLayoutResolvesColumnsLeniently(t *testing.T) {
	t.Parallel()
	layout, err := buildDuckLakeTableLayout(destination.PrepareOptions{
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "tenant_id", DataType: schema.TypeInt64},
			{Name: "created_at", DataType: schema.TypeTimestampTZ},
		}},
		PartitionBy: "Created_At",
		ClusterBy:   []string{" tenant_id ", "", "  "},
	})
	require.NoError(t, err)
	require.Equal(t, "created_at", layout.partitionColumn)
	require.True(t, layout.partitionByDay)
	require.Equal(t, []string{"tenant_id"}, layout.sortColumns)
}

func TestBuildDuckLakeTableLayoutIgnoresBlankRequests(t *testing.T) {
	t.Parallel()
	layout, err := buildDuckLakeTableLayout(destination.PrepareOptions{
		PartitionBy: "   ",
		ClusterBy:   []string{"", " "},
	})
	require.NoError(t, err)
	require.True(t, layout.empty())
}

func TestBuildDuckLakeTableLayoutRequiresSchema(t *testing.T) {
	t.Parallel()
	_, err := buildDuckLakeTableLayout(destination.PrepareOptions{PartitionBy: "created_at"})
	require.ErrorContains(t, err, "schema is required")
}

func TestDuckLakeLayoutAppliesToPreparedSchema(t *testing.T) {
	t.Parallel()
	layout := duckLakeTableLayout{
		partitionSet:    true,
		partitionColumn: "created_at",
		partitionByDay:  true,
		sortSet:         true,
		sortColumns:     []string{"tenant_id", "created_at"},
	}
	require.False(t, duckLakeLayoutAppliesToSchema(layout, &schema.TableSchema{Columns: []schema.Column{
		{Name: "tenant_id", DataType: schema.TypeInt64},
	}}))
	require.False(t, duckLakeLayoutAppliesToSchema(layout, &schema.TableSchema{Columns: []schema.Column{
		{Name: "tenant_id", DataType: schema.TypeInt64},
		{Name: "created_at", DataType: schema.TypeString},
	}}))
	require.True(t, duckLakeLayoutAppliesToSchema(layout, &schema.TableSchema{Columns: []schema.Column{
		{Name: "tenant_id", DataType: schema.TypeInt64},
		{Name: "created_at", DataType: schema.TypeTimestamp},
	}}))
}

func TestDuckLakeSortLayoutMatches(t *testing.T) {
	t.Parallel()
	expected := []string{"tenant_id", "created_at"}
	require.True(t, duckLakeSortLayoutMatches(expected, []duckLakeSortExpression{
		{expression: "tenant_id", direction: "ASC", nullOrder: "NULLS_LAST"},
		{expression: "created_at", direction: "ASC", nullOrder: "NULLS_LAST"},
	}))
	require.False(t, duckLakeSortLayoutMatches(expected, []duckLakeSortExpression{
		{expression: "tenant_id", direction: "DESC", nullOrder: "NULLS_LAST"},
		{expression: "created_at", direction: "ASC", nullOrder: "NULLS_LAST"},
	}))
	require.False(t, duckLakeSortLayoutMatches(expected, []duckLakeSortExpression{
		{expression: "tenant_id", direction: "ASC", nullOrder: "NULLS_LAST"},
	}))
	require.True(t, duckLakeSortLayoutMatches([]string{"Order Details", `say"hello`}, []duckLakeSortExpression{
		{expression: `"Order Details"`, direction: "ASC", nullOrder: "NULLS_LAST"},
		{expression: `"say""hello"`, direction: "ASC", nullOrder: "NULLS_LAST"},
	}))
	require.False(t, duckLakeSortLayoutMatches([]string{"created_at"}, []duckLakeSortExpression{
		{expression: `date_trunc('day', created_at)`, direction: "ASC", nullOrder: "NULLS_LAST"},
	}))
}

func TestDuckLakeSwapRestoresLayoutOnRecreatedTarget(t *testing.T) {
	t.Parallel()
	d := NewDuckLakeDestination()
	require.NotNil(t, d.onTargetRecreated, "cross-schema swap must restore the layout before rows are copied in")
	require.NotNil(t, d.onSchemaEvolvedLocked, "conditional evolution must apply pending layout inside its transaction")

	// No layout recorded for the staging table: the hook must stay out of the way.
	require.Nil(t, d.layoutForSwap("staging.events", "analytics.events"))
	require.NoError(t, d.onTargetRecreated(context.Background(), "staging.events", "analytics.events"))

	opts := destination.PrepareOptions{
		Table: "staging.events",
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "tenant_id", DataType: schema.TypeInt64},
			{Name: "created_at", DataType: schema.TypeTimestamp},
		}},
		PartitionBy: "created_at",
		ClusterBy:   []string{"tenant_id"},
	}
	layout, err := buildDuckLakeTableLayout(opts)
	require.NoError(t, err)
	d.rememberLayout(opts.Table, layout)

	// The recorded staging layout must be replayed against the target table.
	require.Equal(t, []duckLakeLayoutStatement{
		{kind: "partition", sql: `ALTER TABLE "analytics"."events" SET PARTITIONED BY (year("created_at"), month("created_at"), day("created_at"))`},
		{kind: "sort", sql: `ALTER TABLE "analytics"."events" SET SORTED BY ("tenant_id" ASC)`, sortColumns: []string{"tenant_id"}},
	}, d.layoutForSwap("staging.events", "analytics.events"))

	// A later run without layout flags must not resurrect the recorded spec.
	cleared, err := buildDuckLakeTableLayout(destination.PrepareOptions{Table: opts.Table, Schema: opts.Schema})
	require.NoError(t, err)
	d.rememberLayout(opts.Table, cleared)
	require.Nil(t, d.layoutForSwap("staging.events", "analytics.events"))
}
