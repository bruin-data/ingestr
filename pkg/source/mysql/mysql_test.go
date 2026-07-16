package mysql

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestIsVitessServer(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"5.7.9-vitess-14.0.0", true},
		{"8.0.11-Vitess", true},
		{"8.0.34", false},
		{"5.7.40-log", false},
		{"10.6.12-MariaDB", false},
	}

	for _, tc := range cases {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("failed to create sqlmock: %v", err)
		}
		mock.ExpectQuery("SELECT @@version").
			WillReturnRows(sqlmock.NewRows([]string{"@@version"}).AddRow(tc.version))

		got, err := isVitessServer(context.Background(), db)
		if err != nil {
			t.Fatalf("version %q: unexpected error: %v", tc.version, err)
		}
		if got != tc.want {
			t.Errorf("version %q: got %v, want %v", tc.version, got, tc.want)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("version %q: unmet expectations: %v", tc.version, err)
		}
		_ = db.Close()
	}
}

func TestMapMySQLToDataTypeUnsignedWidening(t *testing.T) {
	cases := []struct {
		columnType    string
		want          schema.DataType
		wantPrecision int
	}{
		{"tinyint", schema.TypeInt16, 0},
		{"tinyint unsigned", schema.TypeInt16, 0},
		{"smallint", schema.TypeInt16, 0},
		{"smallint unsigned", schema.TypeInt32, 0},
		{"smallint(5) unsigned", schema.TypeInt32, 0},
		{"mediumint unsigned", schema.TypeInt32, 0},
		{"int", schema.TypeInt32, 0},
		{"int unsigned", schema.TypeInt64, 0},
		{"int(10) unsigned", schema.TypeInt64, 0},
		{"int unsigned zerofill", schema.TypeInt64, 0},
		{"UNSIGNED INT", schema.TypeInt64, 0},
		{"bigint", schema.TypeInt64, 0},
		{"bigint unsigned", schema.TypeDecimal, 20},
		{"bigint(20) unsigned", schema.TypeDecimal, 20},
		{"UNSIGNED BIGINT", schema.TypeDecimal, 20},
	}

	for _, tc := range cases {
		dt, precision, scale, _ := MapMySQLToDataType(tc.columnType)
		if dt != tc.want {
			t.Errorf("MapMySQLToDataType(%q) = %v, want %v", tc.columnType, dt, tc.want)
		}
		if precision != tc.wantPrecision {
			t.Errorf("MapMySQLToDataType(%q) precision = %d, want %d", tc.columnType, precision, tc.wantPrecision)
		}
		if scale != 0 {
			t.Errorf("MapMySQLToDataType(%q) scale = %d, want 0", tc.columnType, scale)
		}
	}
}

func TestBuildSelectQueryPreservesColumnCasing(t *testing.T) {
	columns := []schema.Column{
		{Name: "RowPointer"},
		{Name: "NoteExistsFlag"},
		{Name: "CreatedBy"},
	}

	query := buildSelectQuery("testdb.notes", columns, source.ReadOptions{})

	for _, name := range []string{"`RowPointer`", "`NoteExistsFlag`", "`CreatedBy`"} {
		if !strings.Contains(query, name) {
			t.Errorf("query %q missing original column %q", query, name)
		}
	}
	for _, name := range []string{"row_pointer", "note_exists_flag", "created_by"} {
		if strings.Contains(query, name) {
			t.Errorf("query %q must not contain renamed column %q", query, name)
		}
	}
}

func TestBuildSelectQueryAddsExtractPartitionPredicate(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	windowStart := time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	query := buildSelectQuery("shop.orders", []schema.Column{
		{Name: "id"},
		{Name: "created_at"},
	}, source.ReadOptions{
		IncrementalKey:        "updated_at",
		IntervalStart:         &intervalStart,
		IntervalEnd:           &intervalEnd,
		ExtractPartitionBy:    "created_at",
		ExtractPartitionStart: &windowStart,
		ExtractPartitionEnd:   &windowEnd,
	})

	want := "SELECT `id`, `created_at` FROM `shop`.`orders` WHERE `updated_at` >= '2026-01-01 00:00:00' AND `updated_at` <= '2026-01-31 00:00:00' AND `created_at` >= '2026-01-08 00:00:00' AND `created_at` < '2026-01-15 00:00:00'"
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}
