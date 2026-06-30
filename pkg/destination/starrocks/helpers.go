package starrocks

import (
	"encoding/json"
	"fmt"
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
// objects (keyed by column name) for Stream Load. Returns the body and the row
// count.
func recordBatchToJSON(record arrow.RecordBatch) ([]byte, int64, error) {
	numRows := record.NumRows()
	if numRows == 0 {
		return nil, 0, nil
	}
	numCols := int(record.NumCols())
	fields := record.Schema().Fields()

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
		return nil, 0, err
	}
	return body, numRows, nil
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
		return a.Value(idx)
	case *array.Float64:
		return a.Value(idx)
	case *array.String:
		return a.Value(idx)
	case *array.LargeString:
		return a.Value(idx)
	case *array.Binary:
		return string(a.Value(idx))
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

func mapStarRocksTypeToSchema(dataType string) schema.DataType {
	switch strings.ToLower(dataType) {
	case "boolean", "bool":
		return schema.TypeBoolean
	case "tinyint", "smallint":
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
