//go:build integration

package integration

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/require"
)

func TestOracleMergeWidensStringPrimaryKey(t *testing.T) {
	t.Parallel()
	if oracleDest.uri == "" {
		t.Skip("shared oracle destination container not available")
	}
	db, err := sql.Open("oracle", oracleSQLConnString(oracleDest.uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, tc := range []struct {
		name    string
		columns string
		want    int
	}{
		{name: "unbounded source", want: 4000},
		{name: "explicit override", columns: "ATTACHMENT_KEY:varchar(200)", want: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table := strings.ToUpper("attachment_inventory_" + uniqueSuffix())
			t.Cleanup(func() { _, _ = db.Exec(fmt.Sprintf(`DROP TABLE "%s" PURGE`, table)) })
			_, err := db.ExecContext(t.Context(), fmt.Sprintf(`CREATE TABLE "%s" ("ATTACHMENT_KEY" VARCHAR2(100 CHAR) PRIMARY KEY, "MESSAGE_ID" CLOB)`, table))
			require.NoError(t, err)
			_, err = db.ExecContext(t.Context(), fmt.Sprintf(`INSERT INTO "%s" VALUES ('key1', 'old')`, table))
			require.NoError(t, err)

			path := filepath.Join(t.TempDir(), "attachments.jsonl")
			key2 := strings.Repeat("k", 150)
			require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf("{\"ATTACHMENT_KEY\":\"key1\",\"MESSAGE_ID\":\"updated\"}\n{\"ATTACHMENT_KEY\":%q,\"MESSAGE_ID\":\"new\"}\n", key2)), 0o600))
			cfg := config.DefaultConfig()
			cfg.SourceURI = "jsonl://" + path
			cfg.SourceTable = "attachments"
			cfg.DestURI = oracleDest.uri
			cfg.DestTable = table
			cfg.IncrementalStrategy = config.StrategyMerge
			cfg.PrimaryKeys = []string{"ATTACHMENT_KEY"}
			cfg.NoLoadTimestamp = true
			cfg.NoRunID = true
			cfg.Yes = true
			if tc.columns != "" {
				cfg.Columns = tc.columns
			}
			require.NoError(t, cfg.Validate())
			require.NoError(t, pipeline.New(cfg).Run(t.Context()))

			var count int
			require.NoError(t, db.QueryRowContext(t.Context(), fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, table)).Scan(&count))
			require.Equal(t, 2, count)
			var length int
			require.NoError(t, db.QueryRowContext(t.Context(),
				"SELECT CHAR_LENGTH FROM USER_TAB_COLUMNS WHERE TABLE_NAME = :1 AND COLUMN_NAME = 'ATTACHMENT_KEY'", table,
			).Scan(&length))
			require.Equal(t, tc.want, length)
		})
	}
}

// TestOracleCustomQueryAppendsToTypedTable is the #1464 regression: a custom query must
// keep NUMBER precision and must not try to turn an existing VARCHAR2 column into CLOB.
func TestOracleCustomQueryAppendsToTypedTable(t *testing.T) {
	t.Parallel()
	if oracleDest.uri == "" {
		t.Skip("shared oracle destination container not available")
	}
	db, err := sql.Open("oracle", oracleSQLConnString(oracleDest.uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	suffix := strings.ToUpper(uniqueSuffix())
	src, dst := "CQ_SRC_"+suffix, "CQ_DST_"+suffix
	t.Cleanup(func() {
		_, _ = db.Exec(fmt.Sprintf(`DROP TABLE "%s" PURGE`, src))
		_, _ = db.Exec(fmt.Sprintf(`DROP TABLE "%s" PURGE`, dst))
	})
	for _, stmt := range []string{
		fmt.Sprintf(`CREATE TABLE "%s" ("ID" NUMBER(10,0), "NAME" VARCHAR2(16 CHAR), "AMT" NUMBER(12,2), "YR" NUMBER(4,0), "CODE" NUMBER(8,0))`, src),
		fmt.Sprintf(`INSERT INTO "%s" VALUES (1, 'first', 10.25, 2024, 12345678)`, src),
		fmt.Sprintf(`INSERT INTO "%s" VALUES (2, 'second', 20.5, 2025, 87654321)`, src),
		fmt.Sprintf(`CREATE TABLE "%s" ("ID" NUMBER(10,0), "NAME" VARCHAR2(16 CHAR), "AMT" NUMBER(12,2), "YR" NUMBER(4,0), "CODE" NUMBER(8,0))`, dst),
	} {
		_, err := db.ExecContext(t.Context(), stmt)
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	cfg.SourceURI = oracleDest.uri
	cfg.SourceTable = fmt.Sprintf(`query:SELECT "ID", "NAME", "AMT", "YR", "CODE" FROM "%s"`, src)
	cfg.DestURI = oracleDest.uri
	cfg.DestTable = dst
	cfg.IncrementalStrategy = config.StrategyAppend
	cfg.NoLoadTimestamp = true
	cfg.NoRunID = true
	cfg.Yes = true
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(t.Context()))

	var count, yr, code int
	var amt float64
	require.NoError(t, db.QueryRowContext(t.Context(), fmt.Sprintf(`SELECT COUNT(*), SUM("AMT"), SUM("YR"), SUM("CODE") FROM "%s"`, dst)).Scan(&count, &amt, &yr, &code))
	require.Equal(t, 2, count)
	require.InDelta(t, 30.75, amt, 0.001)
	require.Equal(t, 4049, yr)
	require.Equal(t, 99999999, code)

	rows, err := db.QueryContext(t.Context(),
		"SELECT COLUMN_NAME || ' ' || DATA_TYPE || ' ' || NVL(DATA_PRECISION, CHAR_LENGTH) || ',' || NVL(DATA_SCALE, 0) FROM USER_TAB_COLUMNS WHERE TABLE_NAME = :1 ORDER BY COLUMN_ID", dst)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var columns []string
	for rows.Next() {
		var c string
		require.NoError(t, rows.Scan(&c))
		columns = append(columns, c)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"ID NUMBER 10,0", "NAME VARCHAR2 16,0", "AMT NUMBER 12,2", "YR NUMBER 4,0", "CODE NUMBER 8,0"}, columns)
}

// TestOracleNumber1FlagIntoExistingFlagColumn is the #1464 regression: NUMBER(1,0) flags
// must load into an existing NUMBER(1,0) column, which the destination reads as boolean.
func TestOracleNumber1FlagIntoExistingFlagColumn(t *testing.T) {
	t.Parallel()
	if oracleDest.uri == "" {
		t.Skip("shared oracle destination container not available")
	}
	db, err := sql.Open("oracle", oracleSQLConnString(oracleDest.uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, tc := range []struct{ name, columns string }{
		{name: "inferred"},
		{name: "boolean override", columns: "FLAG:boolean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			suffix := strings.ToUpper(uniqueSuffix())
			src, dst := "FLAG_SRC_"+suffix, "FLAG_DST_"+suffix
			t.Cleanup(func() {
				_, _ = db.Exec(fmt.Sprintf(`DROP TABLE "%s" PURGE`, src))
				_, _ = db.Exec(fmt.Sprintf(`DROP TABLE "%s" PURGE`, dst))
			})
			for _, stmt := range []string{
				fmt.Sprintf(`CREATE TABLE "%s" ("ID" NUMBER(10,0), "FLAG" NUMBER(1,0))`, src),
				fmt.Sprintf(`INSERT INTO "%s" VALUES (1, 1)`, src),
				fmt.Sprintf(`INSERT INTO "%s" VALUES (2, 0)`, src),
				fmt.Sprintf(`INSERT INTO "%s" VALUES (3, NULL)`, src),
				fmt.Sprintf(`CREATE TABLE "%s" ("ID" NUMBER(10,0), "FLAG" NUMBER(1,0))`, dst),
			} {
				_, err := db.ExecContext(t.Context(), stmt)
				require.NoError(t, err)
			}

			cfg := config.DefaultConfig()
			cfg.SourceURI = oracleDest.uri
			cfg.SourceTable = src
			cfg.DestURI = oracleDest.uri
			cfg.DestTable = dst
			cfg.IncrementalStrategy = config.StrategyAppend
			cfg.Columns = tc.columns
			cfg.NoLoadTimestamp = true
			cfg.NoRunID = true
			cfg.Yes = true
			require.NoError(t, cfg.Validate())
			require.NoError(t, pipeline.New(cfg).Run(t.Context()))

			var got string
			require.NoError(t, db.QueryRowContext(t.Context(), fmt.Sprintf(
				`SELECT LISTAGG("ID" || '=' || NVL(TO_CHAR("FLAG"), 'null'), ',') WITHIN GROUP (ORDER BY "ID") FROM "%s"`, dst)).Scan(&got))
			require.Equal(t, "1=1,2=0,3=null", got)
		})
	}
}
