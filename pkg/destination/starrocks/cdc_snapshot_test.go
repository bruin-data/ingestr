package starrocks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func snapshotWriteOptions(parallelism int) destination.WriteOptions {
	return destination.WriteOptions{
		Table: "db.events", Parallelism: parallelism, PrimaryKeys: []string{"id"}, CDCSnapshot: true,
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "value", DataType: schema.TypeString},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
			{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
		}},
	}
}

func snapshotLoadBatch(mem memory.Allocator, id int64, deleted bool, unchanged string) arrow.RecordBatch {
	builder := array.NewRecordBuilder(mem, snapshotWriteOptions(1).Schema.ToArrowSchema())
	defer builder.Release()
	builder.Field(0).(*array.Int64Builder).Append(id)
	builder.Field(1).(*array.StringBuilder).Append("snapshot")
	builder.Field(2).(*array.StringBuilder).Append("00000001/00000020")
	builder.Field(3).(*array.BooleanBuilder).Append(deleted)
	builder.Field(4).(*array.TimestampBuilder).Append(1_000_000)
	builder.Field(5).(*array.StringBuilder).Append(unchanged)
	return builder.NewRecordBatch()
}

func expectSnapshotPrimaryKey(mock sqlmock.Sqlmock, model string, keys ...string) {
	mock.ExpectQuery("SELECT TABLE_MODEL FROM information_schema.tables_config").WithArgs("db", "events").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_MODEL"}).AddRow(model))
	if model == "PRIMARY_KEYS" {
		rows := sqlmock.NewRows([]string{"COLUMN_NAME"})
		for _, key := range keys {
			rows.AddRow(key)
		}
		mock.ExpectQuery("SELECT COLUMN_NAME FROM information_schema.columns").WithArgs("db", "events").WillReturnRows(rows)
	}
}

func expectSnapshotColumns(mock sqlmock.Sqlmock, columns ...string) {
	rows := sqlmock.NewRows([]string{"COLUMN_NAME"})
	for _, column := range columns {
		rows.AddRow(column)
	}
	mock.ExpectQuery("SELECT COLUMN_NAME FROM information_schema.columns").WithArgs("db", "events").WillReturnRows(rows)
}

func TestCanWriteCDCSnapshotRequiresEveryTargetColumn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		columns []string
		want    bool
	}{
		{name: "full schema", columns: []string{"id", "value", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}, want: true},
		{name: "case insensitive", columns: []string{"ID", "VALUE", "_CDC_LSN", "_CDC_DELETED", "_CDC_SYNCED_AT"}, want: true},
		{name: "retained column", columns: []string{"id", "value", "removed", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}},
		{name: "staging metadata", columns: []string{"id", "value", "_cdc_unchanged_cols"}, want: true},
		{name: "missing target"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, mock := newVisibilityTestDestination(t)
			columns := tc.columns
			if tc.want {
				columns = append(append([]string(nil), columns...), cdcOrderColumn)
			}
			expectSnapshotColumns(mock, columns...)
			if tc.want {
				expectSnapshotSchemaChange(mock, "", "")
				expectSnapshotOrderColumn(mock, "largeint", cdcOrderRenderedExpression)
			}
			canWrite, err := destination.CanWriteDirectCDCSnapshot(t.Context(), d, "db.events", snapshotWriteOptions(1).Schema, "")
			require.NoError(t, err)
			require.Equal(t, tc.want, canWrite)
		})
	}
}

func TestCanWriteCDCSnapshotPropagatesMetadataError(t *testing.T) {
	d, mock := newVisibilityTestDestination(t)
	readErr := errors.New("metadata query failed")
	mock.ExpectQuery("SELECT COLUMN_NAME FROM information_schema.columns").WithArgs("db", "events").WillReturnError(readErr)
	canWrite, err := d.CanWriteCDCSnapshot(t.Context(), "db.events", snapshotWriteOptions(1).Schema, "")
	require.ErrorIs(t, err, readErr)
	require.False(t, canWrite)
}

func TestWriteCDCSnapshotRejectsRetainedColumns(t *testing.T) {
	d, mock := newVisibilityTestDestination(t)
	expectSnapshotPrimaryKey(mock, "PRIMARY_KEYS", "id")
	expectSnapshotColumns(mock, "id", "value", "retained")
	err := d.WriteParallel(t.Context(), nil, snapshotWriteOptions(1))
	require.ErrorContains(t, err, "requires every target column")
}

func TestStreamLoadConditionalHeadersSurviveRedirect(t *testing.T) {
	for _, condition := range []string{"", cdcOrderColumn} {
		t.Run(condition, func(t *testing.T) {
			body := `[{"id":1,"_cdc_lsn":"00000001/00000020"}]`
			check := func(r *http.Request) {
				require.Equal(t, condition, r.Header.Get("merge_condition"))
				require.Equal(t, "id,_cdc_lsn", r.Header.Get("columns"))
				require.Equal(t, "label", r.Header.Get("label"))
				require.Equal(t, "true", r.Header.Get("strict_mode"))
				require.Equal(t, "0", r.Header.Get("max_filter_ratio"))
				got, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, body, string(got))
			}
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				check(r)
				require.NoError(t, json.NewEncoder(w).Encode(streamLoadResponse{Status: "Success", NumberLoadedRows: 1, NumberTotalRows: 1}))
			}))
			defer backend.Close()
			frontend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				check(r)
				http.Redirect(w, r, backend.URL, http.StatusTemporaryRedirect)
			}))
			defer frontend.Close()
			require.NoError(t, newTestStreamLoader(t, frontend).load(t.Context(), "db", "events", "label", []byte(body), streamLoadOptions{
				columns: "id,_cdc_lsn", mergeCondition: condition,
			}))
		})
	}
}

func TestWriteCDCSnapshotLoadsConcurrentlyAndProjectsMetadata(t *testing.T) {
	for _, parallelism := range []int{1, 4, 0} {
		t.Run(strconv.Itoa(parallelism), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
				const batches = 16
				records := make(chan source.RecordBatchResult, batches)
				for id := range batches {
					records <- source.RecordBatchResult{Batch: snapshotLoadBatch(mem, int64(id), false, "[]")}
				}
				close(records)
				release := make(chan struct{})
				var active, requests atomic.Int64
				_, _, done := runStreamLoadTest(t, records, mem, snapshotWriteOptions(parallelism), func(r *http.Request) (*http.Response, error) {
					active.Add(1)
					defer active.Add(-1)
					requests.Add(1)
					if r.Header.Get("merge_condition") != cdcOrderColumn {
						return nil, errors.New("missing CDC merge condition")
					}
					if strings.Contains(r.Header.Get("columns"), destination.CDCUnchangedColsColumn) {
						return nil, errors.New("staging column in column mapping")
					}
					var rows []map[string]any
					if err := json.NewDecoder(r.Body).Decode(&rows); err != nil {
						return nil, err
					}
					if len(rows) != 1 || len(rows[0]) != 5 || rows[0][destination.CDCLSNColumn] != "00000001/00000020" || rows[0][destination.CDCDeletedColumn] != false {
						return nil, errors.New("snapshot row metadata changed")
					}
					if _, exists := rows[0][destination.CDCUnchangedColsColumn]; exists {
						return nil, errors.New("staging column in payload")
					}
					select {
					case <-release:
						return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"Status":"Success","NumberLoadedRows":1,"NumberTotalRows":1}`))}, nil
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
				})
				synctest.Wait()
				workers := parallelism
				if workers == 0 {
					workers = 4
				}
				require.EqualValues(t, workers, active.Load())
				// Each worker holds the request its loader is sending and the next one it encoded,
				// and the coordinator holds one more page.
				require.GreaterOrEqual(t, len(records), batches-2*workers-1)
				close(release)
				require.NoError(t, <-done)
				require.EqualValues(t, batches, requests.Load())
				mem.AssertSize(t, 0)
			})
		})
	}
}

func TestWriteCDCSnapshotFailureCancelsAndJoinsLoads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		records := make(chan source.RecordBatchResult, 2)
		for id := range 2 {
			records <- source.RecordBatchResult{Batch: snapshotLoadBatch(mem, int64(id), false, "[]")}
		}
		close(records)
		fail := make(chan struct{})
		loadErr := errors.New("load rejected")
		var requests, canceled atomic.Int64
		_, _, done := runStreamLoadTest(t, records, mem, snapshotWriteOptions(2), func(r *http.Request) (*http.Response, error) {
			if requests.Add(1) == 1 {
				<-r.Context().Done()
				canceled.Add(1)
				return nil, r.Context().Err()
			}
			<-fail
			return nil, loadErr
		})
		synctest.Wait()
		require.EqualValues(t, 2, requests.Load())
		close(fail)
		require.ErrorIs(t, <-done, loadErr)
		require.EqualValues(t, 1, canceled.Load())
		mem.AssertSize(t, 0)
	})
}

func TestWriteCDCSnapshotRejectsUnsafeInput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		deleted   bool
		unchanged string
		truncate  bool
		want      string
	}{
		{name: "tombstone", deleted: true, unchanged: "[]", want: "cannot contain tombstones"},
		{name: "TOAST", unchanged: `["value"]`, want: "cannot contain unchanged TOAST"},
		{name: "invalid marker", unchanged: `not-json`, want: "cannot contain unchanged TOAST"},
		{name: "null marker", unchanged: `null`, want: "cannot contain unchanged TOAST"},
		{name: "truncate", unchanged: "[]", truncate: true, want: "cannot contain TRUNCATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			records := make(chan source.RecordBatchResult, 1)
			records <- source.RecordBatchResult{Batch: snapshotLoadBatch(mem, 1, tc.deleted, tc.unchanged), Truncate: tc.truncate}
			close(records)
			_, _, done := runStreamLoadTest(t, records, mem, snapshotWriteOptions(1), func(*http.Request) (*http.Response, error) {
				return nil, errors.New("invalid snapshot reached Stream Load")
			})
			require.ErrorContains(t, <-done, tc.want)
			mem.AssertSize(t, 0)
		})
	}
}

func TestWriteCDCSnapshotValidatesPhysicalPrimaryKey(t *testing.T) {
	for _, tc := range []struct {
		model string
		keys  []string
	}{
		{model: "DUP_KEYS"},
		{model: "PRIMARY_KEYS", keys: []string{"value"}},
	} {
		t.Run(tc.model, func(t *testing.T) {
			d, mock := newVisibilityTestDestination(t)
			expectSnapshotPrimaryKey(mock, tc.model, tc.keys...)
			err := d.WriteParallel(t.Context(), nil, snapshotWriteOptions(4))
			require.ErrorContains(t, err, "requires PRIMARY KEY (id)")
		})
	}
}

func TestWriteCDCSnapshotRequiresCompleteSchema(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*destination.WriteOptions)
		want   string
	}{
		{name: "no schema", modify: func(o *destination.WriteOptions) { o.Schema = nil }, want: "requires a schema and primary keys"},
		{name: "no key", modify: func(o *destination.WriteOptions) { o.PrimaryKeys = nil }, want: "requires a schema and primary keys"},
		{name: "missing key", modify: func(o *destination.WriteOptions) { o.PrimaryKeys = []string{"absent"} }, want: "invalid primary key"},
		{name: "CDC key", modify: func(o *destination.WriteOptions) { o.PrimaryKeys = []string{destination.CDCLSNColumn} }, want: "invalid primary key"},
		{name: "staging", modify: func(o *destination.WriteOptions) { o.StagingTable = true }, want: "cannot write a staging table"},
		{name: "numeric LSN", modify: func(o *destination.WriteOptions) { o.Schema.Columns[2].DataType = schema.TypeInt64 }, want: "requires string _cdc_lsn"},
		{name: "missing timestamp", modify: func(o *destination.WriteOptions) { o.Schema.Columns = o.Schema.Columns[:4] }, want: "requires timestamp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := snapshotWriteOptions(1)
			tc.modify(&opts)
			err := NewStarRocksDestination().WriteParallel(t.Context(), nil, opts)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestPrepareCDCSnapshotRecordRejectsMissingFullImageColumns(t *testing.T) {
	for _, missing := range []int{0, 1, 2, 3, 4, 5} {
		t.Run(strconv.Itoa(missing), func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			record := snapshotLoadBatch(mem, 1, false, "[]")
			var fields []arrow.Field
			var columns []arrow.Array
			for i, field := range record.Schema().Fields() {
				if i != missing {
					fields = append(fields, field)
					columns = append(columns, record.Column(i))
				}
			}
			partial := array.NewRecordBatch(arrow.NewSchema(fields, nil), columns, record.NumRows())
			record.Release()
			projected, err := prepareCDCSnapshotRecord(partial, snapshotWriteOptions(1))
			partial.Release()
			if missing == 5 {
				require.NoError(t, err, "the staging-only marker may already be projected out")
				require.EqualValues(t, 5, projected.NumCols())
				require.Equal(t, "snapshot", projected.Column(1).(*array.String).Value(0))
				projected.Release()
			} else {
				require.ErrorContains(t, err, "batch is missing column")
			}
			mem.AssertSize(t, 0)
		})
	}
}

func TestWriteCDCSnapshotWaitsForVisibilityAndChecksIncarnation(t *testing.T) {
	for _, finalID := range []string{"100", "101"} {
		t.Run(finalID, func(t *testing.T) {
			d, mock := newVisibilityTestDestination(t)
			expectSnapshotPrimaryKey(mock, "PRIMARY_KEYS", "id")
			expectSnapshotColumns(mock, append(snapshotWriteOptions(1).Schema.ColumnNames(), cdcOrderColumn)...)
			expectSnapshotIncarnation(mock, "100")
			expectSnapshotSchemaChange(mock, "", "")
			expectSnapshotOrderColumn(mock, "largeint", cdcOrderRenderedExpression)
			expectSnapshotIncarnation(mock, finalID)
			waited := false
			d.loader = &streamLoader{
				endpoint: "http://starrocks", client: &http.Client{Transport: parallelLoadTransport(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"Status":"OK","TxnId":42,"NumberLoadedRows":1,"NumberTotalRows":1,"NumberFilteredRows":0,"NumberUnselectedRows":0}`))}, nil
				})},
				waitForPublish: func(ctx context.Context, database string, id int64) error {
					require.Equal(t, "db", database)
					require.EqualValues(t, 42, id)
					waited = true
					return nil
				},
			}
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			records := make(chan source.RecordBatchResult, 1)
			records <- source.RecordBatchResult{Batch: snapshotLoadBatch(mem, 1, false, "[]")}
			close(records)
			opts := snapshotWriteOptions(1)
			opts.CDCExpectedIncarnation = destination.CDCTargetKey("db", "events", "100")
			err := d.WriteParallel(t.Context(), records, opts)
			if finalID == "100" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "physical incarnation changed")
			}
			require.True(t, waited)
			mem.AssertSize(t, 0)
		})
	}
}
