package source

import (
	"context"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestExtractPartitionWindows(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		end      time.Time
		interval time.Duration
		want     []ExtractPartitionWindow
	}{
		{
			name:     "exact weekly ranges",
			end:      start.Add(14 * 24 * time.Hour),
			interval: 7 * 24 * time.Hour,
			want: []ExtractPartitionWindow{
				{Start: start, End: start.Add(7 * 24 * time.Hour)},
				{Start: start.Add(7 * 24 * time.Hour), End: start.Add(14 * 24 * time.Hour)},
			},
		},
		{
			name:     "shorter final window",
			end:      start.Add(10 * 24 * time.Hour),
			interval: 7 * 24 * time.Hour,
			want: []ExtractPartitionWindow{
				{Start: start, End: start.Add(7 * 24 * time.Hour)},
				{Start: start.Add(7 * 24 * time.Hour), End: start.Add(10 * 24 * time.Hour)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractPartitionWindows(start, tt.end, tt.interval)
			if err != nil {
				t.Fatalf("ExtractPartitionWindows() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("window count = %d, want %d: %#v", len(got), len(tt.want), got)
			}
			for i := range got {
				if !got[i].Start.Equal(tt.want[i].Start) || !got[i].End.Equal(tt.want[i].End) {
					t.Fatalf("window[%d] = %#v, want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExtractPartitionWindowsRejectsInvalidInput(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := ExtractPartitionWindows(start, start.Add(time.Hour), 0); err == nil {
		t.Fatal("expected error for zero interval")
	}
	if _, err := ExtractPartitionWindows(start, start, time.Hour); err == nil {
		t.Fatal("expected error for empty range")
	}
	if _, err := ExtractPartitionWindows(start, start.Add(time.Duration(maxExtractPartitionWindows+1)*time.Minute), time.Minute); err == nil {
		t.Fatal("expected error for too many windows")
	}
}

func TestExtractNumericPartitionWindows(t *testing.T) {
	got, err := ExtractNumericPartitionWindows(1, 25, 10)
	if err != nil {
		t.Fatalf("ExtractNumericPartitionWindows() error = %v", err)
	}
	want := []ExtractNumericPartitionWindow{
		{Start: 1, End: 11},
		{Start: 11, End: 21},
		{Start: 21, End: 25},
	}
	if len(got) != len(want) {
		t.Fatalf("window count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("window[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestExtractNumericPartitionWindowsRejectsInvalidInput(t *testing.T) {
	if _, err := ExtractNumericPartitionWindows(1, 10, 0); err == nil {
		t.Fatal("expected error for zero interval")
	}
	if _, err := ExtractNumericPartitionWindows(10, 1, 5); err == nil {
		t.Fatal("expected error for inverted range")
	}
	if _, err := ExtractNumericPartitionWindows(1, maxExtractPartitionWindows+2, 1); err == nil {
		t.Fatal("expected error for too many windows")
	}
}

func TestAutoExtractPartitionInterval(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(8 * 24 * time.Hour)

	got, err := autoExtractPartitionInterval(start, end, 1)
	if err != nil {
		t.Fatalf("autoExtractPartitionInterval() error = %v", err)
	}
	if got != 48*time.Hour {
		t.Fatalf("interval = %v, want 48h", got)
	}
}

func TestAutoExtractNumericPartitionInterval(t *testing.T) {
	got, err := autoExtractNumericPartitionInterval(1, 25, 1)
	if err != nil {
		t.Fatalf("autoExtractNumericPartitionInterval() error = %v", err)
	}
	if got != 7 {
		t.Fatalf("interval = %d, want 7", got)
	}
}

func TestCeilDivInt64(t *testing.T) {
	tests := []struct {
		name    string
		value   int64
		divisor int64
		want    int64
	}{
		{name: "positive", value: 7, divisor: 3, want: 3},
		{name: "negative value", value: -7, divisor: 3, want: -2},
		{name: "negative divisor", value: 7, divisor: -3, want: -2},
		{name: "both negative", value: -7, divisor: -3, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ceilDivInt64(tt.value, tt.divisor); got != tt.want {
				t.Fatalf("ceilDivInt64(%d, %d) = %d, want %d", tt.value, tt.divisor, got, tt.want)
			}
		})
	}
}

func TestValidateExtractPartitionColumn(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "created_at", DataType: schema.TypeTimestamp},
			{Name: "event_date", DataType: schema.TypeDate},
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString},
		},
	}

	got, err := ValidateExtractPartitionColumn(tableSchema, "CREATED_AT")
	if err != nil {
		t.Fatalf("ValidateExtractPartitionColumn() error = %v", err)
	}
	if got != "created_at" {
		t.Fatalf("resolved column = %q, want created_at", got)
	}
	got, err = ValidateExtractPartitionColumn(tableSchema, "id")
	if err != nil {
		t.Fatalf("ValidateExtractPartitionColumn() integer error = %v", err)
	}
	if got != "id" {
		t.Fatalf("resolved integer column = %q, want id", got)
	}

	if _, err := ValidateExtractPartitionColumn(tableSchema, "name"); err == nil {
		t.Fatal("expected error for non-time column")
	}
	if _, err := ValidateExtractPartitionColumn(tableSchema, "missing"); err == nil {
		t.Fatal("expected error for missing column")
	}
}

func TestReadExtractPartitionsMarksOnlyFinalWindowInclusive(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(14 * 24 * time.Hour)
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "created_at", DataType: schema.TypeTimestamp},
		},
	}

	seen := make(chan ReadOptions, 2)
	read := func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
		seen <- opts
		ch := make(chan RecordBatchResult)
		close(ch)
		return ch, nil
	}

	records, err := ReadExtractPartitions(context.Background(), ReadOptions{
		IncrementalKey:           "created_at",
		IntervalStart:            &start,
		IntervalEnd:              &end,
		ExtractPartitionBy:       "created_at",
		ExtractPartitionInterval: 7 * 24 * time.Hour,
		Parallelism:              1,
	}, tableSchema, read, nil)
	if err != nil {
		t.Fatalf("ReadExtractPartitions() error = %v", err)
	}
	for result := range records {
		if result.Err != nil {
			t.Fatalf("unexpected read error: %v", result.Err)
		}
	}

	first := <-seen
	second := <-seen
	if first.ExtractPartitionEndInclusive {
		t.Fatal("first partition end should be exclusive")
	}
	if !second.ExtractPartitionEndInclusive {
		t.Fatal("final partition end should be inclusive")
	}
}

func TestReadExtractPartitionsAutoTime(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(8 * 24 * time.Hour)
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "created_at", DataType: schema.TypeTimestamp},
		},
	}

	seen := make(chan ReadOptions, 4)
	read := func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
		seen <- opts
		ch := make(chan RecordBatchResult)
		close(ch)
		return ch, nil
	}

	records, err := ReadExtractPartitions(context.Background(), ReadOptions{
		IncrementalKey:       "created_at",
		IntervalStart:        &start,
		IntervalEnd:          &end,
		ExtractPartitionBy:   "created_at",
		ExtractPartitionAuto: true,
		Parallelism:          1,
	}, tableSchema, read, nil)
	if err != nil {
		t.Fatalf("ReadExtractPartitions() error = %v", err)
	}
	for result := range records {
		if result.Err != nil {
			t.Fatalf("unexpected read error: %v", result.Err)
		}
	}

	if len(seen) != 4 {
		t.Fatalf("partition count = %d, want 4", len(seen))
	}
	first := <-seen
	if first.ExtractPartitionStart == nil || !first.ExtractPartitionStart.Equal(start) {
		t.Fatalf("first start = %v, want %v", first.ExtractPartitionStart, start)
	}
	wantFirstEnd := start.Add(48 * time.Hour)
	if first.ExtractPartitionEnd == nil || !first.ExtractPartitionEnd.Equal(wantFirstEnd) {
		t.Fatalf("first end = %v, want %v", first.ExtractPartitionEnd, wantFirstEnd)
	}
	var final ReadOptions
	for len(seen) > 0 {
		final = <-seen
	}
	if final.ExtractPartitionEnd == nil || !final.ExtractPartitionEnd.Equal(end) {
		t.Fatalf("final end = %v, want %v", final.ExtractPartitionEnd, end)
	}
	if !final.ExtractPartitionEndInclusive {
		t.Fatal("final auto partition end should be inclusive")
	}
}

func TestReadExtractPartitionsSetsIncrementalKeyDataType(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "updated_at", DataType: schema.TypeTimestamp},
		},
	}

	seen := make(chan ReadOptions, 1)
	read := func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
		seen <- opts
		ch := make(chan RecordBatchResult)
		close(ch)
		return ch, nil
	}

	records, err := ReadExtractPartitions(context.Background(), ReadOptions{
		IncrementalKey: "updated_at",
		IntervalStart:  &start,
		IntervalEnd:    &end,
	}, tableSchema, read, nil)
	if err != nil {
		t.Fatalf("ReadExtractPartitions() error = %v", err)
	}
	for result := range records {
		if result.Err != nil {
			t.Fatalf("unexpected read error: %v", result.Err)
		}
	}

	got := <-seen
	if got.IncrementalKeyDataType != schema.TypeTimestamp {
		t.Fatalf("IncrementalKeyDataType = %v, want %v", got.IncrementalKeyDataType, schema.TypeTimestamp)
	}
}

func TestReadExtractPartitionsDiscoversBoundsForDifferentKey(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	boundsStart := time.Date(2025, 12, 15, 0, 0, 0, 0, time.UTC)
	boundsEnd := time.Date(2025, 12, 20, 0, 0, 0, 0, time.UTC)
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "created_at", DataType: schema.TypeTimestamp},
		},
	}

	var discovered bool
	seen := make(chan ReadOptions, 1)
	read := func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
		seen <- opts
		ch := make(chan RecordBatchResult)
		close(ch)
		return ch, nil
	}
	discover := func(ctx context.Context, opts ReadOptions) (ExtractPartitionBounds, error) {
		discovered = true
		if opts.IncrementalKey != "updated_at" {
			t.Fatalf("discovery incremental key = %q, want updated_at", opts.IncrementalKey)
		}
		return ExtractPartitionBounds{
			Start:    boundsStart,
			End:      boundsEnd,
			HasRange: true,
		}, nil
	}

	records, err := ReadExtractPartitions(context.Background(), ReadOptions{
		IncrementalKey:           "updated_at",
		IntervalStart:            &intervalStart,
		IntervalEnd:              &intervalEnd,
		ExtractPartitionBy:       "created_at",
		ExtractPartitionInterval: 7 * 24 * time.Hour,
		Parallelism:              1,
	}, tableSchema, read, discover)
	if err != nil {
		t.Fatalf("ReadExtractPartitions() error = %v", err)
	}
	for result := range records {
		if result.Err != nil {
			t.Fatalf("unexpected read error: %v", result.Err)
		}
	}
	if !discovered {
		t.Fatal("expected bounds discovery to run")
	}

	got := <-seen
	if got.ExtractPartitionStart == nil || !got.ExtractPartitionStart.Equal(boundsStart) {
		t.Fatalf("partition start = %v, want %v", got.ExtractPartitionStart, boundsStart)
	}
	if got.ExtractPartitionEnd == nil || !got.ExtractPartitionEnd.Equal(boundsEnd) {
		t.Fatalf("partition end = %v, want %v", got.ExtractPartitionEnd, boundsEnd)
	}
	if !got.ExtractPartitionEndInclusive {
		t.Fatal("single discovered partition should be inclusive")
	}
}

func TestReadExtractPartitionsAddsNullPartitionFromDiscoveredBounds(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "created_at", DataType: schema.TypeTimestamp},
		},
	}

	seen := make(chan ReadOptions, 1)
	read := func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
		seen <- opts
		ch := make(chan RecordBatchResult)
		close(ch)
		return ch, nil
	}
	discover := func(ctx context.Context, opts ReadOptions) (ExtractPartitionBounds, error) {
		return ExtractPartitionBounds{HasNulls: true}, nil
	}

	records, err := ReadExtractPartitions(context.Background(), ReadOptions{
		IncrementalKey:           "updated_at",
		IntervalStart:            &intervalStart,
		IntervalEnd:              &intervalEnd,
		ExtractPartitionBy:       "created_at",
		ExtractPartitionInterval: 7 * 24 * time.Hour,
		Parallelism:              1,
	}, tableSchema, read, discover)
	if err != nil {
		t.Fatalf("ReadExtractPartitions() error = %v", err)
	}
	for result := range records {
		if result.Err != nil {
			t.Fatalf("unexpected read error: %v", result.Err)
		}
	}

	got := <-seen
	if !got.ExtractPartitionIsNull {
		t.Fatal("expected null partition read")
	}
	if got.ExtractPartitionStart != nil || got.ExtractPartitionEnd != nil {
		t.Fatalf("null partition should not carry range bounds, got start=%v end=%v", got.ExtractPartitionStart, got.ExtractPartitionEnd)
	}
}

func TestReadExtractPartitionsDoesNotAddNullPartitionWithoutIncrementalKey(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "created_at", DataType: schema.TypeTimestamp},
		},
	}

	seen := make(chan ReadOptions, 5)
	read := func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
		seen <- opts
		ch := make(chan RecordBatchResult)
		close(ch)
		return ch, nil
	}

	records, err := ReadExtractPartitions(context.Background(), ReadOptions{
		IntervalStart:            &intervalStart,
		IntervalEnd:              &intervalEnd,
		ExtractPartitionBy:       "created_at",
		ExtractPartitionInterval: 10 * 24 * time.Hour,
		Parallelism:              1,
	}, tableSchema, read, nil)
	if err != nil {
		t.Fatalf("ReadExtractPartitions() error = %v", err)
	}
	for result := range records {
		if result.Err != nil {
			t.Fatalf("unexpected read error: %v", result.Err)
		}
	}

	if len(seen) != 3 {
		t.Fatalf("partition count = %d, want 3", len(seen))
	}
	for len(seen) > 0 {
		got := <-seen
		if got.RecordBatchBufferSize != extractPartitionReadBufferSize {
			t.Fatalf("RecordBatchBufferSize = %d, want %d", got.RecordBatchBufferSize, extractPartitionReadBufferSize)
		}
		if got.ExtractPartitionIsNull {
			t.Fatal("did not expect null partition read without discovered null bounds")
		}
	}
}

func TestExtractPartitionBoundsFromValues(t *testing.T) {
	minValue := "2025-12-15 00:00:00"
	maxValue := "2025-12-20 00:00:00"
	got, err := ExtractPartitionBoundsFromValues(ExtractPartitionKindTime, minValue, maxValue, 3, 2)
	if err != nil {
		t.Fatalf("ExtractPartitionBoundsFromValues() error = %v", err)
	}
	if !got.HasRange {
		t.Fatal("expected range")
	}
	if !got.HasNulls {
		t.Fatal("expected null marker")
	}
	if !got.Start.Equal(time.Date(2025, 12, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("start = %v", got.Start)
	}
	if !got.End.Equal(time.Date(2025, 12, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("end = %v", got.End)
	}

	empty, err := ExtractPartitionBoundsFromValues(ExtractPartitionKindTime, nil, nil, 0, 0)
	if err != nil {
		t.Fatalf("empty bounds error = %v", err)
	}
	if empty.HasRange || empty.HasNulls {
		t.Fatalf("empty bounds = %#v, want no range and no nulls", empty)
	}
}

func TestExtractPartitionBoundsFromValuesRoundsSubSecondMax(t *testing.T) {
	minValue := "2025-12-15 00:00:00.250000"
	maxValue := "2025-12-20 10:30:45.678000"
	got, err := ExtractPartitionBoundsFromValues(ExtractPartitionKindTime, minValue, maxValue, 2, 2)
	if err != nil {
		t.Fatalf("ExtractPartitionBoundsFromValues() error = %v", err)
	}
	if !got.Start.Equal(time.Date(2025, 12, 15, 0, 0, 0, 250000000, time.UTC)) {
		t.Fatalf("start = %v, want 2025-12-15 00:00:00.25", got.Start)
	}
	if !got.End.Equal(time.Date(2025, 12, 20, 10, 30, 45, 678000000, time.UTC)) {
		t.Fatalf("end = %v, want 2025-12-20 10:30:45.678", got.End)
	}
}

func TestExtractPartitionBoundsFromValuesRoundsSubMicrosecondMax(t *testing.T) {
	minValue := time.Date(2025, 12, 15, 0, 0, 0, 0, time.UTC)
	maxValue := time.Date(2025, 12, 20, 10, 30, 45, 678901234, time.UTC)
	got, err := ExtractPartitionBoundsFromValues(ExtractPartitionKindTime, minValue, maxValue, 2, 2)
	if err != nil {
		t.Fatalf("ExtractPartitionBoundsFromValues() error = %v", err)
	}
	if !got.End.Equal(time.Date(2025, 12, 20, 10, 30, 45, 678902000, time.UTC)) {
		t.Fatalf("end = %v, want 2025-12-20 10:30:45.678902", got.End)
	}
}

func TestSQLExtractPartitionConditionsFormatsDateBounds(t *testing.T) {
	start := time.Date(2026, 1, 8, 0, 0, 0, 123456000, time.UTC)
	end := time.Date(2026, 1, 15, 0, 0, 0, 654321000, time.UTC)
	got := SQLExtractPartitionConditions(ReadOptions{
		ExtractPartitionBy:           "event_date",
		ExtractPartitionStart:        &start,
		ExtractPartitionEnd:          &end,
		ExtractPartitionEndInclusive: true,
		ExtractPartitionDataType:     schema.TypeDate,
	}, func(s string) string { return `"` + s + `"` }, DefaultSQLTimeFormat)

	want := []string{`"event_date" >= '2026-01-08'`, `"event_date" <= '2026-01-15'`}
	if len(got) != len(want) {
		t.Fatalf("conditions = %#v, want %#v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("condition[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSQLExtractPartitionConditionsFormatsTimestampBoundsWithoutTimezone(t *testing.T) {
	start := time.Date(2026, 1, 8, 0, 0, 0, 123456000, time.UTC)
	end := time.Date(2026, 1, 15, 0, 0, 0, 654321000, time.UTC)
	got := SQLExtractPartitionConditions(ReadOptions{
		ExtractPartitionBy:       "created_at",
		ExtractPartitionStart:    &start,
		ExtractPartitionEnd:      &end,
		ExtractPartitionDataType: schema.TypeTimestamp,
	}, func(s string) string { return `"` + s + `"` }, DefaultSQLTimeFormat)

	want := []string{`"created_at" >= '2026-01-08 00:00:00.123456'`, `"created_at" < '2026-01-15 00:00:00.654321'`}
	if len(got) != len(want) {
		t.Fatalf("conditions = %#v, want %#v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("condition[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSQLExtractPartitionBoundsQueryFormatsTimestampIncrementalKeyWithoutTimezone(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 123456000, time.UTC)
	end := time.Date(2026, 1, 31, 0, 0, 0, 654321000, time.UTC)

	got := SQLExtractPartitionBoundsQuery("orders", "created_at", "updated_at", schema.TypeTimestamp, &start, &end, func(s string) string {
		return `"` + s + `"`
	}, func(s string) string {
		return s
	}, DefaultSQLTimeFormat)

	want := `SELECT MIN("created_at"), MAX("created_at"), COUNT(*), COUNT("created_at") FROM orders WHERE "updated_at" >= '2026-01-01 00:00:00.123456' AND "updated_at" <= '2026-01-31 00:00:00.654321'`
	if got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
}

func TestNativeSQLTimeFormat(t *testing.T) {
	exactSecond := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := NativeSQLTimeFormat(exactSecond); got != "2026-01-01 00:00:00" {
		t.Fatalf("exact second = %q, want no fractional seconds", got)
	}

	subsecond := time.Date(2026, 1, 1, 0, 0, 0, 123456789, time.UTC)
	if got := NativeSQLTimeFormat(subsecond); got != "2026-01-01 00:00:00.123456" {
		t.Fatalf("subsecond = %q, want microseconds without timezone", got)
	}
}

func TestReadExtractPartitionsDiscoversNumericBounds(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		},
	}

	seen := make(chan ReadOptions, 3)
	read := func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
		seen <- opts
		ch := make(chan RecordBatchResult)
		close(ch)
		return ch, nil
	}
	discover := func(ctx context.Context, opts ReadOptions) (ExtractPartitionBounds, error) {
		if opts.ExtractPartitionKind != ExtractPartitionKindNumeric {
			t.Fatalf("discovery kind = %v, want numeric", opts.ExtractPartitionKind)
		}
		return ExtractPartitionBounds{
			NumericStart: 1,
			NumericEnd:   25,
			Kind:         ExtractPartitionKindNumeric,
			HasRange:     true,
		}, nil
	}

	records, err := ReadExtractPartitions(context.Background(), ReadOptions{
		IncrementalKey:                  "updated_at",
		IntervalStart:                   &intervalStart,
		IntervalEnd:                     &intervalEnd,
		ExtractPartitionBy:              "id",
		ExtractPartitionNumericInterval: 10,
		Parallelism:                     1,
	}, tableSchema, read, discover)
	if err != nil {
		t.Fatalf("ReadExtractPartitions() error = %v", err)
	}
	for result := range records {
		if result.Err != nil {
			t.Fatalf("unexpected read error: %v", result.Err)
		}
	}

	first := <-seen
	second := <-seen
	third := <-seen
	if first.ExtractPartitionNumericStart == nil || *first.ExtractPartitionNumericStart != 1 {
		t.Fatalf("first numeric start = %v, want 1", first.ExtractPartitionNumericStart)
	}
	if first.ExtractPartitionNumericEnd == nil || *first.ExtractPartitionNumericEnd != 11 {
		t.Fatalf("first numeric end = %v, want 11", first.ExtractPartitionNumericEnd)
	}
	if first.ExtractPartitionEndInclusive {
		t.Fatal("first numeric partition end should be exclusive")
	}
	if second.ExtractPartitionNumericStart == nil || *second.ExtractPartitionNumericStart != 11 {
		t.Fatalf("second numeric start = %v, want 11", second.ExtractPartitionNumericStart)
	}
	if third.ExtractPartitionNumericEnd == nil || *third.ExtractPartitionNumericEnd != 25 {
		t.Fatalf("third numeric end = %v, want 25", third.ExtractPartitionNumericEnd)
	}
	if !third.ExtractPartitionEndInclusive {
		t.Fatal("final numeric partition end should be inclusive")
	}
}

func TestReadExtractPartitionsDiscoversFullNumericBoundsWithoutIncrementalKey(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		},
	}

	discoverCalled := false
	records, err := ReadExtractPartitions(context.Background(), ReadOptions{
		ExtractPartitionBy:              "id",
		ExtractPartitionNumericInterval: 10,
		Parallelism:                     1,
	}, tableSchema, func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
		ch := make(chan RecordBatchResult)
		close(ch)
		return ch, nil
	}, func(ctx context.Context, opts ReadOptions) (ExtractPartitionBounds, error) {
		discoverCalled = true
		return ExtractPartitionBounds{
			NumericStart: 1,
			NumericEnd:   20,
			Kind:         ExtractPartitionKindNumeric,
			HasRange:     true,
		}, nil
	})
	if err != nil {
		t.Fatalf("ReadExtractPartitions() error = %v", err)
	}
	for result := range records {
		if result.Err != nil {
			t.Fatalf("unexpected read error: %v", result.Err)
		}
	}
	if !discoverCalled {
		t.Fatal("bounds discovery should be called")
	}
}

func TestDrainRecordBatchResultsReleasesBatches(t *testing.T) {
	var releases atomic.Int64
	ch := make(chan RecordBatchResult, 2)
	ch <- RecordBatchResult{Batch: &releaseCountingBatch{releases: &releases}}
	ch <- RecordBatchResult{}
	close(ch)

	drainRecordBatchResults(ch)

	if got := releases.Load(); got != 1 {
		t.Fatalf("release count = %d, want 1", got)
	}
}

func TestReadExtractPartitionsReleasesCurrentBatchWhenCancelled(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "created_at", DataType: schema.TypeTimestamp},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	readStarted := make(chan chan RecordBatchResult, 1)
	read := func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
		ch := make(chan RecordBatchResult)
		readStarted <- ch
		return ch, nil
	}

	records, err := ReadExtractPartitions(ctx, ReadOptions{
		IncrementalKey:           "created_at",
		IntervalStart:            &start,
		IntervalEnd:              &end,
		ExtractPartitionBy:       "created_at",
		ExtractPartitionInterval: 24 * time.Hour,
		Parallelism:              1,
	}, tableSchema, read, nil)
	if err != nil {
		t.Fatalf("ReadExtractPartitions() error = %v", err)
	}

	var sourceRecords chan RecordBatchResult
	select {
	case sourceRecords = <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for partition read to start")
	}

	var firstReleases atomic.Int64
	var secondReleases atomic.Int64
	sourceRecords <- RecordBatchResult{Batch: &releaseCountingBatch{releases: &firstReleases}}

	deadline := time.After(time.Second)
	for len(records) == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for output buffer to fill")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()
	sourceRecords <- RecordBatchResult{Batch: &releaseCountingBatch{releases: &secondReleases}}
	close(sourceRecords)

	deadline = time.After(time.Second)
	for secondReleases.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for cancelled batch release")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	result := <-records
	releaseRecordBatchResult(result)
	for result := range records {
		releaseRecordBatchResult(result)
	}

	if got := firstReleases.Load(); got != 1 {
		t.Fatalf("forwarded batch release count = %d, want 1 after test consumer release", got)
	}
	if got := secondReleases.Load(); got != 1 {
		t.Fatalf("cancelled batch release count = %d, want 1", got)
	}
}

type releaseCountingBatch struct {
	releases *atomic.Int64
}

func (b *releaseCountingBatch) MarshalJSON() ([]byte, error) {
	return []byte("null"), nil
}

func (b *releaseCountingBatch) Release() {
	b.releases.Add(1)
}

func (b *releaseCountingBatch) Retain() {}

func (b *releaseCountingBatch) Schema() *arrow.Schema {
	return nil
}

func (b *releaseCountingBatch) NumRows() int64 {
	return 0
}

func (b *releaseCountingBatch) NumCols() int64 {
	return 0
}

func (b *releaseCountingBatch) Columns() []arrow.Array {
	return nil
}

func (b *releaseCountingBatch) Column(i int) arrow.Array {
	return nil
}

func (b *releaseCountingBatch) ColumnName(i int) string {
	return ""
}

func (b *releaseCountingBatch) SetColumn(i int, col arrow.Array) (arrow.RecordBatch, error) {
	return nil, nil
}

func (b *releaseCountingBatch) NewSlice(i, j int64) arrow.RecordBatch {
	return b
}

func TestReadExtractPartitionsAutoNumeric(t *testing.T) {
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		},
	}

	seen := make(chan ReadOptions, 4)
	read := func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error) {
		seen <- opts
		ch := make(chan RecordBatchResult)
		close(ch)
		return ch, nil
	}
	discover := func(ctx context.Context, opts ReadOptions) (ExtractPartitionBounds, error) {
		return ExtractPartitionBounds{
			NumericStart: 1,
			NumericEnd:   25,
			Kind:         ExtractPartitionKindNumeric,
			HasRange:     true,
		}, nil
	}

	records, err := ReadExtractPartitions(context.Background(), ReadOptions{
		IncrementalKey:       "updated_at",
		IntervalStart:        &intervalStart,
		IntervalEnd:          &intervalEnd,
		ExtractPartitionBy:   "id",
		ExtractPartitionAuto: true,
		Parallelism:          1,
	}, tableSchema, read, discover)
	if err != nil {
		t.Fatalf("ReadExtractPartitions() error = %v", err)
	}
	for result := range records {
		if result.Err != nil {
			t.Fatalf("unexpected read error: %v", result.Err)
		}
	}

	if len(seen) != 4 {
		t.Fatalf("partition count = %d, want 4", len(seen))
	}
	want := []ExtractNumericPartitionWindow{
		{Start: 1, End: 8},
		{Start: 8, End: 15},
		{Start: 15, End: 22},
		{Start: 22, End: 25},
	}
	for i, window := range want {
		got := <-seen
		if got.ExtractPartitionNumericStart == nil || *got.ExtractPartitionNumericStart != window.Start {
			t.Fatalf("window[%d] start = %v, want %d", i, got.ExtractPartitionNumericStart, window.Start)
		}
		if got.ExtractPartitionNumericEnd == nil || *got.ExtractPartitionNumericEnd != window.End {
			t.Fatalf("window[%d] end = %v, want %d", i, got.ExtractPartitionNumericEnd, window.End)
		}
		if got.ExtractPartitionEndInclusive != (i == len(want)-1) {
			t.Fatalf("window[%d] inclusive = %v, want %v", i, got.ExtractPartitionEndInclusive, i == len(want)-1)
		}
	}
}

func TestExtractPartitionBoundsFromNumericValues(t *testing.T) {
	got, err := ExtractPartitionBoundsFromValues(ExtractPartitionKindNumeric, []byte("10"), int64(42), 4, 4)
	if err != nil {
		t.Fatalf("ExtractPartitionBoundsFromValues() error = %v", err)
	}
	if !got.HasRange {
		t.Fatal("expected range")
	}
	if got.Kind != ExtractPartitionKindNumeric {
		t.Fatalf("kind = %v, want numeric", got.Kind)
	}
	if got.NumericStart != 10 || got.NumericEnd != 42 {
		t.Fatalf("numeric bounds = %d..%d, want 10..42", got.NumericStart, got.NumericEnd)
	}
}

func TestSQLIntValueAcceptsFloatAndBigIntValues(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  int64
	}{
		{name: "float32", value: float32(12), want: 12},
		{name: "float64", value: float64(42), want: 42},
		{name: "big int", value: big.NewInt(99), want: 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SQLIntValue(tt.value)
			if err != nil {
				t.Fatalf("SQLIntValue() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("SQLIntValue() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSQLIntValueRejectsNonIntegerFloat(t *testing.T) {
	if _, err := SQLIntValue(42.5); err == nil {
		t.Fatal("expected error")
	}
}

func TestSQLCustomQueryBuilders(t *testing.T) {
	quote := func(name string) string { return `"` + name + `"` }
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	schemaQuery := SQLCustomQuerySchemaQuery(" SELECT id, created_at FROM orders; ", quote)
	wantSchema := `SELECT * FROM (SELECT id, created_at FROM orders) AS "__ingestr_query" WHERE 1 = 0`
	if schemaQuery != wantSchema {
		t.Fatalf("schema query = %q, want %q", schemaQuery, wantSchema)
	}

	windowQuery := SQLCustomQuerySelectQuery("SELECT id, created_at FROM orders;", ReadOptions{
		ExtractPartitionBy:           "created_at",
		ExtractPartitionStart:        &start,
		ExtractPartitionEnd:          &end,
		ExtractPartitionEndInclusive: true,
		ExtractPartitionDataType:     schema.TypeTimestamp,
	}, quote, DefaultSQLTimeFormat)
	wantWindow := `SELECT * FROM (SELECT id, created_at FROM orders) AS "__ingestr_query" WHERE "__ingestr_query"."created_at" >= '2026-01-01 00:00:00' AND "__ingestr_query"."created_at" <= '2026-01-02 00:00:00'`
	if windowQuery != wantWindow {
		t.Fatalf("window query = %q, want %q", windowQuery, wantWindow)
	}

	nullQuery := SQLCustomQuerySelectQuery("SELECT id FROM orders", ReadOptions{
		ExtractPartitionBy:     "id",
		ExtractPartitionIsNull: true,
	}, quote, DefaultSQLTimeFormat)
	wantNull := `SELECT * FROM (SELECT id FROM orders) AS "__ingestr_query" WHERE "__ingestr_query"."id" IS NULL`
	if nullQuery != wantNull {
		t.Fatalf("null query = %q, want %q", nullQuery, wantNull)
	}

	boundsQuery := SQLCustomQueryBoundsQuery("SELECT id FROM orders;", "id", quote)
	wantBounds := `SELECT MIN("__ingestr_query"."id"), MAX("__ingestr_query"."id"), COUNT(*), COUNT("__ingestr_query"."id") FROM (SELECT id FROM orders) AS "__ingestr_query"`
	if boundsQuery != wantBounds {
		t.Fatalf("bounds query = %q, want %q", boundsQuery, wantBounds)
	}
}
