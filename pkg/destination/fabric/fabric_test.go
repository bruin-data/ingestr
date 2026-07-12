package fabric

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	mssqldb "github.com/microsoft/go-mssqldb"
)

func TestBuildCreateTableSQLPreservesRequiredness(t *testing.T) {
	got := buildCreateTableSQL("dbo.events", []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString, Nullable: true},
	}, []string{"id"})

	want := "IF OBJECT_ID('dbo.events', 'U') IS NULL CREATE TABLE [dbo].[events] (\n" +
		"  [id] BIGINT NOT NULL,\n" +
		"  [name] VARCHAR(MAX),\n" +
		"  PRIMARY KEY NONCLUSTERED ([id]) NOT ENFORCED\n)"
	if got != want {
		t.Fatalf("buildCreateTableSQL() =\n%s\nwant:\n%s", got, want)
	}
}

func TestURIToConnString(t *testing.T) {
	tests := []struct {
		name          string
		uri           string
		wantErr       bool
		wantDatabase  string
		wantHost      string
		wantUser      string // expected decoded user-id ("" means no userinfo)
		wantPassword  string
		wantQuery     map[string]string
		wantWriteMode string
	}{
		{
			name:          "service principal with tenant defaults to copy",
			uri:           "fabric://client-id:s3cr3t@abc.datawarehouse.fabric.microsoft.com/MyWarehouse?tenant_id=tenant-123",
			wantDatabase:  "MyWarehouse",
			wantHost:      "abc.datawarehouse.fabric.microsoft.com:1433",
			wantUser:      "client-id@tenant-123",
			wantPassword:  "s3cr3t",
			wantWriteMode: writeModeCopy,
			wantQuery: map[string]string{
				"fedauth":  "ActiveDirectoryServicePrincipal",
				"database": "MyWarehouse",
				"encrypt":  "true",
			},
		},
		{
			name:          "no credentials defaults to ActiveDirectoryDefault and copy",
			uri:           "fabric://abc.datawarehouse.fabric.microsoft.com/wh",
			wantDatabase:  "wh",
			wantHost:      "abc.datawarehouse.fabric.microsoft.com:1433",
			wantUser:      "",
			wantWriteMode: writeModeCopy,
			wantQuery: map[string]string{
				"fedauth":  "ActiveDirectoryDefault",
				"database": "wh",
				"encrypt":  "true",
			},
		},
		{
			name:          "explicit fedauth and port are preserved",
			uri:           "fabric://client-id:token@host.example.com:1234/wh?fedauth=ActiveDirectoryServicePrincipalAccessToken&tenant_id=t1",
			wantDatabase:  "wh",
			wantHost:      "host.example.com:1234",
			wantUser:      "client-id@t1",
			wantPassword:  "token",
			wantWriteMode: writeModeCopy,
			wantQuery: map[string]string{
				"fedauth":  "ActiveDirectoryServicePrincipalAccessToken",
				"database": "wh",
				"encrypt":  "true",
			},
		},
		{
			name:          "write_strategy copy is accepted",
			uri:           "fabric://client-id:s3cr3t@abc.datawarehouse.fabric.microsoft.com/wh?tenant_id=t1&write_strategy=copy",
			wantDatabase:  "wh",
			wantHost:      "abc.datawarehouse.fabric.microsoft.com:1433",
			wantUser:      "client-id@t1",
			wantPassword:  "s3cr3t",
			wantWriteMode: writeModeCopy,
			wantQuery: map[string]string{
				"fedauth":  "ActiveDirectoryServicePrincipal",
				"database": "wh",
				"encrypt":  "true",
			},
		},
		{
			name:          "write_strategy insert is accepted",
			uri:           "fabric://client-id:s3cr3t@abc.datawarehouse.fabric.microsoft.com/wh?tenant_id=t1&write_strategy=INSERT",
			wantDatabase:  "wh",
			wantHost:      "abc.datawarehouse.fabric.microsoft.com:1433",
			wantUser:      "client-id@t1",
			wantPassword:  "s3cr3t",
			wantWriteMode: writeModeInsert,
			wantQuery: map[string]string{
				"fedauth":  "ActiveDirectoryServicePrincipal",
				"database": "wh",
				"encrypt":  "true",
			},
		},
		{
			name:    "invalid write_strategy errors",
			uri:     "fabric://client-id:s3cr3t@abc.datawarehouse.fabric.microsoft.com/wh?tenant_id=t1&write_strategy=bulk",
			wantErr: true,
		},
		{
			name:    "wrong scheme errors",
			uri:     "mssql://user:pass@host/db",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connStr, database, writeMode, err := uriToConnString(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (connStr=%q)", connStr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if database != tt.wantDatabase {
				t.Errorf("database = %q, want %q", database, tt.wantDatabase)
			}

			if writeMode != tt.wantWriteMode {
				t.Errorf("writeMode = %q, want %q", writeMode, tt.wantWriteMode)
			}

			u, perr := url.Parse(connStr)
			if perr != nil {
				t.Fatalf("result DSN is not a valid URL: %v", perr)
			}
			if u.Scheme != "sqlserver" {
				t.Errorf("scheme = %q, want sqlserver", u.Scheme)
			}
			if u.Host != tt.wantHost {
				t.Errorf("host = %q, want %q", u.Host, tt.wantHost)
			}

			gotUser := ""
			if u.User != nil {
				gotUser = u.User.Username()
			}
			if gotUser != tt.wantUser {
				t.Errorf("user = %q, want %q", gotUser, tt.wantUser)
			}
			if tt.wantPassword != "" {
				gotPass, _ := u.User.Password()
				if gotPass != tt.wantPassword {
					t.Errorf("password = %q, want %q", gotPass, tt.wantPassword)
				}
			}

			q := u.Query()
			for k, want := range tt.wantQuery {
				if got := q.Get(k); got != want {
					t.Errorf("query[%q] = %q, want %q", k, got, want)
				}
			}
			// tenant_id must never leak into the DSN query.
			if q.Has("tenant_id") {
				t.Errorf("tenant_id should not appear in DSN query, got %q", q.Get("tenant_id"))
			}
			// write_strategy is ingestr-specific and must not leak into the DSN.
			if q.Has("write_strategy") {
				t.Errorf("write_strategy should not appear in DSN query, got %q", q.Get("write_strategy"))
			}
		})
	}
}

func TestBuildDeleteInsertDeleteSQLUsesTableLock(t *testing.T) {
	sql := buildDeleteInsertDeleteSQL("dbo.events", "updated_at")

	if !strings.Contains(sql, "DELETE FROM [dbo].[events] WITH (TABLOCKX, HOLDLOCK)") {
		t.Fatalf("delete SQL missing table lock: %s", sql)
	}
	if !strings.Contains(sql, "[updated_at] >= @p1") || !strings.Contains(sql, "[updated_at] <= @p2") {
		t.Fatalf("delete SQL missing interval predicate: %s", sql)
	}
}

func mustExtractCopyValue(t *testing.T, arr arrow.Array, col *schema.Column) interface{} {
	t.Helper()
	value, err := extractCopyValue(arr, 0, col)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestExtractCopyValueReturnsDriverNativeTypes(t *testing.T) {
	pool := memory.DefaultAllocator

	boolBuilder := array.NewBooleanBuilder(pool)
	boolBuilder.Append(true)
	boolArray := boolBuilder.NewArray()
	defer boolArray.Release()
	boolBuilder.Release()

	if got := mustExtractCopyValue(t, boolArray, nil); got != true {
		t.Fatalf("expected bool true, got %T %v", got, got)
	}

	tsType := &arrow.TimestampType{Unit: arrow.Microsecond}
	tsBuilder := array.NewTimestampBuilder(pool, tsType)
	want := time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC)
	tsBuilder.Append(arrow.Timestamp(want.UnixMicro()))
	tsArray := tsBuilder.NewArray()
	defer tsArray.Release()
	tsBuilder.Release()

	got := mustExtractCopyValue(t, tsArray, nil)
	gotTime, ok := got.(time.Time)
	if !ok {
		t.Fatalf("expected time.Time, got %T %v", got, got)
	}
	if !gotTime.Equal(want) {
		t.Fatalf("expected timestamp %v, got %v", want, gotTime)
	}

	dateBuilder := array.NewDate32Builder(pool)
	dateBuilder.Append(arrow.Date32FromTime(time.Date(2024, 5, 6, 0, 0, 0, 0, time.UTC)))
	dateArray := dateBuilder.NewArray()
	defer dateArray.Release()
	dateBuilder.Release()

	if _, ok := mustExtractCopyValue(t, dateArray, nil).(time.Time); !ok {
		t.Fatalf("expected date to convert to time.Time")
	}

	time64Type := &arrow.Time64Type{Unit: arrow.Microsecond}
	time64Builder := array.NewTime64Builder(pool, time64Type)
	time64Builder.Append(arrow.Time64(3723456789))
	time64Array := time64Builder.NewArray()
	defer time64Array.Release()
	time64Builder.Release()

	wantTime := time.Date(1, 1, 1, 1, 2, 3, 456789000, time.UTC)
	if got := mustExtractCopyValue(t, time64Array, nil); !got.(time.Time).Equal(wantTime) {
		t.Fatalf("expected time64 %v, got %T %v", wantTime, got, got)
	}
}

func TestExtractCopyValueRejectsOutOfRangeTime(t *testing.T) {
	// 25 hours in microseconds — a valid int64 but not a valid time-of-day. A
	// second case uses the max int64 microsecond value, which would overflow and
	// wrap into a small in-range duration if the range check ran after the
	// multiplication by 1000.
	for _, value := range []arrow.Time64{25 * 60 * 60 * 1_000_000, 9223372036854775807} {
		time64Type := &arrow.Time64Type{Unit: arrow.Microsecond}
		builder := array.NewTime64Builder(memory.DefaultAllocator, time64Type)
		builder.Append(value)
		arr := builder.NewArray()

		if _, err := extractCopyValue(arr, 0, nil); err == nil {
			t.Fatalf("expected out-of-range time error for value %d", value)
		}

		arr.Release()
		builder.Release()
	}
}

func TestExtractCopyValueConvertsUUIDStrings(t *testing.T) {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	builder.Append("01234567-89ab-cdef-0123-456789abcdef")
	arr := builder.NewArray()
	defer arr.Release()
	builder.Release()

	got := mustExtractCopyValue(t, arr, &schema.Column{DataType: schema.TypeUUID})
	uuid, ok := got.(mssqldb.UniqueIdentifier)
	if !ok {
		t.Fatalf("expected UniqueIdentifier, got %T %v", got, got)
	}
	if uuid.String() != "01234567-89AB-CDEF-0123-456789ABCDEF" {
		t.Fatalf("unexpected UUID value: %s", uuid.String())
	}
}

func TestExtractCopyValueLeavesNonUUIDStringsUntouched(t *testing.T) {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	builder.Append("01234567-89ab-cdef-0123-456789abcdef")
	arr := builder.NewArray()
	defer arr.Release()
	builder.Release()

	// Without a UUID-typed column, the value must stay a plain string.
	if got := mustExtractCopyValue(t, arr, &schema.Column{DataType: schema.TypeString}); got != "01234567-89ab-cdef-0123-456789abcdef" {
		t.Fatalf("expected untouched string, got %T %v", got, got)
	}
}

func TestExtractCopyValueRejectsInvalidUUIDStrings(t *testing.T) {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	builder.Append("not-a-uuid")
	arr := builder.NewArray()
	defer arr.Release()
	builder.Release()

	if _, err := extractCopyValue(arr, 0, &schema.Column{DataType: schema.TypeUUID}); err == nil {
		t.Fatal("expected invalid UUID error")
	}
}

func TestExtractCopyValueReturnsNilForNulls(t *testing.T) {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	builder.AppendNull()
	arr := builder.NewArray()
	defer arr.Release()
	builder.Release()

	if got := mustExtractCopyValue(t, arr, nil); got != nil {
		t.Fatalf("expected nil, got %T %v", got, got)
	}
}

func TestColumnsForRecordMapsByCaseInsensitiveName(t *testing.T) {
	pool := memory.DefaultAllocator
	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "unmatched", Type: arrow.BinaryTypes.String},
	}, nil)

	idBuilder := array.NewInt64Builder(pool)
	idBuilder.Append(1)
	idArray := idBuilder.NewArray()
	defer idArray.Release()
	idBuilder.Release()

	strBuilder := array.NewStringBuilder(pool)
	strBuilder.Append("x")
	strArray := strBuilder.NewArray()
	defer strArray.Release()
	strBuilder.Release()

	record := array.NewRecordBatch(arrowSchema, []arrow.Array{idArray, strArray}, 1)
	defer record.Release()

	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	cols := columnsForRecord(record, tableSchema)

	if len(cols) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(cols))
	}
	if cols[0] == nil || cols[0].DataType != schema.TypeInt64 {
		t.Fatalf("expected first column mapped to id (int64), got %+v", cols[0])
	}
	if cols[1] != nil {
		t.Fatalf("expected unmatched column to map to nil, got %+v", cols[1])
	}
}
