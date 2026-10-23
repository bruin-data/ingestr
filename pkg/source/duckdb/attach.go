package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
)

// PinSingleConnection pins the *sql.DB pool to exactly one physical connection,
// so ATTACH/USE/secret state set by the bootstrap is visible to every query.
func PinSingleConnection(db *sql.DB) {
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
}

// RunAttachStatements executes bootstrap statements in order, stopping at the
// first failure. onErr may return a friendlier error for a failed statement.
func RunAttachStatements(ctx context.Context, db *sql.DB, tag string, stmts []string, onErr func(stmt string, err error) error) error {
	for _, stmt := range stmts {
		config.Debug("[%s] %s", tag, RedactSecret(stmt))
		if err := execStmt(ctx, db, stmt); err != nil {
			if onErr != nil {
				return onErr(stmt, err)
			}
			return BootstrapError(stmt, err)
		}
	}
	return nil
}

func BootstrapError(stmt string, err error) error {
	return fmt.Errorf("bootstrap failed at %q: %w", FirstLine(stmt), err)
}

func RedactSecret(sql string) string {
	if isSecretStatement(sql) {
		return "CREATE OR REPLACE SECRET (redacted)"
	}
	return sql
}

func isSecretStatement(sql string) bool {
	return strings.HasPrefix(strings.TrimSpace(sql), "CREATE OR REPLACE SECRET")
}

func execStmt(ctx context.Context, db *sql.DB, stmt string) error {
	rows, err := db.QueryContext(ctx, stmt)
	if err != nil {
		return err
	}
	return rows.Close()
}

func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func sanitizeIdentifier(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func escapeSQLStringLiteral(s string) string { return strings.ReplaceAll(s, "'", "''") }
func quoteSQLStringLiteral(s string) string  { return "'" + escapeSQLStringLiteral(s) + "'" }
