package schema

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const partitionColumnPattern = "[`\"]?(?P<col>[^`\",()\\s]+)[`\"]?"

var (
	bigQueryTimePartitionPattern  = regexp.MustCompile(`(?i)^\s*(DATE|DATE_TRUNC|DATETIME_TRUNC|TIMESTAMP_TRUNC)\s*\(\s*` + partitionColumnPattern + `\s*(?:,\s*(\w+)\s*)?\)\s*$`)
	bigQueryRangePartitionPattern = regexp.MustCompile(`(?i)^\s*RANGE_BUCKET\s*\(\s*` + partitionColumnPattern + `\s*,\s*GENERATE_ARRAY\s*\(\s*(-?\d+)\s*,\s*(-?\d+)\s*,\s*(-?\d+)\s*\)\s*\)\s*$`)

	icebergTimePartitionPattern = regexp.MustCompile(`(?i)^\s*(YEARS?|MONTHS?|DAYS?|HOURS?)\s*\(\s*` + partitionColumnPattern + `\s*\)\s*$`)
	// Athena and Spark write bucket(N, col); Trino writes bucket(col, N).
	icebergWidthFirstPattern  = regexp.MustCompile(`(?i)^\s*(BUCKET|TRUNCATE)\s*\(\s*(\d+)\s*,\s*` + partitionColumnPattern + `\s*\)\s*$`)
	icebergColumnFirstPattern = regexp.MustCompile(`(?i)^\s*(BUCKET|TRUNCATE)\s*\(\s*` + partitionColumnPattern + `\s*,\s*(\d+)\s*\)\s*$`)

	partitionPatterns = []*regexp.Regexp{
		bigQueryTimePartitionPattern, bigQueryRangePartitionPattern,
		icebergTimePartitionPattern, icebergWidthFirstPattern, icebergColumnFirstPattern,
	}
)

// PartitionSpec is a parsed partition_by value. A bare column leaves every other field empty,
// which destinations treat as their default partitioning.
type PartitionSpec struct {
	Column      string
	Granularity string
	Range       *PartitionRange
	Bucket      int
	Truncate    int
}

type PartitionRange struct {
	Start    int64
	End      int64
	Interval int64
}

// IsPartitionExpression reports whether partition_by is an expression rather than a bare column.
func IsPartitionExpression(value string) bool {
	return strings.Contains(value, "(")
}

// HasPartitionColumn reports whether partition_by names one of the columns verbatim. Such a value
// is a column even if it looks like an expression, e.g. "Amount (USD)".
func (ts *TableSchema) HasPartitionColumn(value string) bool {
	return ts != nil && slices.Contains(ts.ColumnNames(), strings.TrimSpace(value))
}

// ParseBigQueryPartitionBy accepts a bare column, DATE(col), DATE_TRUNC/DATETIME_TRUNC/TIMESTAMP_TRUNC(col, unit),
// or RANGE_BUCKET(col, GENERATE_ARRAY(start, end, interval)).
func ParseBigQueryPartitionBy(value string) (PartitionSpec, error) {
	if !IsPartitionExpression(value) {
		return PartitionSpec{Column: strings.TrimSpace(value)}, nil
	}
	if m := bigQueryRangePartitionPattern.FindStringSubmatch(value); m != nil {
		return parseRangePartition(value, m)
	}
	m := bigQueryTimePartitionPattern.FindStringSubmatch(value)
	if m == nil {
		return PartitionSpec{}, fmt.Errorf("unsupported partition_by expression %q: use a column, DATE(col), DATE_TRUNC/DATETIME_TRUNC/TIMESTAMP_TRUNC(col, HOUR|DAY|MONTH|YEAR) or RANGE_BUCKET(col, GENERATE_ARRAY(start, end, interval))", value)
	}
	fn, column, unit := strings.ToUpper(m[1]), m[2], strings.ToUpper(m[3])
	if fn == "DATE" {
		if unit != "" {
			return PartitionSpec{}, fmt.Errorf("unsupported partition_by expression %q: DATE() takes only a column", value)
		}
		return PartitionSpec{Column: column, Granularity: "DAY"}, nil
	}
	switch unit {
	case "HOUR", "DAY", "MONTH", "YEAR":
		return PartitionSpec{Column: column, Granularity: unit}, nil
	case "":
		return PartitionSpec{}, fmt.Errorf("unsupported partition_by expression %q: %s requires a unit (HOUR, DAY, MONTH or YEAR)", value, fn)
	default:
		return PartitionSpec{}, fmt.Errorf("unsupported partition granularity %q in %q: use HOUR, DAY, MONTH or YEAR", unit, value)
	}
}

func parseRangePartition(value string, m []string) (PartitionSpec, error) {
	var bounds [3]int64
	for i := range bounds {
		n, err := strconv.ParseInt(m[i+2], 10, 64)
		if err != nil {
			return PartitionSpec{}, fmt.Errorf("invalid partition_by expression %q: %w", value, err)
		}
		bounds[i] = n
	}
	r := &PartitionRange{Start: bounds[0], End: bounds[1], Interval: bounds[2]}
	if r.Interval <= 0 || r.End <= r.Start {
		return PartitionSpec{}, fmt.Errorf("invalid partition_by expression %q: GENERATE_ARRAY needs start < end and a positive interval", value)
	}
	return PartitionSpec{Column: m[1], Range: r}, nil
}

// ParseIcebergPartitionBy accepts a bare column or an Iceberg transform: year/month/day/hour(col)
// (also Spark's plural forms), bucket(N, col) or truncate(W, col), in either argument order.
func ParseIcebergPartitionBy(value string) (PartitionSpec, error) {
	if !IsPartitionExpression(value) {
		return PartitionSpec{Column: strings.TrimSpace(value)}, nil
	}
	if m := icebergTimePartitionPattern.FindStringSubmatch(value); m != nil {
		return PartitionSpec{Column: m[2], Granularity: strings.TrimSuffix(strings.ToUpper(m[1]), "S")}, nil
	}
	var fn, width, column string
	if m := icebergWidthFirstPattern.FindStringSubmatch(value); m != nil {
		fn, width, column = m[1], m[2], m[3]
	} else if m := icebergColumnFirstPattern.FindStringSubmatch(value); m != nil {
		fn, column, width = m[1], m[2], m[3]
	} else {
		return PartitionSpec{}, fmt.Errorf("unsupported partition_by expression %q: use a column, year/month/day/hour(col), bucket(N, col) or truncate(W, col)", value)
	}
	n, err := strconv.Atoi(width)
	if err != nil || n <= 0 {
		return PartitionSpec{}, fmt.Errorf("invalid partition_by expression %q: %s needs a positive width", value, strings.ToLower(fn))
	}
	if strings.EqualFold(fn, "BUCKET") {
		return PartitionSpec{Column: column, Bucket: n}, nil
	}
	return PartitionSpec{Column: column, Truncate: n}, nil
}

// PartitionColumn returns the column a partition_by value refers to, or "" for an unrecognized expression.
func PartitionColumn(value string) string {
	if !IsPartitionExpression(value) {
		return strings.TrimSpace(value)
	}
	if start, end, ok := partitionColumnSpan(value); ok {
		return value[start:end]
	}
	return ""
}

// MapPartitionColumn rewrites the column inside a partition_by value, keeping any wrapping expression.
func MapPartitionColumn(value string, mapColumn func(string) string) string {
	if !IsPartitionExpression(value) {
		return mapColumn(strings.TrimSpace(value))
	}
	if start, end, ok := partitionColumnSpan(value); ok {
		return value[:start] + mapColumn(value[start:end]) + value[end:]
	}
	return value
}

func partitionColumnSpan(value string) (int, int, bool) {
	for _, pattern := range partitionPatterns {
		if idx := pattern.FindStringSubmatchIndex(value); idx != nil {
			col := 2 * pattern.SubexpIndex("col")
			return idx[col], idx[col+1], true
		}
	}
	return 0, 0, false
}
