//go:build integration

package trino_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	_ "github.com/bruin-data/ingestr/internal/testutil/quiettc"
	"github.com/bruin-data/ingestr/pkg/destination/trino"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/mysql"
	_ "github.com/bruin-data/ingestr/pkg/source/postgres_cdc"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	_ "github.com/trinodb/trino-go-client/trino"
)

func startTrinoIcebergContainer(t *testing.T, ctx context.Context, defaultSchema string) (string, *sql.DB) {
	t.Helper()

	testNetwork, err := network.New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testNetwork.Remove(context.Background()) })

	nessie, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          "ghcr.io/projectnessie/nessie:0.104.5",
			ExposedPorts:   []string{"19120/tcp"},
			Networks:       []string{testNetwork.Name},
			NetworkAliases: map[string][]string{testNetwork.Name: {"nessie"}},
			Env:            map[string]string{"NESSIE_VERSION_STORE_TYPE": "IN_MEMORY"},
			WaitingFor:     wait.ForHTTP("/api/v2/config").WithPort("19120/tcp").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = nessie.Terminate(context.Background()) })

	trinoContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "trinodb/trino:483",
			ExposedPorts: []string{"8080/tcp"},
			Networks:     []string{testNetwork.Name},
			Files: []testcontainers.ContainerFile{{
				Reader: strings.NewReader(`connector.name=iceberg
iceberg.catalog.type=nessie
iceberg.nessie-catalog.uri=http://nessie:19120/api/v2
iceberg.nessie-catalog.default-warehouse-dir=local:///warehouse
fs.local.enabled=true
local.location=/tmp
`),
				ContainerFilePath: "/etc/trino/catalog/iceberg.properties",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForLog("======== SERVER STARTED ========").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = trinoContainer.Terminate(context.Background()) })

	host, err := trinoContainer.Host(ctx)
	require.NoError(t, err)
	port, err := trinoContainer.MappedPort(ctx, "8080/tcp")
	require.NoError(t, err)

	uri := fmt.Sprintf("trino://test@%s:%s/iceberg/%s", host, port.Port(), defaultSchema)
	dest := trino.NewTrinoDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS iceberg."%s"`, defaultSchema)))
	require.NoError(t, dest.Close(ctx))

	dsn := fmt.Sprintf("http://test@%s:%s?catalog=iceberg&schema=%s", host, port.Port(), defaultSchema)
	db, err := sql.Open("trino", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return uri, db
}

func TestTrinoPostgresCDCPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Trino postgres+cdc integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	destURI, trinoDB := startTrinoIcebergContainer(t, ctx, "cdc_pg")

	pgContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "postgres:16-alpine",
			Env: map[string]string{
				"POSTGRES_USER":     "testuser",
				"POSTGRES_PASSWORD": "testpass",
				"POSTGRES_DB":       "testdb",
			},
			ExposedPorts: []string{"5432/tcp"},
			Cmd: []string{
				"postgres",
				"-c", "wal_level=logical",
				"-c", "max_replication_slots=4",
				"-c", "max_wal_senders=4",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgContainer.Terminate(context.Background()) })

	pgHost, err := pgContainer.Host(ctx)
	require.NoError(t, err)
	pgPort, err := pgContainer.MappedPort(ctx, "5432")
	require.NoError(t, err)
	pgConnStr := fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", pgHost, pgPort.Port())

	pool, err := pgxpool.New(ctx, pgConnStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	largeToastPayload := strings.Repeat("toast-payload-", 400) // > 5KB to force Postgres TOAST storage
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.orders (
			id INT PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			big_payload TEXT NOT NULL,
			meta JSONB NOT NULL DEFAULT '{"tier":"gold"}'::jsonb
		);
		CREATE TABLE public.orders_hard (
			id INT PRIMARY KEY,
			name VARCHAR(100) NOT NULL
		);
		CREATE PUBLICATION trino_cdc_pub FOR TABLE public.orders, public.orders_hard;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO public.orders (id, name, big_payload) VALUES
			(1, 'item1', $1),
			(2, 'item2', 'small2'),
			(3, 'item3', 'small3')
	`, largeToastPayload)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO public.orders_hard (id, name) VALUES (1, 'one'), (2, 'two')`)
	require.NoError(t, err)

	pgCDCURI := "postgres+cdc://" + pgConnStr[len("postgres://"):] + "&publication=trino_cdc_pub"

	// Soft-delete pipeline: initial snapshot + incremental update (with TOAST unchanged col), insert, delete.
	softCfg := &config.IngestConfig{
		SourceURI:           pgCDCURI,
		SourceTable:         "public.orders",
		DestURI:             destURI,
		DestTable:           "cdc_pg.orders",
		IncrementalStrategy: config.StrategyMerge,
	}
	require.NoError(t, pipeline.New(softCfg).Run(ctx))

	var count int
	require.NoError(t, trinoDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_pg"."orders" WHERE "_cdc_deleted" = false`).Scan(&count))
	require.Equal(t, 3, count)

	_, err = pool.Exec(ctx, `
		UPDATE public.orders SET name = 'item1_v2' WHERE id = 1;
		DELETE FROM public.orders WHERE id = 2;
		INSERT INTO public.orders (id, name, big_payload) VALUES (4, 'item4', 'fresh4');
	`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(softCfg).Run(ctx))
	// Idempotent second resume with no changes.
	require.NoError(t, pipeline.New(softCfg).Run(ctx))

	rows, err := trinoDB.QueryContext(ctx, `SELECT id, name, length(big_payload), "_cdc_deleted" FROM "iceberg"."cdc_pg"."orders" ORDER BY id`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var gotSoft []string
	for rows.Next() {
		var id, payloadLen int
		var name string
		var deleted bool
		require.NoError(t, rows.Scan(&id, &name, &payloadLen, &deleted))
		gotSoft = append(gotSoft, fmt.Sprintf("%d:%s:%d:%t", id, name, payloadLen, deleted))
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{
		fmt.Sprintf("1:item1_v2:%d:false", len(largeToastPayload)),
		"2:item2:6:true",
		"3:item3:6:false",
		"4:item4:6:false",
	}, gotSoft)

	// Hard-delete pipeline: initial snapshot + delete + update + insert + re-insert.
	hardCfg := &config.IngestConfig{
		SourceURI:           pgCDCURI + "&slot=trino_hard_slot",
		SourceTable:         "public.orders_hard",
		DestURI:             destURI,
		DestTable:           "cdc_pg.orders_hard",
		IncrementalStrategy: config.StrategyMerge,
		CDCDeleteBehavior:   config.CDCDeleteHard,
	}
	require.NoError(t, pipeline.New(hardCfg).Run(ctx))

	_, err = pool.Exec(ctx, `
		DELETE FROM public.orders_hard WHERE id = 1;
		UPDATE public.orders_hard SET name = 'two_v2' WHERE id = 2;
		INSERT INTO public.orders_hard (id, name) VALUES (3, 'three');
	`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(hardCfg).Run(ctx))

	hardRows, err := trinoDB.QueryContext(ctx, `SELECT id, name FROM "iceberg"."cdc_pg"."orders_hard" ORDER BY id`)
	require.NoError(t, err)
	defer func() { _ = hardRows.Close() }()
	var gotHard []string
	for hardRows.Next() {
		var id int
		var name string
		require.NoError(t, hardRows.Scan(&id, &name))
		gotHard = append(gotHard, fmt.Sprintf("%d:%s", id, name))
	}
	require.NoError(t, hardRows.Err())
	require.Equal(t, []string{"2:two_v2", "3:three"}, gotHard)

	// Multi-table postgres+cdc pipeline: snapshot + incremental across multiple tables.
	_, err = trinoDB.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS iceberg."cdc_pg_multi"`)
	require.NoError(t, err)
	multiDestURI := strings.Replace(destURI, "/iceberg/cdc_pg", "/iceberg/cdc_pg_multi", 1)
	multiCfg := &config.IngestConfig{
		SourceURI:           pgCDCURI + "&slot=trino_multi_slot",
		SourceTables:        []string{"public.orders", "public.orders_hard"},
		DestURI:             multiDestURI,
		IncrementalStrategy: config.StrategyMerge,
	}
	require.NoError(t, pipeline.New(multiCfg).Run(ctx))

	_, err = pool.Exec(ctx, `
		INSERT INTO public.orders (id, name, big_payload) VALUES (5, 'item5', 'multi5');
		INSERT INTO public.orders_hard (id, name) VALUES (4, 'four');
	`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(multiCfg).Run(ctx))

	var multiOrdersCnt, multiHardCnt int
	require.NoError(t, trinoDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_pg_multi"."orders" WHERE "_cdc_deleted" = false`).Scan(&multiOrdersCnt))
	require.Equal(t, 4, multiOrdersCnt)
	require.NoError(t, trinoDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_pg_multi"."orders_hard" WHERE "_cdc_deleted" = false`).Scan(&multiHardCnt))
	require.Equal(t, 3, multiHardCnt)
}

func TestTrinoMySQLCDCPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Trino mysql+cdc integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	destURI, trinoDB := startTrinoIcebergContainer(t, ctx, "cdc_mysql")

	mysqlContainer, err := tcmysql.Run(
		ctx,
		"mysql:8.0",
		tcmysql.WithDatabase("testdb"),
		tcmysql.WithUsername("root"),
		tcmysql.WithPassword("testpassword"),
		testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
			req.Cmd = []string{
				"--server-id=17777",
				"--log-bin=mysql-bin",
				"--gtid-mode=ON",
				"--enforce-gtid-consistency=ON",
				"--binlog-format=ROW",
				"--binlog-row-image=FULL",
			}
			return nil
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mysqlContainer.Terminate(context.Background()) })

	mysqlHost, err := mysqlContainer.Host(ctx)
	require.NoError(t, err)
	mysqlPort, err := mysqlContainer.MappedPort(ctx, "3306")
	require.NoError(t, err)

	mysqlDSN := fmt.Sprintf("root:testpassword@tcp(%s:%s)/testdb?parseTime=true&multiStatements=true", mysqlHost, mysqlPort.Port())
	mysqlDB, err := sql.Open("mysql", mysqlDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mysqlDB.Close() })

	_, err = mysqlDB.ExecContext(ctx, `
		CREATE TABLE items (
			id INT NOT NULL PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			val INT NOT NULL,
			meta JSON
		);
		INSERT INTO items (id, name, val, meta) VALUES (1, 'one', 10, '{"a":1}'), (2, 'two', 20, '{"a":2}'), (3, 'three', 30, NULL);
	`)
	require.NoError(t, err)

	u := url.URL{
		Scheme:   "mysql+cdc",
		User:     url.UserPassword("root", "testpassword"),
		Host:     fmt.Sprintf("%s:%s", mysqlHost, mysqlPort.Port()),
		Path:     "/testdb",
		RawQuery: "mode=batch&server_id=18888",
	}

	cfg := &config.IngestConfig{
		SourceURI:           u.String(),
		SourceTable:         "items",
		DestURI:             destURI,
		DestTable:           "cdc_mysql.items",
		IncrementalStrategy: config.StrategyMerge,
	}

	// Run 1: snapshot + run lease + managed CDC state.
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var cnt int
	require.NoError(t, trinoDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_mysql"."items" WHERE "_cdc_deleted" = false`).Scan(&cnt))
	require.Equal(t, 3, cnt)

	// Mutate MySQL and run 2: binlog incremental + run lease + managed CDC state resume.
	_, err = mysqlDB.ExecContext(ctx, `
		UPDATE items SET val = 15 WHERE id = 1;
		DELETE FROM items WHERE id = 2;
		INSERT INTO items (id, name, val) VALUES (4, 'four', 40);
	`)
	require.NoError(t, err)

	runCtx, runCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer runCancel()
	require.NoError(t, pipeline.New(cfg).Run(runCtx))

	rows, err := trinoDB.QueryContext(ctx, `SELECT id, name, val, "_cdc_deleted" FROM "iceberg"."cdc_mysql"."items" ORDER BY id`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var id, val int
		var name string
		var deleted bool
		require.NoError(t, rows.Scan(&id, &name, &val, &deleted))
		got = append(got, fmt.Sprintf("%d:%s:%d:%t", id, name, val, deleted))
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{
		"1:one:15:false",
		"2:two:20:true",
		"3:three:30:false",
		"4:four:40:false",
	}, got)

	// Run 3: full-refresh exercises conditional SwapTable (inline-guarded MERGE INTO) on existing target.
	refreshCfg := *cfg
	refreshCfg.FullRefresh = true
	refreshCtx, refreshCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer refreshCancel()
	require.NoError(t, pipeline.New(&refreshCfg).Run(refreshCtx))

	var postRefreshCnt int
	require.NoError(t, trinoDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_mysql"."items" WHERE "_cdc_deleted" = false`).Scan(&postRefreshCnt))
	require.Equal(t, 3, postRefreshCnt)

	// Multi-table mysql+cdc pipeline: snapshot + binlog incremental batch across two tables via MergeCDCTablesAtomically.
	_, err = mysqlDB.ExecContext(ctx, `
		CREATE TABLE items_two (
			id INT NOT NULL PRIMARY KEY,
			label VARCHAR(100) NOT NULL
		);
		INSERT INTO items_two (id, label) VALUES (10, 'alpha'), (20, 'beta');
	`)
	require.NoError(t, err)

	_, err = trinoDB.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS iceberg."cdc_mysql_multi"`)
	require.NoError(t, err)
	multiDestURI := strings.Replace(destURI, "/iceberg/cdc_mysql", "/iceberg/cdc_mysql_multi", 1)

	uMulti := u
	uMulti.RawQuery = "mode=batch&server_id=18889"
	multiMySQLCfg := &config.IngestConfig{
		SourceURI:           uMulti.String(),
		SourceTables:        []string{"items", "items_two"},
		DestURI:             multiDestURI,
		IncrementalStrategy: config.StrategyMerge,
	}

	require.NoError(t, pipeline.New(multiMySQLCfg).Run(ctx))

	var multiItemsCnt, multiItemsTwoCnt int
	require.NoError(t, trinoDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_mysql_multi"."items" WHERE "_cdc_deleted" = false`).Scan(&multiItemsCnt))
	require.Equal(t, 3, multiItemsCnt)
	require.NoError(t, trinoDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_mysql_multi"."items_two" WHERE "_cdc_deleted" = false`).Scan(&multiItemsTwoCnt))
	require.Equal(t, 2, multiItemsTwoCnt)

	// Single MySQL transaction mutating both tables -> binlog incremental exercises MergeCDCTablesAtomically.
	_, err = mysqlDB.ExecContext(ctx, `
		BEGIN;
		UPDATE items SET val = 99 WHERE id = 1;
		INSERT INTO items (id, name, val) VALUES (5, 'five', 50);
		DELETE FROM items_two WHERE id = 10;
		INSERT INTO items_two (id, label) VALUES (30, 'gamma');
		COMMIT;
	`)
	require.NoError(t, err)

	multiIncCtx, multiIncCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer multiIncCancel()
	require.NoError(t, pipeline.New(multiMySQLCfg).Run(multiIncCtx))

	var gotItem1Val int
	require.NoError(t, trinoDB.QueryRowContext(ctx, `SELECT val FROM "iceberg"."cdc_mysql_multi"."items" WHERE id = 1 AND "_cdc_deleted" = false`).Scan(&gotItem1Val))
	require.Equal(t, 99, gotItem1Val)
	require.NoError(t, trinoDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_mysql_multi"."items" WHERE "_cdc_deleted" = false`).Scan(&multiItemsCnt))
	require.Equal(t, 4, multiItemsCnt)

	var item10Deleted bool
	require.NoError(t, trinoDB.QueryRowContext(ctx, `SELECT "_cdc_deleted" FROM "iceberg"."cdc_mysql_multi"."items_two" WHERE id = 10`).Scan(&item10Deleted))
	require.True(t, item10Deleted)
	require.NoError(t, trinoDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_mysql_multi"."items_two" WHERE "_cdc_deleted" = false`).Scan(&multiItemsTwoCnt))
	require.Equal(t, 2, multiItemsTwoCnt)
}
