package starrocks

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/destination"
)

var _ destination.CDCSnapshotSoftReplacer = (*StarRocksDestination)(nil)

func (d *StarRocksDestination) SupportsCDCSnapshotSoftReplace() bool { return true }

func (d *StarRocksDestination) MaxCDCRowLSN(ctx context.Context, table string) (string, error) {
	var lsn sql.NullString
	query := fmt.Sprintf("SELECT MAX(%s) FROM %s", quoteColumn(destination.CDCLSNColumn), d.tableRef(table))
	if err := d.db.QueryRowContext(ctx, query).Scan(&lsn); err != nil {
		return "", fmt.Errorf("read StarRocks CDC position of %s: %w", table, err)
	}
	return lsn.String, nil
}

// RetireCDCRowsBefore rewrites stale live rows as tombstones with INSERT
// SELECT, which upserts on a PRIMARY KEY table and, unlike UPDATE, is accepted
// while a schema change job is still converting the table. Payload columns
// keep their values, matching delete tombstones written by the CDC merge.
func (d *StarRocksDestination) RetireCDCRowsBefore(ctx context.Context, opts destination.CDCRetireOptions) error {
	if len(opts.PrimaryKeys) == 0 {
		return fmt.Errorf("StarRocks CDC retire requires primary keys")
	}
	if opts.Boundary == "" {
		return fmt.Errorf("StarRocks CDC retire requires a boundary position")
	}
	if err := d.validatePrimaryKey(ctx, "CDC target", opts.Table, opts.PrimaryKeys); err != nil {
		return err
	}
	if err := d.checkCDCIncarnation(ctx, opts.Table, opts.ExpectedIncarnation); err != nil {
		return err
	}
	tableSchema, err := d.GetTableSchema(ctx, opts.Table)
	if err != nil {
		return err
	}
	if tableSchema == nil {
		return fmt.Errorf("StarRocks CDC target %s does not exist", opts.Table)
	}
	body, err := buildCDCRetireSQL(d.tableRef(opts.Table), tableSchema.ColumnNames(), opts)
	if err != nil {
		return err
	}
	if err := d.insertVisible(ctx, opts.Table, body); err != nil {
		return fmt.Errorf("retire StarRocks CDC rows before %s: %w", opts.Boundary, err)
	}
	return d.checkCDCIncarnation(ctx, opts.Table, opts.ExpectedIncarnation)
}

func buildCDCRetireSQL(target string, columns []string, opts destination.CDCRetireOptions) (string, error) {
	var hasLSN, hasDeleted bool
	selected := make([]string, len(columns))
	for i, col := range columns {
		switch {
		case strings.EqualFold(col, destination.CDCLSNColumn):
			selected[i] = sqlString(opts.Boundary)
			hasLSN = true
		case strings.EqualFold(col, destination.CDCDeletedColumn):
			selected[i] = "TRUE"
			hasDeleted = true
		case strings.EqualFold(col, destination.CDCSyncedAtColumn):
			selected[i] = sqlString(opts.SyncedAt.UTC().Format("2006-01-02 15:04:05.000000"))
		default:
			selected[i] = quoteColumn(col)
		}
	}
	if !hasLSN || !hasDeleted {
		return "", fmt.Errorf("StarRocks CDC target %s lacks %s or %s", target, destination.CDCLSNColumn, destination.CDCDeletedColumn)
	}
	lsn, deleted := quoteColumn(destination.CDCLSNColumn), quoteColumn(destination.CDCDeletedColumn)
	return fmt.Sprintf("(%s) SELECT %s FROM %s WHERE (%s IS NULL OR %s < %s) AND COALESCE(%s, FALSE) = FALSE",
		strings.Join(quoteColumns(columns), ", "), strings.Join(selected, ", "), target,
		lsn, lsn, sqlString(opts.Boundary), deleted), nil
}
