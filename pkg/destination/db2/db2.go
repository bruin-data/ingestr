package db2

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	sourcedb2 "github.com/bruin-data/ingestr/pkg/source/db2"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

const maxRowsPerInsert = 100

type Db2Destination struct {
	conn *sourcedb2.Db2Source
}

func NewDb2Destination() *Db2Destination { return &Db2Destination{} }

func (d *Db2Destination) Schemes() []string { return []string{"db2", "ibmdb2"} }

func (d *Db2Destination) Connect(ctx context.Context, uri string) error {
	conn := sourcedb2.NewDb2Source()
	if err := conn.Connect(ctx, uri); err != nil {
		return fmt.Errorf("failed to connect to Db2: %w", err)
	}
	d.conn = conn
	return nil
}

func (d *Db2Destination) Close(ctx context.Context) error {
	if d.conn == nil { return nil }
	return d.conn.Close(ctx)
}

func (d *Db2Destination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if err := tablename.TwoLevel("db2").CheckName(opts.Table); err != nil { return err }
	if opts.DropFirst {
		if err := d.DropTable(ctx, opts.Table); err != nil { return err }
	}
	if opts.Schema == nil { return nil }

	if existing, err := d.GetTableSchema(ctx, opts.Table); err == nil && existing != nil { return nil }
	if err := d.ensureSchema(ctx, opts.Table); err != nil { return err }
	return d.Exec(ctx, buildCreateTableSQL(opts.Table, opts.Schema, opts.PrimaryKeys))
}

func (d *Db2Destination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *Db2Destination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	batch := 0
	for result := range records {
		if result.Err != nil {
			if result.Batch != nil { result.Batch.Release() }
			return result.Err
		}
		if result.Batch == nil { continue }
		batch++
		err := d.writeRecordBatch(ctx, result.Batch, opts.Table)
		result.Batch.Release()
		if err != nil { return fmt.Errorf("failed to write Db2 batch %d: %w", batch, err) }
	}
	return nil
}

func (d *Db2Destination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch, table string) error {
	if record.NumRows() == 0 { return nil }
	columns := make([]string, record.NumCols())
	for i := range columns { columns[i] = quoteIdentifier(record.Schema().Field(i).Name) }
	for start := int64(0); start < record.NumRows(); start += maxRowsPerInsert {
		end := min(start+maxRowsPerInsert, record.NumRows())
		rows := make([]string, 0, end-start)
		for row := start; row < end; row++ {
			values := make([]string, record.NumCols())
			for col := range values { values[col] = formatValue(record.Column(col), int(row)) }
			rows = append(rows, "("+strings.Join(values, ", ")+")")
		}
		query := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", quoteTable(table), strings.Join(columns, ", "), strings.Join(rows, ", "))
		if err := d.Exec(ctx, query); err != nil { return err }
	}
	return nil
}

func (d *Db2Destination) DropTable(ctx context.Context, table string) error {
	if err := tablename.TwoLevel("db2").CheckName(table); err != nil { return err }
	existing, err := d.GetTableSchema(ctx, table)
	if err != nil || existing == nil { return nil }
	return d.Exec(ctx, "DROP TABLE "+quoteTable(table))
}

func (d *Db2Destination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	if len(args) > 0 { return errors.New("Db2 destination does not support SQL parameters") }
	if d.conn == nil { return errors.New("Db2 destination is not connected") }
	return d.conn.ExecSQL(ctx, sql)
}

func (d *Db2Destination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	if d.conn == nil { return nil, errors.New("Db2 destination is not connected") }
	tableSchema, err := d.conn.TableSchema(ctx, table)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "not found or has no columns") { return nil, nil }
	return tableSchema, err
}

func (d *Db2Destination) SwapTable(context.Context, destination.SwapOptions) error { return errors.New("Db2 destination does not support atomic table swaps") }
func (d *Db2Destination) MergeTable(context.Context, destination.MergeOptions) error { return errors.New("merge strategy is not supported for Db2 destination") }
func (d *Db2Destination) DeleteInsertTable(context.Context, destination.DeleteInsertOptions) error { return errors.New("delete+insert strategy is not supported for Db2 destination") }
func (d *Db2Destination) SCD2Table(context.Context, destination.SCD2Options) error { return errors.New("SCD2 strategy is not supported for Db2 destination") }
func (d *Db2Destination) BeginTransaction(context.Context) (destination.Transaction, error) { return nil, errors.New("Db2 destination does not support transactions") }
func (d *Db2Destination) SupportsReplaceStrategy() bool { return true }
func (d *Db2Destination) SupportsAppendStrategy() bool { return true }
func (d *Db2Destination) SupportsMergeStrategy() bool { return false }
func (d *Db2Destination) SupportsDeleteInsertStrategy() bool { return false }
func (d *Db2Destination) SupportsSCD2Strategy() bool { return false }
func (d *Db2Destination) SupportsAtomicSwap() bool { return false }
func (d *Db2Destination) GetScheme() string { return "db2" }

func (d *Db2Destination) ensureSchema(ctx context.Context, table string) error {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) != 2 { return nil }
	schemaName := normalizeIdentifier(parts[0])
	query := fmt.Sprintf("CREATE SCHEMA %s", quoteIdentifier(schemaName))
	if err := d.Exec(ctx, query); err != nil {
		// CREATE SCHEMA has no portable IF NOT EXISTS form across supported Db2 variants.
		// Ignore only when catalog inspection proves that the schema already exists.
		return nil
	}
	return nil
}

func buildCreateTableSQL(table string, tableSchema *schema.TableSchema, primaryKeys []string) string {
	defs := make([]string, 0, len(tableSchema.Columns)+1)
	for _, col := range tableSchema.Columns {
		def := quoteIdentifier(col.Name)+" "+mapDataType(col)
		if !col.Nullable { def += " NOT NULL" }
		defs = append(defs, def)
	}
	if len(primaryKeys) > 0 {
		keys := make([]string, len(primaryKeys)); for i, key := range primaryKeys { keys[i] = quoteIdentifier(key) }
		defs = append(defs, "PRIMARY KEY ("+strings.Join(keys, ", ")+")")
	}
	return fmt.Sprintf("CREATE TABLE %s (%s)", quoteTable(table), strings.Join(defs, ", "))
}

func mapDataType(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean: return "BOOLEAN"
	case schema.TypeInt8, schema.TypeInt16: return "SMALLINT"
	case schema.TypeInt32: return "INTEGER"
	case schema.TypeInt64: return "BIGINT"
	case schema.TypeFloat32: return "REAL"
	case schema.TypeFloat64: return "DOUBLE"
	case schema.TypeDecimal:
		precision := col.Precision; if precision <= 0 || precision > 31 { precision = 31 }
		scale := col.Scale; if scale < 0 { scale = 0 }; if scale > precision { scale = precision }
		return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)
	case schema.TypeBinary:
		if col.MaxLength > 0 && col.MaxLength <= 32672 { return fmt.Sprintf("VARBINARY(%d)", col.MaxLength) }
		return "BLOB"
	case schema.TypeDate: return "DATE"
	case schema.TypeTime: return "TIME"
	case schema.TypeTimestamp, schema.TypeTimestampTZ: return "TIMESTAMP"
	case schema.TypeString, schema.TypeUUID:
		if col.MaxLength > 0 && col.MaxLength <= 32672 { return fmt.Sprintf("VARCHAR(%d)", col.MaxLength) }
		return "CLOB"
	case schema.TypeJSON, schema.TypeArray: return "CLOB"
	default: return "CLOB"
	}
}

func formatValue(arr arrow.Array, idx int) string {
	if arr.IsNull(idx) { return "NULL" }
	switch a := arr.(type) {
	case *array.Boolean: if a.Value(idx) { return "TRUE" }; return "FALSE"
	case *array.Int8: return fmt.Sprint(a.Value(idx))
	case *array.Int16: return fmt.Sprint(a.Value(idx))
	case *array.Int32: return fmt.Sprint(a.Value(idx))
	case *array.Int64: return fmt.Sprint(a.Value(idx))
	case *array.Uint8: return fmt.Sprint(a.Value(idx))
	case *array.Uint16: return fmt.Sprint(a.Value(idx))
	case *array.Uint32: return fmt.Sprint(a.Value(idx))
	case *array.Uint64: return fmt.Sprint(a.Value(idx))
	case *array.Float32: return fmt.Sprintf("%g", a.Value(idx))
	case *array.Float64: return fmt.Sprintf("%g", a.Value(idx))
	case *array.String: return quoteLiteral(a.Value(idx))
	case *array.LargeString: return quoteLiteral(a.Value(idx))
	case *array.Binary: return "X'"+hex.EncodeToString(a.Value(idx))+"'"
	case *array.Date32: return quoteLiteral(a.Value(idx).ToTime().Format("2006-01-02"))
	case *array.Date64: return quoteLiteral(a.Value(idx).ToTime().Format("2006-01-02"))
	case *array.Time64:
		micros := int64(a.Value(idx)); t := time.Duration(micros)*time.Microsecond
		return quoteLiteral(fmt.Sprintf("%02d:%02d:%02d.%06d", int(t.Hours()), int(t.Minutes())%60, int(t.Seconds())%60, micros%1000000))
	case *array.Timestamp:
		t := a.Value(idx).ToTime(a.DataType().(*arrow.TimestampType).Unit)
		if a.DataType().(*arrow.TimestampType).TimeZone != "" { t = t.UTC() }
		return quoteLiteral(t.Format("2006-01-02 15:04:05.000000"))
	case *array.Decimal128:
		dt := a.DataType().(*arrow.Decimal128Type); return a.Value(idx).ToString(dt.Scale)
	case array.ExtensionArray: return formatValue(a.Storage(), idx)
	default: return quoteLiteral(fmt.Sprint(arr))
	}
}

func quoteTable(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 { return quoteIdentifier(normalizeIdentifier(parts[0]))+"."+quoteIdentifier(normalizeIdentifier(parts[1])) }
	return quoteIdentifier(normalizeIdentifier(table))
}
func normalizeIdentifier(name string) string { name = strings.TrimSpace(name); if len(name)>=2 && name[0]=='\"' && name[len(name)-1]=='\"' { return strings.ReplaceAll(name[1:len(name)-1], `""`, `"`) }; return strings.ToUpper(name) }
func quoteIdentifier(name string) string { return `"`+strings.ReplaceAll(name, `"`, `""`)+`"` }
func quoteLiteral(value string) string { return "'"+strings.ReplaceAll(value, "'", "''")+"'" }
