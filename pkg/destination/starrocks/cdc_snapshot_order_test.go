package starrocks

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func expectSnapshotIncarnation(mock sqlmock.Sqlmock, id string) {
	mock.ExpectQuery("SELECT TABLE_ID FROM information_schema.tables_config").WithArgs("db", "events").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_ID"}).AddRow(id))
}

func expectSnapshotSchemaChange(mock sqlmock.Sqlmock, job, state string) {
	rows := sqlmock.NewRows([]string{"JobId", "State", "Msg"})
	if job != "" {
		rows.AddRow(job, state, "schema change failed")
	}
	mock.ExpectQuery("SHOW ALTER TABLE COLUMN").WillReturnRows(rows)
}

func expectSnapshotOrderColumn(mock sqlmock.Sqlmock, kind string, expression any) {
	mock.ExpectQuery("SHOW FULL COLUMNS FROM").
		WillReturnRows(sqlmock.NewRows([]string{"Field", "Type"}).AddRow(cdcOrderColumn, kind))
	mock.ExpectQuery("SELECT GENERATION_EXPRESSION FROM information_schema.columns").WithArgs("db", "events", cdcOrderColumn).
		WillReturnRows(sqlmock.NewRows([]string{"GENERATION_EXPRESSION"}).AddRow(expression))
}

func TestCDCOrderColumnVersionAndEngineSupport(t *testing.T) {
	for _, tc := range []struct {
		version, engine string
		want            bool
	}{
		{"2.5.0", "OLAP", false},
		{"3.0.9", "OLAP", false},
		{"3.1.0", "OLAP", true},
		{"3.3.22-753696f", "OLAP", true},
		{"3.4.9", "CLOUD_NATIVE", false},
		{"3.5.0", "CLOUD_NATIVE", true},
		{"4.0.0", "CLOUD_NATIVE", true},
		{"3.3.22", "UNKNOWN", false},
		{"unknown", "OLAP", false},
	} {
		t.Run(tc.version+"/"+tc.engine, func(t *testing.T) {
			require.Equal(t, tc.want, supportsCDCOrderColumn(tc.version, tc.engine))
		})
	}
}

func TestCDCOrderColumnRejectsConflictingMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		expression any
		want       bool
	}{
		{"declared expression", "largeint", cdcOrderExpression, true},
		{"rendered expression", "largeint", cdcOrderRenderedExpression, true},
		{"quoted expression", "LARGEINT", strings.ReplaceAll(cdcOrderRenderedExpression, "_cdc_lsn", "`_cdc_lsn`"), true},
		{"ordinary column", "largeint", "", false},
		{"NULL expression", "largeint", nil, false},
		{"narrow integer", "bigint", cdcOrderExpression, false},
		{"incorrect sequence", "largeint", strings.ReplaceAll(cdcOrderExpression, ",19,16)", ",18,16)"), false},
		{"incorrect arithmetic", "largeint", strings.ReplaceAll(cdcOrderExpression, ")-CAST(", ")+CAST("), false},
		{"space in numeric literal", "largeint", strings.ReplaceAll(cdcOrderExpression, "9223372036854775808", "922337203685477580 8"), false},
		{"backtick in numeric literal", "largeint", strings.ReplaceAll(cdcOrderExpression, "9223372036854775808", "922337203685477580`8"), false},
		{"space in identifier", "largeint", strings.ReplaceAll(cdcOrderExpression, "_cdc_lsn", "`_cdc_ lsn`"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, mock := newVisibilityTestDestination(t)
			expectSnapshotOrderColumn(mock, tc.kind, tc.expression)
			err := d.validateCDCSnapshotOrderColumn(t.Context(), "db.events")
			if tc.want {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "conflicting reserved CDC column")
			}
		})
	}
}

func TestCDCOrderColumnAddsFencedGeneratedColumn(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "DDL failure"}[failure], func(t *testing.T) {
			d, mock := newVisibilityTestDestination(t)
			expectSnapshotIncarnation(mock, "100")
			expectSnapshotSchemaChange(mock, "5", "FINISHED")
			mock.ExpectQuery(regexp.QuoteMeta("SELECT current_version()")).WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("3.3.22-753696f"))
			mock.ExpectQuery("SELECT TABLE_ENGINE FROM information_schema.tables_config").WithArgs("db", "events").WillReturnRows(sqlmock.NewRows([]string{"engine"}).AddRow("OLAP"))
			expectSnapshotIncarnation(mock, "100")
			alter := "ALTER TABLE `db`.`events` ADD COLUMN `" + cdcOrderColumn + "` LARGEINT AS " + cdcOrderExpression
			ddl := mock.ExpectExec(regexp.QuoteMeta(alter))
			ddlErr := errors.New("ALTER privilege denied")
			if failure {
				ddl.WillReturnError(ddlErr)
			} else {
				ddl.WillReturnResult(sqlmock.NewResult(0, 0))
				expectSnapshotSchemaChange(mock, "6", "FINISHED")
				expectSnapshotIncarnation(mock, "100")
				expectSnapshotOrderColumn(mock, "largeint", cdcOrderRenderedExpression)
			}
			ready, err := d.ensureCDCSnapshotOrder(t.Context(), "db.events", false, destination.CDCTargetKey("db", "events", "100"))
			if failure {
				require.ErrorIs(t, err, ddlErr)
				require.False(t, ready)
			} else {
				require.NoError(t, err)
				require.True(t, ready)
			}
		})
	}
}

func TestCDCOrderColumnResumesPendingSchemaChange(t *testing.T) {
	for _, state := range []string{"FINISHED", "CANCELLED"} {
		t.Run(state, func(t *testing.T) {
			d, mock := newVisibilityTestDestination(t)
			expectSnapshotIncarnation(mock, "100")
			expectSnapshotSchemaChange(mock, "6", "RUNNING")
			expectSnapshotSchemaChange(mock, "6", state)
			if state == "FINISHED" {
				expectSnapshotIncarnation(mock, "100")
				mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM information_schema.columns")).WithArgs("db", "events", cdcOrderColumn).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
				expectSnapshotOrderColumn(mock, "largeint", cdcOrderRenderedExpression)
			}
			ready, err := d.ensureCDCSnapshotOrder(t.Context(), "db.events", false, destination.CDCTargetKey("db", "events", "100"))
			if state == "FINISHED" {
				require.NoError(t, err)
				require.True(t, ready)
			} else {
				require.ErrorContains(t, err, "schema change cancelled")
				require.False(t, ready)
			}
		})
	}
}

func TestCDCOrderColumnDoesNotAlterReplacedTarget(t *testing.T) {
	d, mock := newVisibilityTestDestination(t)
	expectSnapshotIncarnation(mock, "101")
	ready, err := d.ensureCDCSnapshotOrder(t.Context(), "db.events", false, destination.CDCTargetKey("db", "events", "100"))
	require.ErrorContains(t, err, "physical incarnation changed")
	require.False(t, ready)
}

func TestCDCSnapshotRejectsReservedSourceColumn(t *testing.T) {
	d := NewStarRocksDestination()
	sourceSchema := snapshotWriteOptions(1).Schema
	sourceSchema.Columns = append(sourceSchema.Columns, schema.Column{Name: cdcOrderColumn, DataType: schema.TypeString})
	ready, err := d.CanWriteCDCSnapshot(t.Context(), "db.events", sourceSchema, "")
	require.ErrorContains(t, err, "reserved for CDC ordering")
	require.False(t, ready)
}

func TestPrepareCDCSnapshotRecordRejectsNoncanonicalPositions(t *testing.T) {
	for _, position := range []string{"", "1/20", "00000001/0000002a", "00000001/0000002Z", "00000001/00000020/0000000000000001"} {
		t.Run(position, func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			record := snapshotLoadBatch(mem, 1, false, "[]")
			builder := array.NewStringBuilder(mem)
			builder.Append(position)
			lsns := builder.NewStringArray()
			builder.Release()
			invalid, err := record.SetColumn(2, lsns)
			require.NoError(t, err)
			lsns.Release()
			record.Release()
			_, err = prepareCDCSnapshotRecord(invalid, snapshotWriteOptions(1))
			require.ErrorContains(t, err, "requires canonical PostgreSQL")
			invalid.Release()
			mem.AssertSize(t, 0)
		})
	}
}
