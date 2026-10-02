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
