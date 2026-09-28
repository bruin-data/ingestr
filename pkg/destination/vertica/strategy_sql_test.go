package vertica

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
)

func TestMergeExpectedSQL(t *testing.T) {
	for _, tt := range []struct {
		name    string
		columns []string
		key     string
		want    string
	}{
		{
			name:    "composite keys and latest version with internal alias collision",
			columns: []string{"tenant", `key"id`, "value", "version", "__bruin_dedup_rn", "_cdc_unchanged_cols"}, key: "version",
			want: `MERGE INTO "warehouse"."order" target
USING (SELECT "tenant", "key""id", "value", "version", "__bruin_dedup_rn" FROM (SELECT "tenant", "key""id", "value", "version", "__bruin_dedup_rn", ROW_NUMBER() OVER (PARTITION BY "tenant", "key""id" ORDER BY "version" DESC) AS "__bruin_dedup_rn_2" FROM "landing"."stage") numbered WHERE "__bruin_dedup_rn_2" = 1) source
ON (target."tenant" = source."tenant" AND target."key""id" = source."key""id")
WHEN MATCHED THEN UPDATE SET "value" = source."value", "version" = source."version", "__bruin_dedup_rn" = source."__bruin_dedup_rn"
WHEN NOT MATCHED THEN INSERT ("tenant", "key""id", "value", "version", "__bruin_dedup_rn") VALUES (source."tenant", source."key""id", source."value", source."version", source."__bruin_dedup_rn")`,
		},
		{
			name:    "keys only omits update and orders by keys",
			columns: []string{"tenant", `key"id`},
			want: `MERGE INTO "warehouse"."order" target
USING (SELECT "tenant", "key""id" FROM (SELECT "tenant", "key""id", ROW_NUMBER() OVER (PARTITION BY "tenant", "key""id" ORDER BY "tenant", "key""id") AS "__bruin_dedup_rn" FROM "landing"."stage") numbered WHERE "__bruin_dedup_rn" = 1) source
ON (target."tenant" = source."tenant" AND target."key""id" = source."key""id")
WHEN NOT MATCHED THEN INSERT ("tenant", "key""id") VALUES (source."tenant", source."key""id")`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			require.NoError(t, err)
			defer db.Close()
			d := &VerticaDestination{db: db}
			mock.ExpectExec(tt.want).WillReturnResult(sqlmock.NewResult(0, 2))
			require.NoError(t, d.MergeTable(t.Context(), destination.MergeOptions{
				TargetTable: "warehouse.order", StagingTable: "landing.stage", Columns: tt.columns,
				PrimaryKeys: []string{"tenant", `key"id`}, IncrementalKey: tt.key,
			}))
			require.NoError(t, mock.ExpectationsWereMet())
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
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			require.NoError(t, err)
			defer db.Close()
			d := &VerticaDestination{db: db}
			opts := destination.DeleteInsertOptions{TargetTable: "warehouse.order", StagingTable: "landing.stage", Columns: []string{"tenant", `key"id`, "version"}, IncrementalKey: "version", IntervalStart: 11, IntervalEnd: 29}
			selectSQL := `SELECT "tenant", "key""id", "version" FROM "landing"."stage"`
			if withKeys {
				opts.PrimaryKeys = []string{"tenant", `key"id`}
				selectSQL = `SELECT "tenant", "key""id", "version" FROM (SELECT "tenant", "key""id", "version" FROM (SELECT "tenant", "key""id", "version", ROW_NUMBER() OVER (PARTITION BY "tenant", "key""id" ORDER BY "version" DESC) AS "__bruin_dedup_rn" FROM "landing"."stage") numbered WHERE "__bruin_dedup_rn" = 1) source`
			}
			mock.ExpectBegin()
			mock.ExpectExec(`DELETE FROM "warehouse"."order" WHERE "version" >= ? AND "version" <= ?`).WithArgs(11, 29).WillReturnResult(sqlmock.NewResult(0, 2))
			mock.ExpectExec(`INSERT INTO "warehouse"."order" ("tenant", "key""id", "version") ` + selectSQL).WillReturnResult(sqlmock.NewResult(0, 2))
			mock.ExpectCommit()
			require.NoError(t, d.DeleteInsertTable(t.Context(), opts))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUnsupportedStrategyOptions(t *testing.T) {
	d := &VerticaDestination{}
	require.False(t, d.SupportsSCD2Strategy())
	require.EqualError(t, d.SCD2Table(t.Context(), destination.SCD2Options{}), "vertica: scd2 strategy is not supported")
	require.EqualError(t, d.MergeTable(t.Context(), destination.MergeOptions{}), "vertica merge requires at least one primary key")
}
