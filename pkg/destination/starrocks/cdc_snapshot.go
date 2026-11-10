package starrocks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func (d *StarRocksDestination) SupportsDirectCDCSnapshot() bool { return true }

func (d *StarRocksDestination) CanWriteCDCSnapshot(ctx context.Context, table string, sourceSchema *schema.TableSchema, expectedIncarnation string) (bool, error) {
	if sourceSchema == nil {
		return false, nil
	}
	columns := make(map[string]bool, len(sourceSchema.Columns))
	for _, column := range sourceSchema.Columns {
		if strings.EqualFold(column.Name, cdcOrderColumn) {
			return false, fmt.Errorf("StarRocks CDC snapshot source column %q is reserved for CDC ordering", column.Name)
		}
		columns[strings.ToLower(column.Name)] = true
	}
	if !columns[destination.CDCLSNColumn] {
		return false, nil
	}
	database, name := d.tableParts(table)
	rows, err := d.db.QueryContext(ctx, "SELECT COLUMN_NAME FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION", database, name)
	if err != nil {
		return false, fmt.Errorf("read StarRocks CDC snapshot target columns: %w", err)
	}
	defer func() { _ = rows.Close() }()
	complete, found, orderColumn := true, false, false
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return false, fmt.Errorf("read StarRocks CDC snapshot target column: %w", err)
		}
		found = true
		if strings.EqualFold(column, cdcOrderColumn) {
			orderColumn = true
		} else if !destination.IsCDCStagingOnlyColumn(column) && !columns[strings.ToLower(column)] {
			complete = false
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read StarRocks CDC snapshot target columns: %w", err)
	}
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("close StarRocks CDC snapshot target columns: %w", err)
	}
	if !found || !complete {
		return false, nil
	}
	return d.ensureCDCSnapshotOrder(ctx, table, orderColumn, expectedIncarnation)
}

func validateCDCSnapshotOptions(opts destination.WriteOptions) error {
	if opts.StagingTable {
		return fmt.Errorf("StarRocks direct CDC snapshot cannot write a staging table")
	}
	if opts.Schema == nil || len(opts.PrimaryKeys) == 0 {
		return fmt.Errorf("StarRocks direct CDC snapshot requires a schema and primary keys")
	}
	columns := make(map[string]schema.DataType, len(opts.Schema.Columns))
	for _, column := range opts.Schema.Columns {
		name := strings.ToLower(column.Name)
		if _, exists := columns[name]; exists {
			return fmt.Errorf("StarRocks CDC snapshot has duplicate column %q", column.Name)
		}
		columns[name] = column.DataType
	}
	for _, key := range opts.PrimaryKeys {
		if _, exists := columns[strings.ToLower(key)]; !exists || destination.IsCDCMetaColumn(key) {
			return fmt.Errorf("StarRocks CDC snapshot has invalid primary key %q", key)
		}
	}
	if columns[destination.CDCLSNColumn] != schema.TypeString || columns[destination.CDCDeletedColumn] != schema.TypeBoolean {
		return fmt.Errorf("StarRocks CDC snapshot requires string %s and boolean %s columns", destination.CDCLSNColumn, destination.CDCDeletedColumn)
	}
	if kind := columns[destination.CDCSyncedAtColumn]; kind != schema.TypeTimestamp && kind != schema.TypeTimestampTZ {
		return fmt.Errorf("StarRocks CDC snapshot requires timestamp %s", destination.CDCSyncedAtColumn)
	}
	return nil
}

// prepareCDCSnapshotRecord rejects partial row images before stripping the
// staging-only TOAST marker. The returned record owns references to its arrays.
func prepareCDCSnapshotRecord(record arrow.RecordBatch, opts destination.WriteOptions) (arrow.RecordBatch, error) {
	indices := make(map[string]int, record.NumCols())
	fields := record.Schema().Fields()
	for i, field := range fields {
		name := strings.ToLower(field.Name)
		if _, exists := indices[name]; exists {
			return nil, fmt.Errorf("StarRocks CDC snapshot has duplicate batch column %q", field.Name)
		}
		indices[name] = i
	}
	for _, column := range opts.Schema.Columns {
		if _, exists := indices[strings.ToLower(column.Name)]; !exists && !destination.IsCDCStagingOnlyColumn(column.Name) {
			return nil, fmt.Errorf("StarRocks CDC snapshot batch is missing column %q", column.Name)
		}
	}
	for _, key := range opts.PrimaryKeys {
		if record.Column(indices[strings.ToLower(key)]).NullN() != 0 {
			return nil, fmt.Errorf("StarRocks CDC snapshot has NULL primary key %q", key)
		}
	}
	type stringArray interface {
		arrow.Array
		Value(int) string
	}
	lsns, ok := record.Column(indices[destination.CDCLSNColumn]).(stringArray)
	if !ok || lsns.NullN() != 0 {
		return nil, fmt.Errorf("StarRocks CDC snapshot requires non-NULL string %s values", destination.CDCLSNColumn)
	}
	deleted, ok := record.Column(indices[destination.CDCDeletedColumn]).(*array.Boolean)
	if !ok || deleted.NullN() != 0 {
		return nil, fmt.Errorf("StarRocks CDC snapshot requires non-NULL boolean %s values", destination.CDCDeletedColumn)
	}
	syncedAt, ok := record.Column(indices[destination.CDCSyncedAtColumn]).(*array.Timestamp)
	if !ok || syncedAt.NullN() != 0 {
		return nil, fmt.Errorf("StarRocks CDC snapshot requires non-NULL timestamp %s values", destination.CDCSyncedAtColumn)
	}
	var unchanged stringArray
	if index, exists := indices[destination.CDCUnchangedColsColumn]; exists {
		unchanged, ok = record.Column(index).(stringArray)
		if !ok || unchanged.NullN() != 0 {
			return nil, fmt.Errorf("StarRocks CDC snapshot requires empty %s arrays", destination.CDCUnchangedColsColumn)
		}
	}
	for row := 0; row < int(record.NumRows()); row++ {
		if !validSnapshotLSN(lsns.Value(row)) {
			return nil, fmt.Errorf("StarRocks CDC snapshot requires canonical PostgreSQL %s values", destination.CDCLSNColumn)
		}
		if deleted.Value(row) {
			return nil, fmt.Errorf("StarRocks CDC snapshot cannot contain tombstones")
		}
		if unchanged != nil && unchanged.Value(row) != "[]" {
			var columns []string
			if err := json.Unmarshal([]byte(unchanged.Value(row)), &columns); err != nil || columns == nil || len(columns) != 0 {
				return nil, fmt.Errorf("StarRocks CDC snapshot cannot contain unchanged TOAST columns")
			}
		}
	}
	projectedFields := make([]arrow.Field, 0, len(fields))
	projectedColumns := make([]arrow.Array, 0, len(fields))
	for i, field := range fields {
		if !destination.IsCDCStagingOnlyColumn(field.Name) {
			projectedFields = append(projectedFields, field)
			projectedColumns = append(projectedColumns, record.Column(i))
		}
	}
	metadata := record.Schema().Metadata()
	return array.NewRecordBatch(arrow.NewSchema(projectedFields, &metadata), projectedColumns, record.NumRows()), nil
}

func validSnapshotLSN(position string) bool {
	if len(position) != 17 || position[8] != '/' {
		return false
	}
	for i, digit := range position {
		if i != 8 && (digit < '0' || digit > '9') && (digit < 'A' || digit > 'F') {
			return false
		}
	}
	return true
}

var _ destination.CDCSnapshotWriter = (*StarRocksDestination)(nil)
