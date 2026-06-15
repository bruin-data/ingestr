package maxcompute

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aliyun/aliyun-odps-go-sdk/odps"
	"github.com/aliyun/aliyun-odps-go-sdk/odps/restclient"
	_ "github.com/aliyun/aliyun-odps-go-sdk/sqldriver"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/connredact"
	"github.com/bruin-data/ingestr/internal/maxcomputeutil"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/mattn/go-sqlite3"
)

type MaxComputeDestination struct {
	db         *sql.DB
	emulator   *sql.DB
	odps       *odps.Odps
	cfg        *odps.Config
	opts       maxcomputeutil.Options
	emulatorDB string
}

func NewMaxComputeDestination() *MaxComputeDestination {
	return &MaxComputeDestination{}
}

func (d *MaxComputeDestination) Schemes() []string {
	return []string{"maxcompute", "odps"}
}

func (d *MaxComputeDestination) Connect(ctx context.Context, rawURI string) error {
	cfg, opts, err := maxcomputeutil.ParseURI(rawURI)
	if err != nil {
		return fmt.Errorf("failed to parse MaxCompute URI: %w", connredact.Redact(rawURI, err))
	}

	db, err := sql.Open("odps", cfg.FormatDsn())
	if err != nil {
		return fmt.Errorf("failed to open MaxCompute connection: %w", connredact.Redact(rawURI, err))
	}

	odpsIns := cfg.GenOdps()
	if opts.Schema != "" {
		odpsIns.SetCurrentSchemaName(opts.Schema)
	}

	d.db = db
	d.odps = odpsIns
	d.cfg = cfg
	d.opts = opts
	d.emulatorDB = opts.EmulatorDBPath

	if opts.EmulatorDBPath != "" {
		emulatorDB, err := sql.Open("sqlite3", opts.EmulatorDBPath)
		if err != nil {
			_ = db.Close()
			return fmt.Errorf("failed to open MaxCompute emulator database: %w", err)
		}
		if _, err := emulatorDB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schemas(table_name TEXT PRIMARY KEY, schema TEXT)`); err != nil {
			_ = emulatorDB.Close()
			_ = db.Close()
			return fmt.Errorf("failed to initialize MaxCompute emulator schema table: %w", err)
		}
		d.emulator = emulatorDB
	}

	return nil
}

func (d *MaxComputeDestination) Close(ctx context.Context) error {
	var firstErr error
	if d.emulator != nil {
		firstErr = d.emulator.Close()
	}
	if d.db != nil {
		if err := d.db.Close(); firstErr == nil {
			firstErr = err
		}
	}
	_ = ctx
	return firstErr
}

func (d *MaxComputeDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if d.emulator != nil {
		return d.prepareEmulatorTable(ctx, opts)
	}

	if opts.DropFirst {
		if err := d.DropTable(ctx, opts.Table); err != nil {
			return err
		}
	}
	if opts.Schema == nil {
		return nil
	}
	createSQL := buildCreateTableSQL(opts.Table, opts.Schema.Columns)
	if err := d.executeSQL(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create MaxCompute table: %w", err)
	}
	return nil
}

func (d *MaxComputeDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *MaxComputeDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	for result := range records {
		if result.Err != nil {
			return result.Err
		}
		if result.Batch == nil {
			continue
		}

		var err error
		if d.emulator != nil {
			_, err = d.writeEmulatorRecordBatch(ctx, result.Batch, opts.Table)
		} else {
			_, err = d.writeRecordBatch(ctx, result.Batch, opts.Table)
		}
		result.Batch.Release()
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *MaxComputeDestination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	if record.NumRows() == 0 {
		return 0, nil
	}

	colNames := make([]string, int(record.NumCols()))
	for i := range colNames {
		colNames[i] = maxcomputeutil.QuoteIdentifier(record.Schema().Field(i).Name)
	}

	const maxRowsPerInsert = 500
	var written int64
	for start := int64(0); start < record.NumRows(); start += maxRowsPerInsert {
		end := start + maxRowsPerInsert
		if end > record.NumRows() {
			end = record.NumRows()
		}

		rows := make([]string, 0, end-start)
		for rowIdx := start; rowIdx < end; rowIdx++ {
			values := make([]string, int(record.NumCols()))
			for colIdx := 0; colIdx < int(record.NumCols()); colIdx++ {
				values[colIdx] = sqlLiteral(extractValue(record.Column(colIdx), int(rowIdx)))
			}
			rows = append(rows, "("+strings.Join(values, ", ")+")")
		}

		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES %s",
			maxcomputeutil.QuoteTable(table),
			strings.Join(colNames, ", "),
			strings.Join(rows, ", "),
		)
		if err := d.executeSQL(ctx, insertSQL); err != nil {
			config.LogFailedQuery(insertSQL, err)
			return written, fmt.Errorf("failed to insert into MaxCompute table %s: %w", table, err)
		}
		written += end - start
	}
	return written, nil
}

func (d *MaxComputeDestination) DropTable(ctx context.Context, table string) error {
	if d.emulator != nil {
		physical := emulatorPhysicalTable(table)
		if _, err := d.emulator.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteSQLiteIdentifier(physical))); err != nil {
			return fmt.Errorf("failed to drop MaxCompute emulator table %s: %w", table, err)
		}
		_, err := d.emulator.ExecContext(ctx, "DELETE FROM schemas WHERE table_name = ?", physical)
		return err
	}

	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", maxcomputeutil.QuoteTable(table))
	if err := d.executeSQL(ctx, dropSQL); err != nil {
		config.LogFailedQuery(dropSQL, err)
		return fmt.Errorf("failed to drop MaxCompute table %s: %w", table, err)
	}
	return nil
}

func (d *MaxComputeDestination) TruncateTable(ctx context.Context, table string) error {
	if d.emulator != nil {
		_, err := d.emulator.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", quoteSQLiteIdentifier(emulatorPhysicalTable(table))))
		return err
	}
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", maxcomputeutil.QuoteTable(table))
	if err := d.executeSQL(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate MaxCompute table %s: %w", table, err)
	}
	return nil
}

func (d *MaxComputeDestination) Exec(ctx context.Context, sqlText string, args ...interface{}) error {
	if d.emulator != nil {
		_, err := d.emulator.ExecContext(ctx, sqlText, args...)
		return err
	}
	if len(args) > 0 {
		_, err := d.db.ExecContext(ctx, sqlText, args...)
		if err != nil {
			config.LogFailedQuery(sqlText, err)
		}
		return err
	}
	if err := d.executeSQL(ctx, sqlText); err != nil {
		config.LogFailedQuery(sqlText, err)
		return err
	}
	return nil
}

func (d *MaxComputeDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	if d.emulator != nil {
		tx, err := d.emulator.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		return &sqlTransaction{tx: tx}, nil
	}
	_ = ctx
	return nil, errors.New("MaxCompute destination does not support transactions")
}

func (d *MaxComputeDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	if d.emulator != nil {
		return d.getEmulatorTableSchema(ctx, table)
	}

	schemaName, tableName := maxcomputeutil.SplitSchemaTable(table, d.opts.Schema)
	t := d.odps.Table(tableName)
	if schemaName != "" {
		t = d.odps.Schema(schemaName).Tables().Get(tableName)
	}
	if err := t.Load(); err != nil {
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load MaxCompute table schema: %w", err)
	}

	tableInfo := t.Schema()
	columns := make([]schema.Column, 0, len(tableInfo.Columns))
	for _, c := range tableInfo.Columns {
		dt, precision, scale, arrayType := maxcomputeutil.MapSDKType(c.Type)
		columns = append(columns, schema.Column{
			Name:      c.Name,
			DataType:  dt,
			Nullable:  !c.NotNull,
			Precision: precision,
			Scale:     scale,
			ArrayType: arrayType,
		})
	}
	return &schema.TableSchema{
		Name:        tableName,
		Schema:      d.opts.Schema,
		Columns:     columns,
		PrimaryKeys: t.PrimaryKeys(),
	}, nil
}

func (d *MaxComputeDestination) GetScheme() string { return "maxcompute" }

func (d *MaxComputeDestination) SupportsReplaceStrategy() bool      { return true }
func (d *MaxComputeDestination) SupportsAppendStrategy() bool       { return true }
func (d *MaxComputeDestination) SupportsMergeStrategy() bool        { return false }
func (d *MaxComputeDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *MaxComputeDestination) SupportsSCD2Strategy() bool         { return false }
func (d *MaxComputeDestination) SupportsAtomicSwap() bool           { return false }

func (d *MaxComputeDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	_ = ctx
	_ = opts
	return errors.New("MaxCompute destination does not support atomic table swap")
}

func (d *MaxComputeDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	_ = ctx
	_ = opts
	return errors.New("MaxCompute destination does not support merge strategy")
}

func (d *MaxComputeDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	_ = ctx
	_ = opts
	return errors.New("MaxCompute destination does not support delete+insert strategy")
}

func (d *MaxComputeDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	_ = ctx
	_ = opts
	return errors.New("MaxCompute destination does not support SCD2 strategy")
}

func (d *MaxComputeDestination) executeSQL(ctx context.Context, sqlText string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ins, err := d.odps.ExecSQl(sqlText, d.cfg.Hints)
	if err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- ins.WaitForSuccess()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if err := ins.Terminate(); err != nil {
			return fmt.Errorf("context canceled while executing MaxCompute SQL: %w; failed to terminate instance: %v", ctx.Err(), err)
		}
		return ctx.Err()
	}
}

func buildCreateTableSQL(table string, columns []schema.Column) string {
	defs := make([]string, len(columns))
	for i, col := range columns {
		defs[i] = fmt.Sprintf("%s %s", maxcomputeutil.QuoteIdentifier(col.Name), MapDataTypeToMaxCompute(col))
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)", maxcomputeutil.QuoteTable(table), strings.Join(defs, ",\n  "))
}

func (d *MaxComputeDestination) prepareEmulatorTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.DropFirst {
		if err := d.DropTable(ctx, opts.Table); err != nil {
			return err
		}
	}
	if opts.Schema == nil {
		return nil
	}

	physical := emulatorPhysicalTable(opts.Table)
	colDefs := make([]string, len(opts.Schema.Columns))
	emulatorColumns := make([]emulatorColumn, len(opts.Schema.Columns))
	for i, col := range opts.Schema.Columns {
		colDefs[i] = fmt.Sprintf("%s %s", quoteSQLiteIdentifier(col.Name), sqliteTypeForEmulator(col))
		emulatorColumns[i] = emulatorColumn{
			Name:         col.Name,
			Type:         emulatorSchemaType(col),
			NotNull:      !col.Nullable,
			PrimaryKey:   containsFold(opts.PrimaryKeys, col.Name),
			PartitionKey: false,
		}
	}

	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", quoteSQLiteIdentifier(physical), strings.Join(colDefs, ", "))
	if _, err := d.emulator.ExecContext(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create MaxCompute emulator table: %w", err)
	}

	schemaJSON, err := json.Marshal(emulatorSchema{Columns: emulatorColumns, PartitionColumns: []emulatorColumn{}})
	if err != nil {
		return err
	}
	if _, err := d.emulator.ExecContext(ctx, "DELETE FROM schemas WHERE table_name = ?", physical); err != nil {
		return fmt.Errorf("failed to replace MaxCompute emulator schema: %w", err)
	}
	_, err = d.emulator.ExecContext(ctx, "INSERT INTO schemas(table_name, schema) VALUES(?, ?)", physical, string(schemaJSON))
	if err != nil {
		return fmt.Errorf("failed to register MaxCompute emulator schema: %w", err)
	}
	return nil
}

func (d *MaxComputeDestination) writeEmulatorRecordBatch(ctx context.Context, record arrow.RecordBatch, table string) (int64, error) {
	if record.NumRows() == 0 {
		return 0, nil
	}

	colNames := make([]string, int(record.NumCols()))
	placeholders := make([]string, int(record.NumCols()))
	for i := 0; i < int(record.NumCols()); i++ {
		colNames[i] = quoteSQLiteIdentifier(record.Schema().Field(i).Name)
		placeholders[i] = "?"
	}
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		quoteSQLiteIdentifier(emulatorPhysicalTable(table)),
		strings.Join(colNames, ", "),
		strings.Join(placeholders, ", "),
	)

	tx, err := d.emulator.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	defer func() { _ = stmt.Close() }()

	for rowIdx := int64(0); rowIdx < record.NumRows(); rowIdx++ {
		values := make([]interface{}, int(record.NumCols()))
		for colIdx := 0; colIdx < int(record.NumCols()); colIdx++ {
			values[colIdx] = extractValue(record.Column(colIdx), int(rowIdx))
		}
		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			_ = tx.Rollback()
			config.LogFailedQuery(insertSQL, err)
			return rowIdx, fmt.Errorf("failed to insert MaxCompute emulator row %d: %w", rowIdx, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return record.NumRows(), nil
}

func (d *MaxComputeDestination) getEmulatorTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	physical := emulatorPhysicalTable(table)
	var schemaText string
	err := d.emulator.QueryRowContext(ctx, "SELECT schema FROM schemas WHERE table_name = ?", physical).Scan(&schemaText)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var emulator emulatorSchema
	if err := json.Unmarshal([]byte(schemaText), &emulator); err != nil {
		return nil, err
	}

	columns := make([]schema.Column, 0, len(emulator.Columns))
	primaryKeys := make([]string, 0)
	for _, col := range emulator.Columns {
		dt, precision, scale, arrayType := maxcomputeutil.MapMaxComputeType(col.Type)
		columns = append(columns, schema.Column{
			Name:         col.Name,
			DataType:     dt,
			Nullable:     !col.NotNull,
			Precision:    precision,
			Scale:        scale,
			ArrayType:    arrayType,
			IsPrimaryKey: col.PrimaryKey,
		})
		if col.PrimaryKey {
			primaryKeys = append(primaryKeys, col.Name)
		}
	}
	return &schema.TableSchema{
		Name:        physical,
		Schema:      d.opts.Schema,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
}

type emulatorSchema struct {
	Columns          []emulatorColumn `json:"columns"`
	PartitionColumns []emulatorColumn `json:"partitionColumns"`
}

type emulatorColumn struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	NotNull      bool   `json:"notNull"`
	DefaultValue string `json:"defaultValue,omitempty"`
	PrimaryKey   bool   `json:"primaryKey"`
	PartitionKey bool   `json:"partitionKey"`
}

func sqliteTypeForEmulator(col schema.Column) string {
	switch col.DataType {
	case schema.TypeBoolean, schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		return "INTEGER"
	case schema.TypeFloat32, schema.TypeFloat64:
		return "REAL"
	case schema.TypeBinary:
		return "BLOB"
	default:
		return "TEXT"
	}
}

func emulatorSchemaType(col schema.Column) string {
	switch col.DataType {
	case schema.TypeString, schema.TypeUUID, schema.TypeJSON, schema.TypeArray, schema.TypeInterval:
		return "STRING"
	case schema.TypeTime:
		return "STRING"
	case schema.TypeDecimal:
		return "DECIMAL"
	default:
		return MapDataTypeToMaxCompute(col)
	}
}

func emulatorPhysicalTable(table string) string {
	_, tableName := maxcomputeutil.SplitSchemaTable(table, "")
	return strings.ToUpper(tableName)
}

func quoteSQLiteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func sqlLiteral(value interface{}) string {
	if value == nil {
		return "NULL"
	}
	switch v := value.(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	case []byte:
		return "X'" + hex.EncodeToString(v) + "'"
	case time.Time:
		return "'" + v.Format("2006-01-02 15:04:05.000000") + "'"
	default:
		return fmt.Sprintf("%v", v)
	}
}

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
		return a.Value(idx)
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Time64:
		timeType := a.DataType().(*arrow.Time64Type)
		return formatTime64(a.Value(idx), timeType.Unit)
	case *array.Timestamp:
		ts := a.Value(idx)
		return ts.ToTime(a.DataType().(*arrow.TimestampType).Unit).Format("2006-01-02 15:04:05.000000")
	case *array.Decimal128:
		return a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal128Type).Scale))
	case array.ExtensionArray:
		return extractValue(a.Storage(), idx)
	default:
		return arr.ValueStr(idx)
	}
}

func formatTime64(value arrow.Time64, unit arrow.TimeUnit) string {
	var duration time.Duration
	switch unit {
	case arrow.Microsecond:
		duration = time.Duration(value) * time.Microsecond
	case arrow.Nanosecond:
		duration = time.Duration(value) * time.Nanosecond
	default:
		return fmt.Sprintf("%v", value)
	}

	hours := int(duration / time.Hour)
	duration %= time.Hour
	mins := int(duration / time.Minute)
	duration %= time.Minute
	secs := int(duration / time.Second)
	duration %= time.Second
	micros := int(duration / time.Microsecond)
	return fmt.Sprintf("%02d:%02d:%02d.%06d", hours, mins, secs, micros)
}

func isNotFoundError(err error) bool {
	var httpErr restclient.HttpError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound
}

func containsFold(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(value, needle) {
			return true
		}
	}
	return false
}

type sqlTransaction struct {
	tx *sql.Tx
}

func (t *sqlTransaction) Exec(ctx context.Context, sqlText string, args ...interface{}) error {
	_, err := t.tx.ExecContext(ctx, sqlText, args...)
	return err
}

func (t *sqlTransaction) Commit(ctx context.Context) error {
	_ = ctx
	return t.tx.Commit()
}

func (t *sqlTransaction) Rollback(ctx context.Context) error {
	_ = ctx
	return t.tx.Rollback()
}
