package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ExpectedRow struct {
	ID     string
	Fields map[string]any
}

type TableExpectation struct {
	SourceTable         string
	DestTable           string
	KeyColumn           string
	ExpectedRowCount    int
	MinExpectedRowCount int
	Rows                []ExpectedRow
	ExpectedSchema      []schema.Column
	IntervalStart       *time.Time
	IntervalEnd         *time.Time
	ExcludeColumns      []string
	IncrementalStrategy config.IncrementalStrategy
	PrimaryKeys         []string
	FullRefresh         bool
}

func RunPipeline(t *testing.T, ctx context.Context, sourceURI, destURI string, exp TableExpectation) {
	t.Helper()

	strategy := config.StrategyReplace
	if exp.IncrementalStrategy != "" {
		strategy = exp.IncrementalStrategy
	}

	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		DestURI:             destURI,
		SourceTable:         exp.SourceTable,
		DestTable:           exp.DestTable,
		IncrementalStrategy: strategy,
		PrimaryKeys:         exp.PrimaryKeys,
		IntervalStart:       exp.IntervalStart,
		IntervalEnd:         exp.IntervalEnd,
		SQLExcludeColumns:   exp.ExcludeColumns,
		FullRefresh:         exp.FullRefresh,
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))
}

func Check(t *testing.T, destURI string, exp TableExpectation) {
	t.Helper()

	db, err := openDuckDB(destURI)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	if exp.ExpectedRowCount > 0 {
		checkRowCount(t, db, exp)
	}
	if exp.MinExpectedRowCount > 0 {
		checkMinRowCount(t, db, exp)
	}
	checkSchema(t, db, exp)
	if exp.KeyColumn != "" {
		checkNoDuplicates(t, db, exp)
	}
	if len(exp.Rows) > 0 {
		checkRows(t, db, exp)
	}
}

func checkRowCount(t *testing.T, db *sql.DB, exp TableExpectation) {
	t.Helper()

	var count int
	require.NoError(t, db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", exp.DestTable)).Scan(&count))
	assert.Equal(t, exp.ExpectedRowCount, count, "Table %s row count mismatch", exp.SourceTable)
	t.Logf("%s: %d rows ingested", exp.SourceTable, count)
}

func checkMinRowCount(t *testing.T, db *sql.DB, exp TableExpectation) {
	t.Helper()

	var count int
	require.NoError(t, db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", exp.DestTable)).Scan(&count))
	assert.GreaterOrEqual(t, count, exp.MinExpectedRowCount, "Table %s should have at least %d rows, got %d", exp.SourceTable, exp.MinExpectedRowCount, count)
	t.Logf("%s: %d rows ingested (expected >= %d)", exp.SourceTable, count, exp.MinExpectedRowCount)
}

func checkSchema(t *testing.T, db *sql.DB, exp TableExpectation) {
	t.Helper()

	excluded := toSet(exp.ExcludeColumns)
	types, err := duckdbSchemaTypes(db, exp.DestTable)
	require.NoError(t, err)

	for _, col := range exp.ExpectedSchema {
		if excluded[col.Name] {
			continue
		}
		actual, exists := types[strings.ToLower(col.Name)]
		assert.True(t, exists, "Column %q should exist in %s", col.Name, exp.DestTable)
		if exists {
			expected := schemaTypeToDuckDB(col.DataType)
			assert.Equal(t, expected, actual, "Column %q type mismatch in %s", col.Name, exp.DestTable)
		}
	}
}

func checkNoDuplicates(t *testing.T, db *sql.DB, exp TableExpectation) {
	t.Helper()

	var total, distinct int
	require.NoError(t, db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", exp.DestTable)).Scan(&total))
	require.NoError(t, db.QueryRow(fmt.Sprintf("SELECT COUNT(DISTINCT CAST(%s AS VARCHAR)) FROM %s", exp.KeyColumn, exp.DestTable)).Scan(&distinct))
	assert.Equal(t, total, distinct, "Table %s has duplicate rows on key column %q", exp.DestTable, exp.KeyColumn)
}

func checkRows(t *testing.T, db *sql.DB, exp TableExpectation) {
	t.Helper()

	require.NotEmpty(t, exp.KeyColumn, "KeyColumn must be set when checking rows")
	keyCol := exp.KeyColumn

	excluded := toSet(exp.ExcludeColumns)
	for _, row := range exp.Rows {
		t.Run(fmt.Sprintf("%s=%s", keyCol, row.ID), func(t *testing.T) {
			cols := make([]string, 0, len(row.Fields))
			for col := range row.Fields {
				if excluded[col] {
					continue
				}
				cols = append(cols, col)
			}
			sort.Strings(cols)

			query := fmt.Sprintf(
				"SELECT %s FROM %s WHERE CAST(%s AS VARCHAR) = ?",
				strings.Join(cols, ", "),
				exp.DestTable,
				keyCol,
			)

			scanDest := make([]any, len(cols))
			scanPtrs := make([]any, len(cols))
			for i := range scanDest {
				scanPtrs[i] = &scanDest[i]
			}

			err := db.QueryRow(query, row.ID).Scan(scanPtrs...)
			require.NoError(t, err, "Row with %s=%s should exist in %s", keyCol, row.ID, exp.DestTable)

			for i, col := range cols {
				expected := row.Fields[col]
				actual := scanDest[i]

				if expected == nil {
					assert.Nil(t, actual, "Column %s should be NULL for %s=%s", col, keyCol, row.ID)
					continue
				}

				if b, ok := actual.([]byte); ok {
					actual = string(b)
				}

				switch ev := expected.(type) {
				case string:
					assert.Equal(t, ev, fmt.Sprintf("%v", actual), "Column %s mismatch for %s=%s", col, keyCol, row.ID)
				case bool:
					assert.Equal(t, ev, actual, "Column %s mismatch for %s=%s", col, keyCol, row.ID)
				case int:
					assert.EqualValues(t, ev, actual, "Column %s mismatch for %s=%s", col, keyCol, row.ID)
				case int64:
					assert.EqualValues(t, ev, actual, "Column %s mismatch for %s=%s", col, keyCol, row.ID)
				case float64:
					assert.InDelta(t, ev, actual, 0.001, "Column %s mismatch for %s=%s", col, keyCol, row.ID)
				case time.Time:
					if at, ok := actual.(time.Time); ok {
						assert.True(t, ev.Equal(at), "Column %s mismatch for %s=%s: expected %v, got %v", col, keyCol, row.ID, ev, at)
					} else {
						assert.Fail(t, fmt.Sprintf("Column %s expected time.Time but got %T for %s=%s", col, actual, keyCol, row.ID))
					}
				default:
					assert.Equal(t, fmt.Sprintf("%v", expected), fmt.Sprintf("%v", actual), "Column %s mismatch for %s=%s", col, keyCol, row.ID)
				}
			}
		})
	}
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func openDuckDB(uri string) (*sql.DB, error) {
	path := strings.TrimPrefix(uri, "duckdb:///")
	return sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
}

func duckdbSchemaTypes(db *sql.DB, table string) (map[string]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info('%s')", table))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var cid int
		var nameRaw, ctypeRaw []byte
		var notnull bool
		var dflt interface{}
		var pk bool
		if err := rows.Scan(&cid, &nameRaw, &ctypeRaw, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[strings.ToLower(string(nameRaw))] = strings.ToLower(strings.TrimSpace(string(ctypeRaw)))
	}
	return out, rows.Err()
}

var schemaTypeToDuckDBMap = map[schema.DataType]string{
	schema.TypeString:      "varchar",
	schema.TypeBoolean:     "boolean",
	schema.TypeInt16:       "smallint",
	schema.TypeInt32:       "integer",
	schema.TypeInt64:       "bigint",
	schema.TypeFloat32:     "real",
	schema.TypeFloat64:     "double",
	schema.TypeDecimal:     "decimal(38,9)",
	schema.TypeBinary:      "blob",
	schema.TypeDate:        "date",
	schema.TypeTime:        "time",
	schema.TypeTimestamp:   "timestamp",
	schema.TypeTimestampTZ: "timestamp with time zone",
	schema.TypeJSON:        "json",
	schema.TypeUUID:        "uuid",
}

func schemaTypeToDuckDB(dt schema.DataType) string {
	if s, ok := schemaTypeToDuckDBMap[dt]; ok {
		return s
	}
	return "varchar"
}
