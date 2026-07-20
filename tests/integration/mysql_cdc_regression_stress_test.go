//go:build stress

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/require"
)

const mysqlCDCRegressionTable = "cdc_regression_items"

type mysqlCDCRegressionHarness struct {
	sourceURI string
	destURI   string
	sourceDB  *sql.DB
	destDB    *sql.DB
}

func newMySQLCDCRegressionHarness(t *testing.T, ctx context.Context, sourceOptions ...string) *mysqlCDCRegressionHarness {
	t.Helper()

	sourceCommand := []string{
		"--server-id=21777",
		"--log-bin=mysql-bin",
		"--binlog-format=ROW",
		"--binlog-row-image=FULL",
		"--max-connections=100",
	}
	sourceCommand = append(sourceCommand, sourceOptions...)
	_, sourceURI, sourceDSN := mysqlStressContainer(t, ctx, sourceCommand)
	_, destURI, destDSN := mysqlStressContainer(t, ctx, []string{"--max-connections=100"})

	return &mysqlCDCRegressionHarness{
		sourceURI: mysqlCDCRegressionSourceURI(t, sourceURI),
		destURI:   destURI,
		sourceDB:  mysqlStressOpenDB(t, sourceDSN, 12),
		destDB:    mysqlStressOpenDB(t, destDSN, 12),
	}
}

func mysqlCDCRegressionSourceURI(t *testing.T, rawURI string) string {
	t.Helper()
	u, err := url.Parse(rawURI)
	require.NoError(t, err)
	u.Scheme = "mysql+cdc"
	query := u.Query()
	query.Set("server_id", "21999")
	u.RawQuery = query.Encode()
	return u.String()
}

func mysqlCDCRegressionURIParam(t *testing.T, rawURI, key, value string) string {
	t.Helper()
	u, err := url.Parse(rawURI)
	require.NoError(t, err)
	query := u.Query()
	query.Set(key, value)
	u.RawQuery = query.Encode()
	return u.String()
}

func (h *mysqlCDCRegressionHarness) run(ctx context.Context) error {
	return pipeline.New(&config.IngestConfig{
		SourceURI:           h.sourceURI,
		SourceTable:         mysqlCDCRegressionTable,
		DestURI:             h.destURI,
		DestTable:           mysqlCDCRegressionTable,
		IncrementalStrategy: config.StrategyMerge,
	}).Run(ctx)
}

func (h *mysqlCDCRegressionHarness) runFullRefresh(ctx context.Context) error {
	return pipeline.New(&config.IngestConfig{
		SourceURI:           h.sourceURI,
		SourceTable:         mysqlCDCRegressionTable,
		DestURI:             h.destURI,
		DestTable:           mysqlCDCRegressionTable,
		IncrementalStrategy: config.StrategyMerge,
		FullRefresh:         true,
	}).Run(ctx)
}

func (h *mysqlCDCRegressionHarness) activeCount(ctx context.Context) (int, error) {
	var count int
	err := h.destDB.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE `_cdc_deleted` = FALSE",
		mysqlCDCRegressionTable,
	)).Scan(&count)
	return count, err
}

func TestMySQLCDC_StressCorrectnessRegressions(t *testing.T) {
	ctx := context.Background()
	if !testutil.DockerProviderHealthy(ctx) {
		t.Skip("skipping stress test: Docker provider is not available/healthy")
	}

	t.Run("compressed transaction payload", func(t *testing.T) {
		h := newMySQLCDCRegressionHarness(t, ctx, "--binlog-transaction-compression=ON")
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE cdc_regression_items (
			id BIGINT NOT NULL PRIMARY KEY,
			payload MEDIUMTEXT NULL
		)`))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB,
			"INSERT INTO cdc_regression_items VALUES (0, 'seed')"))
		require.NoError(t, h.run(ctx))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, "TRUNCATE TABLE performance_schema.binary_log_transaction_compression_stats"))

		conn, err := h.sourceDB.Conn(ctx)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "SET SESSION binlog_transaction_compression = ON"))
		tx, err := conn.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		for firstID := 1; firstID <= 200; firstID += 50 {
			var query strings.Builder
			query.WriteString("INSERT INTO cdc_regression_items (id, payload) VALUES ")
			args := make([]interface{}, 0, 100)
			for id := firstID; id < firstID+50; id++ {
				if id > firstID {
					query.WriteString(",")
				}
				query.WriteString("(?, ?)")
				args = append(args, id, strings.Repeat(fmt.Sprintf("payload-%03d-", id), 100))
			}
			_, err = tx.ExecContext(ctx, query.String(), args...)
			require.NoError(t, err)
		}
		require.NoError(t, tx.Commit())
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "SET SESSION binlog_transaction_compression = OFF"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn,
			"INSERT INTO cdc_regression_items VALUES (1000, 'later-normal-event')"))

		var compressedTransactions int64
		require.NoError(t, h.sourceDB.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(TRANSACTION_COUNTER), 0)
			FROM performance_schema.binary_log_transaction_compression_stats
			WHERE COMPRESSION_TYPE = 'ZSTD'
		`).Scan(&compressedTransactions))
		require.Positive(t, compressedTransactions, "test precondition: MySQL must write a compressed transaction payload")

		require.NoError(t, h.run(ctx))
		count, err := h.activeCount(ctx)
		require.NoError(t, err)
		require.Equal(t, 202, count, "rows nested inside Transaction_payload_event must not be skipped by a later event")

		rows, err := h.destDB.QueryContext(ctx, `
			SELECT _cdc_lsn
			FROM cdc_regression_items
			WHERE id IN (1, 51, 101, 151) AND _cdc_deleted = FALSE
			ORDER BY id
		`)
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		var lsns []string
		for rows.Next() {
			var lsn string
			require.NoError(t, rows.Scan(&lsn))
			lsns = append(lsns, lsn)
		}
		require.NoError(t, rows.Err())
		require.Len(t, lsns, 4)
		for i := 1; i < len(lsns); i++ {
			require.Less(t, lsns[i-1], lsns[i], "row sequence must advance across nested row events")
		}
	})

	t.Run("truncate table", func(t *testing.T) {
		h := newMySQLCDCRegressionHarness(t, ctx)
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE cdc_regression_items (
			id BIGINT NOT NULL PRIMARY KEY,
			payload TEXT NULL
		)`))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB,
			"INSERT INTO cdc_regression_items VALUES (1, 'one'), (2, 'two'), (3, 'three')"))
		require.NoError(t, h.run(ctx))

		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, "TRUNCATE TABLE cdc_regression_items"))
		require.NoError(t, h.run(ctx))
		count, err := h.activeCount(ctx)
		require.NoError(t, err)
		require.Zero(t, count, "source TRUNCATE must empty the materialized destination")
	})

	t.Run("xa rollback", func(t *testing.T) {
		h := newMySQLCDCRegressionHarness(t, ctx)
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE cdc_regression_items (
			id BIGINT NOT NULL PRIMARY KEY,
			payload TEXT NULL
		)`))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB,
			"INSERT INTO cdc_regression_items VALUES (0, 'seed')"))
		require.NoError(t, h.run(ctx))

		conn, err := h.sourceDB.Conn(ctx)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "SET SESSION binlog_transaction_compression = ON"))
		xid := "ingestr-xa-regression"
		rolledBack := false
		defer func() {
			if !rolledBack {
				_, _ = conn.ExecContext(context.Background(), "XA ROLLBACK '"+xid+"'")
			}
		}()

		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA START '"+xid+"'"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn,
			"INSERT INTO cdc_regression_items VALUES (1, 'must-roll-back')"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA END '"+xid+"'"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA PREPARE '"+xid+"'"))
		require.NoError(t, h.run(ctx), "prepared XA rows should not make the CDC run fail")
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA ROLLBACK '"+xid+"'"))
		rolledBack = true
		require.NoError(t, h.run(ctx))

		count, err := h.activeCount(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, count, "rows from a rolled-back XA transaction must not remain active")
		var rolledBackRows int
		require.NoError(t, h.destDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM cdc_regression_items WHERE id = 1 AND `_cdc_deleted` = FALSE").Scan(&rolledBackRows))
		require.Zero(t, rolledBackRows, "the rolled-back XA row must not remain active")

		onePhaseXID := "CaseSensitive-OnePhase"
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA START '"+onePhaseXID+"','branch',17"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn,
			"INSERT INTO cdc_regression_items VALUES (2, 'one-phase-commit')"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA END '"+onePhaseXID+"','branch',17"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA COMMIT '"+onePhaseXID+"','branch',17 ONE PHASE"))
		require.NoError(t, h.run(ctx))

		var committedRows int
		require.NoError(t, h.destDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM cdc_regression_items WHERE id = 2 AND `_cdc_deleted` = FALSE").Scan(&committedRows))
		require.Equal(t, 1, committedRows, "a compressed one-phase XA commit must be applied")
	})

	t.Run("producer session uses noblob row image", func(t *testing.T) {
		h := newMySQLCDCRegressionHarness(t, ctx)
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE cdc_regression_items (
			id BIGINT NOT NULL PRIMARY KEY,
			payload TEXT NULL,
			counter INT NULL
		)`))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB,
			"INSERT INTO cdc_regression_items VALUES (1, 'keep-me', 1)"))
		require.NoError(t, h.run(ctx))

		conn, err := h.sourceDB.Conn(ctx)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "SET SESSION binlog_row_image = 'NOBLOB'"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn,
			"UPDATE cdc_regression_items SET counter = 2 WHERE id = 1"))

		runErr := h.run(ctx)
		require.Error(t, runErr, "unsafe producer-session row images must fail closed")
		require.Contains(t, strings.ToLower(runErr.Error()), "row image")
	})

	t.Run("unresolved empty xa transactions are bounded", func(t *testing.T) {
		h := newMySQLCDCRegressionHarness(t, ctx)
		h.sourceURI = mysqlCDCRegressionURIParam(t, h.sourceURI, "xa_pending_limit", "2")
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE cdc_regression_items (
			id BIGINT NOT NULL PRIMARY KEY,
			payload TEXT NULL
		)`))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE uncaptured_items (
			id BIGINT NOT NULL PRIMARY KEY
		)`))
		require.NoError(t, h.run(ctx))
		conn, err := h.sourceDB.Conn(ctx)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()

		xids := []string{"uncaptured-xa-1", "uncaptured-xa-2", "uncaptured-xa-3"}
		defer func() {
			for _, xid := range xids {
				_, _ = h.sourceDB.ExecContext(context.Background(), "XA ROLLBACK '"+xid+"'")
			}
		}()
		for i, xid := range xids {
			require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA START '"+xid+"'"))
			require.NoError(t, execMySQLCDCRegression(ctx, conn,
				fmt.Sprintf("INSERT INTO uncaptured_items VALUES (%d)", i+1)))
			require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA END '"+xid+"'"))
			require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA PREPARE '"+xid+"'"))
		}

		err = h.run(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "xa_pending_limit")
	})

	t.Run("xa payload bytes are bounded", func(t *testing.T) {
		h := newMySQLCDCRegressionHarness(t, ctx)
		h.sourceURI = mysqlCDCRegressionURIParam(t, h.sourceURI, "xa_buffer_bytes_limit", "4096")
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE cdc_regression_items (
			id BIGINT NOT NULL PRIMARY KEY,
			payload MEDIUMBLOB NULL
		)`))
		require.NoError(t, h.run(ctx))
		conn, err := h.sourceDB.Conn(ctx)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		const smallXID = "small-xa-payload"
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA START '"+smallXID+"'"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn,
			"INSERT INTO cdc_regression_items VALUES (1, REPEAT('x', 128))"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA END '"+smallXID+"'"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA COMMIT '"+smallXID+"' ONE PHASE"))
		require.NoError(t, h.run(ctx), "fixed XA container overhead must fit below the payload budget")

		const xid = "large-xa-payload"
		rolledBack := false
		defer func() {
			if !rolledBack {
				_, _ = h.sourceDB.ExecContext(context.Background(), "XA ROLLBACK '"+xid+"'")
			}
		}()
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA START '"+xid+"'"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn,
			"INSERT INTO cdc_regression_items VALUES (2, REPEAT('x', 4096))"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA END '"+xid+"'"))
		require.NoError(t, execMySQLCDCRegression(ctx, conn, "XA PREPARE '"+xid+"'"))

		runErr := h.run(ctx)
		require.ErrorContains(t, runErr, "xa_buffer_bytes_limit")
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, "XA ROLLBACK '"+xid+"'"))
		rolledBack = true
	})

	t.Run("resume lookup failure", func(t *testing.T) {
		h := newMySQLCDCRegressionHarness(t, ctx)
		h.destURI = mysqlCDCRegressionURIParam(t, h.destURI, "lock_wait_timeout", "1")
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE cdc_regression_items (
			id BIGINT NOT NULL PRIMARY KEY,
			payload TEXT NULL
		)`))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB,
			"INSERT INTO cdc_regression_items VALUES (1, 'one'), (2, 'two')"))
		require.NoError(t, h.run(ctx))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB,
			"DELETE FROM cdc_regression_items WHERE id = 2"))

		lockConn, err := h.destDB.Conn(ctx)
		require.NoError(t, err)
		defer func() { _ = lockConn.Close() }()
		require.NoError(t, execMySQLCDCRegression(ctx, lockConn, "LOCK TABLES `_bruin_staging`.`cdc_state` WRITE"))

		runDone := make(chan error, 1)
		go func() { runDone <- h.run(ctx) }()
		waitForMySQLCDCStateQuery(t, ctx, h.destDB, runDone)
		time.Sleep(1010 * time.Millisecond)
		require.NoError(t, execMySQLCDCRegression(context.Background(), lockConn, "UNLOCK TABLES"))
		runErr := <-runDone
		require.Error(t, runErr, "a failed resume lookup must abort instead of falling back to a snapshot")
		require.Regexp(t, `(?i)(resume|cursor|lsn|state)`, runErr.Error(), "the run should fail specifically at resume lookup")
	})

	t.Run("recovery snapshot replaces stale target", func(t *testing.T) {
		h := newMySQLCDCRegressionHarness(t, ctx)
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE cdc_regression_items (
			id BIGINT NOT NULL PRIMARY KEY,
			payload TEXT NULL
		)`))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB,
			"INSERT INTO cdc_regression_items VALUES (1, 'one'), (2, 'two')"))
		require.NoError(t, h.run(ctx))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, "DELETE FROM cdc_regression_items"))

		var connectorID string
		var generation int64
		require.NoError(t, h.destDB.QueryRowContext(ctx, `
			SELECT connector_id, MAX(state_generation)
			FROM _bruin_staging.cdc_state
			GROUP BY connector_id
		`).Scan(&connectorID, &generation))
		runID := strings.Repeat("a", 32)
		eventID := connectorID + "-" + runID + "-" + strings.Repeat("b", 64)
		require.NoError(t, execMySQLCDCRegression(ctx, h.destDB, `
			INSERT INTO _bruin_staging.cdc_state
				(event_id, state_version, connector_id, source_table, destination_table,
				 state_kind, state_generation, state_status, _cdc_lsn, recorded_at)
			VALUES (?, '2', ?, '', '', 'run', ?, 'in_progress', '00000000/00000000', CURRENT_TIMESTAMP(6))
		`, eventID, connectorID, generation+1))

		require.NoError(t, h.run(ctx))
		count, err := h.activeCount(ctx)
		require.NoError(t, err)
		require.Zero(t, count, "a recovery snapshot must remove rows absent from an empty source snapshot")
	})

	t.Run("concurrent connector run is rejected", func(t *testing.T) {
		h := newMySQLCDCRegressionHarness(t, ctx)
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE cdc_regression_items (
			id BIGINT NOT NULL PRIMARY KEY,
			payload TEXT NULL
		)`))
		require.NoError(t, h.run(ctx))

		var connectorID string
		require.NoError(t, h.destDB.QueryRowContext(ctx, `
			SELECT connector_id
			FROM _bruin_staging.cdc_state
			LIMIT 1
		`).Scan(&connectorID))
		lockConn, err := h.destDB.Conn(ctx)
		require.NoError(t, err)
		defer func() { _ = lockConn.Close() }()
		lockName := "ingestr_cdc_" + connectorID
		var acquired int
		require.NoError(t, lockConn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", lockName).Scan(&acquired))
		require.Equal(t, 1, acquired)
		defer func() {
			var released sql.NullInt64
			_ = lockConn.QueryRowContext(context.Background(), "SELECT RELEASE_LOCK(?)", lockName).Scan(&released)
		}()

		runErr := h.run(ctx)
		require.ErrorContains(t, runErr, "already owns connector")
	})

	t.Run("full refresh uses a fenced atomic swap", func(t *testing.T) {
		h := newMySQLCDCRegressionHarness(t, ctx)
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE cdc_regression_items (
			id BIGINT NOT NULL PRIMARY KEY,
			payload TEXT NULL
		)`))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB,
			"INSERT INTO cdc_regression_items VALUES (1, 'one'), (2, 'two')"))
		require.NoError(t, h.run(ctx))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB,
			"ALTER TABLE cdc_regression_items ADD COLUMN added INT NOT NULL DEFAULT 7"))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB,
			"DELETE FROM cdc_regression_items WHERE id = 2"))

		require.NoError(t, h.runFullRefresh(ctx))
		count, err := h.activeCount(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		var added int
		require.NoError(t, h.destDB.QueryRowContext(ctx,
			"SELECT added FROM cdc_regression_items WHERE id = 1 AND `_cdc_deleted` = FALSE").Scan(&added))
		require.Equal(t, 7, added)
	})

	t.Run("partial target row skips earlier rows in one event", func(t *testing.T) {
		h := newMySQLCDCRegressionHarness(t, ctx)
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE cdc_regression_items (
			id BIGINT NOT NULL PRIMARY KEY,
			payload TEXT NULL
		)`))
		require.NoError(t, h.run(ctx))

		binlogFile, beforePos := currentMySQLCDCRegressionPosition(t, ctx, h.sourceDB)
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB,
			"INSERT INTO cdc_regression_items VALUES (1, 'one'), (2, 'two'), (3, 'three')"))
		rowsEventEnd := mysqlCDCRegressionRowsEventEnd(t, ctx, h.sourceDB, binlogFile, beforePos)
		lsn := formatMySQLCDCRegressionLSN(t, binlogFile, rowsEventEnd, 2)

		require.NoError(t, execMySQLCDCRegression(ctx, h.destDB, `
			INSERT INTO cdc_regression_items
				(id, payload, _cdc_lsn, _cdc_deleted, _cdc_synced_at)
			VALUES (?, 'three', ?, FALSE, CURRENT_TIMESTAMP(6))
		`, 3, lsn))
		require.NoError(t, h.run(ctx))

		count, err := h.activeCount(ctx)
		require.NoError(t, err)
		require.Equal(t, 3, count, "resuming from one durable row must not skip earlier rows from the same binlog event")
	})

	t.Run("legacy state position column is widened", func(t *testing.T) {
		h := newMySQLCDCRegressionHarness(t, ctx)
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB, `CREATE TABLE cdc_regression_items (
			id BIGINT NOT NULL PRIMARY KEY,
			payload TEXT NULL
		)`))
		require.NoError(t, execMySQLCDCRegression(ctx, h.sourceDB,
			"INSERT INTO cdc_regression_items VALUES (1, 'one')"))
		require.NoError(t, execMySQLCDCRegression(ctx, h.destDB, "CREATE DATABASE `_bruin_staging`"))
		require.NoError(t, execMySQLCDCRegression(ctx, h.destDB, `CREATE TABLE _bruin_staging.cdc_state (
			event_id VARCHAR(128) NOT NULL,
			state_version VARCHAR(16) NOT NULL,
			connector_id VARCHAR(64) NOT NULL,
			source_table VARCHAR(1000) NOT NULL,
			destination_table VARCHAR(1000) NOT NULL,
			state_kind VARCHAR(32) NOT NULL,
			state_generation BIGINT NOT NULL,
			state_status VARCHAR(32) NOT NULL,
			_cdc_lsn VARCHAR(64) NOT NULL,
			recorded_at TIMESTAMP(6) NOT NULL,
			PRIMARY KEY (connector_id, event_id)
		)`))

		require.NoError(t, h.run(ctx))
		var dataType string
		require.NoError(t, h.destDB.QueryRowContext(ctx, `
			SELECT DATA_TYPE
			FROM information_schema.columns
			WHERE table_schema = '_bruin_staging'
			  AND table_name = 'cdc_state'
			  AND column_name = '_cdc_lsn'
		`).Scan(&dataType))
		require.Equal(t, "text", strings.ToLower(dataType))
	})
}

type mysqlCDCRegressionExecer interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

func execMySQLCDCRegression(ctx context.Context, execer mysqlCDCRegressionExecer, query string, args ...interface{}) error {
	_, err := execer.ExecContext(ctx, query, args...)
	return err
}

func waitForMySQLCDCStateQuery(t *testing.T, ctx context.Context, db *sql.DB, runDone <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var count int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM information_schema.PROCESSLIST
			WHERE ID <> CONNECTION_ID()
			  AND INFO LIKE '%cdc_state%'
			  AND INFO LIKE '%connector_id%'
		`).Scan(&count)
		require.NoError(t, err)
		if count > 0 {
			return
		}
		select {
		case err := <-runDone:
			require.NoError(t, err, "pipeline exited before its resume query was blocked")
			t.Fatal("pipeline exited before its resume query was observed")
		case <-time.After(10 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for blocked CDC resume query")
		}
	}
}

func currentMySQLCDCRegressionPosition(t *testing.T, ctx context.Context, db *sql.DB) (string, uint64) {
	t.Helper()
	var file string
	var pos uint64
	var doDB, ignoreDB, gtid sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, "SHOW MASTER STATUS").Scan(&file, &pos, &doDB, &ignoreDB, &gtid))
	return file, pos
}

func mysqlCDCRegressionRowsEventEnd(t *testing.T, ctx context.Context, db *sql.DB, file string, from uint64) uint64 {
	t.Helper()
	query := fmt.Sprintf(
		"SHOW BINLOG EVENTS IN '%s' FROM %d",
		strings.ReplaceAll(file, "'", "''"),
		from,
	)
	rows, err := db.QueryContext(ctx, query)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var rowsEventEnd uint64
	for rows.Next() {
		var logName, eventType string
		var eventPos, endPos uint64
		var serverID uint32
		var info sql.NullString
		require.NoError(t, rows.Scan(&logName, &eventPos, &eventType, &serverID, &endPos, &info))
		if strings.Contains(strings.ToLower(eventType), "write_rows") {
			rowsEventEnd = endPos
		}
	}
	require.NoError(t, rows.Err())
	require.NotZero(t, rowsEventEnd, "test precondition: multi-row INSERT must produce a rows event")
	return rowsEventEnd
}

func formatMySQLCDCRegressionLSN(t *testing.T, file string, pos uint64, rowSeq int) string {
	t.Helper()
	dot := strings.LastIndex(file, ".")
	require.NotEqual(t, -1, dot)
	sequence, err := strconv.ParseUint(file[dot+1:], 10, 64)
	require.NoError(t, err)
	return fmt.Sprintf("%020d:%s:%020d:%020d", sequence, file, pos, rowSeq)
}
