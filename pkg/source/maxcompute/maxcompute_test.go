package maxcompute

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
)

const errorRowsDriverName = "maxcompute_error_rows_test"

var errRowsIteration = errors.New("rows iteration failed")

func init() {
	sql.Register(errorRowsDriverName, errorRowsDriver{})
}

func TestIntervalLiteralPreservesTimezoneForTimestampTZ(t *testing.T) {
	t.Parallel()

	cols := []schema.Column{{Name: "created_at", DataType: schema.TypeTimestampTZ}}
	value := time.Date(2024, 1, 1, 12, 0, 0, 123456789, time.FixedZone("IST", 5*60*60+30*60))

	got := intervalLiteral(cols, "created_at", value)
	want := "TIMESTAMP '2024-01-01 06:30:00.123456 +00:00'"
	if got != want {
		t.Fatalf("intervalLiteral(timestamp_tz) = %q, want %q", got, want)
	}
}

func TestIntervalLiteralKeepsBareDatetimeForTimestampNTZ(t *testing.T) {
	t.Parallel()

	cols := []schema.Column{{Name: "created_at", DataType: schema.TypeTimestamp}}
	value := time.Date(2024, 1, 1, 12, 0, 0, 123456789, time.UTC)

	got := intervalLiteral(cols, "created_at", value)
	want := "'2024-01-01 12:00:00'"
	if got != want {
		t.Fatalf("intervalLiteral(timestamp_ntz) = %q, want %q", got, want)
	}
}

func TestRowsToArrowRecordBatchReturnsIteratorErrorBeforeFirstRow(t *testing.T) {
	t.Parallel()

	db, err := sql.Open(errorRowsDriverName, "")
	if err != nil {
		t.Fatalf("open test driver: %v", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(context.Background(), "SELECT id")
	if err != nil {
		t.Fatalf("query test driver: %v", err)
	}
	defer func() { _ = rows.Close() }()

	columns := []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: true}}
	record, count, err := rowsToArrowRecordBatch(rows, buildArrowSchema(columns), columns, 100)
	if !errors.Is(err, errRowsIteration) {
		t.Fatalf("rowsToArrowRecordBatch error = %v, want %v", err, errRowsIteration)
	}
	if record != nil {
		t.Fatal("expected no record batch")
	}
	if count != 0 {
		t.Fatalf("row count = %d, want 0", count)
	}
}

type errorRowsDriver struct{}

func (errorRowsDriver) Open(string) (driver.Conn, error) {
	return errorRowsConn{}, nil
}

type errorRowsConn struct{}

func (errorRowsConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not implemented")
}

func (errorRowsConn) Close() error {
	return nil
}

func (errorRowsConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented")
}

func (errorRowsConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return errorRows{}, nil
}

type errorRows struct{}

func (errorRows) Columns() []string {
	return []string{"id"}
}

func (errorRows) Close() error {
	return nil
}

func (errorRows) Next([]driver.Value) error {
	return errRowsIteration
}
