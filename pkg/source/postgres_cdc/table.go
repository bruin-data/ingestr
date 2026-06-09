package postgres_cdc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/postgres"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FormatLSN formats an LSN as zero-padded hex (e.g. "00000000/0001FA40").
// This ensures correct lexicographic ordering when stored as strings,
// which is critical for SQL MAX(_cdc_lsn) queries in destinations.
func FormatLSN(lsn pglogrepl.LSN) string {
	return fmt.Sprintf("%08X/%08X", uint32(lsn>>32), uint32(lsn))
}

type CDCTable struct {
	source      *PostgresCDCSource
	tableName   string
	primaryKeys []string
	strategy    config.IncrementalStrategy
	tableSchema *schema.TableSchema
}

func NewCDCTable(src *PostgresCDCSource, req source.TableRequest) (*CDCTable, error) {
	ctx := context.Background()

	// Fetch schema from database
	tableSchema, err := getTableSchema(ctx, src.queryPool, req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	// Add CDC metadata columns to schema
	tableSchema = addCDCColumns(tableSchema)

	// Auto-detect primary keys from database if user didn't provide
	pks := req.PrimaryKeys
	if len(pks) == 0 {
		pks = tableSchema.PrimaryKeys
	}

	// CDC requires merge strategy with primary keys
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyMerge
	}

	return &CDCTable{
		source:      src,
		tableName:   req.Name,
		primaryKeys: pks,
		strategy:    strategy,
		tableSchema: tableSchema,
	}, nil
}

func (t *CDCTable) Name() string {
	return t.tableName
}

func (t *CDCTable) PrimaryKeys() []string {
	return t.primaryKeys
}

func (t *CDCTable) IncrementalKey() string {
	return ""
}

func (t *CDCTable) Strategy() config.IncrementalStrategy {
	return t.strategy
}

func (t *CDCTable) HasKnownSchema() bool {
	return true
}

func (t *CDCTable) GetSchema(ctx context.Context) (*schema.TableSchema, error) {
	return t.tableSchema, nil
}

func (t *CDCTable) Read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTotal := time.Now()
	config.Debug("[CDC] Starting CDC read from %s", t.tableName)

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		// Create the CDC reader
		reader := NewCDCReader(t.source, t.tableName, t.tableSchema, t.source.cdcConfig)

		// Start reading
		batches, err := reader.Read(ctx, opts)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to start CDC read: %w", err)}
			return
		}

		batchNum := 0
		var totalRows int64

		for batch := range batches {
			if batch.Err != nil {
				results <- batch
				return
			}

			batchNum++
			totalRows += batch.Batch.NumRows()
			config.Debug("[CDC] Batch %d: %d rows (total: %d)", batchNum, batch.Batch.NumRows(), totalRows)

			results <- batch
		}

		config.Debug("[CDC] Total: %d rows in %d batches, read time: %v", totalRows, batchNum, time.Since(startTotal))
	}()

	return results, nil
}

func getTableSchema(ctx context.Context, pool *pgxpool.Pool, table string) (*schema.TableSchema, error) {
	schemaName, tableName := parseTableName(table)

	query := `
		SELECT
			column_name,
			data_type,
			is_nullable,
			numeric_precision,
			numeric_scale,
			character_maximum_length,
			udt_name
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`

	rows, err := pool.Query(ctx, query, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query schema: %w", err)
	}
	defer func() { rows.Close() }()

	var columns []schema.Column
	for rows.Next() {
		var columnName, dataType, isNullable, udtName string
		var numericPrecision, numericScale, charMaxLen *int

		if err := rows.Scan(&columnName, &dataType, &isNullable, &numericPrecision, &numericScale, &charMaxLen, &udtName); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		pgType := dataType
		if dataType == "ARRAY" {
			pgType = udtName
		}
		if dataType == "USER-DEFINED" {
			pgType = udtName
		}

		dt, precision, scale, arrayType := postgres.MapPostgresToDataType(pgType)

		col := schema.Column{
			Name:      columnName,
			DataType:  dt,
			Nullable:  isNullable == "YES",
			ArrayType: arrayType,
		}

		if numericPrecision != nil {
			col.Precision = *numericPrecision
		} else if precision > 0 {
			col.Precision = precision
		}

		if numericScale != nil {
			col.Scale = *numericScale
		} else if scale > 0 {
			col.Scale = scale
		}

		if charMaxLen != nil {
			col.MaxLength = *charMaxLen
		}

		columns = append(columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s not found or has no columns", table)
	}

	pkQuery := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = ($1 || '.' || $2)::regclass
		AND i.indisprimary
	`

	var primaryKeys []string
	pkRows, err := pool.Query(ctx, pkQuery, schemaName, tableName)
	if err == nil {
		defer func() { pkRows.Close() }()
		for pkRows.Next() {
			var pkName string
			if err := pkRows.Scan(&pkName); err == nil {
				primaryKeys = append(primaryKeys, pkName)
			}
		}
	}

	for i := range columns {
		for _, pk := range primaryKeys {
			if columns[i].Name == pk {
				columns[i].IsPrimaryKey = true
				break
			}
		}
	}

	return &schema.TableSchema{
		Name:        tableName,
		Schema:      schemaName,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, nil
}

func addCDCColumns(tableSchema *schema.TableSchema) *schema.TableSchema {
	cdcColumns := []schema.Column{
		{
			Name:     CDCLSNColumn,
			DataType: schema.TypeString,
			Nullable: false,
		},
		{
			Name:     CDCDeletedColumn,
			DataType: schema.TypeBoolean,
			Nullable: false,
		},
		{
			Name:     CDCSyncedAtColumn,
			DataType: schema.TypeTimestampTZ,
			Nullable: false,
		},
		{
			Name:     CDCUnchangedColsColumn,
			DataType: schema.TypeString,
			Nullable: false,
		},
	}

	newSchema := *tableSchema
	newSchema.Columns = append(tableSchema.Columns, cdcColumns...)

	return &newSchema
}

func parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "public", table
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteTableName(table string) string {
	schemaName, tableName := parseTableName(table)
	return quoteIdentifier(schemaName) + "." + quoteIdentifier(tableName)
}

func buildArrowSchema(columns []schema.Column) *arrow.Schema {
	fields := make([]arrow.Field, len(columns))
	for i, col := range columns {
		fields[i] = arrow.Field{
			Name:     col.Name,
			Type:     schema.DataTypeToArrowType(col),
			Nullable: col.Nullable,
		}
	}
	return arrow.NewSchema(fields, nil)
}

var _ source.SourceTable = (*CDCTable)(nil)
