package vertica

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestMapDataTypeToVertica(t *testing.T) {
	tests := []struct {
		name  string
		col   schema.Column
		isKey bool
		want  string
	}{
		{"bool", schema.Column{DataType: schema.TypeBoolean}, false, "BOOLEAN"},
		{"int16", schema.Column{DataType: schema.TypeInt16}, false, "INT"},
		{"int32", schema.Column{DataType: schema.TypeInt32}, false, "INT"},
		{"int64", schema.Column{DataType: schema.TypeInt64}, false, "INT"},
		{"float32", schema.Column{DataType: schema.TypeFloat32}, false, "FLOAT"},
		{"float64", schema.Column{DataType: schema.TypeFloat64}, false, "FLOAT"},
		{"decimal", schema.Column{DataType: schema.TypeDecimal, Precision: 18, Scale: 4}, false, "NUMERIC(18, 4)"},
		{"decimal default", schema.Column{DataType: schema.TypeDecimal}, false, "NUMERIC(38, 9)"},
		{"string default", schema.Column{DataType: schema.TypeString}, false, "LONG VARCHAR"},
		{"string default key", schema.Column{DataType: schema.TypeString}, true, "VARCHAR(65000)"},
		{"string bounded", schema.Column{DataType: schema.TypeString, MaxLength: 100}, false, "VARCHAR(100)"},
		{"date", schema.Column{DataType: schema.TypeDate}, false, "DATE"},
		{"time", schema.Column{DataType: schema.TypeTime}, false, "TIME"},
		{"timestamp", schema.Column{DataType: schema.TypeTimestamp}, false, "TIMESTAMP"},
		{"timestamptz", schema.Column{DataType: schema.TypeTimestampTZ}, false, "TIMESTAMPTZ"},
		{"binary", schema.Column{DataType: schema.TypeBinary}, false, "LONG VARBINARY"},
		{"binary key", schema.Column{DataType: schema.TypeBinary}, true, "VARBINARY(65000)"},
		{"json", schema.Column{DataType: schema.TypeJSON}, false, "LONG VARCHAR"},
		{"array", schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeInt64}, false, "LONG VARCHAR"},
		{"uuid", schema.Column{DataType: schema.TypeUUID}, false, "LONG VARCHAR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapDataTypeToVertica(tt.col, tt.isKey); got != tt.want {
				t.Errorf("MapDataTypeToVertica() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMapVerticaTypeToSchema(t *testing.T) {
	tests := []struct {
		in   string
		want schema.DataType
	}{
		{"boolean", schema.TypeBoolean},
		{"int", schema.TypeInt64},
		{"bigint", schema.TypeInt64},
		{"float", schema.TypeFloat64},
		{"numeric(38,9)", schema.TypeDecimal},
		{"varchar(65000)", schema.TypeString},
		{"long varchar", schema.TypeString},
		{"varbinary(80)", schema.TypeBinary},
		{"date", schema.TypeDate},
		{"timestamp", schema.TypeTimestamp},
		{"timestamptz", schema.TypeTimestampTZ},
		{"uuid", schema.TypeString},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := mapVerticaTypeToSchema(tt.in); got != tt.want {
				t.Errorf("mapVerticaTypeToSchema(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestQuoteTable(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"users", `"users"`},
		{"public.users", `"public"."users"`},
		{`we"ird`, `"we""ird"`},
	}
	for _, tt := range tests {
		if got := quoteTable(tt.in); got != tt.want {
			t.Errorf("quoteTable(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBuildCreateTableSQL(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString},
			{Name: "amount", DataType: schema.TypeDecimal, Precision: 10, Scale: 2},
		},
	}
	got := buildCreateTableSQL("public.users", tableSchema, []string{"id"})

	for _, want := range []string{
		`CREATE TABLE IF NOT EXISTS "public"."users"`,
		`"id" INT NOT NULL`,
		`"name" LONG VARCHAR`,
		`"amount" NUMERIC(10, 2)`,
		`PRIMARY KEY ("id")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("buildCreateTableSQL() missing %q in:\n%s", want, got)
		}
	}
}

func TestAppendCopyValue(t *testing.T) {
	pool := memory.NewGoAllocator()

	strB := array.NewStringBuilder(pool)
	defer strB.Release()
	strB.Append("plain")
	strB.Append("")
	strB.AppendNull()
	// control bytes that must be escaped so they survive the COPY stream
	strB.Append(string([]byte{copyDelimiter, 'a', copyRecordTerm, 'b', copyNull, 'c', copyEscape}))
	strArr := strB.NewArray()
	defer strArr.Release()

	got := func(idx int) []byte {
		var buf bytes.Buffer
		appendCopyValue(&buf, strArr, idx)
		return buf.Bytes()
	}

	if !bytes.Equal(got(0), []byte("plain")) {
		t.Errorf("plain string mis-encoded: %q", got(0))
	}
	if len(got(1)) != 0 {
		t.Errorf("empty string should encode to empty bytes, got %q", got(1))
	}
	if !bytes.Equal(got(2), []byte{copyNull}) {
		t.Errorf("null should encode to NULL marker, got %q", got(2))
	}
	wantEscaped := []byte{
		copyEscape, copyDelimiter, 'a', copyEscape, copyRecordTerm, 'b',
		copyEscape, copyNull, 'c', copyEscape, copyEscape,
	}
	if !bytes.Equal(got(3), wantEscaped) {
		t.Errorf("control bytes not escaped: got %v want %v", got(3), wantEscaped)
	}

	intB := array.NewInt64Builder(pool)
	defer intB.Release()
	intB.Append(-42)
	intB.AppendNull()
	intArr := intB.NewArray()
	defer intArr.Release()
	var buf bytes.Buffer
	appendCopyValue(&buf, intArr, 0)
	if buf.String() != "-42" {
		t.Errorf("int encode = %q, want -42", buf.String())
	}
	buf.Reset()
	appendCopyValue(&buf, intArr, 1)
	if !bytes.Equal(buf.Bytes(), []byte{copyNull}) {
		t.Errorf("null int should encode to NULL marker, got %q", buf.Bytes())
	}

	tsB := array.NewTimestampBuilder(pool, &arrow.TimestampType{Unit: arrow.Microsecond})
	defer tsB.Release()
	tsB.Append(arrow.Timestamp(1673790330123456)) // 2023-01-15 13:45:30.123456 UTC
	tsArr := tsB.NewArray()
	defer tsArr.Release()
	buf.Reset()
	appendCopyValue(&buf, tsArr, 0)
	if got := buf.String(); got != "2023-01-15 13:45:30.123456+00:00" {
		t.Errorf("timestamp encode = %q", got)
	}
}

func TestFormatISOInterval(t *testing.T) {
	tests := []struct {
		months, days, nanos int64
		want                string
	}{
		{0, 3, 0, "P3D"},
		{1, 0, 0, "P1M"},
		{0, 0, 5_400_000_000_000, "PT5400S"},
		{0, 0, 0, "PT0S"},
		{1, 2, 3_000_000_000, "P1M2DT3S"},
	}
	for _, tt := range tests {
		if got := formatISOInterval(tt.months, tt.days, tt.nanos); got != tt.want {
			t.Errorf("formatISOInterval(%d,%d,%d) = %q, want %q", tt.months, tt.days, tt.nanos, got, tt.want)
		}
	}
}

func TestDedupSource(t *testing.T) {
	got := dedupSource([]string{"id", "name"}, []string{"id"}, `"public"."stg"`, `"id" DESC`)
	for _, want := range []string{
		`ROW_NUMBER() OVER (PARTITION BY "id" ORDER BY "id" DESC)`,
		`FROM "public"."stg"`,
		`WHERE "__bruin_dedup_rn" = 1`,
		`) source`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("dedupSource() missing %q in:\n%s", want, got)
		}
	}
}

func TestDedupSourceColumnCollision(t *testing.T) {
	// A user column named __bruin_dedup_rn must not collide with the row-number alias.
	got := dedupSource([]string{"id", "__bruin_dedup_rn"}, []string{"id"}, `"public"."stg"`, `"id"`)
	if !strings.Contains(got, `AS "__bruin_dedup_rn_2"`) {
		t.Errorf("expected collision-avoiding alias __bruin_dedup_rn_2 in:\n%s", got)
	}
	if !strings.Contains(got, `WHERE "__bruin_dedup_rn_2" = 1`) {
		t.Errorf("expected WHERE on collision-avoiding alias in:\n%s", got)
	}
}

func TestRenameSwapBackupNameFromStaging(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	d := &VerticaDestination{db: db, currentSchema: "public"}

	existsQuery := `SELECT COUNT\(\*\) FROM v_catalog\.tables WHERE table_schema = \? AND table_name = \?`
	mock.ExpectQuery(existsQuery).WithArgs("public", "users").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	// The backup name is derived from the unique staging name, dropped before the
	// swap, then the atomic rename displaces the target onto it.
	mock.ExpectExec(`DROP TABLE IF EXISTS "public"."users_stg_run1_old" CASCADE`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`ALTER TABLE "public"."users", "public"."users_stg_run1" RENAME TO "users_stg_run1_old", "users"`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`DROP TABLE IF EXISTS "public"."users_stg_run1_old" CASCADE`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := d.renameSwap(context.Background(), "public.users_stg_run1", "public.users"); err != nil {
		t.Fatalf("renameSwap() error: %v", err)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUniqueInternalName(t *testing.T) {
	if got := uniqueInternalName([]string{"id", "name"}, "rn"); got != "rn" {
		t.Errorf("uniqueInternalName no collision = %q, want rn", got)
	}
	if got := uniqueInternalName([]string{"id", "RN"}, "rn"); got != "rn_2" {
		t.Errorf("uniqueInternalName case-insensitive collision = %q, want rn_2", got)
	}
	if got := uniqueInternalName([]string{"rn", "rn_2"}, "rn"); got != "rn_3" {
		t.Errorf("uniqueInternalName double collision = %q, want rn_3", got)
	}
}
