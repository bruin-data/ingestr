package trino

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
)

func TestMergeExpectedSQL(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()
	d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}
	mock.ExpectExec(`MERGE INTO "iceberg"."analytics"."order" AS t
USING (SELECT "tenant", "key""id", "value" FROM (SELECT "tenant", "key""id", "value", ROW_NUMBER() OVER (PARTITION BY "tenant", "key""id") AS __bruin_dedup_rn FROM "landing"."raw"."stage") AS _numbered WHERE __bruin_dedup_rn = 1) AS s
ON t."tenant" = s."tenant" AND t."key""id" = s."key""id" AND (t."value" > 10 OR t."value" IS NULL)
WHEN MATCHED THEN UPDATE SET "tenant" = s."tenant", "key""id" = s."key""id", "value" = s."value"
WHEN NOT MATCHED THEN INSERT ("tenant", "key""id", "value") VALUES (s."tenant", s."key""id", s."value")`).WillReturnResult(sqlmock.NewResult(0, 2))
	require.NoError(t, d.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable: "order", StagingTable: "landing.raw.stage",
		Columns: []string{"tenant", `key"id`, "value"}, PrimaryKeys: []string{"tenant", `key"id`},
		IncrementalPredicate: `t."value" > 10 OR t."value" IS NULL`,
	}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSCD2ChangeConditions(t *testing.T) {
	require.Equal(t, "false", buildSCD2ChangeDetectionSubquery(`"target"`, `"stage"`, []string{"id"}, nil))
	require.NoError(t, sqlmock.QueryMatcherEqual.Match(`EXISTS (
SELECT 1 FROM "stage" AS source
WHERE source."id" = "target"."id"
AND ("target"."first" IS DISTINCT FROM source."first" OR "target"."second" IS DISTINCT FROM source."second")
)`, buildSCD2ChangeDetectionSubquery(`"target"`, `"stage"`, []string{"id"}, []string{"first", "second"})))
}

func TestSCD2ExpectedSQL(t *testing.T) {
	for _, incremental := range []bool{false, true} {
		name := "snapshot"
		if incremental {
			name = "incremental"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			require.NoError(t, err)
			defer db.Close()
			d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}
			mock.ExpectExec(`UPDATE "iceberg"."analytics"."order" SET
"_scd_valid_to" = ( SELECT source."_scd_valid_from" FROM "landing"."raw"."stage" AS source
WHERE source."tenant" = "iceberg"."analytics"."order"."tenant" AND source."key""id" = "iceberg"."analytics"."order"."key""id" ),
"_scd_is_current" = false
WHERE "_scd_is_current" = true AND (EXISTS (
SELECT 1 FROM "landing"."raw"."stage" AS source
WHERE source."tenant" = "iceberg"."analytics"."order"."tenant" AND source."key""id" = "iceberg"."analytics"."order"."key""id"
AND ("iceberg"."analytics"."order"."value" IS DISTINCT FROM source."value") ))`).WillReturnResult(sqlmock.NewResult(0, 1))
			if !incremental {
				mock.ExpectExec(`UPDATE "iceberg"."analytics"."order" SET
"_scd_valid_to" = TIMESTAMP '2026-02-03 04:05:06.123456', "_scd_is_current" = false
WHERE "_scd_is_current" = true AND NOT EXISTS (
SELECT 1 FROM "landing"."raw"."stage" AS source
WHERE source."tenant" = "iceberg"."analytics"."order"."tenant" AND source."key""id" = "iceberg"."analytics"."order"."key""id" )`).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectExec(`INSERT INTO "iceberg"."analytics"."order" ("tenant", "key""id", "value", "_scd_valid_from", "_scd_valid_to", "_scd_is_current")
SELECT "tenant", "key""id", "value", "_scd_valid_from", "_scd_valid_to", "_scd_is_current" FROM "landing"."raw"."stage" AS source
WHERE NOT EXISTS ( SELECT 1 FROM "iceberg"."analytics"."order" AS target
WHERE target."tenant" = source."tenant" AND target."key""id" = source."key""id" AND target."_scd_is_current" = true )`).WillReturnResult(sqlmock.NewResult(0, 2))
			opts := destination.SCD2Options{
				TargetTable: "order", StagingTable: "landing.raw.stage",
				Columns:     []string{"tenant", `key"id`, "value", "_scd_valid_from", "_scd_valid_to", "_scd_is_current"},
				PrimaryKeys: []string{"tenant", `key"id`}, Timestamp: time.Date(2026, 2, 3, 4, 5, 6, 123456000, time.UTC),
			}
			if incremental {
				opts.IncrementalKey = "value"
			}
			require.NoError(t, d.SCD2Table(t.Context(), opts))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
