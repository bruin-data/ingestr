//go:build integration

package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	chmodule "github.com/testcontainers/testcontainers-go/modules/clickhouse"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startPublishTestClickHouse(t *testing.T, ctx context.Context) *ClickHouseDestination {
	t.Helper()
	container, err := chmodule.Run(
		ctx,
		"clickhouse/clickhouse-server:24.3",
		chmodule.WithDatabase("testdb"),
		chmodule.WithUsername("default"),
		chmodule.WithPassword("clickhouse"),
		testcontainers.WithWaitStrategy(
			wait.ForAll(
				wait.ForListeningPort("9000/tcp"),
				wait.ForHTTP("/ping").WithPort("8123/tcp").WithStatusCodeMatcher(func(status int) bool { return status == 200 }),
			).WithDeadline(120*time.Second),
		),
	)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "9000")
	require.NoError(t, err)

	d := NewClickHouseDestination()
	require.NoError(t, d.Connect(ctx, fmt.Sprintf("clickhouse://default:clickhouse@%s:%s/testdb", host, port.Port())))
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	return d
}

func TestClickHouseNativeNullabilityAndPublicationIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	d := startPublishTestClickHouse(t, ctx)

	exec := func(t *testing.T, query string) {
		t.Helper()
		require.NoError(t, d.conn.Exec(ctx, query), query)
	}
	scalar := func(t *testing.T, query string, args ...any) string {
		t.Helper()
		var out string
		require.NoError(t, d.conn.QueryRow(ctx, query, args...).Scan(&out), query)
		return out
	}
	tableUUID := func(t *testing.T, db, name string) string {
		t.Helper()
		return scalar(t, "SELECT toString(uuid) FROM system.tables WHERE database = ? AND name = ?", db, name)
	}
	leftoverShadows := func(t *testing.T) []string {
		t.Helper()
		rows, err := d.conn.Query(ctx, "SELECT name FROM system.tables WHERE database = '_bruin_staging' AND startsWith(name, '__ingestr_cdc_')")
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		var names []string
		for rows.Next() {
			var name string
			require.NoError(t, rows.Scan(&name))
			names = append(names, name)
		}
		return names
	}
	cdcCols := []string{"id", "title", destination.CDCLSNColumn, destination.CDCDeletedColumn}
	createCDCTarget := func(t *testing.T, ref string) {
		t.Helper()
		exec(t, fmt.Sprintf("CREATE TABLE %s (id Int64, title Nullable(String), _cdc_lsn String, _cdc_deleted Bool) ENGINE = ReplacingMergeTree() ORDER BY (id)", ref))
	}
	createStage := func(t *testing.T, ref string, values string) {
		t.Helper()
		exec(t, "CREATE DATABASE IF NOT EXISTS `_bruin_staging`")
		exec(t, fmt.Sprintf("CREATE TABLE %s (id Int64, title Nullable(String), _cdc_lsn String, _cdc_deleted Bool) ENGINE = MergeTree() ORDER BY (id)", ref))
		exec(t, fmt.Sprintf("INSERT INTO %s VALUES %s", ref, values))
	}
	titles := func(t *testing.T, ref string) string {
		t.Helper()
		return scalar(t, fmt.Sprintf("SELECT arrayStringConcat(groupArray(concat(toString(id), '=', ifNull(title, 'NULL'))), ',') FROM (SELECT id, title FROM %s FINAL WHERE NOT _cdc_deleted ORDER BY id)", ref))
	}

	t.Run("data version ignores merges and detects every write", func(t *testing.T) {
		exec(t, "CREATE TABLE testdb.fp (id Int64, p UInt8, v String) ENGINE = MergeTree ORDER BY id PARTITION BY p")
		for _, row := range []string{"(1, 1, 'a')", "(2, 2, 'b')", "(3, 1, 'c')", "(4, 1, 'd')", "(5, 2, 'e')"} {
			exec(t, "INSERT INTO testdb.fp VALUES "+row)
		}
		version := func() string {
			v, err := d.tableDataVersion(ctx, "testdb", "fp")
			require.NoError(t, err)
			return v
		}
		v := version()
		require.NotEmpty(t, v)
		exec(t, "OPTIMIZE TABLE testdb.fp FINAL")
		require.Equal(t, v, version(), "background merges must not change the data version")

		steps := []string{
			"INSERT INTO testdb.fp VALUES (6, 3, 'f')",
			"ALTER TABLE testdb.fp UPDATE v = 'z' WHERE id = 2 SETTINGS mutations_sync = 1",
			"DELETE FROM testdb.fp WHERE id = 6",
			"INSERT INTO testdb.fp VALUES (7, 4, 'g')",
			"ALTER TABLE testdb.fp DROP PARTITION 3",
			"TRUNCATE TABLE testdb.fp",
		}
		for _, step := range steps {
			exec(t, step)
			next := version()
			require.NotEqual(t, v, next, "data version must change after %s", step)
			v = next
		}

		exec(t, "CREATE TABLE testdb.fp_parts (id Int64) ENGINE = MergeTree ORDER BY id")
		exec(t, "INSERT INTO testdb.fp_parts VALUES (1)")
		exec(t, "INSERT INTO testdb.fp_parts VALUES (2)")
		before, err := d.tableDataVersion(ctx, "testdb", "fp_parts")
		require.NoError(t, err)
		exec(t, "ALTER TABLE testdb.fp_parts DROP PART 'all_1_1_0'")
		after, err := d.tableDataVersion(ctx, "testdb", "fp_parts")
		require.NoError(t, err)
		require.NotEqual(t, before, after, "dropping the oldest part must change the data version")
	})

	t.Run("DateTime64(9) nullability relaxation keeps nanosecond precision and native types", func(t *testing.T) {
		for _, name := range []string{"events_cond", "events_plain"} {
			exec(t, fmt.Sprintf(`CREATE TABLE testdb.%s (
				id Int64,
				happened_at DateTime64(9, 'UTC'),
				local_at DateTime64(9),
				label LowCardinality(String),
				stamps Array(DateTime64(9)),
				_cdc_lsn String,
				_cdc_deleted Bool
			) ENGINE = ReplacingMergeTree() ORDER BY (id)`, name))
			exec(t, fmt.Sprintf("INSERT INTO testdb.%s VALUES (1, '2026-01-02 03:04:05.123456789', '2026-01-02 03:04:05.987654321', 'x', ['2026-01-02 03:04:05.111111111'], '0001', false)", name))
		}
		relax := func(col string, typ schema.DataType) schemaevolution.SchemaChange {
			return schemaevolution.SchemaChange{
				Type:       schemaevolution.ChangeRelaxNullability,
				ColumnName: col,
				OldColumn:  &schema.Column{Name: col, DataType: typ},
				NewColumn:  schema.Column{Name: col, DataType: typ, Nullable: true},
			}
		}
		comparison := &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{
				relax("happened_at", schema.TypeTimestampTZ),
				relax("label", schema.TypeString),
				relax("stamps", schema.TypeArray),
				{
					Type:       schemaevolution.ChangeRemoveColumn,
					ColumnName: "local_at",
					OldColumn:  &schema.Column{Name: "local_at", DataType: schema.TypeTimestamp},
				},
			},
		}

		inc, exists, err := d.CDCTargetIncarnation(ctx, "testdb.events_cond")
		require.NoError(t, err)
		require.True(t, exists)
		_, evolvedInc, err := d.ApplySchemaEvolutionIfIncarnation(ctx, "testdb.events_cond", comparison, inc)
		require.NoError(t, err)
		require.Equal(t, inc, evolvedInc)
		_, err = d.ApplySchemaEvolution(ctx, "testdb.events_plain", comparison)
		require.NoError(t, err)

		for _, name := range []string{"events_cond", "events_plain"} {
			colType := func(col string) string {
				return scalar(t, "SELECT type FROM system.columns WHERE database = 'testdb' AND table = ? AND name = ?", name, col)
			}
			require.Equal(t, "Nullable(DateTime64(9, 'UTC'))", colType("happened_at"), name)
			require.Equal(t, "Nullable(DateTime64(9))", colType("local_at"), name)
			require.Equal(t, "LowCardinality(Nullable(String))", colType("label"), name)
			require.Equal(t, "Array(Nullable(DateTime64(9)))", colType("stamps"), name)
			require.Equal(t, "2026-01-02 03:04:05.123456789", scalar(t, fmt.Sprintf("SELECT toString(happened_at) FROM testdb.%s WHERE id = 1", name)), name)
			require.Equal(t, "2026-01-02 03:04:05.987654321", scalar(t, fmt.Sprintf("SELECT toString(local_at) FROM testdb.%s WHERE id = 1", name)), name)
			require.Equal(t, "['2026-01-02 03:04:05.111111111']", scalar(t, fmt.Sprintf("SELECT toString(stamps) FROM testdb.%s WHERE id = 1", name)), name)
			exec(t, fmt.Sprintf("INSERT INTO testdb.%s (id, happened_at, local_at, label, _cdc_lsn, _cdc_deleted) VALUES (2, NULL, NULL, NULL, '0002', false)", name))
		}
		require.Empty(t, leftoverShadows(t))
	})

	t.Run("write committed while a shadow is prepared aborts publication and keeps the write", func(t *testing.T) {
		createCDCTarget(t, "testdb.w_target")
		exec(t, "INSERT INTO testdb.w_target VALUES (1, 'one', '0001', false)")
		uuid := tableUUID(t, "testdb", "w_target")
		inc, _, err := d.CDCTargetIncarnation(ctx, "testdb.w_target")
		require.NoError(t, err)

		plan, err := d.prepareCDCShadowPlan(ctx, "testdb.w_target", inc, true)
		require.NoError(t, err)
		plans := []*cdcShadowPlan{plan}
		exec(t, fmt.Sprintf("INSERT INTO %s VALUES (1, 'one-shadow', '0002', false)", plan.shadowRef()))
		exec(t, "INSERT INTO testdb.w_target VALUES (99, 'external', '0001', false)")

		err = d.publishCDCShadowPlans(ctx, plans)
		d.cleanupCDCShadowPlans(ctx, plans)
		require.ErrorContains(t, err, "written outside this run")
		require.Equal(t, uuid, tableUUID(t, "testdb", "w_target"))
		require.Equal(t, "1=one,99=external", titles(t, "testdb.w_target"))
		require.Empty(t, leftoverShadows(t))
	})

	t.Run("replacement during preparation is never displaced", func(t *testing.T) {
		createCDCTarget(t, "testdb.r_target")
		exec(t, "INSERT INTO testdb.r_target VALUES (1, 'one', '0001', false)")
		inc, _, err := d.CDCTargetIncarnation(ctx, "testdb.r_target")
		require.NoError(t, err)

		plan, err := d.prepareCDCShadowPlan(ctx, "testdb.r_target", inc, true)
		require.NoError(t, err)
		plans := []*cdcShadowPlan{plan}
		exec(t, "DROP TABLE testdb.r_target SYNC")
		createCDCTarget(t, "testdb.r_target")
		exec(t, "INSERT INTO testdb.r_target VALUES (7, 'replacement', '0009', false)")
		replacementUUID := tableUUID(t, "testdb", "r_target")

		err = d.publishCDCShadowPlans(ctx, plans)
		d.cleanupCDCShadowPlans(ctx, plans)
		require.ErrorContains(t, err, "physical incarnation changed")
		require.Equal(t, replacementUUID, tableUUID(t, "testdb", "r_target"))
		require.Equal(t, "7=replacement", titles(t, "testdb.r_target"))
		require.Empty(t, leftoverShadows(t))
	})

	t.Run("batch publication uses one single-pair EXCHANGE per table", func(t *testing.T) {
		createCDCTarget(t, "testdb.b_users")
		createCDCTarget(t, "testdb.b_orders")
		exec(t, "INSERT INTO testdb.b_users VALUES (1, 'u1', '0001', false), (2, 'u2', '0001', false)")
		exec(t, "INSERT INTO testdb.b_orders VALUES (1, 'o1', '0001', false)")
		createStage(t, "_bruin_staging.b_users_stage", "(3, 'u3', '0002', false)")
		createStage(t, "_bruin_staging.b_orders_stage", "(1, 'o1-v2', '0002', false)")
		usersInc, _, err := d.CDCTargetIncarnation(ctx, "testdb.b_users")
		require.NoError(t, err)
		ordersInc, _, err := d.CDCTargetIncarnation(ctx, "testdb.b_orders")
		require.NoError(t, err)

		started := scalar(t, "SELECT toString(now64(6))")
		require.NoError(t, d.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{Truncate: true, Options: destination.MergeOptions{
				TargetTable: "testdb.b_users", StagingTable: "_bruin_staging.b_users_stage", PrimaryKeys: []string{"id"}, Columns: cdcCols, CDCExpectedIncarnation: usersInc,
			}},
			{Options: destination.MergeOptions{
				TargetTable: "testdb.b_orders", StagingTable: "_bruin_staging.b_orders_stage", PrimaryKeys: []string{"id"}, Columns: cdcCols, CDCExpectedIncarnation: ordersInc,
			}},
		}))
		require.Equal(t, "3=u3", titles(t, "testdb.b_users"))
		require.Equal(t, "1=o1-v2", titles(t, "testdb.b_orders"))
		afterUsers, _, err := d.CDCTargetIncarnation(ctx, "testdb.b_users")
		require.NoError(t, err)
		require.Equal(t, usersInc, afterUsers)
		afterOrders, _, err := d.CDCTargetIncarnation(ctx, "testdb.b_orders")
		require.NoError(t, err)
		require.Equal(t, ordersInc, afterOrders)
		require.Empty(t, leftoverShadows(t))

		exec(t, "SYSTEM FLUSH LOGS")
		countQueries := func(prefix, table string) string {
			return scalar(t, "SELECT toString(count()) FROM system.query_log WHERE type = 'QueryStart' AND event_time_microseconds >= toDateTime64(?, 6) AND startsWith(query, ?) AND position(query, ?) > 0",
				started, prefix, table)
		}
		for _, table := range []string{"`testdb`.`b_users`", "`testdb`.`b_orders`"} {
			require.Equal(t, "1", countQueries("EXCHANGE TABLES ", table), table)
			require.Equal(t, "0", countQueries("RENAME TABLE ", table), table)
		}
	})

	t.Run("batch rolls back published tables when a later exchange fails", func(t *testing.T) {
		require.NoError(t, d.conn.Exec(
			clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"allow_deprecated_database_ordinary": 1})),
			"CREATE DATABASE legacy_ordinary ENGINE = Ordinary",
		))
		createCDCTarget(t, "testdb.f_users")
		createCDCTarget(t, "legacy_ordinary.f_orders")
		exec(t, "INSERT INTO testdb.f_users VALUES (1, 'u1', '0001', false)")
		exec(t, "INSERT INTO legacy_ordinary.f_orders VALUES (1, 'o1', '0001', false)")
		createStage(t, "_bruin_staging.f_users_stage", "(2, 'u2', '0002', false)")
		createStage(t, "_bruin_staging.f_orders_stage", "(2, 'o2', '0002', false)")
		usersUUID := tableUUID(t, "testdb", "f_users")
		usersInc, _, err := d.CDCTargetIncarnation(ctx, "testdb.f_users")
		require.NoError(t, err)
		ordersInc, _, err := d.CDCTargetIncarnation(ctx, "legacy_ordinary.f_orders")
		require.NoError(t, err)

		err = d.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{Truncate: true, Options: destination.MergeOptions{
				TargetTable: "testdb.f_users", StagingTable: "_bruin_staging.f_users_stage", PrimaryKeys: []string{"id"}, Columns: cdcCols, CDCExpectedIncarnation: usersInc,
			}},
			{Options: destination.MergeOptions{
				TargetTable: "legacy_ordinary.f_orders", StagingTable: "_bruin_staging.f_orders_stage", PrimaryKeys: []string{"id"}, Columns: cdcCols, CDCExpectedIncarnation: ordersInc,
			}},
		})
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "Atomic"), err.Error())

		exec(t, "SYSTEM FLUSH LOGS")
		require.Equal(t, "2", scalar(t,
			"SELECT toString(count()) FROM system.query_log WHERE type = 'QueryFinish' AND startsWith(query, 'EXCHANGE TABLES ') AND position(query, '`testdb`.`f_users`') > 0"),
			"first table must be published and then exchanged back")

		require.Equal(t, usersUUID, tableUUID(t, "testdb", "f_users"), "first table must be exchanged back")
		require.Equal(t, "1=u1", titles(t, "testdb.f_users"), "truncate+merge of the first table must not stay published")
		require.Equal(t, "1=o1", titles(t, "legacy_ordinary.f_orders"))
		restoredInc, _, err := d.CDCTargetIncarnation(ctx, "testdb.f_users")
		require.NoError(t, err)
		require.Equal(t, usersInc, restoredInc)
		require.Empty(t, leftoverShadows(t))
	})
}
