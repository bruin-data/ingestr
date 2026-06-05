package mssql

import (
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestBuildCreateTableSQL_StringPrimaryKeyUsesIndexableType(t *testing.T) {
	sql := buildCreateTableSQL("dbo.events", []schema.Column{
		{Name: "_id", DataType: schema.TypeString},
		{Name: "payload", DataType: schema.TypeJSON},
	}, []string{"_id"})

	assertContains(t, sql, "[_id] NVARCHAR(450)")
	assertContains(t, sql, "[payload] NVARCHAR(MAX)")
	assertContains(t, sql, "PRIMARY KEY ([_id])")
}

func TestBuildCreateTableSQL_CapsLongStringPrimaryKeyLength(t *testing.T) {
	sql := buildCreateTableSQL("dbo.events", []schema.Column{
		{Name: "id", DataType: schema.TypeString, MaxLength: 1000},
		{Name: "name", DataType: schema.TypeString, MaxLength: 1000},
	}, []string{"id"})

	assertContains(t, sql, "[id] NVARCHAR(450)")
	assertContains(t, sql, "[name] NVARCHAR(1000)")
}

func TestExtractValueReturnsNativeTypesForBulkCopy(t *testing.T) {
	boolBuilder := array.NewBooleanBuilder(memory.DefaultAllocator)
	boolBuilder.Append(true)
	boolArray := boolBuilder.NewArray()
	defer boolArray.Release()
	boolBuilder.Release()

	if got := extractValue(boolArray, 0); got != true {
		t.Fatalf("expected bool true, got %T %v", got, got)
	}

	int16Builder := array.NewInt16Builder(memory.DefaultAllocator)
	int16Builder.Append(42)
	int16Array := int16Builder.NewArray()
	defer int16Array.Release()
	int16Builder.Release()

	if got := extractValue(int16Array, 0); got != int32(42) {
		t.Fatalf("expected int32 42, got %T %v", got, got)
	}

	tsType := &arrow.TimestampType{Unit: arrow.Microsecond}
	tsBuilder := array.NewTimestampBuilder(memory.DefaultAllocator, tsType)
	want := time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC)
	tsBuilder.Append(arrow.Timestamp(want.UnixMicro()))
	tsArray := tsBuilder.NewArray()
	defer tsArray.Release()
	tsBuilder.Release()

	if got := extractValue(tsArray, 0); !got.(time.Time).Equal(want) {
		t.Fatalf("expected timestamp %v, got %T %v", want, got, got)
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("SQL does not contain %q:\n%s", want, got)
	}
}
