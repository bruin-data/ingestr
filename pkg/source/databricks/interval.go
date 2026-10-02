package databricks

import (
	"fmt"
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	dbsql "github.com/databricks/databricks-sdk-go/service/sql"
)

var dayTimeFields = []string{"DAY", "HOUR", "MINUTE", "SECOND"}

var microsPerField = map[string]int64{
	"DAY":    86_400_000_000,
	"HOUR":   3_600_000_000,
	"MINUTE": 60_000_000,
	"SECOND": 1_000_000,
}

// intervalQualifiers returns each column's interval qualifier (e.g. "DAY TO SECOND"),
// or "" for non-interval columns.
func intervalQualifiers(m *dbsql.ResultManifest) []string {
	if m == nil || m.Schema == nil {
		return nil
	}
	out := make([]string, len(m.Schema.Columns))
	for i, c := range m.Schema.Columns {
		if q, ok := strings.CutPrefix(strings.ToUpper(strings.TrimSpace(c.TypeText)), "INTERVAL "); ok {
			out[i] = q
		}
	}
	return out
}

// formatIntervals renders Arrow duration/month_interval columns as Databricks
// interval literals (INTERVAL '1 02:03:04.5' DAY TO SECOND), matching JSON_ARRAY output.
func formatIntervals(alloc memory.Allocator, rec arrow.RecordBatch, qualifiers []string) arrow.RecordBatch {
	var cols []arrow.Array
	var fields []arrow.Field
	for i, col := range rec.Columns() {
		if i >= len(qualifiers) || qualifiers[i] == "" {
			continue
		}
		var format func(int) string
		switch a := col.(type) {
		case *array.Duration:
			scale := durationMicros(a.DataType().(*arrow.DurationType).Unit)
			format = func(j int) string { return formatDayTimeInterval(scale(int64(a.Value(j))), qualifiers[i]) }
		case *array.MonthInterval:
			format = func(j int) string { return formatYearMonthInterval(int64(a.Value(j)), qualifiers[i]) }
		default:
			continue
		}
		if cols == nil {
			cols = slices.Clone(rec.Columns())
			fields = slices.Clone(rec.Schema().Fields())
			for _, c := range cols {
				c.Retain()
			}
		}
		b := array.NewStringBuilder(alloc)
		for j := 0; j < col.Len(); j++ {
			if col.IsNull(j) {
				b.AppendNull()
			} else {
				b.Append(format(j))
			}
		}
		cols[i].Release()
		cols[i] = b.NewArray()
		b.Release()
		fields[i].Type = arrow.BinaryTypes.String
	}
	if cols == nil {
		rec.Retain()
		return rec
	}
	defer func() {
		for _, c := range cols {
			c.Release()
		}
	}()
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, rec.NumRows())
}

func durationMicros(unit arrow.TimeUnit) func(int64) int64 {
	switch unit {
	case arrow.Second:
		return func(v int64) int64 { return v * 1_000_000 }
	case arrow.Millisecond:
		return func(v int64) int64 { return v * 1_000 }
	case arrow.Nanosecond:
		return func(v int64) int64 { return v / 1_000 }
	default:
		return func(v int64) int64 { return v }
	}
}

func formatDayTimeInterval(micros int64, qualifier string) string {
	start, end, _ := strings.Cut(qualifier, " TO ")
	if end == "" {
		end = start
	}
	si, ei := slices.Index(dayTimeFields, start), slices.Index(dayTimeFields, end)
	if si < 0 || ei < si {
		return fmt.Sprintf("%dus", micros)
	}

	var b strings.Builder
	if micros < 0 {
		b.WriteByte('-')
	}
	rest := uint64(micros)
	if micros < 0 {
		rest = uint64(-micros)
	}
	for k := si; k <= ei; k++ {
		field := dayTimeFields[k]
		if k > si {
			if dayTimeFields[k-1] == "DAY" {
				b.WriteByte(' ')
			} else {
				b.WriteByte(':')
			}
		}
		unit := uint64(microsPerField[field])
		v := rest / unit
		rest %= unit
		if k == si && field != "SECOND" {
			fmt.Fprintf(&b, "%d", v)
		} else {
			fmt.Fprintf(&b, "%02d", v)
		}
		if field == "SECOND" && rest > 0 {
			b.WriteByte('.')
			b.WriteString(strings.TrimRight(fmt.Sprintf("%06d", rest), "0"))
		}
	}
	return fmt.Sprintf("INTERVAL '%s' %s", b.String(), qualifier)
}

func formatYearMonthInterval(months int64, qualifier string) string {
	sign := ""
	if months < 0 {
		sign = "-"
		months = -months
	}
	var body string
	switch qualifier {
	case "YEAR":
		body = fmt.Sprintf("%d", months/12)
	case "MONTH":
		body = fmt.Sprintf("%d", months)
	default:
		body = fmt.Sprintf("%d-%d", months/12, months%12)
	}
	return fmt.Sprintf("INTERVAL '%s%s' %s", sign, body, qualifier)
}
