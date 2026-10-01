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

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

// maxPrimaryKeyStringLength is the widest utf8mb4 VARCHAR an InnoDB key can
// index: 3072 bytes at 4 bytes per character.
const maxPrimaryKeyStringLength = 768

func (d *MySQLDestination) NormalizeSchemaEvolutionSourceColumn(source, dest schema.Column) schema.Column {
	// An unbounded source widens a string key to the indexable maximum, never to TEXT.
	if dest.IsPrimaryKey && dest.DataType == schema.TypeString && dest.MaxLength > 0 &&
		source.DataType == schema.TypeString && source.MaxLength <= 0 {
		source.MaxLength = max(dest.MaxLength, maxPrimaryKeyStringLength)
	}
	return source
}

// stringKeyBudget is how many utf8mb4 characters the string columns of a key
// can share, reserving 8 bytes for each non-string key column.
func stringKeyBudget(nonStringKeys int) int {
	return maxPrimaryKeyStringLength - 2*nonStringKeys
}

// boundKeyColumns gives unbounded string key columns an equal share of the
// key budget left by the other key columns, since TEXT cannot be a key.
func boundKeyColumns(columns []schema.Column, primaryKeys []string) []schema.Column {
	nonStringKeys, unbounded := 0, 0
	remaining := 0
	for _, col := range columns {
		if !containsFold(primaryKeys, col.Name) {
			continue
		}
		switch {
		case col.DataType != schema.TypeString:
			nonStringKeys++
		case col.MaxLength <= 0:
			unbounded++
		default:
			remaining -= col.MaxLength
		}
	}
	if unbounded == 0 {
		return columns
	}
	share := (stringKeyBudget(nonStringKeys) + remaining) / unbounded
	if share < 1 {
		share = maxPrimaryKeyStringLength / unbounded
	}
	bounded := append([]schema.Column(nil), columns...)
	for i, col := range bounded {
		if containsFold(primaryKeys, col.Name) && col.DataType == schema.TypeString && col.MaxLength <= 0 {
			bounded[i].MaxLength = share
		}
	}
	return bounded
}

// fitKeyWidenings caps widenings of string key columns so a composite key
// stays within the InnoDB key limit; a single key is already bounded by
// NormalizeSchemaEvolutionSourceColumn.
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
	if err != nil || current == nil || len(current.PrimaryKeys) < 2 {
		return comparison, err
	}

	nonStringKeys, fixed := 0, 0
	for _, col := range current.Columns {
		if !containsFold(current.PrimaryKeys, col.Name) {
			continue
		}
		switch {
		case col.DataType != schema.TypeString:
			nonStringKeys++
		case !widened[strings.ToLower(col.Name)]:
			fixed += col.MaxLength
		}
	}

	var keyChanges, requests, floors []int
	for i, change := range comparison.Changes {
		if isTypeChange(change) && change.OldColumn != nil && change.NewColumn.DataType == schema.TypeString &&
			containsFold(current.PrimaryKeys, change.ColumnName) {
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
	allowed := fairShares(requests, floors, stringKeyBudget(nonStringKeys)-fixed)

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
