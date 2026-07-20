//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
)

func setupMySQLCDCContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	t.Helper()

	container, err := tcmysql.Run(
		ctx,
		"mysql:8.0",
		tcmysql.WithDatabase(mysqlDB),
		tcmysql.WithUsername(mysqlUser),
		tcmysql.WithPassword(mysqlPassword),
		testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
			req.Cmd = []string{
				"--server-id=17777",
				"--log-bin=mysql-bin",
				"--binlog-format=ROW",
				"--binlog-row-image=FULL",
				// Non-UTC server time zone: snapshot and binlog rows must still
				// agree on TIMESTAMP instants.
				"--default-time-zone=+03:00",
			}
			return nil
		}),
	)
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "3306")
	require.NoError(t, err)

	uri := fmt.Sprintf("mysql://%s:%s@%s:%s/%s", mysqlUser, mysqlPassword, host, port.Port(), mysqlDB)
	return container, uri
}

func mysqlCDCURI(t *testing.T, baseURI string, params map[string]string) string {
	t.Helper()

	u, err := url.Parse(baseURI)
	require.NoError(t, err)
	u.Scheme = "mysql+cdc"
	q := u.Query()
	for key, value := range params {
		q.Set(key, value)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func insertMySQLCDCItems(t *testing.T, ctx context.Context, db *sql.DB, startID int, endID int) {
	t.Helper()

	const batchSize = 1000
	for batchStart := startID; batchStart <= endID; batchStart += batchSize {
		batchEnd := batchStart + batchSize - 1
		if batchEnd > endID {
			batchEnd = endID
		}

		var query strings.Builder
		args := make([]interface{}, 0, (batchEnd-batchStart+1)*3)
		query.WriteString("INSERT INTO items (id, name, value) VALUES ")
		for id := batchStart; id <= batchEnd; id++ {
			if id > batchStart {
				query.WriteString(",")
			}
			query.WriteString("(?, ?, ?)")
			args = append(args, id, fmt.Sprintf("item%d", id), id*100)
		}

		_, err := db.ExecContext(ctx, query.String(), args...)
		require.NoError(t, err)
	}
}

func TestMySQLCDC_SnapshotAndIncremental_MySQL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	const initialRows = 10000
	const newID = initialRows + 1
	const pathAgreementID = initialRows + 2

	ctx := context.Background()
	sourceContainer, sourceURI := setupMySQLCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	sourceDB, err := sql.Open("mysql", mysqlDSN(sourceURI))
	require.NoError(t, err)
	defer func() { _ = sourceDB.Close() }()

	_, err = sourceDB.ExecContext(ctx, `CREATE TABLE items (
		id INT NOT NULL PRIMARY KEY,
		name VARCHAR(100) NOT NULL,
		value INT NULL,
		big_unsigned BIGINT UNSIGNED NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	require.NoError(t, err)
	insertMySQLCDCItems(t, ctx, sourceDB, 1, initialRows)
	// Row 5 arrives via the snapshot; a row with identical values arrives later
	// via the binlog. The destination must agree on both paths.
	_, err = sourceDB.ExecContext(ctx, `UPDATE items SET big_unsigned = 18446744073709551615, updated_at = '2026-01-02 03:04:05' WHERE id = 5`)
	require.NoError(t, err)

	cfg := &config.IngestConfig{
		SourceURI:   mysqlCDCURI(t, sourceURI, map[string]string{"mode": "batch", "server_id": "18888"}),
		SourceTable: "items",
		DestURI:     sourceURI,
		DestTable:   "items_dest",
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	queryCount := func(query string) int {
		t.Helper()
		var n int
		require.NoError(t, sourceDB.QueryRow(query).Scan(&n))
		return n
	}

	assert.Equal(t, initialRows, queryCount(`SELECT COUNT(*) FROM items_dest`))
	assert.Equal(t, 0, queryCount("SELECT COUNT(*) FROM items_dest WHERE `_cdc_deleted` = true"))
	firstDistinctLSNs := queryCount("SELECT COUNT(DISTINCT `_cdc_lsn`) FROM items_dest")
	assert.Equal(t, 1, firstDistinctLSNs)

	_, err = sourceDB.ExecContext(ctx, `INSERT INTO items (id, name, value) VALUES (?, ?, ?)`, newID, fmt.Sprintf("item%d", newID), 400)
	require.NoError(t, err)
	_, err = sourceDB.ExecContext(ctx, `UPDATE items SET value = 150 WHERE id = 1`)
	require.NoError(t, err)
	_, err = sourceDB.ExecContext(ctx, `DELETE FROM items WHERE id = 2`)
	require.NoError(t, err)
	_, err = sourceDB.ExecContext(ctx, `UPDATE items SET name = 'item3_final', value = 999 WHERE id = 3`)
	require.NoError(t, err)
	_, err = sourceDB.ExecContext(ctx, `DELETE FROM items WHERE id = 3`)
	require.NoError(t, err)
	_, err = sourceDB.ExecContext(ctx, fmt.Sprintf(`INSERT INTO items (id, name, value, big_unsigned, updated_at) VALUES (%d, 'twin', 500, 18446744073709551615, '2026-01-02 03:04:05')`, pathAgreementID))
	require.NoError(t, err)

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	require.NoError(t, pipeline.New(cfg).Run(ctxWithTimeout))

	assert.Equal(t, initialRows+2, queryCount(`SELECT COUNT(*) FROM items_dest`))
	assert.Equal(t, 1, queryCount("SELECT COUNT(*) FROM items_dest WHERE id = 1 AND value = 150 AND `_cdc_deleted` = false"), "plain update should be applied")
	assert.Equal(t, 1, queryCount("SELECT COUNT(*) FROM items_dest WHERE id = 2 AND value = 200 AND `_cdc_deleted` = true"), "delete should be soft-applied")
	assert.Equal(t, 1, queryCount("SELECT COUNT(*) FROM items_dest WHERE id = 3 AND name = 'item3_final' AND value = 999 AND `_cdc_deleted` = true"), "update then delete should keep last values")
	assert.Equal(t, 1, queryCount(fmt.Sprintf("SELECT COUNT(*) FROM items_dest WHERE id = %d AND name = 'item%d' AND value = 400 AND `_cdc_deleted` = false", newID, newID)), "insert should be applied")
	assert.Greater(t, queryCount("SELECT COUNT(DISTINCT `_cdc_lsn`) FROM items_dest"), firstDistinctLSNs)

	agreementIDs := fmt.Sprintf("(5, %d)", pathAgreementID)
	assert.Equal(t, 1, queryCount(`SELECT COUNT(DISTINCT updated_at) FROM items_dest WHERE id IN `+agreementIDs), "snapshot and binlog rows must agree on TIMESTAMP instants despite the non-UTC server time zone")
	assert.Equal(t, 1, queryCount(`SELECT COUNT(DISTINCT big_unsigned) FROM items_dest WHERE id IN `+agreementIDs), "snapshot and binlog rows must agree on BIGINT UNSIGNED values")
	assert.Equal(t, 2, queryCount(`SELECT COUNT(*) FROM items_dest WHERE id IN `+agreementIDs+` AND big_unsigned > 9.3e18`), "BIGINT UNSIGNED must keep its unsigned range instead of clamping to MaxInt64 or wrapping negative")
}
