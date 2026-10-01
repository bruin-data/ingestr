//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	mssqldest "github.com/bruin-data/ingestr/pkg/destination/mssql"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/require"
)

func TestMSSQLMergeWidensStringPrimaryKeyToIndexableLength(t *testing.T) {
	if mssqlDest.uri == "" {
		t.Skip("shared SQL Server destination container not available")
	}
	db := openMSSQLTestDB(t, mssqlDest.uri)
	t.Cleanup(func() { _ = db.Close() })
	dest := mssqldest.NewMSSQLDestination()
	require.NoError(t, dest.Connect(t.Context(), mssqlDest.uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })

	for _, length := range []int{100, 450} {
		t.Run(fmt.Sprintf("length=%d", length), func(t *testing.T) {
			table := "dbo.attachment_inventory_" + uniqueSuffix()
			quoted := quoteTableMSSQL(table)
			_, err := db.ExecContext(t.Context(), fmt.Sprintf(`CREATE TABLE %s (
				attachment_key NVARCHAR(%d) NOT NULL PRIMARY KEY,
				message_id NVARCHAR(100) NULL
			)`, quoted, length))
			require.NoError(t, err)
			t.Cleanup(func() { dropMSSQLTable(t, context.Background(), db, table) })
			_, err = db.ExecContext(t.Context(), "INSERT INTO "+quoted+" VALUES (N'key1', N'old')")
			require.NoError(t, err)

			path := filepath.Join(t.TempDir(), "attachments.jsonl")
			require.NoError(t, os.WriteFile(path, []byte("{\"attachment_key\":\"key1\",\"message_id\":\"updated\"}\n{\"attachment_key\":\"key2\",\"message_id\":\"new\"}\n"), 0o600))
			cfg := config.DefaultConfig()
			cfg.SourceURI = "jsonl://" + path
			cfg.SourceTable = "attachments"
			cfg.DestURI = mssqlDest.uri
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
			require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+quoted).Scan(&count))
			require.Equal(t, 2, count)
			var message string
			require.NoError(t, db.QueryRowContext(t.Context(), "SELECT message_id FROM "+quoted+" WHERE attachment_key = N'key1'").Scan(&message))
			require.Equal(t, "updated", message)
			actual, err := dest.GetTableSchema(t.Context(), table)
			require.NoError(t, err)
			require.Equal(t, []string{"attachment_key"}, actual.PrimaryKeys)
			require.True(t, actual.Columns[0].IsPrimaryKey)
			require.False(t, actual.Columns[0].Nullable)
			require.Equal(t, max(length, 450), actual.Columns[0].MaxLength)
			require.Equal(t, -1, actual.Columns[1].MaxLength)
		})
	}
}

func TestMSSQLMergeExplainsPrimaryKeyValueOverIndexLimit(t *testing.T) {
	if mssqlDest.uri == "" {
		t.Skip("shared SQL Server destination container not available")
	}
	db := openMSSQLTestDB(t, mssqlDest.uri)
	t.Cleanup(func() { _ = db.Close() })

	table := "dbo.attachment_inventory_" + uniqueSuffix()
	quoted := quoteTableMSSQL(table)
	_, err := db.ExecContext(t.Context(), fmt.Sprintf(`CREATE TABLE %s (
		attachment_key NVARCHAR(600) NOT NULL PRIMARY KEY,
		message_id NVARCHAR(MAX) NULL
	)`, quoted))
	require.NoError(t, err)
	t.Cleanup(func() { dropMSSQLTable(t, context.Background(), db, table) })

	path := filepath.Join(t.TempDir(), "attachments.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf("{\"attachment_key\":%q,\"message_id\":\"new\"}\n", strings.Repeat("k", 500))), 0o600))
	cfg := config.DefaultConfig()
	cfg.SourceURI = "jsonl://" + path
	cfg.SourceTable = "attachments"
	cfg.DestURI = mssqlDest.uri
	cfg.DestTable = table
	cfg.IncrementalStrategy = config.StrategyMerge
	cfg.PrimaryKeys = []string{"attachment_key"}
	cfg.NoLoadTimestamp = true
	cfg.NoRunID = true
	cfg.Yes = true
	require.NoError(t, cfg.Validate())

	err = pipeline.New(cfg).Run(t.Context())
	require.ErrorContains(t, err, "key value is too long for index \"PK__")
	require.ErrorContains(t, err, "exceeds the maximum length of 900 bytes")
}
