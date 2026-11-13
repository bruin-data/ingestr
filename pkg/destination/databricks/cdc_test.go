package databricks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/databricks/databricks-sdk-go"
	dbsql "github.com/databricks/databricks-sdk-go/service/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockStatementHandler func(stmt string, req dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error)

func newTestDatabricksDestination(t *testing.T, handler mockStatementHandler) (*DatabricksDestination, *[]string) {
	t.Helper()
	var (
		mu         sync.Mutex
		statements []string
		chunks     = map[string][][]string{}
		nextID     atomic.Int64
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/2.0/sql/statements":
			var req dbsql.ExecuteStatementRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			statements = append(statements, req.Statement)
			mu.Unlock()

			resp, err := handler(req.Statement, req)
			if err != nil {
				resp = dbsql.StatementResponse{
					StatementId: fmt.Sprintf("stmt-%d", nextID.Add(1)),
					Status: &dbsql.StatementStatus{
						State: dbsql.StatementStateFailed,
						Error: &dbsql.ServiceError{Message: err.Error()},
					},
				}
			} else {
				if resp.StatementId == "" {
					resp.StatementId = fmt.Sprintf("stmt-%d", nextID.Add(1))
				}
				if resp.Status == nil {
					resp.Status = &dbsql.StatementStatus{State: dbsql.StatementStateSucceeded}
				}
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/2.0/sql/statements/") && strings.Contains(r.URL.Path, "/result/chunks/"):
			trimmed := strings.TrimPrefix(r.URL.Path, "/api/2.0/sql/statements/")
			parts := strings.Split(trimmed, "/result/chunks/")
			if len(parts) != 2 {
				http.Error(w, "invalid chunk path", http.StatusBadRequest)
				return
			}
			chunkIdx, err := strconv.Atoi(parts[1])
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			rows := chunks[parts[0]]
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(dbsql.ResultData{
				ChunkIndex: chunkIdx,
				DataArray:  rows,
			})
		default:
			http.Error(w, fmt.Sprintf("unexpected request: %s %s", r.Method, r.URL.Path), http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)

	client, err := databricks.NewWorkspaceClient(&databricks.Config{
		Host:     server.URL,
		Token:    "test-token",
		AuthType: "pat",
	})
	require.NoError(t, err)

	dest := &DatabricksDestination{
		client:     client,
		catalog:    "main",
		schemaName: "analytics",
		httpPath:   "/sql/1.0/warehouses/test",
	}
	return dest, &statements
}

func makeQueryResponse(cols []string, rows [][]string) dbsql.StatementResponse {
	colInfos := make([]dbsql.ColumnInfo, len(cols))
	for i, c := range cols {
		colInfos[i] = dbsql.ColumnInfo{Name: c, Position: i}
	}
	return dbsql.StatementResponse{
		Status: &dbsql.StatementStatus{State: dbsql.StatementStateSucceeded},
		Manifest: &dbsql.ResultManifest{
			Format: dbsql.FormatJsonArray,
			Schema: &dbsql.ResultSchema{
				ColumnCount: len(cols),
				Columns:     colInfos,
			},
			TotalRowCount: int64(len(rows)),
		},
		Result: &dbsql.ResultData{
			RowCount:  int64(len(rows)),
			DataArray: rows,
		},
	}
}

func TestDatabricksCDCCapabilitiesAndInterfaces(t *testing.T) {
	t.Parallel()

	dest := NewDatabricksDestination()
	assert.True(t, dest.SupportsCDCMerge())
	assert.True(t, dest.SupportsCDCHardDelete())
	assert.True(t, dest.SupportsCDCUnchangedCols())
	assert.True(t, dest.RequiresSerializedCDCRuns())
	assert.True(t, dest.SupportsCDCConditionalMerge())
	assert.True(t, dest.SupportsCDCConditionalSwap())
	assert.True(t, destination.SupportsCDCHardDelete(dest))
	assert.Equal(t, "main", dest.ManagedCDCStateCatalog())
	assert.Equal(t, destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingManagedSchema,
		DefaultManagedSchema: "ingestr_staging",
	}, dest.ReplaceStagingPolicy())
	assert.Equal(t, destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingManagedSchema,
		DefaultManagedSchema: "ingestr_staging",
	}, dest.ManagedStagingPolicy())

	var _ destination.CDCStateReader = dest
	var _ destination.CDCStateFenceReader = dest
	var _ destination.CDCStatePruner = dest
	var _ destination.CDCTargetClaimer = dest
	var _ destination.CDCTargetIdentityProvider = dest
	var _ destination.CDCTargetIncarnationProvider = dest
	var _ destination.CDCTruncateCapable = dest
	var _ destination.CDCResumeProvider = dest
	var _ destination.ManagedCDCStateCatalogProvider = dest
	var _ destination.StagingTableLister = dest
	var _ destination.ManagedCDCRunLeaser = dest
	var _ destination.CDCConditionalMergeCapable = dest
	var _ destination.CDCConditionalTruncater = dest
	var _ destination.CDCMultiTableAtomicMerger = dest
	var _ destination.CDCLateTargetClaimPreparer = dest
	var _ destination.CDCConditionalSwapCapable = dest
	var _ destination.CDCConditionalSwapPlanner = dest
}

func TestPrepareAndTableRefRespectSchemaAndDropFirst(t *testing.T) {
	dest, stmts := newTestDatabricksDestination(t, func(stmt string, _ dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error) {
		return dbsql.StatementResponse{}, nil
	})

	err := dest.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:     "analytics.orders",
		DropFirst: true,
		Schema: &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
				{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			},
		},
	})
	require.NoError(t, err)

	joined := strings.Join(*stmts, "\n")
	assert.Contains(t, joined, "DROP TABLE IF EXISTS `main`.`analytics`.`orders`")
	assert.Contains(t, joined, "CREATE TABLE IF NOT EXISTS `main`.`analytics`.`orders`")
	assert.Contains(t, joined, "CREATE VOLUME IF NOT EXISTS `main`.`ingestr_staging`.`files`")

	*stmts = nil
	require.NoError(t, dest.DropTable(context.Background(), "other_cat.scratch.temp_orders"))
	assert.Equal(t, []string{"DROP TABLE IF EXISTS `other_cat`.`scratch`.`temp_orders`"}, *stmts)

	*stmts = nil
	require.NoError(t, dest.InsertFromStaging(context.Background(), destination.InsertFromStagingOptions{
		StagingTable: "ingestr_staging.stg_orders",
		TargetTable:  "analytics.orders",
		Columns:      []string{"id", "name", destination.CDCUnchangedColsColumn},
	}))
	assert.Equal(t, []string{
		"INSERT INTO `main`.`analytics`.`orders` (`id`, `name`) SELECT `id`, `name` FROM `main`.`ingestr_staging`.`stg_orders`",
	}, *stmts)
}

func TestBuildCDCMergeSQL_SoftDeleteWithoutUnchangedCols(t *testing.T) {
	t.Parallel()

	dest := &DatabricksDestination{catalog: "main", schemaName: "analytics"}
	sql := dest.buildCDCMergeSQL(destination.MergeOptions{
		StagingTable: "ingestr_staging._cdc_stg_orders",
		TargetTable:  "analytics.orders",
		PrimaryKeys:  []string{"id"},
		Columns: []string{
			"id", "amount",
			destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn,
		},
	})

	assert.Contains(t, sql, "MERGE INTO `main`.`analytics`.`orders` AS target")
	assert.Contains(t, sql, "FROM `main`.`ingestr_staging`.`_cdc_stg_orders`")
	assert.Contains(t, sql, "ORDER BY `_cdc_lsn` DESC, `_cdc_deleted` DESC")
	assert.Contains(t, sql, "WHERE `_cdc_deleted` = false")
	assert.Contains(t, sql, "act.`_cdc_lsn` IS NOT NULL AS `__ingestr_has_active`")
	assert.Contains(t, sql, "act.`_cdc_lsn` AS `__ingestr_active_lsn`")
	assert.Contains(t, sql, "WHEN MATCHED AND (target.`_cdc_lsn` IS NULL OR source.`_cdc_lsn` > target.`_cdc_lsn` OR (source.`_cdc_lsn` = target.`_cdc_lsn` AND source.`_cdc_deleted` = true AND COALESCE(target.`_cdc_deleted`, false) = false)) AND (source.`_cdc_deleted` = false OR (source.`__ingestr_has_active` AND (target.`_cdc_lsn` IS NULL OR source.`__ingestr_active_lsn` >= target.`_cdc_lsn`))) THEN\n  UPDATE SET target.`amount` = source.`amount`, target.`_cdc_lsn` = source.`_cdc_lsn`, target.`_cdc_deleted` = source.`_cdc_deleted`, target.`_cdc_synced_at` = source.`_cdc_synced_at`")
	assert.Contains(t, sql, "WHEN MATCHED AND (target.`_cdc_lsn` IS NULL OR source.`_cdc_lsn` > target.`_cdc_lsn` OR (source.`_cdc_lsn` = target.`_cdc_lsn` AND source.`_cdc_deleted` = true AND COALESCE(target.`_cdc_deleted`, false) = false)) AND source.`_cdc_deleted` = true THEN\n  UPDATE SET target.`_cdc_deleted` = true, target.`_cdc_lsn` = source.`_cdc_lsn`, target.`_cdc_synced_at` = source.`_cdc_synced_at`")
	assert.Contains(t, sql, "WHEN NOT MATCHED THEN\n  INSERT (`id`, `amount`, `_cdc_lsn`, `_cdc_deleted`, `_cdc_synced_at`)")
	assert.NotContains(t, sql, "FROM_JSON")
}

func TestBuildCDCMergeSQL_SoftDeleteWithUnchangedColsAndPredicate(t *testing.T) {
	t.Parallel()

	dest := &DatabricksDestination{catalog: "main", schemaName: "analytics"}
	sql := dest.buildCDCMergeSQL(destination.MergeOptions{
		StagingTable:         "ingestr_staging._cdc_stg_users",
		TargetTable:          "analytics.users",
		PrimaryKeys:          []string{"id"},
		IncrementalPredicate: "target.`_cdc_synced_at` >= '2026-01-01'",
		Columns: []string{
			"id", "Bio",
			destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn,
		},
	})

	assert.Contains(t, sql, "MAX_BY(`Bio`, CASE WHEN NOT COALESCE(ARRAY_CONTAINS(FROM_JSON(LOWER(COALESCE(`_cdc_unchanged_cols`, '[]')), 'ARRAY<STRING>'), 'bio'), TRUE) THEN `_cdc_lsn` END) AS `Bio`")
	assert.Contains(t, sql, "(MAX(CASE WHEN NOT COALESCE(ARRAY_CONTAINS(FROM_JSON(LOWER(COALESCE(`_cdc_unchanged_cols`, '[]')), 'ARRAY<STRING>'), 'bio'), TRUE) THEN 1 ELSE 0 END) = 0) AS `__ingestr_unchanged_bio`")
	assert.Contains(t, sql, "target.`Bio` = CASE WHEN source.`__ingestr_unchanged_bio` THEN target.`Bio` ELSE source.`Bio` END")
	assert.Contains(t, sql, "(target.`_cdc_synced_at` >= '2026-01-01') AND (target.`_cdc_lsn` IS NULL")
	assert.NotContains(t, sql, "INSERT (`id`, `Bio`, `_cdc_lsn`, `_cdc_deleted`, `_cdc_synced_at`, `_cdc_unchanged_cols`)")
}

func TestBuildCDCMergeSQL_HardDeleteAndSnapshotOverride(t *testing.T) {
	t.Parallel()

	dest := &DatabricksDestination{catalog: "main", schemaName: "analytics"}
	hardSQL := dest.buildCDCMergeSQL(destination.MergeOptions{
		StagingTable:  "ingestr_staging._cdc_stg_orders",
		TargetTable:   "analytics.orders",
		PrimaryKeys:   []string{"id"},
		CDCHardDelete: true,
		Columns: []string{
			"id", "amount",
			destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn,
		},
	})
	assert.Contains(t, hardSQL, "WHEN MATCHED AND source.`_cdc_deleted` = true AND (target.`_cdc_lsn` IS NULL OR source.`_cdc_lsn` >= target.`_cdc_lsn`) THEN\n  DELETE")
	assert.Contains(t, hardSQL, "WHEN MATCHED AND source.`_cdc_deleted` = false AND (target.`_cdc_lsn` IS NULL OR source.`_cdc_lsn` > target.`_cdc_lsn`) THEN\n  UPDATE SET")
	assert.Contains(t, hardSQL, "WHEN NOT MATCHED AND source.`_cdc_deleted` = false THEN\n  INSERT")

	hardUnchangedSQL := dest.buildCDCMergeSQL(destination.MergeOptions{
		StagingTable:  "ingestr_staging._cdc_stg_users",
		TargetTable:   "analytics.users",
		PrimaryKeys:   []string{"id"},
		CDCHardDelete: true,
		Columns: []string{
			"id", "bio",
			destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn,
		},
	})
	assert.Contains(t, hardUnchangedSQL, "WHEN MATCHED AND source.`_cdc_deleted` = true AND (target.`_cdc_lsn` IS NULL OR source.`_cdc_lsn` >= target.`_cdc_lsn`) THEN\n  DELETE")
	assert.Contains(t, hardUnchangedSQL, "target.`bio` = CASE WHEN source.`__ingestr_unchanged_bio` THEN target.`bio` ELSE source.`bio` END")

	snapshotSQL := dest.buildCDCMergeSQL(destination.MergeOptions{
		StagingTable:  "ingestr_staging._cdc_stg_orders",
		TargetTable:   "analytics.orders",
		PrimaryKeys:   []string{"id"},
		CDCHardDelete: true,
		CDCSnapshot:   true,
		Columns: []string{
			"id", "amount",
			destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn,
		},
	})
	assert.NotContains(t, snapshotSQL, "\n  DELETE\n")
	assert.Contains(t, snapshotSQL, "WHEN NOT MATCHED THEN\n  INSERT")
}

func TestCDCStateOperationsAndGetMaxCDCLSN(t *testing.T) {
	ctx := context.Background()
	tableID := "uuid-table-1"
	dest, stmts := newTestDatabricksDestination(t, func(stmt string, _ dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error) {
		switch {
		case strings.HasPrefix(stmt, "DESCRIBE DETAIL `main`.`analytics`.`missing`"):
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
		case strings.HasPrefix(stmt, "DESCRIBE DETAIL `main`.`analytics`.`orders`"):
			return makeQueryResponse([]string{"format", "id", "createdAt"}, [][]string{{"delta", tableID, "2026-01-01T00:00:00Z"}}), nil
		case strings.HasPrefix(stmt, "DESCRIBE TABLE `main`.`analytics`.`orders`"):
			return makeQueryResponse([]string{"col_name", "data_type", "comment"}, [][]string{
				{"id", "bigint", ""},
				{"_cdc_lsn", "string", ""},
				{"# Partition Information", "", ""},
				{"# col_name", "data_type", "comment"},
			}), nil
		case strings.HasPrefix(stmt, "DESCRIBE TABLE `main`.`analytics`.`plain`"):
			return makeQueryResponse([]string{"col_name", "data_type", "comment"}, [][]string{{"id", "bigint", ""}}), nil
		case strings.HasPrefix(stmt, "DESCRIBE DETAIL `main`.`analytics`.`plain`"):
			return makeQueryResponse([]string{"format", "id"}, [][]string{{"delta", "uuid-plain"}}), nil
		case strings.HasPrefix(stmt, "SELECT MAX(`_cdc_lsn`) FROM `main`.`analytics`.`orders`"):
			return makeQueryResponse([]string{"max(_cdc_lsn)"}, [][]string{{"00000001/00000200"}}), nil
		case strings.Contains(stmt, "FROM `main`.`ingestr_staging`.`cdc_state`") && strings.Contains(stmt, "`state_kind` = 'run'"):
			return makeQueryResponse([]string{"event_id", "state_generation"}, [][]string{
				{"evt-2", "3"},
				{"evt-3", "3"},
			}), nil
		case strings.Contains(stmt, "FROM `main`.`ingestr_staging`.`cdc_state`"):
			return makeQueryResponse(
				[]string{"event_id", "source_table", "destination_table", "state_kind", "state_generation", "state_status", "_cdc_lsn"},
				[][]string{{"evt-1", "public.orders", "analytics.orders", "checkpoint", "3", "completed", "00000001/00000200"}},
			), nil
		case strings.HasPrefix(stmt, "DELETE FROM `main`.`ingestr_staging`.`cdc_state`"):
			return dbsql.StatementResponse{}, nil
		case strings.HasPrefix(stmt, "SHOW TABLES IN `main`.`ingestr_staging`"):
			return makeQueryResponse([]string{"database", "tableName", "isTemporary"}, [][]string{
				{"ingestr_staging", "_cdc_stg_1", "false"},
				{"ingestr_staging", "other_tbl", "false"},
				{"ingestr_staging", "_cdc_stg_tmp", "true"},
			}), nil
		default:
			return dbsql.StatementResponse{}, nil
		}
	})

	canonical, err := dest.CanonicalCDCTarget(ctx, "analytics.orders")
	require.NoError(t, err)
	assert.Equal(t, destination.CDCTargetKey("main", "analytics", "orders"), canonical)

	inc, exists, err := dest.CDCTargetIncarnation(ctx, "analytics.orders")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, destination.CDCTargetKey("main", "analytics", "orders", tableID), inc)

	_, exists, err = dest.CDCTargetIncarnation(ctx, "analytics.missing")
	require.NoError(t, err)
	assert.False(t, exists)

	maxLSN, err := dest.GetMaxCDCLSN(ctx, "analytics.orders")
	require.NoError(t, err)
	assert.Equal(t, "00000001/00000200", maxLSN)

	plainLSN, err := dest.GetMaxCDCLSN(ctx, "analytics.plain")
	require.NoError(t, err)
	assert.Equal(t, "", plainLSN)

	missingLSN, err := dest.GetMaxCDCLSN(ctx, "analytics.missing")
	require.NoError(t, err)
	assert.Equal(t, "", missingLSN)

	entries, err := dest.LoadCDCState(ctx, "ingestr_staging.cdc_state", "conn-1")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, destination.CDCStateEntry{
		EventID:          "evt-1",
		SourceTable:      "public.orders",
		DestinationTable: "analytics.orders",
		StateKind:        "checkpoint",
		Generation:       3,
		Status:           "completed",
		Position:         "00000001/00000200",
	}, entries[0])

	fence, err := dest.LoadCDCStateFence(ctx, "ingestr_staging.cdc_state", "conn-1")
	require.NoError(t, err)
	assert.Equal(t, destination.CDCStateFence{
		Generation:  3,
		RunEventIDs: []string{"evt-2", "evt-3"},
	}, fence)

	require.NoError(t, dest.DeleteCDCStateEvents(ctx, "ingestr_staging.cdc_state", "conn-1", []string{"evt-1", "evt-2"}))
	assert.Contains(t, (*stmts)[len(*stmts)-1], "DELETE FROM `main`.`ingestr_staging`.`cdc_state` WHERE `connector_id` = 'conn-1' AND `event_id` IN ('evt-1', 'evt-2')")

	staged, err := dest.ListStagingTables(ctx, "ingestr_staging", "_cdc_stg_")
	require.NoError(t, err)
	assert.Equal(t, []string{"_cdc_stg_1"}, staged)

	tblSchema, err := dest.GetTableSchema(ctx, "analytics.orders")
	require.NoError(t, err)
	require.NotNil(t, tblSchema)
	require.Len(t, tblSchema.Columns, 2)
	assert.Equal(t, schema.TypeInt64, tblSchema.Columns[0].DataType)
	assert.Equal(t, schema.TypeString, tblSchema.Columns[1].DataType)
}

func TestClaimCDCTargetConflictAndClaimAndPrepareEmptyCDCTarget(t *testing.T) {
	ctx := context.Background()
	claimOwner := "other-connector"
	var createdTarget bool
	var rolledBackView, rolledBackRow bool

	dest, stmts := newTestDatabricksDestination(t, func(stmt string, _ dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error) {
		switch {
		case strings.HasPrefix(stmt, "CREATE VIEW IF NOT EXISTS"):
			if claimOwner == "" {
				idx := strings.Index(stmt, "SELECT '")
				rest := stmt[idx+len("SELECT '"):]
				end := strings.Index(rest, "' AS owner_id")
				claimOwner = rest[:end]
			}
			return dbsql.StatementResponse{}, nil
		case strings.HasPrefix(stmt, "SELECT owner_id FROM"):
			if claimOwner == "" {
				return dbsql.StatementResponse{}, errors.New("TABLE_OR_VIEW_NOT_FOUND")
			}
			return makeQueryResponse([]string{"owner_id"}, [][]string{{claimOwner}}), nil
		case strings.HasPrefix(stmt, "DESCRIBE DETAIL `main`.`analytics`.`late_orders`"):
			if !createdTarget {
				return dbsql.StatementResponse{}, errors.New("TABLE_OR_VIEW_NOT_FOUND")
			}
			return makeQueryResponse([]string{"id"}, [][]string{{"uuid-late-1"}}), nil
		case strings.HasPrefix(stmt, "CREATE TABLE `main`.`analytics`.`late_orders`"):
			if strings.Contains(stmt, "fail_col") {
				return dbsql.StatementResponse{}, errors.New("simulated create failure")
			}
			createdTarget = true
			return dbsql.StatementResponse{}, nil
		case strings.HasPrefix(stmt, "DROP VIEW IF EXISTS"):
			claimOwner = ""
			rolledBackView = true
			return dbsql.StatementResponse{}, nil
		case strings.HasPrefix(stmt, "DELETE FROM `main`.`ingestr_staging`.`cdc_targets`"):
			rolledBackRow = true
			return dbsql.StatementResponse{}, nil
		default:
			return dbsql.StatementResponse{}, nil
		}
	})

	err := dest.ClaimCDCTarget(ctx, "ingestr_staging.cdc_targets", destination.CDCTargetClaim{
		DestinationTable: "analytics.orders",
		ConnectorID:      "my-connector",
		SourceTable:      "public.orders",
	})
	require.ErrorContains(t, err, "already claimed")

	claimOwner = ""
	_, err = dest.ClaimAndPrepareEmptyCDCTarget(
		ctx,
		"ingestr_staging.cdc_targets",
		destination.CDCTargetClaim{
			DestinationTable: "analytics.late_orders",
			ConnectorID:      "my-connector",
			SourceTable:      "public.orders",
		},
		destination.PrepareOptions{
			Table: "analytics.late_orders",
			Schema: &schema.TableSchema{
				Columns: []schema.Column{{Name: "fail_col", DataType: schema.TypeInt64}},
			},
		},
	)
	require.ErrorContains(t, err, "simulated create failure")
	assert.True(t, rolledBackView)
	assert.True(t, rolledBackRow)

	claimOwner = destination.CDCTargetOwnerID("my-connector", "public.orders")
	inc, err := dest.ClaimAndPrepareEmptyCDCTarget(
		ctx,
		"ingestr_staging.cdc_targets",
		destination.CDCTargetClaim{
			DestinationTable: "analytics.late_orders",
			ConnectorID:      "my-connector",
			SourceTable:      "public.orders",
		},
		destination.PrepareOptions{
			Table: "analytics.late_orders",
			Schema: &schema.TableSchema{
				Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}},
			},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, destination.CDCTargetKey("main", "analytics", "late_orders", "uuid-late-1"), inc)
	assert.Contains(t, strings.Join(*stmts, "\n"), "CREATE TABLE `main`.`analytics`.`late_orders` (")
}

func TestConditionalFencingAndSwapIncarnations(t *testing.T) {
	ctx := context.Background()
	targetID := "uuid-target-1"
	stagingID := "uuid-staging-1"
	copyID := "uuid-copy-1"
	var copyTable string
	var droppedCopy bool

	dest, stmts := newTestDatabricksDestination(t, func(stmt string, _ dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error) {
		switch {
		case strings.HasPrefix(stmt, "DESCRIBE DETAIL `main`.`analytics`.`orders`"):
			return makeQueryResponse([]string{"id"}, [][]string{{targetID}}), nil
		case strings.HasPrefix(stmt, "DESCRIBE DETAIL `main`.`ingestr_staging`.`orders_stg`"):
			return makeQueryResponse([]string{"id"}, [][]string{{stagingID}}), nil
		case strings.HasPrefix(stmt, "CREATE TABLE `main`.`analytics`.`orders_swap_"):
			parts := strings.Split(stmt, " ")
			copyTable = parts[2]
			return dbsql.StatementResponse{}, nil
		case copyTable != "" && strings.HasPrefix(stmt, "DESCRIBE DETAIL "+copyTable):
			return makeQueryResponse([]string{"id"}, [][]string{{copyID}}), nil
		case copyTable != "" && strings.HasPrefix(stmt, "DROP TABLE IF EXISTS "+copyTable):
			droppedCopy = true
			return dbsql.StatementResponse{}, nil
		default:
			return dbsql.StatementResponse{}, nil
		}
	})

	expectedTarget := destination.CDCTargetKey("main", "analytics", "orders", targetID)
	require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, "analytics.orders", expectedTarget))
	err := dest.TruncateCDCTableIfIncarnation(ctx, "analytics.orders", destination.CDCTargetKey("main", "analytics", "orders", "wrong-id"))
	require.ErrorContains(t, err, "physical incarnation changed")

	cancelledCtx, cancel := context.WithCancelCause(ctx)
	cancel(errors.Join(source.ErrConnectorLeaseLost, errors.New("lost")))
	err = dest.MergeCDCTablesAtomically(cancelledCtx, []destination.CDCAtomicTableMerge{{
		Options: destination.MergeOptions{TargetTable: "analytics.orders", StagingTable: "ingestr_staging.orders_stg", PrimaryKeys: []string{"id"}, Columns: []string{"id", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn}},
	}})
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)

	stgInc, resInc, err := dest.CDCConditionalSwapIncarnations(ctx, "analytics.orders", "ingestr_staging.orders_stg")
	require.NoError(t, err)
	assert.Equal(t, destination.CDCTargetKey("main", "ingestr_staging", "orders_stg", stagingID), stgInc)
	assert.Equal(t, destination.CDCTargetKey("main", "analytics", "orders", stagingID), resInc)

	*stmts = nil
	crossStgInc, crossResInc, err := dest.CDCConditionalSwapIncarnations(ctx, "main.analytics.orders", "other_cat.ingestr_staging.orders_stg")
	require.NoError(t, err)
	assert.NotEmpty(t, copyTable)
	copyTablePlain := strings.TrimPrefix(strings.ReplaceAll(copyTable, "`", ""), "main.analytics.")
	assert.Equal(t, destination.CDCTargetKey("main", "analytics", copyTablePlain, copyID), crossStgInc)
	assert.Equal(t, destination.CDCTargetKey("main", "analytics", "orders", copyID), crossResInc)

	targetID = copyID
	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		TargetTable:                   "main.analytics.orders",
		StagingTable:                  "other_cat.ingestr_staging.orders_stg",
		CDCExpectedIncarnation:        crossResInc,
		CDCExpectedStagingIncarnation: crossStgInc,
		CDCExpectedResultIncarnation:  crossResInc,
	}))
	assert.False(t, droppedCopy)

	*stmts = nil
	require.NoError(t, dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{{
		Truncate: true,
		Options: destination.MergeOptions{
			TargetTable:            "main.analytics.orders",
			StagingTable:           "ingestr_staging.orders_stg",
			PrimaryKeys:            []string{"id"},
			CDCExpectedIncarnation: crossResInc,
			Columns:                []string{"id", "amount", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
		},
	}}))
	joined := strings.Join(*stmts, "\n")
	assert.Contains(t, joined, "TRUNCATE TABLE `main`.`analytics`.`orders`")
	assert.Contains(t, joined, "MERGE INTO `main`.`analytics`.`orders` AS target")

	err = dest.MergeTable(ctx, destination.MergeOptions{
		TargetTable:            "main.analytics.orders",
		StagingTable:           "ingestr_staging.orders_stg",
		PrimaryKeys:            []string{"id"},
		CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "orders", "stale-id"),
		Columns:                []string{"id", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
	})
	require.ErrorContains(t, err, "physical incarnation changed")
}

func TestManagedCDCRunLeaseLifecycleAndExpiredTakeover(t *testing.T) {
	ctx := context.Background()
	var (
		mu         sync.Mutex
		leaseOwner string
		leaseAge   = "45"
		leaseLost  bool
		dropped    string
	)

	dest, _ := newTestDatabricksDestination(t, func(stmt string, _ dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasPrefix(stmt, "CREATE TABLE `main`.`ingestr_staging`.`cdc_lease_"):
			idx := strings.Index(stmt, "COMMENT '")
			comment := strings.TrimSuffix(stmt[idx+len("COMMENT '"):], "'")
			if leaseOwner != "" {
				return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_ALREADY_EXISTS] Table already exists")
			}
			leaseOwner = comment
			return dbsql.StatementResponse{}, nil
		case strings.Contains(stmt, "DESCRIBE DETAIL `main`.`ingestr_staging`.`cdc_lease_") && strings.Contains(stmt, "_expired_"):
			return makeQueryResponse([]string{"description", "createdAt"}, [][]string{{cdcLeaseCommentHead + "dead-holder", "2026-01-01T00:00:00Z"}}), nil
		case strings.Contains(stmt, "DESCRIBE DETAIL `main`.`ingestr_staging`.`cdc_lease_") && strings.Contains(stmt, "_released_"):
			return makeQueryResponse([]string{"description", "createdAt"}, [][]string{{leaseOwner, "2026-01-01T00:00:00Z"}}), nil
		case strings.HasPrefix(stmt, "DESCRIBE DETAIL `main`.`ingestr_staging`.`cdc_lease_"):
			if leaseOwner == "" {
				return dbsql.StatementResponse{}, errors.New("TABLE_OR_VIEW_NOT_FOUND")
			}
			return makeQueryResponse([]string{"description", "createdAt"}, [][]string{{leaseOwner, "2026-01-01T00:00:00Z"}}), nil
		case strings.Contains(stmt, "UNIX_TIMESTAMP(MAX(`heartbeat`))"):
			return makeQueryResponse([]string{"age"}, [][]string{{leaseAge}}), nil
		case strings.Contains(stmt, "RENAME TO") && strings.Contains(stmt, "_expired_"):
			leaseOwner = ""
			return dbsql.StatementResponse{}, nil
		case strings.Contains(stmt, "RENAME TO") && strings.Contains(stmt, "_released_"):
			return dbsql.StatementResponse{}, nil
		case strings.HasPrefix(stmt, "DROP TABLE IF EXISTS"):
			dropped = stmt
			leaseOwner = ""
			return dbsql.StatementResponse{}, nil
		default:
			if leaseLost {
				return dbsql.StatementResponse{}, errors.New("unexpected")
			}
			return dbsql.StatementResponse{}, nil
		}
	})

	// Seed an expired lease held by a crashed previous process.
	leaseOwner = cdcLeaseCommentHead + "dead-holder"
	lease, err := dest.AcquireManagedCDCRunLease(ctx, "conn-lease-1")
	require.NoError(t, err)
	require.NotEmpty(t, dropped)

	// Normal release renames and drops the lease table.
	require.NoError(t, lease.Release())
	assert.Contains(t, dropped, "_released_")

	// Acquiring a fresh lease and losing ownership causes Done() to close with ErrConnectorLeaseLost.
	leaseOwner = ""
	lease2, err := dest.AcquireManagedCDCRunLease(ctx, "conn-lease-2")
	require.NoError(t, err)

	mu.Lock()
	leaseOwner = cdcLeaseCommentHead + "hijacker"
	mu.Unlock()

	select {
	case <-lease2.Done():
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for lease loss notification")
	}
	require.ErrorIs(t, lease2.Err(), source.ErrConnectorLeaseLost)
	err = lease2.Release()
	require.ErrorContains(t, err, "release Databricks CDC run lease")
}

func TestWriteParallelCDCFencing(t *testing.T) {
	var (
		mu          sync.Mutex
		statements  []string
		uploaded    bool
		deletedFile bool
		targetID    = "uuid-target-1"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/2.0/fs/files/"):
			mu.Lock()
			uploaded = true
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/2.0/fs/files/"):
			mu.Lock()
			deletedFile = true
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/2.0/sql/statements":
			var req dbsql.ExecuteStatementRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			mu.Lock()
			statements = append(statements, req.Statement)
			curID := targetID
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			if strings.HasPrefix(req.Statement, "DESCRIBE DETAIL `main`.`analytics`.`orders`") {
				_ = json.NewEncoder(w).Encode(makeQueryResponse([]string{"id"}, [][]string{{curID}}))
				return
			}
			_ = json.NewEncoder(w).Encode(dbsql.StatementResponse{
				Status: &dbsql.StatementStatus{State: dbsql.StatementStateSucceeded},
			})
		default:
			http.Error(w, fmt.Sprintf("unexpected request: %s %s", r.Method, r.URL.Path), http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client, err := databricks.NewWorkspaceClient(&databricks.Config{
		Host: server.URL, Token: "test-token", AuthType: "pat",
	})
	require.NoError(t, err)
	dest := &DatabricksDestination{
		client: client, catalog: "main", schemaName: "analytics", httpPath: "/sql/1.0/warehouses/test",
	}

	makeBatchChan := func() <-chan source.RecordBatchResult {
		bldr := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		}, nil))
		bldr.Field(0).(*array.Int64Builder).Append(1)
		rec := bldr.NewRecordBatch()
		bldr.Release()

		ch := make(chan source.RecordBatchResult, 1)
		ch <- source.RecordBatchResult{Batch: rec}
		close(ch)
		return ch
	}

	// Lease loss stops write before upload or COPY INTO.
	lostCtx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.Join(source.ErrConnectorLeaseLost, errors.New("lost")))
	err = dest.WriteParallel(lostCtx, makeBatchChan(), destination.WriteOptions{
		Table:                  "analytics.orders",
		CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "orders", targetID),
	})
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	assert.False(t, uploaded)

	// Incarnation mismatch stops write before upload or COPY INTO.
	err = dest.WriteParallel(context.Background(), makeBatchChan(), destination.WriteOptions{
		Table:                  "analytics.orders",
		CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "orders", "wrong-id"),
	})
	require.ErrorContains(t, err, "physical incarnation changed")
	assert.False(t, uploaded)

	// Matching incarnation succeeds through upload, COPY INTO, cleanup, and post-check.
	expectedInc := destination.CDCTargetKey("main", "analytics", "orders", targetID)
	require.NoError(t, dest.WriteParallel(context.Background(), makeBatchChan(), destination.WriteOptions{
		Table:                  "analytics.orders",
		CDCExpectedIncarnation: expectedInc,
	}))
	assert.True(t, uploaded)
	assert.True(t, deletedFile)
	assert.Contains(t, strings.Join(statements, "\n"), "COPY INTO `main`.`analytics`.`orders`")
}
