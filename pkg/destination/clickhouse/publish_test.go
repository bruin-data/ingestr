package clickhouse

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/stretchr/testify/require"
)

func TestRelaxColumnNullabilitySQLPreservesNativeType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		native string
		col    schema.Column
		want   string
	}{
		{"DateTime64 nanoseconds", "DateTime64(9)", schema.Column{DataType: schema.TypeTimestamp}, "Nullable(DateTime64(9))"},
		{"DateTime64 with timezone", "DateTime64(9, 'UTC')", schema.Column{DataType: schema.TypeTimestampTZ}, "Nullable(DateTime64(9, 'UTC'))"},
		{"Decimal256", "Decimal(76, 20)", schema.Column{DataType: schema.TypeDecimal, Precision: 76, Scale: 20}, "Nullable(Decimal(76, 20))"},
		{"FixedString", "FixedString(16)", schema.Column{DataType: schema.TypeString}, "Nullable(FixedString(16))"},
		{"LowCardinality", "LowCardinality(String)", schema.Column{DataType: schema.TypeString}, "LowCardinality(Nullable(String))"},
		{"Array elements", "Array(DateTime64(9))", schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeTimestamp}, "Array(Nullable(DateTime64(9)))"},
		{"already nullable", "Nullable(DateTime64(9))", schema.Column{DataType: schema.TypeTimestamp, Nullable: true}, "Nullable(DateTime64(9))"},
		{"Map cannot be nullable", "Map(String, UInt64)", schema.Column{DataType: schema.TypeJSON}, ""},
		{"Tuple cannot be nullable", "Tuple(a String)", schema.Column{DataType: schema.TypeJSON}, ""},
		{"Variant already accepts NULL", "Variant(String, UInt64)", schema.Column{DataType: schema.TypeJSON}, "Variant(String, UInt64)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.col.Name = "c"
			d := &Dialect{
				columns:     map[string]schema.Column{"c": tt.col},
				nativeTypes: map[string]string{"c": tt.native},
			}
			got := d.RelaxColumnNullabilitySQL("`db`.`t`", "C")
			if tt.want == "" {
				require.Empty(t, got)
				return
			}
			require.Equal(t, "ALTER TABLE `db`.`t` MODIFY COLUMN `C` "+tt.want, got)
		})
	}

	t.Run("falls back to schema mapping without a native type", func(t *testing.T) {
		t.Parallel()
		d := &Dialect{cluster: "c1", columns: map[string]schema.Column{"ts": {Name: "ts", DataType: schema.TypeTimestamp}}}
		require.Equal(t, "ALTER TABLE `db`.`t` ON CLUSTER `c1` MODIFY COLUMN `ts` Nullable(DateTime64(6))", d.RelaxColumnNullabilitySQL("`db`.`t`", "ts"))
	})

	t.Run("primary key and unknown columns are never relaxed", func(t *testing.T) {
		t.Parallel()
		d := &Dialect{
			columns:     map[string]schema.Column{"id": {Name: "id", DataType: schema.TypeInt64, IsPrimaryKey: true}},
			nativeTypes: map[string]string{"id": "Int64"},
		}
		require.Empty(t, d.RelaxColumnNullabilitySQL("`db`.`t`", "id"))
		require.Empty(t, d.RelaxColumnNullabilitySQL("`db`.`t`", "missing"))
	})
}

func TestSchemaEvolutionKeepsDateTime64Precision(t *testing.T) {
	t.Parallel()

	targetUUID := "aaaa1111-1111-1111-1111-111111111111"
	expected := destination.CDCTargetKey("warehouse", "events", targetUUID)
	newCatalog := func() *mockCatalogConn {
		cat := newMockCatalogConn()
		cat.putTable("warehouse", "events", &simTable{
			uuid: targetUUID, modTime: "2026-01-01 00:00:00", engine: "ReplacingMergeTree", sortingKey: "id",
			partitions: []string{"all"}, tag: "events-orig",
			columns: [][]any{
				{"id", "Int64", uint8(1)},
				{"happened_at", "DateTime64(9, 'UTC')", uint8(0)},
				{"dropped_at", "DateTime64(9)", uint8(0)},
			},
		})
		return cat
	}

	t.Run("conditional evolution", func(t *testing.T) {
		t.Parallel()
		cat := newCatalog()
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		_, inc, err := dest.ApplySchemaEvolutionIfIncarnation(t.Context(), "events", &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{
				{
					Type:       schemaevolution.ChangeRelaxNullability,
					ColumnName: "happened_at",
					OldColumn:  &schema.Column{Name: "happened_at", DataType: schema.TypeTimestampTZ},
					NewColumn:  schema.Column{Name: "happened_at", DataType: schema.TypeTimestampTZ, Nullable: true},
				},
				{
					Type:       schemaevolution.ChangeRemoveColumn,
					ColumnName: "dropped_at",
					OldColumn:  &schema.Column{Name: "dropped_at", DataType: schema.TypeTimestamp},
				},
			},
		}, expected)
		require.NoError(t, err)
		require.Equal(t, expected, inc)

		var relaxed, dropped bool
		for _, q := range cat.execs {
			require.NotContains(t, q, "DateTime64(6", "nullability relaxation must not lose sub-microsecond precision: %s", q)
			if strings.Contains(q, "MODIFY COLUMN `happened_at` Nullable(DateTime64(9, 'UTC'))") {
				relaxed = true
			}
			if strings.Contains(q, "MODIFY COLUMN `dropped_at` Nullable(DateTime64(9))") {
				dropped = true
			}
		}
		require.True(t, relaxed)
		require.True(t, dropped)
	})

	t.Run("unconditional evolution", func(t *testing.T) {
		t.Parallel()
		cat := newCatalog()
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		_, err := dest.ApplySchemaEvolution(t.Context(), "events", &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{{
				Type:       schemaevolution.ChangeRelaxNullability,
				ColumnName: "happened_at",
				OldColumn:  &schema.Column{Name: "happened_at", DataType: schema.TypeTimestampTZ},
				NewColumn:  schema.Column{Name: "happened_at", DataType: schema.TypeTimestampTZ, Nullable: true},
			}},
		})
		require.NoError(t, err)
		require.Contains(t, cat.execs, "ALTER TABLE `warehouse`.`events` MODIFY COLUMN `happened_at` Nullable(DateTime64(9, 'UTC'))")
	})
}

func twoTableCatalog() (*mockCatalogConn, []destination.CDCAtomicTableMerge) {
	u1 := "11111111-1111-1111-1111-111111111111"
	u2 := "22222222-2222-2222-2222-222222222222"
	cat := newMockCatalogConn()
	cat.putTable("warehouse", "users", &simTable{
		uuid: u1, modTime: "2026-01-01 00:00:00", engine: "ReplacingMergeTree", sortingKey: "id", partitions: []string{"all"}, tag: "users-orig", version: 7,
	})
	cat.putTable("warehouse", "orders", &simTable{
		uuid: u2, modTime: "2026-01-01 00:00:00", engine: "ReplacingMergeTree", sortingKey: "id", partitions: []string{"all"}, tag: "orders-orig", version: 9,
	})
	cols := []string{"id", "name", destination.CDCLSNColumn, destination.CDCDeletedColumn}
	merges := []destination.CDCAtomicTableMerge{
		{Truncate: true, Options: destination.MergeOptions{
			TargetTable: "users", StagingTable: "_bruin_staging.users_stage", PrimaryKeys: []string{"id"}, Columns: cols,
			CDCExpectedIncarnation: destination.CDCTargetKey("warehouse", "users", u1),
		}},
		{Options: destination.MergeOptions{
			TargetTable: "orders", StagingTable: "_bruin_staging.orders_stage", PrimaryKeys: []string{"id"}, Columns: cols,
			CDCExpectedIncarnation: destination.CDCTargetKey("warehouse", "orders", u2),
		}},
	}
	return cat, merges
}

func requireOriginal(t *testing.T, cat *mockCatalogConn, name, uuid, tag string) {
	t.Helper()
	live, ok := cat.getTable("warehouse", name)
	require.True(t, ok)
	require.Equal(t, uuid, live.uuid)
	require.Equal(t, tag, live.tag)
	require.False(t, live.mutated, "%s must not be mutated", name)
}

func shadowTables(cat *mockCatalogConn) []*simTable {
	cat.mu.Lock()
	defer cat.mu.Unlock()
	var out []*simTable
	for k, v := range cat.tables {
		if strings.Contains(k, "__ingestr_cdc_") {
			out = append(out, v)
		}
	}
	return out
}

func TestMergeCDCTablesAtomicallyPublishesWithPerTableExchange(t *testing.T) {
	t.Parallel()

	t.Run("success exchanges each table once and never renames", func(t *testing.T) {
		t.Parallel()
		cat, merges := twoTableCatalog()
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		require.NoError(t, dest.MergeCDCTablesAtomically(t.Context(), merges))

		exchanges := 0
		for _, q := range cat.execs {
			require.False(t, strings.HasPrefix(q, "RENAME TABLE "), "publication must not use a non-atomic multi-pair RENAME: %s", q)
			if strings.HasPrefix(q, "EXCHANGE TABLES ") {
				require.Equal(t, 1, strings.Count(q, " AND "), "each EXCHANGE must swap exactly one pair: %s", q)
				exchanges++
			}
		}
		require.Equal(t, 2, exchanges)
		for _, name := range []string{"users", "orders"} {
			live, ok := cat.getTable("warehouse", name)
			require.True(t, ok)
			require.True(t, strings.HasPrefix(live.tag, "clone-of-"), "%s should now be the published shadow", name)
			inc, exists, err := dest.CDCTargetIncarnation(t.Context(), name)
			require.NoError(t, err)
			require.True(t, exists)
			require.Equal(t, merges[map[string]int{"users": 0, "orders": 1}[name]].Options.CDCExpectedIncarnation, inc)
		}
		require.Empty(t, shadowTables(cat), "displaced originals and shadows must be dropped")
	})

	t.Run("exchange failure on second table restores the first", func(t *testing.T) {
		t.Parallel()
		cat, merges := twoTableCatalog()
		cat.beforeExec = func(norm string, _ *mockCatalogConn) error {
			if strings.HasPrefix(norm, "EXCHANGE TABLES ") && strings.Contains(norm, "`warehouse`.`orders`") {
				return errors.New("simulated exchange failure")
			}
			return nil
		}
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.MergeCDCTablesAtomically(t.Context(), merges)
		require.ErrorContains(t, err, "simulated exchange failure")

		requireOriginal(t, cat, "users", "11111111-1111-1111-1111-111111111111", "users-orig")
		requireOriginal(t, cat, "orders", "22222222-2222-2222-2222-222222222222", "orders-orig")
		require.Empty(t, shadowTables(cat))

		var forward, back int
		for _, q := range cat.execs {
			if strings.HasPrefix(q, "EXCHANGE TABLES ") && strings.Contains(q, "`warehouse`.`users`") {
				if forward == 0 {
					forward++
				} else {
					back++
				}
			}
		}
		require.Equal(t, 1, forward)
		require.Equal(t, 1, back, "first table must be exchanged back")
	})

	t.Run("write committed to a target while its shadow was prepared aborts before publishing", func(t *testing.T) {
		t.Parallel()
		cat, merges := twoTableCatalog()
		var wrote atomic.Bool
		cat.beforeExec = func(norm string, c *mockCatalogConn) error {
			if strings.HasPrefix(norm, "INSERT INTO `_bruin_staging`.`__ingestr_cdc_shw_") && strings.Contains(norm, "`orders_stage`") && wrote.CompareAndSwap(false, true) {
				c.mu.Lock()
				c.tables["warehouse.orders"].version++
				c.mu.Unlock()
			}
			return nil
		}
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.MergeCDCTablesAtomically(t.Context(), merges)
		require.ErrorContains(t, err, "written outside this run")
		for _, q := range cat.execs {
			require.False(t, strings.HasPrefix(q, "EXCHANGE TABLES "), "nothing may be published: %s", q)
		}
		requireOriginal(t, cat, "users", "11111111-1111-1111-1111-111111111111", "users-orig")
		require.Empty(t, shadowTables(cat))
	})

	t.Run("write racing the exchange is detected on the displaced table and rolled back", func(t *testing.T) {
		t.Parallel()
		cat, merges := twoTableCatalog()
		var wrote atomic.Bool
		cat.beforeExec = func(norm string, c *mockCatalogConn) error {
			if strings.HasPrefix(norm, "EXCHANGE TABLES ") && strings.Contains(norm, "`warehouse`.`orders`") && wrote.CompareAndSwap(false, true) {
				c.mu.Lock()
				c.tables["warehouse.orders"].version++
				c.mu.Unlock()
			}
			return nil
		}
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.MergeCDCTablesAtomically(t.Context(), merges)
		require.ErrorContains(t, err, "written outside this run")
		requireOriginal(t, cat, "users", "11111111-1111-1111-1111-111111111111", "users-orig")
		orders, ok := cat.getTable("warehouse", "orders")
		require.True(t, ok)
		require.Equal(t, "orders-orig", orders.tag)
		require.Equal(t, int64(10), orders.version, "the concurrent write must stay in the live table")
		require.Empty(t, shadowTables(cat))
	})

	t.Run("write to a published table is not rolled back when a later table fails", func(t *testing.T) {
		t.Parallel()
		cat, merges := twoTableCatalog()
		cat.beforeExec = func(norm string, c *mockCatalogConn) error {
			if strings.HasPrefix(norm, "EXCHANGE TABLES ") && strings.Contains(norm, "`warehouse`.`orders`") {
				c.mu.Lock()
				c.tables["warehouse.users"].version++
				c.mu.Unlock()
				return errors.New("simulated exchange failure")
			}
			return nil
		}
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.MergeCDCTablesAtomically(t.Context(), merges)
		require.ErrorContains(t, err, "simulated exchange failure")
		require.ErrorContains(t, err, "left published so that write is kept")

		users, ok := cat.getTable("warehouse", "users")
		require.True(t, ok)
		require.Equal(t, "clone-of-users-orig", users.tag, "the written published table must stay live")
		requireOriginal(t, cat, "orders", "22222222-2222-2222-2222-222222222222", "orders-orig")
		var usersExchanges int
		for _, q := range cat.execs {
			if strings.HasPrefix(q, "EXCHANGE TABLES ") && strings.Contains(q, "`warehouse`.`users`") {
				usersExchanges++
			}
		}
		require.Equal(t, 1, usersExchanges, "the written table must not be exchanged back")
	})

	t.Run("write racing the restore keeps the published table instead of dropping it", func(t *testing.T) {
		t.Parallel()
		cat, merges := twoTableCatalog()
		var usersExchanges atomic.Int32
		cat.beforeExec = func(norm string, c *mockCatalogConn) error {
			if !strings.HasPrefix(norm, "EXCHANGE TABLES ") {
				return nil
			}
			if strings.Contains(norm, "`warehouse`.`orders`") {
				return errors.New("simulated exchange failure")
			}
			if usersExchanges.Add(1) > 1 {
				c.mu.Lock()
				c.tables["warehouse.users"].version++
				c.mu.Unlock()
			}
			return nil
		}
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.MergeCDCTablesAtomically(t.Context(), merges)
		require.ErrorContains(t, err, "simulated exchange failure")
		require.ErrorContains(t, err, "while it was being restored; that table is kept at _bruin_staging.__ingestr_cdc_shw_")

		requireOriginal(t, cat, "users", "11111111-1111-1111-1111-111111111111", "users-orig")
		var kept bool
		for _, tbl := range shadowTables(cat) {
			if tbl.tag == "clone-of-users-orig" {
				kept = true
			}
		}
		require.True(t, kept, "the table holding the outside write must not be dropped")
	})

	t.Run("failed restore keeps the displaced original instead of dropping it", func(t *testing.T) {
		t.Parallel()
		cat, merges := twoTableCatalog()
		var usersExchanges atomic.Int32
		cat.beforeExec = func(norm string, _ *mockCatalogConn) error {
			if !strings.HasPrefix(norm, "EXCHANGE TABLES ") {
				return nil
			}
			if strings.Contains(norm, "`warehouse`.`orders`") {
				return errors.New("simulated exchange failure")
			}
			if usersExchanges.Add(1) > 1 {
				return errors.New("simulated restore failure")
			}
			return nil
		}
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.MergeCDCTablesAtomically(t.Context(), merges)
		require.ErrorContains(t, err, "simulated exchange failure")
		require.ErrorContains(t, err, "simulated restore failure")
		require.ErrorContains(t, err, "is kept at _bruin_staging.__ingestr_cdc_shw_")

		var kept bool
		for _, tbl := range shadowTables(cat) {
			if tbl.uuid == "11111111-1111-1111-1111-111111111111" {
				kept = true
				require.Equal(t, "users-orig", tbl.tag)
				require.False(t, tbl.mutated)
			}
		}
		require.True(t, kept, "original users table must not be dropped when it cannot be restored")
	})
}

func TestFencedSwapTableUsesSingleAtomicStatement(t *testing.T) {
	t.Parallel()

	targetUUID := "aaaa1111-1111-1111-1111-111111111111"
	stagingUUID := "bbbb2222-2222-2222-2222-222222222222"
	stageInc := destination.CDCTargetKey("_bruin_staging", "users_stage", stagingUUID)
	resultInc := destination.CDCTargetKey("warehouse", "users", stagingUUID)

	t.Run("EXCHANGE failure never falls back to renaming the live target away", func(t *testing.T) {
		t.Parallel()
		cat := newMockCatalogConn()
		cat.putTable("warehouse", "users", &simTable{uuid: targetUUID, modTime: "2026-01-01 00:00:00", tag: "users-orig"})
		cat.putTable("_bruin_staging", "users_stage", &simTable{uuid: stagingUUID, modTime: "2026-01-01 00:00:00", tag: "stage-new"})
		cat.beforeExec = func(norm string, _ *mockCatalogConn) error {
			if strings.HasPrefix(norm, "EXCHANGE TABLES ") {
				return errors.New("EXCHANGE is supported only for Atomic databases")
			}
			return nil
		}
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		err := dest.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:                   "warehouse.users",
			StagingTable:                  "_bruin_staging.users_stage",
			CDCExpectedIncarnation:        destination.CDCTargetKey("warehouse", "users", targetUUID),
			CDCExpectedStagingIncarnation: stageInc,
			CDCExpectedResultIncarnation:  resultInc,
		})
		require.ErrorContains(t, err, "requires EXCHANGE TABLES")
		for _, q := range cat.execs {
			require.False(t, strings.HasPrefix(q, "RENAME TABLE "), "fenced swap must not fall back to RENAME: %s", q)
		}
		requireOriginal(t, cat, "users", targetUUID, "users-orig")
	})

	t.Run("missing target is published with RENAME that cannot overwrite a racing table", func(t *testing.T) {
		t.Parallel()
		cat := newMockCatalogConn()
		cat.putTable("_bruin_staging", "users_stage", &simTable{uuid: stagingUUID, modTime: "2026-01-01 00:00:00", tag: "stage-new"})
		var raced atomic.Bool
		cat.beforeExec = func(norm string, c *mockCatalogConn) error {
			if strings.HasPrefix(norm, "RENAME TABLE `_bruin_staging`.`users_stage` TO `warehouse`.`users`") && raced.CompareAndSwap(false, true) {
				c.putTable("warehouse", "users", &simTable{uuid: "cccc3333-3333-3333-3333-333333333333", modTime: "2026-01-01 00:01:00", tag: "racing-table"})
			}
			return nil
		}
		dest := &ClickHouseDestination{conn: cat, database: "warehouse"}
		opts := destination.SwapOptions{
			TargetTable:                   "warehouse.users",
			StagingTable:                  "_bruin_staging.users_stage",
			CDCExpectedStagingIncarnation: stageInc,
			CDCExpectedResultIncarnation:  resultInc,
		}
		err := dest.SwapTable(t.Context(), opts)
		require.ErrorContains(t, err, "physical incarnation changed")
		for _, q := range cat.execs {
			require.False(t, strings.HasPrefix(q, "EXCHANGE TABLES "), "a missing target must not be exchanged: %s", q)
		}
		requireOriginal(t, cat, "users", "cccc3333-3333-3333-3333-333333333333", "racing-table")
		_, stagingKept := cat.getTable("_bruin_staging", "users_stage")
		require.True(t, stagingKept)

		cat.mu.Lock()
		delete(cat.tables, "warehouse.users")
		cat.mu.Unlock()
		require.NoError(t, dest.SwapTable(t.Context(), opts))
		live, ok := cat.getTable("warehouse", "users")
		require.True(t, ok)
		require.Equal(t, "stage-new", live.tag)
	})
}
