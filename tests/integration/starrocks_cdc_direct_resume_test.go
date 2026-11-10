//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/starrocks"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

type directSnapshotResumeGate struct {
	table          string
	stateDatabase  string
	armed          atomic.Bool
	loads          atomic.Int32
	snapshotMerges atomic.Int32
	blocked        chan struct{}
}

var directSnapshotResumeGates sync.Map

type directSnapshotResumeStarRocks struct {
	*starrocks.StarRocksDestination
	gate *directSnapshotResumeGate
}

func init() {
	registry.Default.RegisterDestination([]string{"snapshotresumestarrocks"}, func() interface{} {
		return &directSnapshotResumeStarRocks{StarRocksDestination: starrocks.NewStarRocksDestination()}
	})
}

func (*directSnapshotResumeStarRocks) Schemes() []string {
	return []string{"snapshotresumestarrocks"}
}

func (d *directSnapshotResumeStarRocks) ManagedStagingPolicy() destination.ReplaceStagingPolicy {
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingManagedSchema,
		DefaultManagedSchema: d.gate.stateDatabase,
	}
}

func (d *directSnapshotResumeStarRocks) Connect(ctx context.Context, raw string) error {
	uri, err := url.Parse(raw)
	if err != nil {
		return err
	}
	query := uri.Query()
	gate, ok := directSnapshotResumeGates.Load(query.Get("snapshot_resume_gate"))
	if !ok {
		return fmt.Errorf("direct snapshot resume test gate is missing")
	}
	d.gate = gate.(*directSnapshotResumeGate)
	query.Del("snapshot_resume_gate")
	uri.RawQuery, uri.Scheme = query.Encode(), "starrocks"
	return d.StarRocksDestination.Connect(ctx, uri.String())
}

func (d *directSnapshotResumeStarRocks) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if opts.Table == d.gate.table && opts.CDCSnapshot && d.gate.armed.Load() && d.gate.loads.Add(1) == 3 {
		close(d.gate.blocked)
		<-ctx.Done()
		return ctx.Err()
	}
	return d.StarRocksDestination.WriteParallel(ctx, records, opts)
}

func (d *directSnapshotResumeStarRocks) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if opts.TargetTable == d.gate.table && opts.CDCSnapshot {
		d.gate.snapshotMerges.Add(1)
	}
	return d.StarRocksDestination.MergeTable(ctx, opts)
}

func TestStarRocksCDCDirectSnapshotResumesAfterRestart(t *testing.T) {
	if testing.Short() || !containerWanted("starrocks") {
		t.Skip("Docker CDC integration")
	}
	ctx, cancelTest := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancelTest()
	dsn, destURI := os.Getenv("INGESTR_TEST_STARROCKS_DSN"), os.Getenv("INGESTR_TEST_STARROCKS_URI")
	if dsn == "" || destURI == "" {
		dsn, destURI = sharedStarRocks(t)
	}
	waitForStarRocksBackend(t, dsn)
	mysqlConfig, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	mysqlConfig.InterpolateParams = true
	db, err := sql.Open("mysql", mysqlConfig.FormatDSN())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	database := "directresume_" + strings.ToLower(uniqueSuffix())
	execEventually(t, db, "CREATE DATABASE "+database)
	t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE IF EXISTS " + database + " FORCE") })
	execEventually(t, db, "CREATE TABLE "+database+".ready_probe (id INT) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES ('replication_num'='1')")
	execEventually(t, db, "DROP TABLE "+database+".ready_probe")

	sourceURI := os.Getenv("INGESTR_TEST_POSTGRES_CDC_URI")
	if sourceURI == "" {
		container, uri := setupPostgresCDCContainer(t, ctx)
		sourceURI = uri
		t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	}
	pg, sourceURI, pgExec := setupStarRocksCDCPostgres(t, ctx, sourceURI, database)
	const rowCount = 20_000
	pgExec(fmt.Sprintf(`CREATE TABLE %[1]s.items (id BIGINT PRIMARY KEY, payload TEXT);
		INSERT INTO %[1]s.items SELECT g, 'p' || g FROM generate_series(1, %[2]d) g;
		CREATE TABLE %[1]s.anchor (id BIGINT PRIMARY KEY, payload TEXT);
		INSERT INTO %[1]s.anchor VALUES (1, 'anchor');
		CREATE PUBLICATION %[1]s FOR TABLE %[1]s.items, %[1]s.anchor;`, database, rowCount))

	items := database + ".items"
	stateDatabase := database + "_staging"
	gate := &directSnapshotResumeGate{table: items, stateDatabase: stateDatabase, blocked: make(chan struct{})}
	directSnapshotResumeGates.Store(database, gate)
	t.Cleanup(func() { directSnapshotResumeGates.Delete(database) })
	uri, err := url.Parse(destURI)
	require.NoError(t, err)
	uri.Scheme = "snapshotresumestarrocks"
	query := uri.Query()
	query.Set("snapshot_resume_gate", database)
	uri.RawQuery = query.Encode()
	cfg := config.DefaultConfig()
	cfg.SourceURI = strings.Replace(sourceURI, "postgres://", "postgres+cdc://", 1) + "&publication=" + database + "&slot=" + database + "&dest_schema=" + database
	cfg.DestURI = uri.String()
	cfg.StagingDataset = stateDatabase
	t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE IF EXISTS " + stateDatabase + " FORCE") })
	cfg.CDCTableNaming = config.TableNamingTable
	cfg.IncrementalStrategy = config.StrategyMerge
	cfg.CDCDeleteBehavior = config.CDCDeleteHard
	cfg.PageSize, cfg.FlushRecords = 1000, 1000
	cfg.DestinationParallelism = 4
	cfg.FlushInterval = time.Hour
	cfg.NoLoadTimestamp, cfg.NoRunID = true, true
	cfg.Progress = config.ProgressLog
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	require.Zero(t, gate.snapshotMerges.Load(), "the initial snapshot must use direct loads")

	var initialStamp string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT _cdc_lsn FROM "+items+" WHERE id = 1").Scan(&initialStamp))
	pgExec("ALTER TABLE " + items + " ADD COLUMN extra TEXT DEFAULT 'added'")
	cfg.Stream = true
	gate.armed.Store(true)
	start := func() (context.CancelFunc, <-chan error) {
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		exited := make(chan struct{})
		go func() {
			defer close(exited)
			done <- pipeline.New(cfg).Run(runCtx)
		}()
		t.Cleanup(func() {
			cancel()
			select {
			case <-exited:
			case <-time.After(90 * time.Second):
				t.Error("direct snapshot pipeline did not stop during cleanup")
			}
		})
		return cancel, done
	}
	stop := func(cancel context.CancelFunc, done <-chan error) {
		t.Helper()
		cancel()
		select {
		case <-done:
		case <-time.After(90 * time.Second):
			t.Fatal("direct snapshot pipeline did not stop")
		}
	}
	cancel, done := start()
	select {
	case <-gate.blocked:
	case err := <-done:
		t.Fatalf("pipeline stopped before its third direct snapshot load: %v", err)
	case <-time.After(3 * time.Minute):
		t.Fatal("replacement snapshot did not reach its third direct load")
	}
	var progress struct {
		Boundary string `json:"b"`
		Key      string `json:"k"`
	}
	var status, position string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT state_status, _cdc_lsn FROM "+stateDatabase+`.cdc_state
		WHERE state_kind = 'snapshot_progress' AND source_table = ? ORDER BY recorded_at DESC LIMIT 1`, items).Scan(&status, &position))
	require.Equal(t, "in_progress", status)
	require.NoError(t, json.Unmarshal([]byte(position), &progress))
	require.Greater(t, progress.Boundary, initialStamp)
	var keys []string
	require.NoError(t, json.Unmarshal([]byte(progress.Key), &keys))
	require.Len(t, keys, 1)
	lastKey, err := strconv.Atoi(keys[0])
	require.NoError(t, err)
	require.Greater(t, lastKey, 0)
	require.Less(t, lastKey, rowCount)
	stop(cancel, done)
	var firstStamp string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT _cdc_lsn FROM "+items+" WHERE id = 1").Scan(&firstStamp))
	require.Equal(t, progress.Boundary, firstStamp)
	var rewritten int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM "+items+" WHERE _cdc_lsn = ?", progress.Boundary).Scan(&rewritten))
	require.GreaterOrEqual(t, rewritten, lastKey)
	require.Less(t, rewritten, rowCount)
	require.Zero(t, gate.snapshotMerges.Load(), "the interrupted replacement must use direct loads")

	pgExec(fmt.Sprintf(`UPDATE %[1]s SET payload = 'updated-low' WHERE id = 2;
		DELETE FROM %[1]s WHERE id = 3;
		UPDATE %[1]s SET payload = 'updated-high' WHERE id = %[2]d;
		DELETE FROM %[1]s WHERE id = %[3]d;`, items, rowCount-1, rowCount))
	cfg.FlushInterval = 100 * time.Millisecond
	cancel, done = start()
	pgExec("INSERT INTO " + items + " (id, payload) VALUES (900000001, 'during-resume')")
	require.Eventually(t, func() bool {
		select {
		case err := <-done:
			t.Fatalf("resumed pipeline stopped: %v", err)
		default:
		}
		var total, added, inserted, deleted, updated int64
		err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*),
			SUM(CASE WHEN extra = 'added' THEN 1 ELSE 0 END),
			SUM(CASE WHEN id = 900000001 THEN 1 ELSE 0 END),
			SUM(CASE WHEN id IN (3, %[2]d) THEN 1 ELSE 0 END),
			SUM(CASE WHEN id = 2 AND payload = 'updated-low' OR id = %[3]d AND payload = 'updated-high' THEN 1 ELSE 0 END)
			FROM %[1]s`, items, rowCount, rowCount-1)).Scan(&total, &added, &inserted, &deleted, &updated)
		return err == nil && total == rowCount-1 && added == total && inserted == 1 && deleted == 0 && updated == 2
	}, 3*time.Minute, 200*time.Millisecond, "resumed direct snapshot did not converge")
	stop(cancel, done)
	require.Greater(t, gate.loads.Load(), int32(3), "the resumed snapshot must use direct loads")
	require.Zero(t, gate.snapshotMerges.Load(), "snapshot replay must not fall back to a staged merge")
	var lowStamp string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT _cdc_lsn FROM "+items+" WHERE id = 1").Scan(&lowStamp))
	require.Equal(t, firstStamp, lowStamp, "durable snapshot rows must survive without being rewritten")
	var stamps, tombstones int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(DISTINCT _cdc_lsn) FROM %s WHERE id BETWEEN 4 AND %d", items, rowCount-2)).Scan(&stamps))
	require.Equal(t, 2, stamps, "unchanged rows must retain both interrupted and resumed snapshot positions")
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM "+items+" WHERE _cdc_deleted").Scan(&tombstones))
	require.Zero(t, tombstones)

	type item struct {
		ID      int64
		Payload string
		Extra   string
	}
	sourceRows, err := pg.Query(ctx, "SELECT id, payload, extra FROM "+items+" ORDER BY id")
	require.NoError(t, err)
	var expected []item
	for sourceRows.Next() {
		var row item
		require.NoError(t, sourceRows.Scan(&row.ID, &row.Payload, &row.Extra))
		expected = append(expected, row)
	}
	require.NoError(t, sourceRows.Err())
	sourceRows.Close()
	targetRows, err := db.QueryContext(ctx, "SELECT id, payload, extra FROM "+items+" ORDER BY id")
	require.NoError(t, err)
	defer func() { _ = targetRows.Close() }()
	var actual []item
	for targetRows.Next() {
		var row item
		require.NoError(t, targetRows.Scan(&row.ID, &row.Payload, &row.Extra))
		actual = append(actual, row)
	}
	require.NoError(t, targetRows.Err())
	require.Equal(t, expected, actual)
}
