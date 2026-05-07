package athena

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
)

var errTableNotFound = errors.New("athena table not found")

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateIdent(ident string) error {
	if ident == "" {
		return errors.New("identifier is empty")
	}
	if !identRe.MatchString(ident) {
		return fmt.Errorf("invalid identifier %q (allowed: [A-Za-z_][A-Za-z0-9_]*)", ident)
	}
	return nil
}

func formatQualifiedTable(database, table string) (string, error) {
	if err := validateIdent(database); err != nil {
		return "", fmt.Errorf("invalid database name: %w", err)
	}
	if err := validateIdent(table); err != nil {
		return "", fmt.Errorf("invalid table name: %w", err)
	}
	return database + "." + table, nil
}

func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func buildCreateIcebergTableSQL(database, table string, columns []schema.Column, location string) (string, error) {
	qualified, err := formatQualifiedTable(database, table)
	if err != nil {
		return "", err
	}

	selectCols := make([]string, 0, len(columns))
	for _, c := range columns {
		if err := validateIdent(c.Name); err != nil {
			return "", fmt.Errorf("invalid column name %q: %w", c.Name, err)
		}
		selectCols = append(selectCols, fmt.Sprintf("CAST(NULL AS %s) AS %s", athenaCastTypeForColumn(c), c.Name))
	}

	// For Iceberg tables, Athena uses CTAS with table properties. `is_external=false` is required
	// in some environments (managed Iceberg). We create an empty table by selecting NULLs.
	return fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s WITH (table_type='ICEBERG', format='PARQUET', is_external=false, location='%s', write_compression='SNAPPY') AS SELECT %s WHERE 1=0",
		qualified,
		escapeSQLString(location),
		strings.Join(selectCols, ", "),
	), nil
}

func athenaCastTypeForColumn(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean:
		return "boolean"
	case schema.TypeInt16:
		return "smallint"
	case schema.TypeInt32:
		return "integer"
	case schema.TypeInt64:
		return "bigint"
	case schema.TypeFloat32:
		return "real"
	case schema.TypeFloat64:
		return "double"
	case schema.TypeDecimal:
		p := col.Precision
		if p == 0 {
			p = 38
		}
		return fmt.Sprintf("decimal(%d,%d)", p, col.Scale)
	case schema.TypeBinary:
		return "varbinary"
	case schema.TypeDate:
		return "date"
	case schema.TypeTime:
		return "time"
	case schema.TypeTimestamp:
		return "timestamp"
	case schema.TypeTimestampTZ:
		return "timestamp with time zone"
	case schema.TypeArray:
		elem := schema.Column{DataType: col.ArrayType, Precision: col.Precision, Scale: col.Scale}
		return "array(" + athenaCastTypeForColumn(elem) + ")"
	case schema.TypeJSON, schema.TypeUUID, schema.TypeString, schema.TypeUnknown:
		fallthrough
	default:
		// Trino uses varchar as unbounded string type.
		return "varchar"
	}
}

func buildInsertSQL(database, table string, colNames []string, rec arrow.RecordBatch, startRow, endRow int) (string, error) {
	if startRow >= endRow {
		return "", errors.New("empty insert range")
	}
	if int(rec.NumCols()) != len(colNames) {
		return "", errors.New("column name count mismatch")
	}

	qualified, err := formatQualifiedTable(database, table)
	if err != nil {
		return "", err
	}
	for _, c := range colNames {
		if err := validateIdent(c); err != nil {
			return "", fmt.Errorf("invalid column name %q: %w", c, err)
		}
	}

	var b strings.Builder
	b.Grow((endRow - startRow) * 64)

	b.WriteString("INSERT INTO ")
	b.WriteString(qualified)
	b.WriteString(" (")
	b.WriteString(strings.Join(colNames, ", "))
	b.WriteString(") VALUES ")

	for row := startRow; row < endRow; row++ {
		if row > startRow {
			b.WriteString(", ")
		}
		b.WriteByte('(')
		for colIdx := 0; colIdx < int(rec.NumCols()); colIdx++ {
			if colIdx > 0 {
				b.WriteString(", ")
			}
			arr := rec.Column(colIdx)
			lit, err := arrowLiteral(arr, rec.Schema().Field(colIdx).Type, row)
			if err != nil {
				return "", err
			}
			b.WriteString(lit)
		}
		b.WriteByte(')')
	}

	return b.String(), nil
}

func arrowLiteral(arr arrow.Array, dt arrow.DataType, idx int) (string, error) {
	if arr == nil || arr.IsNull(idx) {
		return "NULL", nil
	}

	// Unwrap extension types (e.g. JSON).
	if ext, ok := arr.(array.ExtensionArray); ok {
		return arrowLiteral(ext.Storage(), ext.Storage().DataType(), idx)
	}

	switch a := arr.(type) {
	case interface{ Value(int) bool }:
		if a.Value(idx) {
			return "TRUE", nil
		}
		return "FALSE", nil
	case interface{ Value(int) int16 }:
		return strconv.FormatInt(int64(a.Value(idx)), 10), nil
	case interface{ Value(int) int32 }:
		return strconv.FormatInt(int64(a.Value(idx)), 10), nil
	case interface{ Value(int) int64 }:
		return strconv.FormatInt(a.Value(idx), 10), nil
	case interface{ Value(int) float32 }:
		return strconv.FormatFloat(float64(a.Value(idx)), 'g', -1, 32), nil
	case interface{ Value(int) float64 }:
		return strconv.FormatFloat(a.Value(idx), 'g', -1, 64), nil
	case interface{ Value(int) string }:
		return quoteString(a.Value(idx)), nil
	case interface{ Value(int) []byte }:
		return quoteVarbinary(a.Value(idx)), nil
	case *array.Decimal128:
		v := a.Value(idx)
		dt := a.DataType().(*arrow.Decimal128Type)
		return v.ToString(dt.Scale), nil
	case *array.Date32:
		t := a.Value(idx).ToTime()
		return "DATE " + quoteString(t.Format("2006-01-02")), nil
	case *array.Time64:
		micros := int64(a.Value(idx))
		h := micros / 3600000000
		micros %= 3600000000
		m := micros / 60000000
		micros %= 60000000
		s := micros / 1000000
		micros %= 1000000
		t := time.Date(0, 1, 1, int(h), int(m), int(s), int(micros)*1000, time.UTC)
		return "TIME " + quoteString(t.Format("15:04:05.999999")), nil
	case *array.Timestamp:
		t := a.Value(idx).ToTime(arrow.Microsecond).UTC()
		if tsType, ok := dt.(*arrow.TimestampType); ok && tsType.TimeZone != "" {
			// Preserve timezone-aware semantics for Iceberg `timestamp with time zone`.
			return "from_iso8601_timestamp(" + quoteString(t.Format(time.RFC3339Nano)) + ")", nil
		}
		return "TIMESTAMP " + quoteString(t.Format("2006-01-02 15:04:05.999999")), nil
	case *array.List:
		start, end := a.ValueOffsets(idx)
		values := a.ListValues()
		var b strings.Builder
		b.WriteString("ARRAY[")
		for i := int(start); i < int(end); i++ {
			if i > int(start) {
				b.WriteString(", ")
			}
			lit, err := arrowLiteral(values, values.DataType(), i)
			if err != nil {
				return "", err
			}
			b.WriteString(lit)
		}
		b.WriteString("]")
		return b.String(), nil
	default:
		// Best-effort: try formatting decimal128 stored as binary string etc.
		switch dt.ID() {
		case arrow.DECIMAL128:
			if decArr, ok := arr.(*array.Decimal128); ok {
				v := decArr.Value(idx)
				dt := decArr.DataType().(*arrow.Decimal128Type)
				return v.ToString(dt.Scale), nil
			}
		}
	}

	return "NULL", nil
}

func quoteString(s string) string {
	return "'" + escapeSQLString(s) + "'"
}

func quoteVarbinary(b []byte) string {
	return "X'" + hex.EncodeToString(b) + "'"
}
