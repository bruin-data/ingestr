package clickhouse

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
)

type strategySQLConn struct {
	driver.Conn
	t          *testing.T
	statements []string
}

func (c *strategySQLConn) Exec(_ context.Context, query string, _ ...any) error {
	c.statements = append(c.statements, strings.Join(strings.Fields(query), " "))
	return nil
}

func (c *strategySQLConn) Query(_ context.Context, query string, args ...any) (driver.Rows, error) {
	require.Equal(c.t, "SELECT count(), anyIf(latest_fail_reason, latest_fail_reason != '') FROM system.mutations WHERE database = ? AND table = ? AND is_done = 0", query)
	require.Equal(c.t, []any{"warehouse", "order"}, args)
	c.statements = append(c.statements, "WAIT FOR MUTATIONS")
	return &completedMutationRows{}, nil
}

type completedMutationRows struct{ driver.Rows }

func (*completedMutationRows) Next() bool { return true }
func (*completedMutationRows) Scan(dest ...any) error {
	*dest[0].(*uint64) = 0
	*dest[1].(*string) = ""
	return nil
}
func (*completedMutationRows) Err() error   { return nil }
func (*completedMutationRows) Close() error { return nil }

func TestMergeExpectedSQL(t *testing.T) {
	c := &strategySQLConn{t: t}
	d := &ClickHouseDestination{conn: c, database: "warehouse"}
	require.NoError(t, d.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable: "order", StagingTable: "landing.stage",
		Columns: []string{"tenant", "key`id", "value"}, PrimaryKeys: []string{"tenant", "key`id"},
	}))
	require.Equal(t, []string{
		"INSERT INTO `warehouse`.`order` (`tenant`, `key``id`, `value`) SELECT `tenant`, `key``id`, `value` FROM `landing`.`stage`",
		"OPTIMIZE TABLE `warehouse`.`order` FINAL",
	}, c.statements)
}

func TestSCD2ChangeConditions(t *testing.T) {
	require.Equal(t, "0", buildChangeConditionsClickHouse(nil, "target", "source"))
	require.Equal(t,
		"NOT (ifNull(target.`first` = source.`first`, 0) OR (target.`first` IS NULL AND source.`first` IS NULL)) OR NOT (ifNull(target.`second` = source.`second`, 0) OR (target.`second` IS NULL AND source.`second` IS NULL))",
		buildChangeConditionsClickHouse([]string{"first", "second"}, "target", "source"))
}

func TestSCD2ExpectedSQL(t *testing.T) {
	for _, incremental := range []bool{false, true} {
		name := "snapshot"
		if incremental {
			name = "incremental"
		}
		t.Run(name, func(t *testing.T) {
			c := &strategySQLConn{t: t}
			d := &ClickHouseDestination{conn: c, database: "warehouse"}
			opts := destination.SCD2Options{
				TargetTable: "order", StagingTable: "landing.stage",
				Columns:     []string{"tenant", "key`id", "value", "_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
				PrimaryKeys: []string{"tenant", "key`id"}, Timestamp: time.Date(2026, 2, 3, 4, 5, 6, 123456000, time.UTC),
			}
			if incremental {
				opts.IncrementalKey = "value"
			}
			require.NoError(t, d.SCD2Table(t.Context(), opts))
			want := []string{
				"ALTER TABLE `warehouse`.`order` UPDATE _scd_valid_to = toDateTime64('2026-02-03 04:05:06.123456', 6), _scd_is_current = 0 WHERE _scd_is_current = 1 AND (`tenant`, `key``id`) IN ( SELECT target.`tenant`, target.`key``id` FROM `warehouse`.`order` AS target INNER JOIN `landing`.`stage` AS source ON target.`tenant` = source.`tenant` AND target.`key``id` = source.`key``id` WHERE target._scd_is_current = 1 AND (NOT (ifNull(target.`value` = source.`value`, 0) OR (target.`value` IS NULL AND source.`value` IS NULL))) )",
				"WAIT FOR MUTATIONS",
			}
			if !incremental {
				want = append(want, "ALTER TABLE `warehouse`.`order` UPDATE _scd_valid_to = toDateTime64('2026-02-03 04:05:06.123456', 6), _scd_is_current = 0 WHERE _scd_is_current = 1 AND (`tenant`, `key``id`) NOT IN (SELECT `tenant`, `key``id` FROM `landing`.`stage`)", "WAIT FOR MUTATIONS")
			}
			want = append(want, "INSERT INTO `warehouse`.`order` (`tenant`, `key``id`, `value`, `_scd_valid_from`, `_scd_valid_to`, `_scd_is_current`) SELECT source.`tenant`, source.`key``id`, source.`value`, source.`_scd_valid_from`, source.`_scd_valid_to`, source.`_scd_is_current` FROM `landing`.`stage` AS source LEFT ANTI JOIN `warehouse`.`order` AS target ON target.`tenant` = source.`tenant` AND target.`key``id` = source.`key``id` AND target._scd_is_current = 1")
			require.Equal(t, want, c.statements)
		})
	}
}

func TestDeleteInsertExpectedSQL(t *testing.T) {
	for _, withKeys := range []bool{false, true} {
		name := "without keys"
		if withKeys {
			name = "composite keys"
		}
		t.Run(name, func(t *testing.T) {
			c := &strategySQLConn{t: t}
			d := &ClickHouseDestination{conn: c, database: "warehouse"}
			opts := destination.DeleteInsertOptions{TargetTable: "order", StagingTable: "landing.stage", Columns: []string{"tenant", "id", "version"}, IncrementalKey: "version", IntervalStart: 11, IntervalEnd: 29}
			selectSQL := "SELECT `tenant`, `id`, `version` FROM `landing`.`stage`"
			if withKeys {
				opts.PrimaryKeys = []string{"tenant", "id"}
				selectSQL = "SELECT `tenant`, `id`, `version` FROM (SELECT `tenant`, `id`, `version`, ROW_NUMBER() OVER (PARTITION BY `tenant`, `id` ORDER BY `version` DESC) AS __bruin_dedup_rn FROM `landing`.`stage`) AS _numbered WHERE __bruin_dedup_rn = 1"
			}
			require.NoError(t, d.DeleteInsertTable(t.Context(), opts))
			require.Equal(t, []string{"ALTER TABLE `warehouse`.`order` DELETE WHERE `version` >= 11 AND `version` <= 29", "WAIT FOR MUTATIONS", "INSERT INTO `warehouse`.`order` (`tenant`, `id`, `version`) " + selectSQL}, c.statements)
		})
	}
}
