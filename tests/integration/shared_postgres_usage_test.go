//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

var identRE = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sharedPostgresURI(t *testing.T, role string) string {
	t.Helper()
	switch role {
	case "source":
		if pgSource.uri == "" {
			t.Skip("shared postgres source container not available")
		}
		return pgSource.uri
	case "dest":
		if pgDest.uri == "" {
			t.Skip("shared postgres dest container not available")
		}
		return pgDest.uri
	default:
		t.Fatalf("unknown postgres role: %s", role)
		return ""
	}
}

func uniqueSchemaName(t *testing.T, prefix string) string {
	t.Helper()
	name := strings.ToLower(prefix + "_" + t.Name())
	name = identRE.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if len(name) > 32 {
		name = name[:32]
	}
	return fmt.Sprintf("%s_%d", name, time.Now().UnixNano())
}

func ensurePostgresSchema(t *testing.T, ctx context.Context, uri string, schema string) {
	t.Helper()
	db, err := sql.Open("pgx", uri)
	requireNoErr(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS "%s"`, schema))
	requireNoErr(t, err)
}

func dropPostgresSchema(t *testing.T, ctx context.Context, uri string, schema string) {
	t.Helper()
	db, err := sql.Open("pgx", uri)
	if err != nil {
		return
	}
	defer func() { _ = db.Close() }()
	_, _ = db.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema))
}

func pqIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func pqTable(schema, table string) string {
	return pqIdent(schema) + "." + pqIdent(table)
}

// requireNoErr avoids pulling testify into this helper file; callers already use require.
func requireNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
