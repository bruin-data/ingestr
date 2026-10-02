package mysql

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

const (
	maxKeyBytes = 3072
	// utf8mb4BytesPerChar is assumed whenever a charset cannot be read.
	utf8mb4BytesPerChar = 4
	// maxPrimaryKeyStringLength is the widest utf8mb4 VARCHAR an InnoDB key can index.
	maxPrimaryKeyStringLength = maxKeyBytes / utf8mb4BytesPerChar
)

func (d *MySQLDestination) NormalizeSchemaEvolutionSourceColumn(source, dest schema.Column) schema.Column {
	// An unbounded source widens a string key to the indexable maximum, never to TEXT.
	if dest.IsPrimaryKey && dest.DataType == schema.TypeString && dest.MaxLength > 0 &&
		source.DataType == schema.TypeString && source.MaxLength <= 0 {
		source.MaxLength = max(dest.MaxLength, maxPrimaryKeyStringLength)
	}
	return source
}

// stringKeyBudget is how many characters of a charBytes-wide charset the string
// columns of a key can share after the other key columns take their bytes.
func stringKeyBudget(otherKeyBytes, charBytes int) int {
	return (maxKeyBytes - otherKeyBytes) / charBytes
}

// keyBytes is how many bytes a non-string column takes in an InnoDB key, for
// the type MapDataTypeToMySQL creates it as or the length read from the table.
func keyBytes(col schema.Column, charBytes int) int {
	switch col.DataType {
	case schema.TypeBoolean, schema.TypeInt8:
		return 1
	case schema.TypeInt16:
		return 2
	case schema.TypeInt32, schema.TypeFloat32:
		return 4
	case schema.TypeDate:
		return 3
	case schema.TypeTime:
		return 6
	case schema.TypeTimestampTZ:
		return 7
	case schema.TypeDecimal:
		if col.Precision <= 0 {
			return decimalBytes(38, 9)
		}
		return decimalBytes(col.Precision, min(max(col.Scale, 0), col.Precision))
	case schema.TypeBinary:
		if col.MaxLength > 0 {
			return col.MaxLength
		}
		return 8
	case schema.TypeUUID:
		return 36 * charBytes
	case schema.TypeInterval:
		return 255 * charBytes
	default:
		return 8
	}
}

// decimalBytes follows MySQL's DECIMAL storage: 4 bytes per 9 digits on each
// side of the point, plus a partial word for the leftover digits.
func decimalBytes(precision, scale int) int {
	leftover := [9]int{0, 1, 1, 2, 2, 3, 3, 4, 4}
	digits := func(n int) int { return n/9*4 + leftover[n%9] }
	return digits(precision-scale) + digits(scale)
}

// needsKeyBounding reports whether a table created with these columns would
// get an unbounded string key, which MySQL cannot index.
func needsKeyBounding(columns []schema.Column, primaryKeys []string) bool {
	for _, col := range columns {
		if col.DataType == schema.TypeString && col.MaxLength <= 0 && containsFold(primaryKeys, col.Name) {
			return true
		}
	}
	return false
}

// boundKeyColumns gives unbounded string key columns an equal share of the
// key budget left by the other key columns, since TEXT cannot be a key.
// charBytes is the width of the charset the table is created with.
func boundKeyColumns(columns []schema.Column, primaryKeys []string, charBytes int) []schema.Column {
	otherKeyBytes, unbounded := 0, 0
	remaining := 0
	for _, col := range columns {
		if !containsFold(primaryKeys, col.Name) {
			continue
		}
		switch {
		case col.DataType != schema.TypeString:
			otherKeyBytes += keyBytes(col, charBytes)
		case col.MaxLength <= 0:
			unbounded++
		default:
			remaining -= col.MaxLength
		}
	}
	if unbounded == 0 {
		return columns
	}
	share := (stringKeyBudget(otherKeyBytes, charBytes) + remaining) / unbounded
	if share < 1 {
		share = stringKeyBudget(0, charBytes) / unbounded
	}
	bounded := append([]schema.Column(nil), columns...)
	for i, col := range bounded {
		if containsFold(primaryKeys, col.Name) && col.DataType == schema.TypeString && col.MaxLength <= 0 {
			bounded[i].MaxLength = share
		}
	}
	return bounded
}

// fitKeyWidenings caps widenings of string key columns so the key stays within
// the InnoDB key limit; lengths set explicitly by an override are applied as given.
func (d *MySQLDestination) fitKeyWidenings(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) (*schemaevolution.SchemaComparison, error) {
	if comparison == nil || !comparison.HasChanges {
		return comparison, nil
	}
	widened := map[string]bool{}
	for _, change := range comparison.Changes {
		if isTypeChange(change) && change.NewColumn.DataType == schema.TypeString {
			widened[strings.ToLower(change.ColumnName)] = true
		}
	}
	if len(widened) == 0 {
		return comparison, nil
	}
	current, err := d.GetTableSchema(ctx, table)
	if err != nil || current == nil || len(current.PrimaryKeys) == 0 {
		return comparison, err
	}

	// MODIFY COLUMN converts a widened column to the table's default charset,
	// while unchanged key columns keep their own.
	columnCharBytes, tableCharBytes := d.tableCharBytes(ctx, table)
	otherKeyBytes := 0
	for _, col := range current.Columns {
		if !containsFold(current.PrimaryKeys, col.Name) {
			continue
		}
		switch {
		case widened[strings.ToLower(col.Name)]:
		case col.DataType != schema.TypeString:
			otherKeyBytes += keyBytes(col, tableCharBytes)
		default:
			otherKeyBytes += col.MaxLength * columnCharBytes(col.Name)
		}
	}

	var keyChanges, requests, floors []int
	for i, change := range comparison.Changes {
		if isTypeChange(change) && change.OldColumn != nil && change.NewColumn.DataType == schema.TypeString &&
			containsFold(current.PrimaryKeys, change.ColumnName) {
			if change.LengthFromOverride {
				otherKeyBytes += change.NewColumn.MaxLength * tableCharBytes
				continue
			}
			keyChanges = append(keyChanges, i)
			requests = append(requests, change.NewColumn.MaxLength)
			floor := 0
			if change.OldColumn.DataType == schema.TypeString {
				floor = change.OldColumn.MaxLength
			}
			floors = append(floors, floor)
		}
	}
	if len(keyChanges) == 0 {
		return comparison, nil
	}
	allowed := fairShares(requests, floors, stringKeyBudget(otherKeyBytes, tableCharBytes))

	changes := append([]schemaevolution.SchemaChange(nil), comparison.Changes...)
	drop := map[int]bool{}
	for j, i := range keyChanges {
		change := &changes[i]
		change.NewColumn.MaxLength = max(allowed[j], change.OldColumn.MaxLength)
		if change.OldColumn.DataType == schema.TypeString && change.NewColumn.MaxLength == change.OldColumn.MaxLength {
			drop[i] = true
		}
	}
	kept := changes[:0]
	for i, change := range changes {
		if !drop[i] {
			kept = append(kept, change)
		}
	}
	changes = kept
	return &schemaevolution.SchemaComparison{Changes: changes, HasChanges: len(changes) > 0}, nil
}

// databaseCharBytes is the bytes per character of the default charset new
// tables in database get, assuming utf8mb4 when it cannot be read.
func (d *MySQLDestination) databaseCharBytes(ctx context.Context, database string) int {
	query := `SELECT cs.MAXLEN FROM information_schema.SCHEMATA s
		JOIN information_schema.CHARACTER_SETS cs ON cs.CHARACTER_SET_NAME = s.DEFAULT_CHARACTER_SET_NAME
		WHERE s.SCHEMA_NAME = ` + mysqlSchemaFilterExpr(database)
	var args []any
	if database != "" {
		args = append(args, database)
	}
	var charBytes int
	if err := d.db.QueryRowContext(ctx, query, args...).Scan(&charBytes); err != nil || charBytes <= 0 {
		config.Debug("[MYSQL] could not read the default charset of database %q, assuming utf8mb4: %v", database, err)
		return utf8mb4BytesPerChar
	}
	return charBytes
}

// tableCharBytes reads the bytes per character of each column's charset and of
// the table's default charset, assuming utf8mb4 for anything it cannot read.
// The table charset is matched by collation prefix, which MariaDB's newer
// collation names (e.g. utf8mb4_uca1400_ai_ci) also follow.
func (d *MySQLDestination) tableCharBytes(ctx context.Context, table string) (func(column string) int, int) {
	columns := map[string]int{}
	tableDefault := 0
	column := func(name string) int {
		if charBytes := columns[strings.ToLower(name)]; charBytes > 0 {
			return charBytes
		}
		return utf8mb4BytesPerChar
	}

	database, tableName := splitDatabaseTable(table)
	if database == "" {
		database = d.database
	}
	query := `SELECT c.COLUMN_NAME, COALESCE(MAX(cs.MAXLEN), 0), COALESCE(MAX(ts.MAXLEN), 0)
		FROM information_schema.COLUMNS c
		JOIN information_schema.TABLES t ON t.TABLE_SCHEMA = c.TABLE_SCHEMA AND t.TABLE_NAME = c.TABLE_NAME
		LEFT JOIN information_schema.CHARACTER_SETS ts
			ON LEFT(t.TABLE_COLLATION, CHAR_LENGTH(ts.CHARACTER_SET_NAME) + 1) = CONCAT(ts.CHARACTER_SET_NAME, '_')
		LEFT JOIN information_schema.CHARACTER_SETS cs ON cs.CHARACTER_SET_NAME = c.CHARACTER_SET_NAME
		WHERE c.TABLE_SCHEMA = ` + mysqlSchemaFilterExpr(database) + ` AND c.TABLE_NAME = ?
		GROUP BY c.COLUMN_NAME`
	rows, err := d.db.QueryContext(ctx, query, mysqlSchemaFilterArgs(database, tableName)...)
	if err != nil {
		config.Debug("[MYSQL] could not read the charsets of %q, assuming utf8mb4: %v", table, err)
		return column, utf8mb4BytesPerChar
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var columnBytes, tableBytes int
		if err := rows.Scan(&name, &columnBytes, &tableBytes); err != nil {
			config.Debug("[MYSQL] could not read the charsets of %q, assuming utf8mb4: %v", table, err)
			clear(columns)
			return column, utf8mb4BytesPerChar
		}
		columns[strings.ToLower(name)] = columnBytes
		tableDefault = max(tableDefault, tableBytes)
	}
	if err := rows.Err(); err != nil || tableDefault <= 0 {
		return column, utf8mb4BytesPerChar
	}
	return column, tableDefault
}

// fairShares fits the requested lengths (0 meaning unbounded) into available.
// Columns never shrink below their floor; the space left above the floors goes
// to the smallest requests first, and the rest is split between the others.
func fairShares(requests, floors []int, available int) []int {
	allowed := append([]int(nil), floors...)
	for _, floor := range floors {
		available -= floor
	}
	if available <= 0 {
		return allowed
	}
	needs := make([]int, len(requests))
	order := make([]int, len(requests))
	for i := range requests {
		needs[i] = max(effectiveRequest(requests[i])-floors[i], 0)
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return needs[order[a]] < needs[order[b]] })
	for n, i := range order {
		extra := min(needs[i], available/(len(order)-n))
		allowed[i] += extra
		available -= extra
	}
	return allowed
}

func effectiveRequest(length int) int {
	if length <= 0 {
		return math.MaxInt
	}
	return length
}

func isTypeChange(change schemaevolution.SchemaChange) bool {
	return change.Type == schemaevolution.ChangeWidenType || change.Type == schemaevolution.ChangeOverrideType
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *MySQLDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	comparison, err := d.fitKeyWidenings(ctx, table, comparison)
	if err != nil {
		return nil, err
	}
	return destination.ApplyEvolution(ctx, d, &Dialect{}, table, comparison)
}

func (d *MySQLDestination) ApplySchemaEvolutionIfIncarnation(
	ctx context.Context,
	table string,
	comparison *schemaevolution.SchemaComparison,
	expectedIncarnation string,
) ([]string, string, error) {
	if expectedIncarnation == "" {
		return nil, "", fmt.Errorf("cannot conditionally evolve %s without a destination incarnation", table)
	}
	comparison, err := d.fitKeyWidenings(ctx, table, comparison)
	if err != nil {
		return nil, "", err
	}
	statements, warnings, err := destination.RenderEvolution(&Dialect{}, table, comparison)
	if err != nil {
		return nil, "", err
	}
	if len(statements) == 0 {
		current, exists, err := d.CDCTargetIncarnation(ctx, table)
		if err != nil {
			return warnings, "", err
		}
		if !exists || current != expectedIncarnation {
			return warnings, "", fmt.Errorf("MySQL CDC target %q physical incarnation changed before schema evolution", table)
		}
		return warnings, current, nil
	}

	conn, err := d.db.Conn(ctx)
	if err != nil {
		return warnings, "", fmt.Errorf("failed to reserve MySQL schema evolution connection: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, "UNLOCK TABLES")
		_ = conn.Close()
	}()

	locked := false
	lockTable := func() error {
		if _, err := conn.ExecContext(ctx, "LOCK TABLES "+quoteTable(table)+" WRITE"); err != nil {
			return fmt.Errorf("failed to lock MySQL CDC target %q for schema evolution: %w", table, err)
		}
		locked = true
		return nil
	}
	if err := lockTable(); err != nil {
		return warnings, "", err
	}

	current, exists, err := d.mysqlCDCTargetIncarnation(ctx, conn, table)
	if err != nil {
		return warnings, "", err
	}
	if !exists || current != expectedIncarnation {
		return warnings, "", fmt.Errorf("MySQL CDC target %q physical incarnation changed before schema evolution", table)
	}

	database, tableName := splitDatabaseTable(table)
	if database == "" {
		database = d.database
	}
	var originalComment, sqlMode string
	if err := conn.QueryRowContext(
		ctx,
		`SELECT TABLE_COMMENT FROM information_schema.tables WHERE table_schema = ? AND table_name = ?`,
		database, tableName,
	).Scan(&originalComment); err != nil {
		return warnings, "", fmt.Errorf("failed to read MySQL CDC target %q table comment: %w", table, err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT @@SESSION.sql_mode").Scan(&sqlMode); err != nil {
		return warnings, "", fmt.Errorf("failed to read MySQL SQL mode before schema evolution: %w", err)
	}
	guard, err := newMySQLSchemaEvolutionGuard()
	if err != nil {
		return warnings, "", err
	}

	guardMayExist := false
	restoreGuard := func() error {
		if !guardMayExist {
			return nil
		}
		if !locked {
			if err := lockTable(); err != nil {
				return err
			}
		}
		matches, err := mysqlTableCommentMatches(ctx, conn, database, tableName, guard)
		if err != nil {
			return err
		}
		if !matches {
			guardMayExist = false
			return nil
		}
		restoreSQL := fmt.Sprintf(
			"ALTER TABLE %s COMMENT = %s, ALGORITHM=INPLACE",
			quoteTable(table),
			mysqlStringLiteral(originalComment, sqlMode),
		)
		_, err = conn.ExecContext(ctx, restoreSQL)
		locked = false
		if err != nil {
			return fmt.Errorf("failed to restore MySQL CDC target %q table comment: %w", table, err)
		}
		guardMayExist = false
		return nil
	}
	fail := func(applyErr error) ([]string, string, error) {
		return warnings, "", errors.Join(applyErr, restoreGuard())
	}

	guardLiteral := mysqlStringLiteral(guard, sqlMode)
	for _, statement := range statements {
		guardMayExist = true
		guardedStatement := statement + ", COMMENT = " + guardLiteral
		_, err := conn.ExecContext(ctx, guardedStatement)
		locked = false
		if err != nil {
			return fail(fmt.Errorf("apply schema evolution: %s: %w", statement, err))
		}
		if err := lockTable(); err != nil {
			return fail(err)
		}
		matches, err := mysqlTableCommentMatches(ctx, conn, database, tableName, guard)
		if err != nil {
			return fail(err)
		}
		if !matches {
			guardMayExist = false
			return warnings, "", fmt.Errorf("MySQL CDC target %q was replaced during schema evolution", table)
		}
	}

	resultIncarnation, exists, err := d.mysqlCDCTargetIncarnation(ctx, conn, table)
	if err != nil {
		return fail(err)
	}
	if !exists || resultIncarnation == "" {
		return fail(fmt.Errorf("MySQL CDC target %q disappeared during schema evolution", table))
	}
	if err := restoreGuard(); err != nil {
		return warnings, "", err
	}
	return warnings, resultIncarnation, nil
}

func newMySQLSchemaEvolutionGuard() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("failed to generate MySQL schema evolution guard: %w", err)
	}
	return "__ingestr_cdc_schema_guard_" + hex.EncodeToString(token[:]), nil
}

func mysqlTableCommentMatches(ctx context.Context, q mysqlCDCQueryRower, database, table, expected string) (bool, error) {
	var matches bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = ? AND table_name = ? AND table_comment = ?
	)`, database, table, expected).Scan(&matches)
	if err != nil {
		return false, fmt.Errorf("failed to verify MySQL schema evolution guard: %w", err)
	}
	return matches, nil
}

func mysqlStringLiteral(value, sqlMode string) string {
	if !strings.Contains(strings.ToUpper(sqlMode), "NO_BACKSLASH_ESCAPES") {
		value = strings.ReplaceAll(value, `\`, `\\`)
	}
	value = strings.ReplaceAll(value, `'`, `''`)
	return `'` + value + `'`
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *MySQLDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
