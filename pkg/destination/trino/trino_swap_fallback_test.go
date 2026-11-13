package trino

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

const (
	fallbackTargetFQN  = `"hive"."analytics"."orders"`
	fallbackStagingFQN = `"hive"."_bruin_staging"."orders_stg"`
)

func newFallbackSwapMock(t *testing.T) (*TrinoDestination, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &TrinoDestination{db: db, catalog: "hive", schema: "analytics"}, mock
}

func expectReplaceUnsupported(mock sqlmock.Sqlmock, selectList string) {
	mock.ExpectExec(regexp.QuoteMeta(`CREATE SCHEMA IF NOT EXISTS "hive"."analytics"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(buildReplaceTableFromStagingSQL(fallbackTargetFQN, fallbackStagingFQN, selectList, jsonTypeVarchar))).
		WillReturnError(errors.New("NOT_SUPPORTED: This connector does not support replacing tables"))
	mock.ExpectQuery(`FROM "hive".information_schema.tables`).
		WillReturnRows(sqlmock.NewRows([]string{"one"}).AddRow(1))
}

func expectColumns(mock sqlmock.Sqlmock, schemaName, tableName string, cols ...[2]string) {
	rows := sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"})
	for _, c := range cols {
		rows.AddRow(c[0], c[1], "YES")
	}
	mock.ExpectQuery(regexp.QuoteMeta(`FROM "hive".information_schema.columns`) + `[\s\S]*'` + schemaName + `'[\s\S]*'` + tableName + `'`).
		WillReturnRows(rows)
}

func fallbackSwapSchema(cols ...string) *schema.TableSchema {
	s := &schema.TableSchema{}
	for _, c := range cols {
		s.Columns = append(s.Columns, schema.Column{Name: c, DataType: schema.TypeString})
	}
	return s
}

func TestSwapTableWithoutReplaceSupport(t *testing.T) {
	t.Parallel()

	oldTablePattern := `"hive"\."analytics"\."orders_old_\d+"`

	t.Run("schema change replaces target instead of inserting into old columns", func(t *testing.T) {
		t.Parallel()
		d, mock := newFallbackSwapMock(t)

		expectReplaceUnsupported(mock, `"id", "name", "email"`)
		expectColumns(mock, "_bruin_staging", "orders_stg", [2]string{"id", "varchar"}, [2]string{"name", "varchar"}, [2]string{"email", "varchar"})
		expectColumns(mock, "analytics", "orders", [2]string{"id", "varchar"}, [2]string{"name", "varchar"})
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE `+fallbackTargetFQN+` RENAME TO `) + oldTablePattern).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE ` + fallbackStagingFQN + ` RENAME TO ` + fallbackTargetFQN)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS `) + oldTablePattern).WillReturnResult(sqlmock.NewResult(0, 0))

		require.NoError(t, d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:  "analytics.orders",
			StagingTable: "_bruin_staging.orders_stg",
			Schema:       fallbackSwapSchema("id", "name", "email"),
		}))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("reordered columns replace target instead of inserting by position", func(t *testing.T) {
		t.Parallel()
		d, mock := newFallbackSwapMock(t)

		expectReplaceUnsupported(mock, `"name", "id"`)
		expectColumns(mock, "_bruin_staging", "orders_stg", [2]string{"name", "varchar"}, [2]string{"id", "varchar"})
		expectColumns(mock, "analytics", "orders", [2]string{"id", "varchar"}, [2]string{"name", "varchar"})
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE `+fallbackTargetFQN+` RENAME TO `) + oldTablePattern).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE ` + fallbackStagingFQN + ` RENAME TO ` + fallbackTargetFQN)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS `) + oldTablePattern).WillReturnResult(sqlmock.NewResult(0, 0))

		require.NoError(t, d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:  "analytics.orders",
			StagingTable: "_bruin_staging.orders_stg",
			Schema:       fallbackSwapSchema("name", "id"),
		}))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("matching shape without transactions falls back to rename", func(t *testing.T) {
		t.Parallel()
		d, mock := newFallbackSwapMock(t)

		expectReplaceUnsupported(mock, `"id", "name"`)
		expectColumns(mock, "_bruin_staging", "orders_stg", [2]string{"id", "varchar"}, [2]string{"name", "varchar"})
		expectColumns(mock, "analytics", "orders", [2]string{"id", "varchar"}, [2]string{"name", "varchar"})
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE `+fallbackTargetFQN+` RENAME TO `) + oldTablePattern).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE ` + fallbackStagingFQN + ` RENAME TO ` + fallbackTargetFQN)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS `) + oldTablePattern).WillReturnResult(sqlmock.NewResult(0, 0))

		require.NoError(t, d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:  "analytics.orders",
			StagingTable: "_bruin_staging.orders_stg",
			Schema:       fallbackSwapSchema("id", "name"),
		}))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("staging-only columns are projected out before rename", func(t *testing.T) {
		t.Parallel()
		d, mock := newFallbackSwapMock(t)

		expectReplaceUnsupported(mock, `"id", "name"`)
		expectColumns(mock, "_bruin_staging", "orders_stg", [2]string{"id", "varchar"}, [2]string{"name", "varchar"}, [2]string{"_cdc_unchanged_cols", "varchar"})
		expectColumns(mock, "analytics", "orders", [2]string{"id", "varchar"})
		newTablePattern := `"hive"\."analytics"\."orders_new_\d+"`
		mock.ExpectExec(regexp.QuoteMeta(`CREATE TABLE `) + newTablePattern + regexp.QuoteMeta("\nAS SELECT \"id\", \"name\" FROM "+fallbackStagingFQN)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE `+fallbackTargetFQN+` RENAME TO `) + oldTablePattern).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE `) + newTablePattern + regexp.QuoteMeta(` RENAME TO `+fallbackTargetFQN)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS `) + oldTablePattern).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS ` + fallbackStagingFQN)).WillReturnResult(sqlmock.NewResult(0, 0))

		require.NoError(t, d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:  "analytics.orders",
			StagingTable: "_bruin_staging.orders_stg",
			Schema:       fallbackSwapSchema("id", "name", "_cdc_unchanged_cols"),
		}))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("failed rename restores the original target", func(t *testing.T) {
		t.Parallel()
		d, mock := newFallbackSwapMock(t)

		expectReplaceUnsupported(mock, `"id", "name", "email"`)
		expectColumns(mock, "_bruin_staging", "orders_stg", [2]string{"id", "varchar"}, [2]string{"name", "varchar"}, [2]string{"email", "varchar"})
		expectColumns(mock, "analytics", "orders", [2]string{"id", "varchar"}, [2]string{"name", "varchar"})
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE `+fallbackTargetFQN+` RENAME TO `) + oldTablePattern).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE ` + fallbackStagingFQN + ` RENAME TO ` + fallbackTargetFQN)).WillReturnError(errors.New("rename failed"))
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE `) + oldTablePattern + regexp.QuoteMeta(` RENAME TO `+fallbackTargetFQN)).WillReturnResult(sqlmock.NewResult(0, 0))

		err := d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:  "analytics.orders",
			StagingTable: "_bruin_staging.orders_stg",
			Schema:       fallbackSwapSchema("id", "name", "email"),
		})
		require.ErrorContains(t, err, "rename failed")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestAtomicRecoveryAccessDeniedOnlyBlocksCDCWork(t *testing.T) {
	t.Parallel()

	accessDenied := errors.New(`trino: query failed (200 OK): "Access Denied: Cannot select from table hive._bruin_staging.__ingestr_cdc_batch_journal"`)
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}

	t.Run("ordinary prepare proceeds", func(t *testing.T) {
		t.Parallel()
		d, mock := newFallbackSwapMock(t)
		d.atomicRecoveryEnabled = true

		mock.ExpectQuery(regexp.QuoteMeta(`FROM "hive"."_bruin_staging"."__ingestr_cdc_batch_journal"`)).WillReturnError(accessDenied)
		mock.ExpectExec(regexp.QuoteMeta(`CREATE SCHEMA IF NOT EXISTS "hive"."analytics"`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`CREATE TABLE IF NOT EXISTS "hive"."analytics"."orders"`)).WillReturnResult(sqlmock.NewResult(0, 0))

		require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "analytics.orders", Schema: tableSchema}))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("CDC prepare still fails", func(t *testing.T) {
		t.Parallel()
		d, mock := newFallbackSwapMock(t)
		d.atomicRecoveryEnabled = true

		mock.ExpectQuery(regexp.QuoteMeta(`FROM "hive"."_bruin_staging"."__ingestr_cdc_batch_journal"`)).WillReturnError(accessDenied)

		err := d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "analytics.orders", Schema: tableSchema, CDCMode: true})
		require.ErrorContains(t, err, "Access Denied")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("other recovery errors still fail ordinary loads", func(t *testing.T) {
		t.Parallel()
		d, mock := newFallbackSwapMock(t)
		d.atomicRecoveryEnabled = true

		mock.ExpectQuery(regexp.QuoteMeta(`FROM "hive"."_bruin_staging"."__ingestr_cdc_batch_journal"`)).WillReturnError(errors.New("connection reset"))

		err := d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "analytics.orders", Schema: tableSchema})
		require.ErrorContains(t, err, "connection reset")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
