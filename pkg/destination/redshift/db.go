package redshift

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	intredshift "github.com/bruin-data/ingestr/internal/redshift"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type redshiftRow interface {
	Scan(dest ...any) error
}

type redshiftRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

type redshiftQueryer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (redshiftRows, error)
	QueryRow(ctx context.Context, sql string, args ...any) redshiftRow
}

type redshiftTx interface {
	redshiftQueryer
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type redshiftDB interface {
	redshiftQueryer
	BeginTx(ctx context.Context) (redshiftTx, error)
	Ping(ctx context.Context) error
	Close()
}

type pgxPoolAdapter struct {
	pool *pgxpool.Pool
}

func (a *pgxPoolAdapter) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return a.pool.Exec(ctx, sql, args...)
}

func (a *pgxPoolAdapter) Query(ctx context.Context, sql string, args ...any) (redshiftRows, error) {
	return a.pool.Query(ctx, sql, args...)
}

func (a *pgxPoolAdapter) QueryRow(ctx context.Context, sql string, args ...any) redshiftRow {
	return a.pool.QueryRow(ctx, sql, args...)
}

func (a *pgxPoolAdapter) BeginTx(ctx context.Context) (redshiftTx, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &pgxTxAdapter{tx: tx}, nil
}

func (a *pgxPoolAdapter) Ping(ctx context.Context) error {
	return a.pool.Ping(ctx)
}

func (a *pgxPoolAdapter) Close() {
	a.pool.Close()
}

type pgxTxAdapter struct {
	tx pgx.Tx
}

func (a *pgxTxAdapter) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return a.tx.Exec(ctx, sql, args...)
}

func (a *pgxTxAdapter) Query(ctx context.Context, sql string, args ...any) (redshiftRows, error) {
	return a.tx.Query(ctx, sql, args...)
}

func (a *pgxTxAdapter) QueryRow(ctx context.Context, sql string, args ...any) redshiftRow {
	return a.tx.QueryRow(ctx, sql, args...)
}

func (a *pgxTxAdapter) Commit(ctx context.Context) error {
	return a.tx.Commit(ctx)
}

func (a *pgxTxAdapter) Rollback(ctx context.Context) error {
	return a.tx.Rollback(ctx)
}

type redshiftTransaction struct {
	tx redshiftTx
}

func (t *redshiftTransaction) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := t.tx.Exec(ctx, sql, args...)
	return err
}

func (t *redshiftTransaction) Commit(ctx context.Context) error {
	return t.tx.Commit(ctx)
}

func (t *redshiftTransaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback(ctx)
}

func (d *RedshiftDestination) Connect(ctx context.Context, uri string) error {
	var db redshiftDB
	if d.openDB != nil {
		opened, err := d.openDB(ctx, uri)
		if err != nil {
			return err
		}
		db = opened
	} else {
		normalizedURI := intredshift.NormalizeURI(uri)
		poolConfig, err := pgxpool.ParseConfig(normalizedURI)
		if err != nil {
			return fmt.Errorf("failed to parse Redshift URI: %w", err)
		}
		if poolConfig.MaxConns == 0 || poolConfig.MaxConns < 16 {
			poolConfig.MaxConns = 16
		}
		poolConfig.MinConns = 1
		poolConfig.MaxConnLifetime = 30 * time.Minute
		poolConfig.MaxConnIdleTime = 5 * time.Minute

		pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
		if err != nil {
			return fmt.Errorf("failed to connect to Redshift: %w", err)
		}
		db = &pgxPoolAdapter{pool: pool}
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping Redshift: %w", err)
	}

	d.db = db
	d.dbOID = 0
	if err := d.recoverInterruptedSchemaEvolutions(ctx, d.db); err != nil {
		db.Close()
		d.db = nil
		return err
	}
	return nil
}

func (d *RedshiftDestination) Close(_ context.Context) error {
	if d.db != nil {
		d.db.Close()
		d.db = nil
	}
	return nil
}

func (d *RedshiftDestination) requireDB() (redshiftDB, error) {
	if d.db == nil {
		return nil, errors.New("not connected to Redshift")
	}
	return d.db, nil
}

func (d *RedshiftDestination) Exec(ctx context.Context, sql string, args ...any) error {
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, sql, args...)
	return err
}

func (d *RedshiftDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	db, err := d.requireDB()
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin Redshift transaction: %w", err)
	}
	return &redshiftTransaction{tx: tx}, nil
}

func (d *RedshiftDestination) withTx(ctx context.Context, fn func(tx redshiftTx) error) error {
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin Redshift transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit Redshift transaction: %w", err)
	}
	committed = true
	return nil
}

func (d *RedshiftDestination) DropTable(ctx context.Context, table string) error {
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return err
	}
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTable(schemaName, tableName))
	if _, err := db.Exec(ctx, dropSQL); err != nil {
		return fmt.Errorf("failed to drop table %s: %w", table, err)
	}
	return nil
}

func (d *RedshiftDestination) TruncateTable(ctx context.Context, table string) error {
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return err
	}
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", quoteTable(schemaName, tableName))
	if _, err := db.Exec(ctx, truncateSQL); err != nil {
		return fmt.Errorf("failed to truncate table %s: %w", table, err)
	}
	return nil
}

func (d *RedshiftDestination) ListStagingTables(ctx context.Context, schemaName, prefix string) ([]string, error) {
	db, err := d.requireDB()
	if err != nil {
		return nil, err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return nil, err
	}
	if schemaName == "" {
		schemaName = d.defaultSchema()
	}
	rows, err := db.Query(ctx,
		`SELECT table_name FROM information_schema.tables WHERE table_schema = $1 AND table_type = 'BASE TABLE'`,
		schemaName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list Redshift staging tables in %q: %w", schemaName, err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan Redshift table name: %w", err)
		}
		if prefix == "" || strings.HasPrefix(name, prefix) {
			tables = append(tables, schemaName+"."+name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate Redshift staging tables: %w", err)
	}
	return tables, nil
}

func (d *RedshiftDestination) TruncateInsertFromStaging(ctx context.Context, opts destination.TruncateInsertFromStagingOptions) error {
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return err
	}
	targetSchema, targetTable, err := d.resolveSchemaTable(ctx, db, opts.TargetTable)
	if err != nil {
		return err
	}
	stagingSchema, stagingTable, err := d.resolveSchemaTable(ctx, db, opts.StagingTable)
	if err != nil {
		return err
	}
	targetRef := quoteTable(targetSchema, targetTable)
	stagingRef := quoteTable(stagingSchema, stagingTable)

	quotedCols := make([]string, len(opts.Columns))
	for i, c := range opts.Columns {
		quotedCols[i] = destination.QuoteIdentifier(c)
	}
	colList := strings.Join(quotedCols, ", ")

	// DELETE FROM instead of TRUNCATE keeps the wipe and reload inside one Redshift transaction.
	return d.withTx(ctx, func(tx redshiftTx) error {
		if _, err := tx.Exec(ctx, "LOCK "+targetRef); err != nil {
			return fmt.Errorf("failed to lock target table %s: %w", opts.TargetTable, err)
		}
		if _, err := tx.Exec(ctx, "DELETE FROM "+targetRef); err != nil {
			return fmt.Errorf("failed to clear target table %s: %w", opts.TargetTable, err)
		}
		insertSQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s", targetRef, colList, colList, stagingRef)
		if len(quotedCols) == 0 {
			insertSQL = fmt.Sprintf("INSERT INTO %s SELECT * FROM %s", targetRef, stagingRef)
		}
		if _, err := tx.Exec(ctx, insertSQL); err != nil {
			return fmt.Errorf("failed to insert staging rows into %s: %w", opts.TargetTable, err)
		}
		return nil
	})
}

func (d *RedshiftDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	if opts.IncrementalKey == "" {
		return errors.New("delete+insert requires an incremental key")
	}
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return err
	}
	targetSchema, targetTable, err := d.resolveSchemaTable(ctx, db, opts.TargetTable)
	if err != nil {
		return err
	}
	stagingSchema, stagingTable, err := d.resolveSchemaTable(ctx, db, opts.StagingTable)
	if err != nil {
		return err
	}
	targetRef := quoteTable(targetSchema, targetTable)
	stagingRef := quoteTable(stagingSchema, stagingTable)
	keyCol := destination.QuoteIdentifier(actualColName(opts.Columns, opts.IncrementalKey))

	quotedCols := make([]string, len(opts.Columns))
	for i, c := range opts.Columns {
		quotedCols[i] = destination.QuoteIdentifier(c)
	}
	colList := strings.Join(quotedCols, ", ")

	quotedPKs := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		quotedPKs[i] = destination.QuoteIdentifier(actualColName(opts.Columns, pk))
	}
	pkList := strings.Join(quotedPKs, ", ")
	if len(quotedCols) == 0 && pkList != "" {
		return errors.New("delete+insert with primary keys requires columns")
	}

	return d.withTx(ctx, func(tx redshiftTx) error {
		if _, err := tx.Exec(ctx, "LOCK "+targetRef); err != nil {
			return fmt.Errorf("failed to lock target table %s: %w", opts.TargetTable, err)
		}
		deleteSQL := fmt.Sprintf(
			`DELETE FROM %s WHERE %s >= $1 AND %s <= $2`,
			targetRef, keyCol, keyCol,
		)
		if _, err := tx.Exec(ctx, deleteSQL, opts.IntervalStart, opts.IntervalEnd); err != nil {
			return fmt.Errorf("failed to delete matching rows from %s: %w", opts.TargetTable, err)
		}
		var insertSQL string
		if len(quotedCols) == 0 {
			insertSQL = fmt.Sprintf("INSERT INTO %s SELECT * FROM %s", targetRef, stagingRef)
		} else {
			selectClause := destination.DedupStagingSelect(colList, pkList, stagingRef, keyCol)
			insertSQL = fmt.Sprintf("INSERT INTO %s (%s) %s", targetRef, colList, selectClause)
		}
		if _, err := tx.Exec(ctx, insertSQL); err != nil {
			return fmt.Errorf("failed to insert rows into %s: %w", opts.TargetTable, err)
		}
		return nil
	})
}

func (d *RedshiftDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	if len(opts.PrimaryKeys) == 0 {
		return errors.New("scd2 requires at least one primary key")
	}
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return err
	}
	targetSchema, targetTable, err := d.resolveSchemaTable(ctx, db, opts.TargetTable)
	if err != nil {
		return err
	}
	stagingSchema, stagingTable, err := d.resolveSchemaTable(ctx, db, opts.StagingTable)
	if err != nil {
		return err
	}
	targetRef := quoteTable(targetSchema, targetTable)
	stagingRef := quoteTable(stagingSchema, stagingTable)

	validToCol := destination.QuoteIdentifier(destination.SCD2ValidToColumn)
	isCurCol := destination.QuoteIdentifier(destination.SCD2IsCurrentColumn)

	nonDataSet := make(map[string]struct{})
	for _, col := range destination.SCD2NonDataColumns(opts.PrimaryKeys) {
		nonDataSet[strings.ToLower(col)] = struct{}{}
	}

	var valueCols []string
	for _, col := range opts.Columns {
		if _, skip := nonDataSet[strings.ToLower(col)]; !skip {
			valueCols = append(valueCols, col)
		}
	}

	allCols := destination.AppendSCD2Columns(opts.Columns)
	insertCols := make([]string, len(allCols))
	selectCols := make([]string, len(allCols))
	ts := opts.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	for i, col := range allCols {
		qc := destination.QuoteIdentifier(col)
		insertCols[i] = qc
		switch strings.ToLower(col) {
		case destination.SCD2ValidFromColumn:
			selectCols[i] = "$1::timestamptz"
		case destination.SCD2ValidToColumn:
			selectCols[i] = "NULL"
		case destination.SCD2IsCurrentColumn:
			selectCols[i] = "TRUE"
		default:
			selectCols[i] = "source." + qc
		}
	}

	joinTargetSource := buildJoinCondition(opts.PrimaryKeys, targetRef, "source")
	changeConds := make([]string, len(valueCols))
	for i, c := range valueCols {
		qc := destination.QuoteIdentifier(c)
		changeConds[i] = fmt.Sprintf("(%s.%s IS DISTINCT FROM source.%s)", targetRef, qc, qc)
	}
	changedClause := "FALSE"
	if len(changeConds) > 0 {
		changedClause = "(" + strings.Join(changeConds, " OR ") + ")"
	}

	return d.withTx(ctx, func(tx redshiftTx) error {
		if _, err := tx.Exec(ctx, "LOCK "+targetRef); err != nil {
			return fmt.Errorf("failed to lock target table %s: %w", opts.TargetTable, err)
		}

		closeSQL := fmt.Sprintf(
			`UPDATE %s SET %s = $1::timestamptz, %s = FALSE FROM %s AS source WHERE %s AND %s.%s = TRUE AND %s`,
			targetRef, validToCol, isCurCol, stagingRef, joinTargetSource, targetRef, isCurCol, changedClause,
		)
		if _, err := tx.Exec(ctx, closeSQL, ts); err != nil {
			return fmt.Errorf("failed to close changed SCD2 rows in %s: %w", opts.TargetTable, err)
		}

		if opts.IncrementalKey == "" {
			softDeleteSQL := fmt.Sprintf(
				`UPDATE %s SET %s = $1::timestamptz, %s = FALSE WHERE %s.%s = TRUE AND NOT EXISTS (SELECT 1 FROM %s AS source WHERE %s)`,
				targetRef, validToCol, isCurCol, targetRef, isCurCol, stagingRef, joinTargetSource,
			)
			if _, err := tx.Exec(ctx, softDeleteSQL, ts); err != nil {
				return fmt.Errorf("failed to close missing SCD2 rows in %s: %w", opts.TargetTable, err)
			}
		}

		insertSQL := fmt.Sprintf(
			`INSERT INTO %s (%s) SELECT %s FROM %s AS source WHERE NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s AND target.%s = TRUE)`,
			targetRef, strings.Join(insertCols, ", "), strings.Join(selectCols, ", "), stagingRef, targetRef,
			buildJoinCondition(opts.PrimaryKeys, "target", "source"), isCurCol,
		)
		if _, err := tx.Exec(ctx, insertSQL, ts); err != nil {
			return fmt.Errorf("failed to insert new SCD2 rows into %s: %w", opts.TargetTable, err)
		}
		return nil
	})
}
