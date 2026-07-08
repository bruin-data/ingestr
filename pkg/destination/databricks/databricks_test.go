package databricks

import (
	"context"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestMapDataTypeToDatabricks_SizedString(t *testing.T) {
	tests := []struct {
		name     string
		col      schema.Column
		expected string
	}{
		{"sized", schema.Column{DataType: schema.TypeString, MaxLength: 50}, "VARCHAR(50)"},
		{"unsized", schema.Column{DataType: schema.TypeString}, "STRING"},
		{"over cap clamps to max", schema.Column{DataType: schema.TypeString, MaxLength: 70000}, "VARCHAR(65535)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapDataTypeToDatabricks(tt.col); got != tt.expected {
				t.Fatalf("MapDataTypeToDatabricks() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestBuildDeleteInsertSQLUsesAtomicBlock(t *testing.T) {
	t.Parallel()

	dest := &DatabricksDestination{catalog: "main"}
	if !dest.SupportsDeleteInsertStrategy() {
		t.Fatal("SupportsDeleteInsertStrategy() = false, want true")
	}

	deleteSQL, insertSQL, atomicSQL := dest.buildDeleteInsertSQL(destination.DeleteInsertOptions{
		StagingTable:   "scratch.orders_di",
		TargetTable:    "analytics.orders",
		IncrementalKey: "updated_at",
		IntervalStart:  "2026-01-01",
		IntervalEnd:    "2026-01-31",
		Columns:        []string{"id", "name", "updated_at"},
		PrimaryKeys:    []string{"id"},
	})

	if want := "DELETE FROM `main`.`analytics`.`orders` WHERE `updated_at` >= '2026-01-01' AND `updated_at` <= '2026-01-31'"; deleteSQL != want {
		t.Fatalf("deleteSQL = %q, want %q", deleteSQL, want)
	}
	for _, want := range []string{
		"INSERT INTO `main`.`analytics`.`orders` (`id`, `name`, `updated_at`)",
		"ROW_NUMBER() OVER (PARTITION BY `id` ORDER BY `updated_at` DESC)",
		"FROM `main`.`ingestr_staging`.`orders_di`",
	} {
		if !strings.Contains(insertSQL, want) {
			t.Fatalf("insertSQL missing %q:\n%s", want, insertSQL)
		}
	}

	wantAtomic := "BEGIN ATOMIC\n  " + deleteSQL + ";\n  " + insertSQL + ";\nEND;"
	if atomicSQL != wantAtomic {
		t.Fatalf("atomicSQL = %q, want %q", atomicSQL, wantAtomic)
	}
}

func TestBeginTransactionUnsupported(t *testing.T) {
	t.Parallel()

	dest := NewDatabricksDestination()
	tx, err := dest.BeginTransaction(context.Background())
	if err == nil {
		t.Fatal("BeginTransaction() error = nil, want unsupported error")
	}
	if tx != nil {
		t.Fatalf("BeginTransaction() tx = %#v, want nil", tx)
	}
	if !strings.Contains(err.Error(), "does not support transactions") {
		t.Fatalf("BeginTransaction() error = %v, want transaction unsupported error", err)
	}
}
