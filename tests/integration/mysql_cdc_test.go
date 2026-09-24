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
	requireDocker(t)
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
				"--gtid-mode=ON",
				"--enforce-gtid-consistency=ON",
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

func TestMySQLCDC_FullRefreshAndIncremental_PostgresSnakeCase(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	destURI := sharedPostgresURI(t, "dest")
	sourceContainer, sourceURI := setupMySQLCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	sourceDB, err := sql.Open("mysql", mysqlDSN(sourceURI))
	require.NoError(t, err)
	defer func() { _ = sourceDB.Close() }()
	_, err = sourceDB.ExecContext(ctx, `CREATE TABLE MRP (
		ID INT NOT NULL PRIMARY KEY,
		MR_DIS_TYPE VARCHAR(100) NOT NULL
	)`)
	require.NoError(t, err)
	_, err = sourceDB.ExecContext(ctx, `INSERT INTO MRP VALUES (1, 'initial'), (2, 'unchanged')`)
	require.NoError(t, err)

	destSchema := uniqueSchemaName(t, "mysql_cdc_naming")
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, destURI, destSchema) })
	destDB, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	defer func() { _ = destDB.Close() }()

	cdcURI := mysqlCDCURI(t, sourceURI, map[string]string{"mode": "batch", "server_id": "18889"})
	run := func(fullRefresh bool) {
		t.Helper()
		runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cfg := &config.IngestConfig{
			SourceURI:    cdcURI,
			SourceTable:  "MRP",
			DestURI:      destURI,
			DestTable:    destSchema + ".mrp",
			SchemaNaming: "auto",
			FullRefresh:  fullRefresh,
		}
		require.NoError(t, pipeline.New(cfg).Run(runCtx))
	}
	assertRows := func(expected map[int]string) {
		t.Helper()
		rows, err := destDB.QueryContext(ctx, `SELECT id, mr_dis_type, _cdc_deleted FROM `+pqTable(destSchema, "mrp"))
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		actual := make(map[int]string)
		for rows.Next() {
			var id int
			var value string
			var deleted bool
			require.NoError(t, rows.Scan(&id, &value, &deleted))
			assert.False(t, deleted)
			_, duplicate := actual[id]
			assert.False(t, duplicate, "duplicate primary key %d", id)
			actual[id] = value
		}
		require.NoError(t, rows.Err())
		assert.Equal(t, expected, actual)
	}

	run(true)
	assertRows(map[int]string{1: "initial", 2: "unchanged"})
	run(true)
	assertRows(map[int]string{1: "initial", 2: "unchanged"})

	_, err = sourceDB.ExecContext(ctx, `UPDATE MRP SET MR_DIS_TYPE = 'updated' WHERE ID = 1`)
	require.NoError(t, err)
	_, err = sourceDB.ExecContext(ctx, `INSERT INTO MRP VALUES (3, 'inserted')`)
	require.NoError(t, err)
	run(false)
	assertRows(map[int]string{1: "updated", 2: "unchanged", 3: "inserted"})

	var distinctLSNs int
	require.NoError(t, destDB.QueryRowContext(ctx, `SELECT COUNT(DISTINCT _cdc_lsn) FROM `+pqTable(destSchema, "mrp")).Scan(&distinctLSNs))
	assert.Greater(t, distinctLSNs, 1, "incremental changes should retain the unchanged row's snapshot LSN")
}

func TestMySQLCDC_SingleTableRenamedColumns_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceContainer, sourceURI := setupMySQLCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	sourceDB, err := sql.Open("mysql", mysqlDSN(sourceURI))
	require.NoError(t, err)
	defer func() { _ = sourceDB.Close() }()
	for _, stmt := range []string{
		`CREATE TABLE Orders (orderId INT NOT NULL PRIMARY KEY, CustomerName VARCHAR(50) NOT NULL, SHIP_CITY VARCHAR(50) NOT NULL)`,
		`INSERT INTO Orders VALUES (1, 'ann', 'Berlin'), (2, 'bob', 'Paris')`,
	} {
		_, err := sourceDB.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}

	destURI := sharedPostgresURI(t, "dest")
	destSchema := uniqueSchemaName(t, "mysql_cdc_renamed")
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, destURI, destSchema) })
	cfg := &config.IngestConfig{
		SourceURI:   mysqlCDCURI(t, sourceURI, map[string]string{"mode": "batch", "server_id": "18890"}),
		SourceTable: "Orders",
		DestURI:     destURI,
		DestTable:   destSchema + ".orders",
	}
	run := func() {
		t.Helper()
		runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		require.NoError(t, pipeline.New(cfg).Run(runCtx))
	}
	run()
	dest, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })
	destTable := pqTable(destSchema, "orders")
	requireRenamedCDCOrders(t, dest, destTable, map[int64]namingSourceRow{1: {"ann", "Berlin"}, 2: {"bob", "Paris"}}, nil)

	for _, stmt := range []string{
		`UPDATE Orders SET CustomerName = 'bob-upd' WHERE orderId = 2`,
		`INSERT INTO Orders VALUES (3, 'cem', 'Rome')`,
		`DELETE FROM Orders WHERE orderId = 1`,
	} {
		_, err := sourceDB.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
	run()
	requireRenamedCDCOrders(t, dest, destTable, map[int64]namingSourceRow{2: {"bob-upd", "Paris"}, 3: {"cem", "Rome"}}, []int64{1})
}
