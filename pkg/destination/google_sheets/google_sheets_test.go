package google_sheets

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestParseURIAllowsMissingCredentials(t *testing.T) {
	creds, err := parseURI("gsheets://")
	if err != nil {
		t.Fatalf("parseURI returned error: %v", err)
	}
	if creds != nil {
		t.Fatalf("creds = %v, want nil when no credentials provided", creds)
	}
}

func TestParseURIDecodesBase64Credentials(t *testing.T) {
	payload := []byte(`{"type":"service_account"}`)
	uri := "gsheets://?credentials_base64=" + base64.StdEncoding.EncodeToString(payload)
	creds, err := parseURI(uri)
	if err != nil {
		t.Fatalf("parseURI returned error: %v", err)
	}
	if string(creds) != string(payload) {
		t.Fatalf("creds = %q, want %q", creds, payload)
	}
}

func TestParseURIRejectsBadScheme(t *testing.T) {
	if _, err := parseURI("postgres://"); err == nil {
		t.Fatal("expected error for invalid scheme, got nil")
	}
}

func TestParseTableName(t *testing.T) {
	id, sheet, err := parseTableName("abc123.Sheet1")
	if err != nil {
		t.Fatalf("parseTableName returned error: %v", err)
	}
	if id != "abc123" || sheet != "Sheet1" {
		t.Fatalf("got (%q, %q), want (abc123, Sheet1)", id, sheet)
	}

	for _, bad := range []string{"", "onlyid", ".sheet", "id."} {
		if _, _, err := parseTableName(bad); err == nil {
			t.Fatalf("expected error for %q, got nil", bad)
		}
	}
}

func TestValueToCell(t *testing.T) {
	pool := memory.NewGoAllocator()

	// Strings pass through; nulls become blank.
	strB := array.NewStringBuilder(pool)
	strB.Append("hello")
	strB.AppendNull()
	strArr := strB.NewArray()
	defer strArr.Release()
	if got := valueToCell(strArr, 0); got != "hello" {
		t.Fatalf("string value = %v, want hello", got)
	}
	if got := valueToCell(strArr, 1); got != "" {
		t.Fatalf("null value = %v, want empty string", got)
	}

	// Numbers and booleans stay native (not text).
	intB := array.NewInt64Builder(pool)
	intB.Append(42)
	intArr := intB.NewArray()
	defer intArr.Release()
	if got := valueToCell(intArr, 0); got != int64(42) {
		t.Fatalf("int value = %v (%T), want int64(42)", got, got)
	}

	floatB := array.NewFloat64Builder(pool)
	floatB.Append(3.14159)
	floatArr := floatB.NewArray()
	defer floatArr.Release()
	if got := valueToCell(floatArr, 0); got != 3.14159 {
		t.Fatalf("float value = %v (%T), want 3.14159", got, got)
	}

	boolB := array.NewBooleanBuilder(pool)
	boolB.Append(true)
	boolArr := boolB.NewArray()
	defer boolArr.Release()
	if got := valueToCell(boolArr, 0); got != true {
		t.Fatalf("bool value = %v (%T), want true", got, got)
	}

	// Dates, timestamps, and times are written as text.
	tsB := array.NewTimestampBuilder(pool, &arrow.TimestampType{Unit: arrow.Microsecond})
	tsB.Append(arrow.Timestamp(0))
	tsArr := tsB.NewArray()
	defer tsArr.Release()
	if got := valueToCell(tsArr, 0); got != "1970-01-01 00:00:00" {
		t.Fatalf("timestamp value = %v, want 1970-01-01 00:00:00", got)
	}

	dateB := array.NewDate32Builder(pool)
	dateB.Append(arrow.Date32FromTime(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)))
	dateArr := dateB.NewArray()
	defer dateArr.Release()
	if got := valueToCell(dateArr, 0); got != "2024-01-02" {
		t.Fatalf("date value = %v, want 2024-01-02", got)
	}

	timeB := array.NewTime64Builder(pool, &arrow.Time64Type{Unit: arrow.Microsecond})
	timeB.Append(arrow.Time64(6 * 60 * 60 * 1_000_000))      // 06:00:00
	timeB.Append(arrow.Time64(12*60*60*1_000_000 + 500_000)) // 12:00:00.5
	timeArr := timeB.NewArray()
	defer timeArr.Release()
	if got := valueToCell(timeArr, 0); got != "06:00:00" {
		t.Fatalf("time value = %v, want 06:00:00", got)
	}
	if got := valueToCell(timeArr, 1); got != "12:00:00.5" {
		t.Fatalf("fractional time value = %v, want 12:00:00.5", got)
	}
}
