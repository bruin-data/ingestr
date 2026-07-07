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
