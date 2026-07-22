package mysql

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *MySQLDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
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
