package redshift

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	"github.com/jackc/pgx/v5"
)

const (
	redshiftMaxIdentifierLength = 127
	defaultManagedStagingSchema = "_bruin_staging"
)

func (d *RedshiftDestination) defaultSchema() string {
	if d != nil && d.DefaultSchema != "" {
		return d.DefaultSchema
	}
	return "public"
}

func quoteTable(schemaName, tableName string) string {
	if schemaName == "" {
		return destination.QuoteIdentifier(tableName)
	}
	return destination.QuoteIdentifier(schemaName) + "." + destination.QuoteIdentifier(tableName)
}

func splitSchemaTableRaw(table, defaultSchema string) (string, string, error) {
	if err := tablename.TwoLevel("redshift").CheckName(table); err != nil {
		return "", "", err
	}
	parts := tablename.SplitRaw(table)
	switch len(parts) {
	case 1:
		return defaultSchema, unquoteRedshiftIdent(parts[0]), nil
	case 2:
		return unquoteRedshiftIdent(parts[0]), unquoteRedshiftIdent(parts[1]), nil
	default:
		return "", "", fmt.Errorf("invalid Redshift table name %q: expected table or schema.table", table)
	}
}

func unquoteRedshiftIdent(ident string) string {
	ident = strings.TrimSpace(ident)
	if len(ident) >= 2 && ident[0] == '"' && ident[len(ident)-1] == '"' {
		return strings.ReplaceAll(ident[1:len(ident)-1], `""`, `"`)
	}
	return ident
}

func (d *RedshiftDestination) resolveSchemaTable(ctx context.Context, queryer redshiftQueryer, table string) (string, string, error) {
	schemaName, tableName, err := splitSchemaTableRaw(table, d.defaultSchema())
	if err != nil {
		return "", "", err
	}
	if queryer == nil {
		return schemaName, tableName, nil
	}

	var resolvedSchema, resolvedTable string
	err = queryer.QueryRow(ctx, `
		SELECT n.nspname, c.relname
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p')
		  AND n.nspname = $1
		  AND c.relname = $2
		LIMIT 1
	`, schemaName, tableName).Scan(&resolvedSchema, &resolvedTable)
	if err == nil {
		return resolvedSchema, resolvedTable, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("failed to resolve Redshift table %q: %w", table, err)
	}

	rows, err := queryer.Query(ctx, `
		SELECT n.nspname, c.relname
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p')
		  AND LOWER(n.nspname) = LOWER($1)
		  AND LOWER(c.relname) = LOWER($2)
	`, schemaName, tableName)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve Redshift table %q: %w", table, err)
	}
	defer rows.Close()

	var matches [][2]string
	for rows.Next() {
		var s, t string
		if scanErr := rows.Scan(&s, &t); scanErr != nil {
			return "", "", fmt.Errorf("failed to scan Redshift table %q: %w", table, scanErr)
		}
		matches = append(matches, [2]string{s, t})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return "", "", fmt.Errorf("failed to iterate Redshift table lookup for %q: %w", table, rowsErr)
	}
	if len(matches) == 1 {
		return matches[0][0], matches[0][1], nil
	}
	return schemaName, tableName, nil
}

func ensureSchemaExists(ctx context.Context, queryer redshiftQueryer, schemaName string) error {
	if schemaName == "" {
		return nil
	}
	sql := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", destination.QuoteIdentifier(schemaName))
	if _, err := queryer.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create schema %q: %w", schemaName, err)
	}
	return nil
}

func tableExists(ctx context.Context, queryer redshiftQueryer, schemaName, tableName string) (bool, error) {
	var exists bool
	err := queryer.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_class c
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind IN ('r', 'p')
			  AND n.nspname = $1
			  AND c.relname = $2
		)
	`, schemaName, tableName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check existence of %s.%s: %w", schemaName, tableName, err)
	}
	return exists, nil
}

func columnExists(ctx context.Context, queryer redshiftQueryer, schemaName, tableName, colName string) (bool, error) {
	var exists bool
	err := queryer.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = $1
			  AND table_name = $2
			  AND LOWER(column_name) = LOWER($3)
		)
	`, schemaName, tableName, colName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check column %q on %s.%s: %w", colName, schemaName, tableName, err)
	}
	return exists, nil
}

func redshiftPrimaryKeyColumns(ctx context.Context, queryer redshiftQueryer, schemaName, tableName string) ([]string, error) {
	rows, err := queryer.Query(ctx, `
		SELECT kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_schema = tc.constraint_schema
		 AND kcu.constraint_name = tc.constraint_name
		 AND kcu.table_schema = tc.table_schema
		 AND kcu.table_name = tc.table_name
		WHERE tc.constraint_type = 'PRIMARY KEY'
		  AND tc.table_schema = $1
		  AND tc.table_name = $2
		ORDER BY kcu.ordinal_position
	`, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query primary key columns for %s.%s: %w", schemaName, tableName, err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var col string
		if scanErr := rows.Scan(&col); scanErr != nil {
			return nil, fmt.Errorf("failed to scan primary key column for %s.%s: %w", schemaName, tableName, scanErr)
		}
		cols = append(cols, col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate primary key columns for %s.%s: %w", schemaName, tableName, err)
	}
	return cols, nil
}

func primaryKeySetsEqual(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}
	seen := make(map[string]struct{}, len(expected))
	for _, col := range expected {
		seen[strings.ToLower(col)] = struct{}{}
	}
	for _, col := range actual {
		if _, ok := seen[strings.ToLower(col)]; !ok {
			return false
		}
	}
	return true
}

func buildRedshiftCreateTableSQL(schemaName, tableName string, opts destination.PrepareOptions) string {
	dialect := &Dialect{}
	tableRef := quoteTable(schemaName, tableName)

	pkSet := make(map[string]struct{}, len(opts.PrimaryKeys)+len(opts.DeferredPrimaryKeys))
	for _, pk := range opts.PrimaryKeys {
		pkSet[strings.ToLower(pk)] = struct{}{}
	}
	for _, pk := range opts.DeferredPrimaryKeys {
		pkSet[strings.ToLower(pk)] = struct{}{}
	}
	cdcKeySet := make(map[string]struct{}, len(opts.CDCKeys))
	for _, k := range opts.CDCKeys {
		cdcKeySet[strings.ToLower(k)] = struct{}{}
	}

	var colDefs []string
	if opts.Schema != nil {
		for _, col := range opts.Schema.Columns {
			colType := dialect.TypeName(col)
			_, isPK := pkSet[strings.ToLower(col.Name)]
			_, isCDCKey := cdcKeySet[strings.ToLower(col.Name)]

			notNull := false
			if isPK || isCDCKey {
				notNull = true
			} else if !opts.CDCMode && !col.Nullable {
				notNull = true
			}

			def := fmt.Sprintf("%s %s", destination.QuoteIdentifier(col.Name), colType)
			if notNull {
				def += " NOT NULL"
			}
			colDefs = append(colDefs, def)
		}
	}

	if len(opts.PrimaryKeys) > 0 && len(opts.DeferredPrimaryKeys) == 0 {
		quotedPKs := make([]string, len(opts.PrimaryKeys))
		for i, pk := range opts.PrimaryKeys {
			quotedPKs[i] = destination.QuoteIdentifier(pk)
		}
		colDefs = append(colDefs, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(quotedPKs, ", ")))
	}

	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)", tableRef, strings.Join(colDefs, ",\n  "))
}

func (d *RedshiftDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, opts.Table)
	if err != nil {
		return err
	}
	if err := ensureSchemaExists(ctx, db, schemaName); err != nil {
		return err
	}

	tableRef := quoteTable(schemaName, tableName)
	if opts.DropFirst {
		if _, err := db.Exec(ctx, "DROP TABLE IF EXISTS "+tableRef); err != nil {
			return fmt.Errorf("failed to drop existing table %s: %w", opts.Table, err)
		}
	}

	exists, err := tableExists(ctx, db, schemaName, tableName)
	if err != nil {
		return err
	}
	if !exists {
		if opts.Schema == nil || len(opts.Schema.Columns) == 0 {
			return fmt.Errorf("cannot create table %s without schema columns", opts.Table)
		}
		createSQL := buildRedshiftCreateTableSQL(schemaName, tableName, opts)
		if _, err := db.Exec(ctx, createSQL); err != nil {
			return fmt.Errorf("failed to create Redshift table %s: %w", opts.Table, err)
		}
		return nil
	}

	if len(opts.PrimaryKeys) > 0 && (!opts.DropFirst || opts.RequirePrimaryKeyMatch) {
		if err := d.ensurePrimaryKey(ctx, db, schemaName, tableName, opts.PrimaryKeys, opts.RequirePrimaryKeyMatch); err != nil {
			return fmt.Errorf("failed to ensure primary key: %w", err)
		}
	}
	return nil
}

func (d *RedshiftDestination) ensurePrimaryKey(ctx context.Context, queryer redshiftQueryer, schemaName, tableName string, primaryKeys []string, requireMatch bool) error {
	actualKeys, err := redshiftPrimaryKeyColumns(ctx, queryer, schemaName, tableName)
	if err != nil {
		return err
	}
	if len(actualKeys) > 0 {
		if requireMatch && !primaryKeySetsEqual(primaryKeys, actualKeys) {
			return fmt.Errorf("primary key mismatch on %s.%s: expected %v, got %v", schemaName, tableName, primaryKeys, actualKeys)
		}
		return nil
	}

	quotedKeys := make([]string, len(primaryKeys))
	for i, k := range primaryKeys {
		quotedKeys[i] = destination.QuoteIdentifier(k)
	}
	alterSQL := fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s)", quoteTable(schemaName, tableName), strings.Join(quotedKeys, ", "))
	if _, err := queryer.Exec(ctx, alterSQL); err != nil {
		return fmt.Errorf("failed to add primary key on %s.%s: %w", schemaName, tableName, err)
	}
	return nil
}

func (d *RedshiftDestination) GeneratedColumns(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (d *RedshiftDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	db, err := d.requireDB()
	if err != nil {
		return nil, err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(ctx, `
		SELECT column_name, data_type, is_nullable,
		       numeric_precision, numeric_scale, character_maximum_length
		FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = $2
		ORDER BY ordinal_position
	`, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query Redshift table schema for %s: %w", table, err)
	}
	defer rows.Close()

	var cols []schema.Column
	for rows.Next() {
		var colName, dataType, isNullable string
		var numPrec, numScale, charLen *int
		if scanErr := rows.Scan(&colName, &dataType, &isNullable, &numPrec, &numScale, &charLen); scanErr != nil {
			return nil, fmt.Errorf("failed to scan Redshift column for %s: %w", table, scanErr)
		}

		mapped := mapRedshiftDataType(dataType)
		col := schema.Column{
			Name:     colName,
			DataType: mapped,
			Nullable: strings.EqualFold(isNullable, "YES"),
		}
		if mapped == schema.TypeDecimal && numPrec != nil {
			col.Precision = *numPrec
			if numScale != nil {
				col.Scale = *numScale
			}
		}
		if mapped == schema.TypeString && charLen != nil && *charLen > 0 {
			col.MaxLength = *charLen
		}
		cols = append(cols, col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate Redshift columns for %s: %w", table, err)
	}
	if len(cols) == 0 {
		return nil, nil
	}

	pks, err := redshiftPrimaryKeyColumns(ctx, db, schemaName, tableName)
	if err != nil {
		return nil, err
	}

	return &schema.TableSchema{
		Name:        tableName,
		Schema:      schemaName,
		Columns:     cols,
		PrimaryKeys: pks,
	}, nil
}

func mapRedshiftDataType(dataType string) schema.DataType {
	t := strings.ToLower(strings.TrimSpace(dataType))
	switch t {
	case "smallint", "int2":
		return schema.TypeInt16
	case "integer", "int", "int4":
		return schema.TypeInt32
	case "bigint", "int8":
		return schema.TypeInt64
	case "real", "float4":
		return schema.TypeFloat32
	case "double precision", "float8", "float":
		return schema.TypeFloat64
	case "numeric", "decimal":
		return schema.TypeDecimal
	case "boolean", "bool":
		return schema.TypeBoolean
	case "date":
		return schema.TypeDate
	case "time", "time without time zone", "time with time zone", "timetz":
		return schema.TypeTime
	case "timestamp", "timestamp without time zone":
		return schema.TypeTimestamp
	case "timestamp with time zone", "timestamptz":
		return schema.TypeTimestampTZ
	case "super":
		return schema.TypeJSON
	case "character varying", "varchar", "character", "char", "text", "bpchar", "nvarchar", "nchar":
		return schema.TypeString
	default:
		if strings.HasPrefix(t, "character varying") || strings.HasPrefix(t, "varchar") {
			return schema.TypeString
		}
		if strings.HasPrefix(t, "decimal") || strings.HasPrefix(t, "numeric") {
			return schema.TypeDecimal
		}
		if strings.HasPrefix(t, "timestamp with time zone") {
			return schema.TypeTimestampTZ
		}
		if strings.HasPrefix(t, "timestamp") {
			return schema.TypeTimestamp
		}
		return schema.TypeString
	}
}

func (d *RedshiftDestination) ReplaceStagingPolicy() destination.ReplaceStagingPolicy {
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingTargetSchema,
		DefaultTargetSchema:  d.defaultSchema(),
		DefaultManagedSchema: defaultManagedStagingSchema,
	}
}

func (d *RedshiftDestination) ManagedStagingPolicy() destination.ReplaceStagingPolicy {
	return d.ReplaceStagingPolicy()
}

func (d *RedshiftDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	db, err := d.requireDB()
	if err != nil {
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

	conditional := opts.CDCExpectedIncarnation != "" ||
		opts.CDCExpectedStagingIncarnation != "" ||
		opts.CDCExpectedResultIncarnation != ""
	if conditional {
		if opts.CDCExpectedIncarnation == "" || opts.CDCExpectedStagingIncarnation == "" || opts.CDCExpectedResultIncarnation == "" {
			return errors.New("redshift CDC conditional swap requires target, staging, and result incarnations")
		}
		if opts.CDCExpectedResultIncarnation != opts.CDCExpectedStagingIncarnation {
			return fmt.Errorf("redshift CDC swap result incarnation %q cannot match staging relation %q", opts.CDCExpectedResultIncarnation, opts.CDCExpectedStagingIncarnation)
		}
		if !strings.EqualFold(targetSchema, stagingSchema) {
			return fmt.Errorf("redshift CDC conditional swap requires staging table %q to share schema %q with target %q", opts.StagingTable, targetSchema, opts.TargetTable)
		}
	}

	if err := ensureSchemaExists(ctx, db, targetSchema); err != nil {
		return err
	}

	targetRef := quoteTable(targetSchema, targetTable)
	stagingRef := quoteTable(stagingSchema, stagingTable)

	return d.withTx(ctx, func(tx redshiftTx) error {
		_, targetExists, err := d.targetIncarnationInTx(ctx, tx, targetSchema, targetTable)
		if err != nil {
			return err
		}
		if targetExists {
			if _, err := tx.Exec(ctx, "LOCK "+targetRef); err != nil {
				return fmt.Errorf("failed to lock target table %s: %w", opts.TargetTable, err)
			}
			if conditional {
				targetInc, exists, err := d.targetIncarnationInTx(ctx, tx, targetSchema, targetTable)
				if err != nil {
					return err
				}
				if !exists || targetInc != opts.CDCExpectedIncarnation {
					return fmt.Errorf("redshift CDC target %q physical incarnation changed", opts.TargetTable)
				}
			}
		} else if opts.CDCExpectedIncarnation != "" {
			return fmt.Errorf("redshift CDC target %q physical incarnation changed", opts.TargetTable)
		}

		_, stagingExists, err := d.targetIncarnationInTx(ctx, tx, stagingSchema, stagingTable)
		if err != nil {
			return err
		}
		if !stagingExists {
			return fmt.Errorf("redshift staging table %q does not exist for swap", opts.StagingTable)
		}
		if _, err := tx.Exec(ctx, "LOCK "+stagingRef); err != nil {
			return fmt.Errorf("failed to lock staging table %s: %w", opts.StagingTable, err)
		}
		if conditional {
			stagingInc, exists, err := d.targetIncarnationInTx(ctx, tx, stagingSchema, stagingTable)
			if err != nil {
				return err
			}
			if !exists || stagingInc != opts.CDCExpectedStagingIncarnation {
				return fmt.Errorf("redshift CDC staging table %q physical incarnation changed", opts.StagingTable)
			}
		}

		if strings.EqualFold(targetSchema, stagingSchema) {
			if targetExists {
				if _, err := tx.Exec(ctx, "DROP TABLE IF EXISTS "+targetRef); err != nil {
					return fmt.Errorf("failed to drop existing target table %s: %w", opts.TargetTable, err)
				}
			}
			// Redshift ALTER TABLE RENAME TO takes an unqualified destination name in the same schema.
			renameSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", stagingRef, destination.QuoteIdentifier(targetTable))
			if _, err := tx.Exec(ctx, renameSQL); err != nil {
				return fmt.Errorf("failed to rename staging table %s to %s: %w", opts.StagingTable, opts.TargetTable, err)
			}
			return nil
		}

		// Redshift does not support ALTER TABLE ... SET SCHEMA, so cross-schema swaps copy within the transaction.
		if targetExists {
			if _, err := tx.Exec(ctx, "DROP TABLE IF EXISTS "+targetRef); err != nil {
				return fmt.Errorf("failed to drop existing target table %s: %w", opts.TargetTable, err)
			}
		}
		createSQL := fmt.Sprintf("CREATE TABLE %s (LIKE %s INCLUDING DEFAULTS)", targetRef, stagingRef)
		if _, err := tx.Exec(ctx, createSQL); err != nil {
			return fmt.Errorf("failed to recreate target table %s from %s: %w", opts.TargetTable, opts.StagingTable, err)
		}
		insertSQL := fmt.Sprintf("INSERT INTO %s SELECT * FROM %s", targetRef, stagingRef)
		if _, err := tx.Exec(ctx, insertSQL); err != nil {
			return fmt.Errorf("failed to copy staging table %s to %s: %w", opts.StagingTable, opts.TargetTable, err)
		}
		if _, err := tx.Exec(ctx, "DROP TABLE "+stagingRef); err != nil {
			return fmt.Errorf("failed to drop staging table %s: %w", opts.StagingTable, err)
		}
		if len(opts.PrimaryKeys) > 0 {
			if err := d.ensurePrimaryKey(ctx, tx, targetSchema, targetTable, opts.PrimaryKeys, false); err != nil {
				return err
			}
		}
		return nil
	})
}
