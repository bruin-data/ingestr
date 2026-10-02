//go:build integration

package integration

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/require"
)

func TestMySQLMergeWidensStringPrimaryKeyToIndexableLength(t *testing.T) {
	if mysqlDest.uri == "" {
		t.Skip("shared mysql destination container not available")
	}
	db, err := sql.Open("mysql", mysqlDSN(mysqlDest.uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, length := range []int{0, 100, 768} {
		t.Run(fmt.Sprintf("length=%d", length), func(t *testing.T) {
			table := "attachment_inventory_" + uniqueSuffix()
			t.Cleanup(func() { _, _ = db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS `%s`", table)) })
			if length > 0 {
				_, err := db.ExecContext(t.Context(), fmt.Sprintf("CREATE TABLE `%s` (attachment_key VARCHAR(%d) PRIMARY KEY, message_id VARCHAR(100))", table, length))
				require.NoError(t, err)
				_, err = db.ExecContext(t.Context(), fmt.Sprintf("INSERT INTO `%s` VALUES ('key1', 'old')", table))
				require.NoError(t, err)
			}

			path := filepath.Join(t.TempDir(), "attachments.jsonl")
			require.NoError(t, os.WriteFile(path, []byte("{\"attachment_key\":\"key1\",\"message_id\":\"updated\"}\n{\"attachment_key\":\"key2\",\"message_id\":\"new\"}\n"), 0o600))
			cfg := config.DefaultConfig()
			cfg.SourceURI = "jsonl://" + path
			cfg.SourceTable = "attachments"
			cfg.DestURI = mysqlDest.uri
			cfg.DestTable = table
			cfg.IncrementalStrategy = config.StrategyMerge
			cfg.PrimaryKeys = []string{"attachment_key"}
			cfg.NoLoadTimestamp = true
			cfg.NoRunID = true
			cfg.Yes = true
			require.NoError(t, cfg.Validate())
			require.NoError(t, pipeline.New(cfg).Run(t.Context()))
			require.NoError(t, pipeline.New(cfg).Run(t.Context()))

			var count int
			require.NoError(t, db.QueryRowContext(t.Context(), fmt.Sprintf("SELECT COUNT(*) FROM `%s`", table)).Scan(&count))
			require.Equal(t, 2, count)
			var message string
			require.NoError(t, db.QueryRowContext(t.Context(), fmt.Sprintf("SELECT message_id FROM `%s` WHERE attachment_key = 'key1'", table)).Scan(&message))
			require.Equal(t, "updated", message)
			var columnType, columnKey string
			require.NoError(t, db.QueryRowContext(t.Context(),
				"SELECT COLUMN_TYPE, COLUMN_KEY FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = 'attachment_key'", table,
			).Scan(&columnType, &columnKey))
			require.Equal(t, "varchar(768)", columnType)
			require.Equal(t, "PRI", columnKey)
		})
	}
}
