package starrocks

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func quoteColumn(name string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}

// tlsParam maps a user-facing `ssl` value to go-sql-driver/mysql's `tls` DSN
// value (empty = plaintext). Mirrors the StarRocks source's handling.
func tlsParam(ssl string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(ssl)) {
	case "", "false", "0", "disable", "disabled":
		return "", nil
	case "true", "1", "enable", "enabled", "require", "required":
		return "true", nil
	case "skip-verify", "skip_verify", "insecure":
		return "skip-verify", nil
	default:
		return "", fmt.Errorf("invalid ssl value %q: use true, false, or skip-verify", ssl)
	}
}

func quoteColumns(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = quoteColumn(n)
	}
	return out
}

func quoteTable(table string) string {
	db, name := splitDatabaseTable(table)
	if db != "" {
		return quoteColumn(db) + "." + quoteColumn(name)
	}
	return quoteColumn(name)
}

func splitDatabaseTable(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", table
}

// recordBatchToJSON encodes an Arrow record batch as a JSON array of row
// objects (keyed by column name) for Stream Load. It returns the body, the row
// count, and the names of binary columns (hex-encoded in the body, so the
// loader must decode them server-side).
func recordBatchToJSON(record arrow.RecordBatch) ([]byte, int64, []string, error) {
	numRows := record.NumRows()
	if numRows == 0 {
		return nil, 0, nil, nil
	}
	numCols := int(record.NumCols())
	fields := record.Schema().Fields()

	var binaryCols []string
	for c := 0; c < numCols; c++ {
		if id := fields[c].Type.ID(); id == arrow.BINARY || id == arrow.LARGE_BINARY {
			binaryCols = append(binaryCols, fields[c].Name)
		}
	}

	rows := make([]map[string]interface{}, 0, numRows)
	for r := int64(0); r < numRows; r++ {
		row := make(map[string]interface{}, numCols)
		for c := 0; c < numCols; c++ {
			row[fields[c].Name] = extractValue(record.Column(c), int(r))
		}
		rows = append(rows, row)
	}

	body, err := json.Marshal(rows)
	if err != nil {
		return nil, 0, nil, err
	}
	return body, numRows, binaryCols, nil
}

// columnsHeader builds the Stream Load `columns` value that decodes hex
// binary columns back to their raw bytes, or "" when there are none. It lists
// every batch column in order, then a to_binary() transform for each binary
// column.
func columnsHeader(sc *arrow.Schema, binaryCols []string) string {
	if len(binaryCols) == 0 {
		return ""
	}
	fields := sc.Fields()
	parts := make([]string, 0, len(fields)+len(binaryCols))
	for _, f := range fields {
		parts = append(parts, quoteColumn(f.Name))
	}
	// Hex keeps empty values non-NULL; from_base64('') returns NULL.
	for _, name := range binaryCols {
		parts = append(parts, fmt.Sprintf("%s=to_binary(%s, 'hex')", quoteColumn(name), quoteColumn(name)))
	}
	return strings.Join(parts, ", ")
}

// extractValue converts an Arrow value to a JSON-serializable Go value that
// StarRocks Stream Load accepts (decimals/dates/timestamps as strings, JSON as
// raw JSON).
func extractValue(arr arrow.Array, idx int) interface{} {
	if arr.IsNull(idx) {
		return nil
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx)
	case *array.Int8:
		return a.Value(idx)
	case *array.Int16:
		return a.Value(idx)
	case *array.Int32:
		return a.Value(idx)
	case *array.Int64:
		return a.Value(idx)
	case *array.Float32:
		// Keep float32 so JSON uses its shortest 32-bit form (0.1, not
		// 0.10000000149011612).
		if v := a.Value(idx); !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) {
			return v
		}
		return nil
	case *array.Float64:
		return finiteOrNil(a.Value(idx))
	case *array.String:
		return a.Value(idx)
	case *array.LargeString:
		return a.Value(idx)
	case *array.Binary:
		// Hex keeps the bytes valid UTF-8 for JSON; the loader adds a
		// to_binary() transform so StarRocks stores the original bytes.
		return hex.EncodeToString(a.Value(idx))
	case *array.LargeBinary:
		return hex.EncodeToString(a.Value(idx))
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Time64:
		micros := int64(a.Value(idx))
		if a.DataType().(*arrow.Time64Type).Unit == arrow.Nanosecond {
			micros /= 1_000
		}
		secs := micros / 1_000_000
		return fmt.Sprintf("%02d:%02d:%02d.%06d", secs/3600, (secs%3600)/60, secs%60, micros%1_000_000)
	case *array.Timestamp:
		ts := a.Value(idx)
		return ts.ToTime(a.DataType().(*arrow.TimestampType).Unit).Format("2006-01-02 15:04:05.000000")
	case *array.Decimal128:
		return a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal128Type).Scale))
	case *array.Decimal256:
		return a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal256Type).Scale))
	case *array.List:
		start, end := a.ValueOffsets(idx)
		return listElements(a.ListValues(), int(start), int(end))
	case *array.LargeList:
		start, end := a.ValueOffsets(idx)
		return listElements(a.ListValues(), int(start), int(end))
	case array.ExtensionArray:
		storage := a.Storage()
		val := extractValue(storage, idx)
		// JSON columns must be embedded as raw JSON, not a quoted string.
		if a.DataType().(arrow.ExtensionType).ExtensionName() == schema.JSONExtensionName {
			if s, ok := val.(string); ok && json.Valid([]byte(s)) {
				return json.RawMessage(s)
			}
		}
		return val
	default:
		// ValueStr returns the value at idx for any Arrow type; never format the
		// whole array (which would corrupt every row for uncovered types).
		return arr.ValueStr(idx)
	}
}

// listElements converts an Arrow list slice into a Go slice so it serializes as
// a JSON array (not a quoted string) into a StarRocks JSON column.
func listElements(values arrow.Array, start, end int) []interface{} {
	out := make([]interface{}, 0, end-start)
	for j := start; j < end; j++ {
		// Binary inside JSON gets no to_binary transform; keep base64 as other
		// destinations do.
		switch b := values.(type) {
		case *array.Binary:
			if !b.IsNull(j) {
				out = append(out, base64.StdEncoding.EncodeToString(b.Value(j)))
				continue
			}
		case *array.LargeBinary:
			if !b.IsNull(j) {
				out = append(out, base64.StdEncoding.EncodeToString(b.Value(j)))
				continue
			}
		}
		out = append(out, extractValue(values, j))
	}
	return out
}

func mapStarRocksTypeToSchema(dataType string) schema.DataType {
	switch strings.ToLower(dataType) {
	case "boolean", "bool":
		return schema.TypeBoolean
	case "tinyint":
		return schema.TypeInt8
	case "smallint":
		return schema.TypeInt16
	case "int", "integer":
		return schema.TypeInt32
	case "bigint":
		return schema.TypeInt64
	case "largeint":
		return schema.TypeString
	case "float":
		return schema.TypeFloat32
	case "double":
		return schema.TypeFloat64
	case "decimal", "decimalv2", "decimal32", "decimal64", "decimal128":
		return schema.TypeDecimal
	case "char", "varchar", "string":
		return schema.TypeString
	case "varbinary", "binary":
		return schema.TypeBinary
	case "date":
		return schema.TypeDate
	case "datetime", "timestamp":
		return schema.TypeTimestamp
	case "json":
		return schema.TypeJSON
	default:
		return schema.TypeString
	}
}

var starRocksDecimalArgs = regexp.MustCompile(`\((\d+)\s*,\s*(\d+)\)`)

// mapStarRocksArrayType reads an array column's element from COLUMN_TYPE, e.g. array<DECIMAL64(5,2)>.
func mapStarRocksArrayType(columnType string) (elem schema.DataType, precision, scale int) {
	inner := strings.TrimSpace(columnType)
	if i, j := strings.Index(inner, "<"), strings.LastIndex(inner, ">"); i >= 0 && j > i {
		inner = strings.TrimSpace(inner[i+1 : j])
	}
	base := strings.ToLower(inner)
	if i := strings.IndexAny(base, "(<"); i >= 0 {
		base = base[:i]
	}
	if base == "array" {
		return schema.TypeArray, 0, 0
	}
	elem = mapStarRocksTypeToSchema(base)
	if m := starRocksDecimalArgs.FindStringSubmatch(inner); elem == schema.TypeDecimal && m != nil {
		precision, _ = strconv.Atoi(m[1])
		scale, _ = strconv.Atoi(m[2])
	}
	return elem, precision, scale
}

func mapStarRocksColumnType(dataType, columnType string) schema.DataType {
	if strings.EqualFold(columnType, "tinyint(1)") {
		return schema.TypeBoolean
	}
	return mapStarRocksTypeToSchema(dataType)
}

// StarRocks cannot store NaN or infinities, and JSON cannot encode them.
func finiteOrNil(v float64) any {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return v
}
