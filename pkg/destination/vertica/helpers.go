package vertica

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// COPY FROM STDIN control bytes. They are unlikely to appear in real data, and
// any that do are escaped, so embedded delimiters, terminators, and the NULL
// marker survive the round trip. NULL is distinguished from the empty string.
const (
	copyDelimiter  = 0x01
	copyRecordTerm = 0x02
	copyNull       = 0x03
	copyEscape     = 0x04
)

func quoteColumn(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteColumns(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = quoteColumn(n)
	}
	return out
}

// splitSchemaTable splits a possibly schema-qualified name into its schema and
// table parts. An unqualified name returns an empty schema.
func splitSchemaTable(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", table
}

func quoteTable(table string) string {
	schemaName, name := splitSchemaTable(table)
	if schemaName != "" {
		return quoteColumn(schemaName) + "." + quoteColumn(name)
	}
	return quoteColumn(name)
}

// filterColumns returns the columns not present in exclude (case-insensitive).
func filterColumns(columns []string, exclude []string) []string {
	excludeSet := make(map[string]struct{}, len(exclude))
	for _, c := range exclude {
		excludeSet[strings.ToLower(c)] = struct{}{}
	}
	out := make([]string, 0, len(columns))
	for _, c := range columns {
		if _, ok := excludeSet[strings.ToLower(c)]; !ok {
			out = append(out, c)
		}
	}
	return out
}

func sourceColumnRefs(columns []string, alias string) []string {
	out := make([]string, len(columns))
	for i, c := range columns {
		out[i] = alias + "." + quoteColumn(c)
	}
	return out
}

func buildJoinCondition(keys []string, targetAlias, sourceAlias string) string {
	conditions := make([]string, len(keys))
	for i, k := range keys {
		conditions[i] = fmt.Sprintf("%s.%s = %s.%s", targetAlias, quoteColumn(k), sourceAlias, quoteColumn(k))
	}
	return strings.Join(conditions, " AND ")
}

// buildUpdateSet renders the SET clause for a MERGE. Vertica rejects a target
// alias on the assigned column, so the left-hand side is left unqualified.
func buildUpdateSet(columns []string, sourceAlias string) string {
	sets := make([]string, len(columns))
	for i, c := range columns {
		sets[i] = fmt.Sprintf("%s = %s.%s", quoteColumn(c), sourceAlias, quoteColumn(c))
	}
	return strings.Join(sets, ", ")
}

// appendCopyValue encodes one Arrow value into the COPY FROM STDIN stream,
// writing the NULL marker for nulls and escaping any control bytes otherwise.
func appendCopyValue(buf *bytes.Buffer, arr arrow.Array, idx int) {
	if arr.IsNull(idx) {
		buf.WriteByte(copyNull)
		return
	}

	switch a := arr.(type) {
	case *array.Boolean:
		if a.Value(idx) {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case *array.Int8:
		writeCopyString(buf, strconv.FormatInt(int64(a.Value(idx)), 10))
	case *array.Int16:
		writeCopyString(buf, strconv.FormatInt(int64(a.Value(idx)), 10))
	case *array.Int32:
		writeCopyString(buf, strconv.FormatInt(int64(a.Value(idx)), 10))
	case *array.Int64:
		writeCopyString(buf, strconv.FormatInt(a.Value(idx), 10))
	case *array.Uint8:
		writeCopyString(buf, strconv.FormatUint(uint64(a.Value(idx)), 10))
	case *array.Uint16:
		writeCopyString(buf, strconv.FormatUint(uint64(a.Value(idx)), 10))
	case *array.Uint32:
		writeCopyString(buf, strconv.FormatUint(uint64(a.Value(idx)), 10))
	case *array.Uint64:
		writeCopyString(buf, strconv.FormatUint(a.Value(idx), 10))
	case *array.Float32:
		writeCopyString(buf, strconv.FormatFloat(float64(a.Value(idx)), 'g', -1, 32))
	case *array.Float64:
		writeCopyString(buf, strconv.FormatFloat(a.Value(idx), 'g', -1, 64))
	case *array.String:
		writeCopyString(buf, a.Value(idx))
	case *array.LargeString:
		writeCopyString(buf, a.Value(idx))
	case *array.Binary:
		writeCopyBytes(buf, a.Value(idx))
	case *array.LargeBinary:
		writeCopyBytes(buf, a.Value(idx))
	case *array.Date32:
		writeCopyString(buf, a.Value(idx).ToTime().Format("2006-01-02"))
	case *array.Date64:
		writeCopyString(buf, a.Value(idx).ToTime().Format("2006-01-02"))
	case *array.Time64:
		micros := int64(a.Value(idx))
		if a.DataType().(*arrow.Time64Type).Unit == arrow.Nanosecond {
			micros /= 1_000
		}
		secs := micros / 1_000_000
		writeCopyString(buf, fmt.Sprintf("%02d:%02d:%02d.%06d", secs/3600, (secs%3600)/60, secs%60, micros%1_000_000))
	case *array.Timestamp:
		t := a.Value(idx).ToTime(a.DataType().(*arrow.TimestampType).Unit).UTC()
		writeCopyString(buf, t.Format("2006-01-02 15:04:05.000000-07:00"))
	case *array.Decimal128:
		writeCopyString(buf, a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal128Type).Scale)))
	case *array.Decimal256:
		writeCopyString(buf, a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal256Type).Scale)))
	case array.ExtensionArray:
		appendCopyValue(buf, a.Storage(), idx)
	case *array.MonthInterval:
		writeCopyString(buf, formatISOInterval(int64(a.Value(idx)), 0, 0))
	case *array.DayTimeInterval:
		v := a.Value(idx)
		writeCopyString(buf, formatISOInterval(0, int64(v.Days), int64(v.Milliseconds)*1_000_000))
	case *array.MonthDayNanoInterval:
		v := a.Value(idx)
		writeCopyString(buf, formatISOInterval(int64(v.Months), int64(v.Days), v.Nanoseconds))
	case *array.List, *array.LargeList, *array.Map, *array.Struct:
		writeCopyString(buf, marshalArrowJSON(arr, idx))
	default:
		writeCopyString(buf, arr.ValueStr(idx))
	}
}

// formatISOInterval renders an interval as an ISO 8601 duration so it survives
// as a portable, parseable string in Vertica's text-typed interval column.
func formatISOInterval(months, days, nanos int64) string {
	var b strings.Builder
	b.WriteByte('P')
	if months != 0 {
		fmt.Fprintf(&b, "%dM", months)
	}
	if days != 0 {
		fmt.Fprintf(&b, "%dD", days)
	}
	if nanos != 0 || (months == 0 && days == 0) {
		secs := float64(nanos) / 1e9
		fmt.Fprintf(&b, "T%gS", secs)
	}
	return b.String()
}

func marshalArrowJSON(arr arrow.Array, idx int) string {
	data, err := json.Marshal(arr.GetOneForMarshal(idx))
	if err != nil {
		return arr.ValueStr(idx)
	}
	return string(data)
}

func writeCopyString(buf *bytes.Buffer, s string) {
	writeCopyBytes(buf, []byte(s))
}

func writeCopyBytes(buf *bytes.Buffer, b []byte) {
	for _, c := range b {
		if c >= copyDelimiter && c <= copyEscape {
			buf.WriteByte(copyEscape)
		}
		buf.WriteByte(c)
	}
}
