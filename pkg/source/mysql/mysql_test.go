package mysql

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestBufferedReadConnPreservesBytes(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})

	want := bytes.Repeat([]byte("mysql-row-packet"), 10_000)
	go func() {
		for offset := 0; offset < len(want); {
			end := min(offset+997, len(want))
			_, _ = server.Write(want[offset:end])
			offset = end
		}
		_ = server.Close()
	}()

	conn := &bufferedReadConn{
		Conn:   client,
		reader: bufio.NewReaderSize(client, mysqlReadBufferSize),
	}
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("buffered connection returned %d bytes, want %d", len(got), len(want))
	}
}

func TestReplaceGCRestoreReleasesPreviousLease(t *testing.T) {
	source := NewMySQLSource()
	firstCalls := 0
	secondCalls := 0

	source.replaceGCRestore(func() { firstCalls++ })
	source.replaceGCRestore(func() { secondCalls++ })
	if firstCalls != 1 || secondCalls != 0 {
		t.Fatalf("replacing restore called first %d times and second %d times", firstCalls, secondCalls)
	}

	source.replaceGCRestore(nil)
	source.replaceGCRestore(nil)
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("clearing restore called first %d times and second %d times", firstCalls, secondCalls)
	}
}

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

func TestQueryRowsUsesPreparedStatement(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare("SELECT 1").
		WillBeClosed().
		ExpectQuery().
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(1))

	rows, closeStmt, err := queryRows(t.Context(), db, "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	var got int
	if !rows.Next() {
		t.Fatal("prepared query returned no rows")
	}
	if err := rows.Scan(&got); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	closeStmt()
	if got != 1 {
		t.Fatalf("prepared query value = %d, want 1", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestQueryRowsFallsBackWhenPrepareIsUnsupported(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectPrepare("SELECT 1").WillReturnError(errors.New("prepare unsupported"))
	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(1))

	rows, closeStmt, err := queryRows(t.Context(), db, "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	defer closeStmt()
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatal("fallback query returned no rows")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMariaDBJSONValidColumn(t *testing.T) {
	cases := []struct {
		clause string
		column string
		ok     bool
	}{
		{"json_valid(`json_val`)", "json_val", true},
		{"JSON_VALID(`payload`)", "payload", true},
		{" json_valid(`j`) ", "j", true},
		{"json_valid(`a`) and length(`a`) > 0", "", false},
		{"length(`json_val`) > 0", "", false},
		{"json_valid(json_val)", "", false},
	}
	for _, tc := range cases {
		column, ok := mariadbJSONValidColumn(tc.clause)
		if ok != tc.ok || column != tc.column {
			t.Fatalf("mariadbJSONValidColumn(%q) = %q, %v; want %q, %v", tc.clause, column, ok, tc.column, tc.ok)
		}
	}
}
