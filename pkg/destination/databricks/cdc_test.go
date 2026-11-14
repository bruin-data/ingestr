package databricks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"regexp"
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
	var _ destination.CDCTargetIncarnationInitializer = dest
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
		case strings.HasPrefix(stmt, "DESCRIBE DETAIL `main`.`analytics`.`__ingestr_swap_old_"):
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
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
	var lateComment string
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
		case strings.HasPrefix(stmt, "DESCRIBE DETAIL `main`.`analytics`.`__ingestr_swap_old_"):
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
		case isMockInfoSchemaQuery(stmt):
			if !createdTarget {
				return makeQueryResponse([]string{"created", "comment"}, nil), nil
			}
			return makeQueryResponse([]string{"created", "comment"}, [][]string{{mockCreatedMicros("uuid-late-1"), lateComment}}), nil
		case strings.HasPrefix(stmt, "COMMENT ON TABLE `main`.`analytics`.`late_orders` IS "):
			lateComment = unquoteMockSQLString(strings.TrimPrefix(stmt, "COMMENT ON TABLE `main`.`analytics`.`late_orders` IS "))
			return dbsql.StatementResponse{}, nil
		case strings.HasPrefix(stmt, "DESCRIBE DETAIL `main`.`analytics`.`late_orders`"):
			if !createdTarget {
				return dbsql.StatementResponse{}, errors.New("TABLE_OR_VIEW_NOT_FOUND")
			}
			return makeQueryResponse([]string{"id", "description"}, [][]string{{"uuid-late-1", lateComment}}), nil
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
	tableIDs := map[string]string{
		"`main`.`analytics`.`orders`":                targetID,
		"`main`.`ingestr_staging`.`orders_stg`":      stagingID,
		"`other_cat`.`ingestr_staging`.`orders_stg`": stagingID,
	}
	tableComments := map[string]string{}

	dest, stmts := newTestDatabricksDestination(t, func(stmt string, _ dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error) {
		switch {
		case isMockInfoSchemaQuery(stmt):
			return mockInfoSchemaResponse(stmt, func(ref string) (string, string, bool) {
				id, ok := tableIDs[ref]
				return mockCreatedMicros(id), tableComments[ref], ok
			}), nil
		case strings.HasPrefix(stmt, "COMMENT ON TABLE "):
			ref, comment, ok := parseMockCommentOnTable(stmt)
			if !ok {
				return dbsql.StatementResponse{}, fmt.Errorf("malformed COMMENT ON TABLE: %s", stmt)
			}
			if _, exists := tableIDs[ref]; !exists {
				return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
			}
			tableComments[ref] = comment
			return dbsql.StatementResponse{}, nil
		case strings.HasPrefix(stmt, "DESCRIBE DETAIL "):
			ref := strings.TrimSpace(strings.TrimPrefix(stmt, "DESCRIBE DETAIL "))
			if id, ok := tableIDs[ref]; ok {
				return makeQueryResponse([]string{"id"}, [][]string{{id}}), nil
			}
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
		case strings.HasPrefix(stmt, "CREATE TABLE `main`.`analytics`.`orders_swap_"):
			parts := strings.Split(stmt, " ")
			copyTable = parts[2]
			tableIDs[copyTable] = copyID
			return dbsql.StatementResponse{}, nil
		case strings.HasPrefix(stmt, "ALTER TABLE ") && strings.Contains(stmt, " RENAME TO "):
			rest := strings.TrimPrefix(stmt, "ALTER TABLE ")
			parts := strings.SplitN(rest, " RENAME TO ", 2)
			fromRef, toRef := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			id, ok := tableIDs[fromRef]
			if !ok {
				return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
			}
			if _, exists := tableIDs[toRef]; exists {
				return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_ALREADY_EXISTS] Table already exists")
			}
			delete(tableIDs, fromRef)
			tableIDs[toRef] = id
			tableComments[toRef] = tableComments[fromRef]
			delete(tableComments, fromRef)
			return dbsql.StatementResponse{}, nil
		case strings.HasPrefix(stmt, "DROP TABLE IF EXISTS "):
			ref := strings.TrimSpace(strings.TrimPrefix(stmt, "DROP TABLE IF EXISTS "))
			if copyTable != "" && ref == copyTable {
				droppedCopy = true
			}
			delete(tableIDs, ref)
			delete(tableComments, ref)
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

	tableIDs["`main`.`analytics`.`orders`"] = copyID
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
	assert.Contains(t, joined, "BEGIN ATOMIC")
	assert.Contains(t, joined, "DELETE FROM `main`.`analytics`.`orders`")
	assert.Contains(t, joined, "MERGE INTO `main`.`analytics`.`orders` AS target")
	assert.Contains(t, joined, "SET MESSAGE_TEXT = '"+cdcIncarnationGuardMarker)
	assert.NotContains(t, joined, "RENAME TO")

	err = dest.MergeTable(ctx, destination.MergeOptions{
		TargetTable:            "main.analytics.orders",
		StagingTable:           "ingestr_staging.orders_stg",
		PrimaryKeys:            []string{"id"},
		CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "orders", "stale-id"),
		Columns:                []string{"id", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
	})
	require.ErrorContains(t, err, "physical incarnation changed")
}

// mockLeaseTable models the single owner/heartbeat row of a Databricks run
// lease. Conditional UPDATEs only apply while their WHERE clause still matches,
// which is how Delta resolves two takeovers racing for the same stale row.
type mockLeaseTable struct {
	owner      string
	ageSeconds int64
}

func TestManagedCDCRunLeaseLifecycleAndExpiredTakeover(t *testing.T) {
	ctx := context.Background()
	var (
		mu            sync.Mutex
		leases        = map[string]*mockLeaseTable{}
		staleSnapshot *mockLeaseTable
		structural    []string
	)
	leaseRe := regexp.MustCompile("`(cdc_lease_[0-9a-f]+)`")
	quoted := regexp.MustCompile(`'([^']*)'`)

	dest, _ := newTestDatabricksDestination(t, func(stmt string, _ dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error) {
		mu.Lock()
		defer mu.Unlock()
		m := leaseRe.FindStringSubmatch(stmt)
		if m == nil {
			return dbsql.StatementResponse{}, nil
		}
		name := m[1]
		row := leases[name]
		literals := quoted.FindAllStringSubmatch(stmt, -1)
		switch {
		case strings.Contains(stmt, " RENAME TO "), strings.HasPrefix(stmt, "DROP TABLE"):
			structural = append(structural, stmt)
			return dbsql.StatementResponse{}, nil
		case strings.HasPrefix(stmt, "CREATE TABLE "):
			if row != nil {
				return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_ALREADY_EXISTS] Table already exists")
			}
			leases[name] = &mockLeaseTable{owner: literals[0][1]}
		case row == nil:
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
		case strings.HasPrefix(stmt, "SELECT `owner`, CAST("):
			view := row
			if staleSnapshot != nil {
				view, staleSnapshot = staleSnapshot, nil
			}
			return makeQueryResponse([]string{"owner", "age"}, [][]string{{view.owner, strconv.FormatInt(view.ageSeconds, 10)}}), nil
		case strings.HasPrefix(stmt, "SELECT `owner` FROM "):
			return makeQueryResponse([]string{"owner"}, [][]string{{row.owner}}), nil
		case strings.HasPrefix(stmt, "UPDATE ") && strings.Contains(stmt, "SET `owner` = "):
			if row.owner == literals[1][1] && row.ageSeconds > int64(cdcLeaseTTL/time.Second) {
				row.owner, row.ageSeconds = literals[0][1], 0
			}
		case strings.HasPrefix(stmt, "UPDATE ") && strings.Contains(stmt, "TIMESTAMP '1970-01-01"):
			if row.owner == literals[1][1] {
				row.ageSeconds = 1 << 40
			}
		case strings.HasPrefix(stmt, "UPDATE ") && strings.Contains(stmt, "SET `heartbeat` = CURRENT_TIMESTAMP()"):
			if row.owner == literals[0][1] {
				row.ageSeconds = 0
			}
		}
		return dbsql.StatementResponse{}, nil
	})
	leaseName := func(connectorID string) string {
		sum := sha256.Sum256([]byte(connectorID))
		return "cdc_lease_" + hex.EncodeToString(sum[:10])
	}
	ownerOf := func(connectorID string) string {
		mu.Lock()
		defer mu.Unlock()
		return leases[leaseName(connectorID)].owner
	}

	// 1. An expired lease left by a crashed run is taken over in place.
	leases[leaseName("conn-1")] = &mockLeaseTable{owner: "dead-holder", ageSeconds: 45}
	acquired, err := dest.AcquireManagedCDCRunLease(ctx, "conn-1")
	require.NoError(t, err)
	holder := acquired.(*databricksCDCRunLease)
	assert.Equal(t, holder.token, ownerOf("conn-1"))

	// 2. A second waiter that read the same expired owner before the takeover
	// must not displace the new holder: its conditional UPDATE no longer matches.
	mu.Lock()
	staleSnapshot = &mockLeaseTable{owner: "dead-holder", ageSeconds: 45}
	mu.Unlock()
	waiter := &databricksCDCRunLease{d: dest, catalog: "main", schema: stagingSchema, name: leaseName("conn-1"), token: "late-waiter"}
	won, err := waiter.tryAcquire(ctx)
	require.NoError(t, err)
	assert.False(t, won)
	assert.Equal(t, holder.token, ownerOf("conn-1"))
	require.NoError(t, holder.Err())

	// 3. Release backdates the heartbeat so the next run takes over at once.
	require.NoError(t, holder.Release())
	next, err := dest.AcquireManagedCDCRunLease(ctx, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, next.(*databricksCDCRunLease).token, ownerOf("conn-1"))
	require.NoError(t, next.Release())

	// 4. Losing ownership closes Done() with ErrConnectorLeaseLost and release fails.
	lease2, err := dest.AcquireManagedCDCRunLease(ctx, "conn-2")
	require.NoError(t, err)
	mu.Lock()
	leases[leaseName("conn-2")].owner = "hijacker"
	mu.Unlock()
	select {
	case <-lease2.Done():
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for lease loss notification")
	}
	require.ErrorIs(t, lease2.Err(), source.ErrConnectorLeaseLost)
	require.ErrorContains(t, lease2.Release(), "release Databricks CDC run lease")

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, structural, "lease tables must never be renamed or dropped")
}

func TestWriteParallelCDCFencing(t *testing.T) {
	var (
		mu          sync.Mutex
		statements  []string
		uploaded    bool
		deletedFile bool
		targetID    = "uuid-target-1"
		tableIDs    = map[string]string{
			"`main`.`analytics`.`orders`": targetID,
		}
		tableComments = map[string]string{}
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
			w.Header().Set("Content-Type", "application/json")

			mu.Lock()
			statements = append(statements, req.Statement)
			switch {
			case isMockInfoSchemaQuery(req.Statement):
				resp := mockInfoSchemaResponse(req.Statement, func(ref string) (string, string, bool) {
					id, ok := tableIDs[ref]
					return mockCreatedMicros(id), tableComments[ref], ok
				})
				mu.Unlock()
				_ = json.NewEncoder(w).Encode(resp)
				return
			case strings.HasPrefix(req.Statement, "COMMENT ON TABLE "):
				if ref, comment, ok := parseMockCommentOnTable(req.Statement); ok {
					tableComments[ref] = comment
				}
			case strings.HasPrefix(req.Statement, "DESCRIBE TABLE "):
				mu.Unlock()
				_ = json.NewEncoder(w).Encode(makeQueryResponse([]string{"col_name", "data_type"}, [][]string{{"id", "bigint"}}))
				return
			case strings.HasPrefix(req.Statement, "DESCRIBE DETAIL "):
				ref := strings.TrimSpace(strings.TrimPrefix(req.Statement, "DESCRIBE DETAIL "))
				id, ok := tableIDs[ref]
				mu.Unlock()
				if !ok {
					_ = json.NewEncoder(w).Encode(dbsql.StatementResponse{
						StatementId: "stmt-missing",
						Status: &dbsql.StatementStatus{
							State: dbsql.StatementStateFailed,
							Error: &dbsql.ServiceError{Message: "[TABLE_OR_VIEW_NOT_FOUND] Table or view not found"},
						},
					})
					return
				}
				_ = json.NewEncoder(w).Encode(makeQueryResponse([]string{"id"}, [][]string{{id}}))
				return
			case strings.HasPrefix(req.Statement, "ALTER TABLE ") && strings.Contains(req.Statement, " RENAME TO "):
				rest := strings.TrimPrefix(req.Statement, "ALTER TABLE ")
				parts := strings.SplitN(rest, " RENAME TO ", 2)
				fromRef, toRef := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
				id := tableIDs[fromRef]
				delete(tableIDs, fromRef)
				tableIDs[toRef] = id
			}
			mu.Unlock()

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

	// Matching incarnation loads a private table, appends it to the live target in a guarded
	// BEGIN ATOMIC block, drops the private table, cleans the volume file, and never renames the target.
	expectedInc := destination.CDCTargetKey("main", "analytics", "orders", targetID)
	require.NoError(t, dest.WriteParallel(context.Background(), makeBatchChan(), destination.WriteOptions{
		Table:                  "analytics.orders",
		CDCExpectedIncarnation: expectedInc,
	}))
	assert.True(t, uploaded)
	assert.True(t, deletedFile)
	joined := strings.Join(statements, "\n")
	assert.Contains(t, joined, "COPY INTO `main`.`ingestr_staging`.`__ingestr_load_orders_")
	assert.Contains(t, joined, "INSERT INTO `main`.`analytics`.`orders` (`id`) SELECT `id` FROM `main`.`ingestr_staging`.`__ingestr_load_orders_")
	assert.Contains(t, joined, "DROP TABLE IF EXISTS `main`.`ingestr_staging`.`__ingestr_load_orders_")
	assert.NotContains(t, joined, "RENAME TO")
	assert.NotContains(t, joined, "COPY INTO `main`.`analytics`.`orders`")
}

type mockUCState struct {
	mu          sync.Mutex
	tables      map[string]mockUCTable
	views       map[string]string
	columns     map[string][]describedColumn
	loadColumns []describedColumn
	onStmt      func(stmt string, uc *mockUCState) error
	onAtomicSub func(sub string, uc *mockUCState)
}

type mockUCTable struct {
	id            string
	rows          int
	comment       string
	createdMicros string
}

func (t mockUCTable) effectiveCreatedMicros() string {
	if t.createdMicros != "" {
		return t.createdMicros
	}
	return mockCreatedMicros(t.id)
}

func newMockUCState(initial map[string]mockUCTable) *mockUCState {
	tables := make(map[string]mockUCTable, len(initial))
	for k, v := range initial {
		tables[normalizeMockUCRef(k)] = v
	}
	return &mockUCState{
		tables:  tables,
		views:   make(map[string]string),
		columns: make(map[string][]describedColumn),
	}
}

func mockCreatedMicros(id string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return strconv.FormatInt(int64(h.Sum64()>>1), 10)
}

func unquoteMockSQLString(lit string) string {
	lit = strings.TrimSpace(lit)
	if len(lit) >= 2 && lit[0] == '\'' && lit[len(lit)-1] == '\'' {
		lit = lit[1 : len(lit)-1]
	}
	lit = strings.ReplaceAll(lit, "''", "'")
	lit = strings.ReplaceAll(lit, `\\`, `\`)
	return lit
}

func parseMockCommentOnTable(stmt string) (ref, comment string, ok bool) {
	if !strings.HasPrefix(stmt, "COMMENT ON TABLE ") {
		return "", "", false
	}
	rest := strings.TrimPrefix(stmt, "COMMENT ON TABLE ")
	idx := strings.Index(rest, " IS '")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(rest[:idx]), unquoteMockSQLString(rest[idx+len(" IS "):]), true
}

var mockInfoSchemaPredicate = regexp.MustCompile(`lower\(table_catalog\) = '((?:[^']|'')*)' AND lower\(table_schema\) = '((?:[^']|'')*)' AND lower\(table_name\) = '((?:[^']|'')*)'(?: AND unix_micros\(created\) = (\d+) AND coalesce\(comment, ''\) = ('(?:[^']|'')*'))?`)

func parseMockInfoSchemaPredicate(t testing.TB, stmt string) (ref, created, comment string, hasGuard bool) {
	m := mockInfoSchemaPredicate.FindStringSubmatch(stmt)
	if m == nil {
		if t != nil {
			t.Fatalf("statement has no information_schema predicate: %s", stmt)
		}
		return "", "", "", false
	}
	unq := func(v string) string { return strings.ReplaceAll(v, "''", "'") }
	normRef := normalizeMockUCRef(quoteTableParts(unq(m[1]), unq(m[2]), unq(m[3])))
	if m[4] == "" {
		return normRef, "", "", false
	}
	return normRef, m[4], unquoteMockSQLString(m[5]), true
}

func mockInfoSchemaResponse(stmt string, lookup func(ref string) (created, comment string, ok bool)) dbsql.StatementResponse {
	ref, _, _, _ := parseMockInfoSchemaPredicate(nil, stmt)
	for kRef := range map[string]struct{}{ref: {}} {
		if created, comment, ok := lookup(kRef); ok {
			return makeQueryResponse([]string{"created", "comment"}, [][]string{{created, comment}})
		}
	}
	// Also check exact-case ref from tableIDs map if caller stores non-normalized keys:
	m := mockInfoSchemaPredicate.FindStringSubmatch(stmt)
	if m != nil {
		unq := func(v string) string { return strings.ReplaceAll(v, "''", "'") }
		exactRef := quoteTableParts(unq(m[1]), unq(m[2]), unq(m[3]))
		if created, comment, ok := lookup(exactRef); ok {
			return makeQueryResponse([]string{"created", "comment"}, [][]string{{created, comment}})
		}
	}
	return makeQueryResponse([]string{"created", "comment"}, nil)
}

func isMockInfoSchemaQuery(stmt string) bool {
	return strings.HasPrefix(stmt, "SELECT CAST(unix_micros(created) AS STRING), COALESCE(comment, '') FROM ")
}

func normalizeMockUCRef(ref string) string {
	return strings.ToLower(strings.TrimSpace(ref))
}

func (uc *mockUCState) getTable(ref string) (mockUCTable, bool) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	tbl, ok := uc.tables[normalizeMockUCRef(ref)]
	return tbl, ok
}

func (uc *mockUCState) setTable(ref string, tbl mockUCTable) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.tables[normalizeMockUCRef(ref)] = tbl
}

func (uc *mockUCState) handle(stmt string, _ dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	if uc.onStmt != nil {
		if err := uc.onStmt(stmt, uc); err != nil {
			return dbsql.StatementResponse{}, err
		}
	}
	return uc.dispatch(stmt)
}

// checkGuard evaluates a fence guard the way Databricks would, returning the
// SIGNAL error when the target no longer has the bound identity.
func (uc *mockUCState) checkGuard(sub string) error {
	ref, created, expectedComment, hasGuard := parseMockInfoSchemaPredicate(nil, sub)
	tbl, ok := uc.tables[ref]
	if !hasGuard || expectedComment == "" || !strings.Contains(sub, "THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '"+cdcIncarnationGuardMarker) {
		return fmt.Errorf("malformed guard: %s", sub)
	}
	if !ok || tbl.effectiveCreatedMicros() != created || tbl.comment != expectedComment {
		return fmt.Errorf("[USER_RAISED_EXCEPTION] %s: %s SQLSTATE: 45000", cdcIncarnationGuardMarker, ref)
	}
	return nil
}

func splitMockBlock(stmt, begin string) []string {
	body := strings.TrimPrefix(stmt, begin+"\n")
	body = strings.TrimSuffix(strings.TrimSpace(body), "END;")
	var subs []string
	for _, rawSub := range strings.Split(body, ";\n") {
		if sub := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rawSub), ";")); sub != "" {
			subs = append(subs, sub)
		}
	}
	return subs
}

func (uc *mockUCState) dispatch(stmt string) (dbsql.StatementResponse, error) {
	switch {
	case strings.HasPrefix(stmt, "DESCRIBE DETAIL "):
		ref := normalizeMockUCRef(strings.TrimPrefix(stmt, "DESCRIBE DETAIL "))
		tbl, ok := uc.tables[ref]
		if !ok {
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
		}
		return makeQueryResponse([]string{"id", "description"}, [][]string{{tbl.id, tbl.comment}}), nil

	case strings.HasPrefix(stmt, "COMMENT ON TABLE "):
		rawRef, comment, ok := parseMockCommentOnTable(stmt)
		if !ok {
			return dbsql.StatementResponse{}, fmt.Errorf("malformed COMMENT ON TABLE: %s", stmt)
		}
		ref := normalizeMockUCRef(rawRef)
		tbl, exists := uc.tables[ref]
		if !exists {
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
		}
		tbl.comment = comment
		uc.tables[ref] = tbl
		return dbsql.StatementResponse{}, nil

	case strings.HasPrefix(stmt, "ALTER TABLE ") && strings.Contains(stmt, " RENAME TO "):
		rest := strings.TrimPrefix(stmt, "ALTER TABLE ")
		parts := strings.SplitN(rest, " RENAME TO ", 2)
		fromRef := normalizeMockUCRef(parts[0])
		toRef := normalizeMockUCRef(parts[1])
		tbl, ok := uc.tables[fromRef]
		if !ok {
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
		}
		if _, exists := uc.tables[toRef]; exists {
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_ALREADY_EXISTS] Table already exists")
		}
		delete(uc.tables, fromRef)
		uc.tables[toRef] = tbl
		return dbsql.StatementResponse{}, nil

	case strings.HasPrefix(stmt, "DROP TABLE IF EXISTS "):
		ref := normalizeMockUCRef(strings.TrimPrefix(stmt, "DROP TABLE IF EXISTS "))
		delete(uc.tables, ref)
		return dbsql.StatementResponse{}, nil

	case strings.HasPrefix(stmt, "TRUNCATE TABLE "):
		ref := normalizeMockUCRef(strings.TrimPrefix(stmt, "TRUNCATE TABLE "))
		tbl, ok := uc.tables[ref]
		if !ok {
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
		}
		tbl.rows = 0
		uc.tables[ref] = tbl
		return dbsql.StatementResponse{}, nil

	case strings.HasPrefix(stmt, "COPY INTO "):
		rest := strings.TrimPrefix(stmt, "COPY INTO ")
		idx := strings.Index(rest, " FROM ")
		ref := normalizeMockUCRef(rest[:idx])
		tbl, ok := uc.tables[ref]
		if !ok {
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
		}
		tbl.rows++
		uc.tables[ref] = tbl
		if uc.loadColumns != nil && strings.Contains(ref, "`__ingestr_load_") {
			uc.columns[ref] = uc.loadColumns
		}
		return dbsql.StatementResponse{}, nil

	case strings.HasPrefix(stmt, "MERGE INTO ") && strings.Contains(stmt, " AS target\n"):
		idx := strings.Index(stmt, " AS target\n")
		ref := normalizeMockUCRef(strings.TrimPrefix(stmt[:idx], "MERGE INTO "))
		tbl, ok := uc.tables[ref]
		if !ok {
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
		}
		tbl.rows += 10
		uc.tables[ref] = tbl
		return dbsql.StatementResponse{}, nil

	case strings.HasPrefix(stmt, "BEGIN\n"):
		// A plain SQL script is not transactional: statements before a SIGNAL stay applied.
		for _, sub := range splitMockBlock(stmt, "BEGIN") {
			if uc.onAtomicSub != nil {
				uc.onAtomicSub(sub, uc)
			}
			if strings.HasPrefix(sub, "IF NOT EXISTS (SELECT 1 FROM ") {
				if err := uc.checkGuard(sub); err != nil {
					return dbsql.StatementResponse{}, err
				}
				continue
			}
			if _, err := uc.dispatch(sub); err != nil {
				return dbsql.StatementResponse{}, err
			}
		}
		return dbsql.StatementResponse{}, nil

	case strings.HasPrefix(stmt, "BEGIN ATOMIC\n"):
		undo := map[string]int{}
		touch := func(ref string) (mockUCTable, bool) {
			tbl, ok := uc.tables[ref]
			if ok {
				if _, seen := undo[tbl.id]; !seen {
					undo[tbl.id] = tbl.rows
				}
			}
			return tbl, ok
		}
		rollback := func() {
			for ref, tbl := range uc.tables {
				if rows, ok := undo[tbl.id]; ok {
					tbl.rows = rows
					uc.tables[ref] = tbl
				}
			}
		}
		for _, sub := range splitMockBlock(stmt, "BEGIN ATOMIC") {
			if uc.onAtomicSub != nil {
				uc.onAtomicSub(sub, uc)
			}
			switch {
			case strings.HasPrefix(sub, "IF NOT EXISTS (SELECT 1 FROM "):
				if err := uc.checkGuard(sub); err != nil {
					rollback()
					return dbsql.StatementResponse{}, err
				}
			case strings.HasPrefix(sub, "TRUNCATE TABLE "):
				rollback()
				return dbsql.StatementResponse{}, errors.New("[MULTI_STATEMENT_TRANSACTION_NOT_SUPPORTED] DDL TRUNCATE is not allowed in BEGIN ATOMIC")
			case strings.HasPrefix(sub, "COPY INTO "), strings.HasPrefix(sub, "ALTER TABLE "), strings.HasPrefix(sub, "COMMENT ON TABLE "):
				rollback()
				return dbsql.StatementResponse{}, fmt.Errorf("[UNSUPPORTED_FEATURE.SQL_SCRIPTING_WITH_UNSUPPORTED_COMMANDS_IN_ATOMIC_BODY] %s", sub)
			case strings.HasPrefix(sub, "DELETE FROM "):
				ref := normalizeMockUCRef(strings.TrimPrefix(sub, "DELETE FROM "))
				tbl, ok := touch(ref)
				if !ok {
					rollback()
					return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
				}
				tbl.rows = 0
				uc.tables[ref] = tbl
			case strings.HasPrefix(sub, "INSERT INTO "):
				rest := strings.TrimPrefix(sub, "INSERT INTO ")
				ref := normalizeMockUCRef(rest[:strings.Index(rest, " (")])
				tbl, ok := touch(ref)
				if !ok {
					rollback()
					return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
				}
				tbl.rows++
				uc.tables[ref] = tbl
			case strings.HasPrefix(sub, "MERGE INTO "):
				if strings.Contains(sub, "`fail_staging`") {
					rollback()
					return dbsql.StatementResponse{}, errors.New("simulated mid-batch merge failure")
				}
				idx := strings.Index(sub, " AS target\n")
				ref := normalizeMockUCRef(strings.TrimPrefix(sub[:idx], "MERGE INTO "))
				tbl, ok := touch(ref)
				if !ok {
					rollback()
					return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
				}
				tbl.rows += 10
				uc.tables[ref] = tbl
			default:
				rollback()
				return dbsql.StatementResponse{}, fmt.Errorf("unexpected statement in BEGIN ATOMIC: %s", sub)
			}
		}
		return dbsql.StatementResponse{}, nil

	case isMockInfoSchemaQuery(stmt):
		return mockInfoSchemaResponse(stmt, func(ref string) (string, string, bool) {
			tbl, ok := uc.tables[normalizeMockUCRef(ref)]
			return tbl.effectiveCreatedMicros(), tbl.comment, ok
		}), nil

	case strings.HasPrefix(stmt, "DESCRIBE TABLE "):
		ref := normalizeMockUCRef(strings.TrimPrefix(stmt, "DESCRIBE TABLE "))
		if _, ok := uc.tables[ref]; !ok {
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
		}
		cols, ok := uc.columns[ref]
		if !ok {
			cols = []describedColumn{{name: "id", dataType: "bigint"}}
		}
		rows := make([][]string, len(cols))
		for i, c := range cols {
			rows[i] = []string{c.name, c.dataType, ""}
		}
		return makeQueryResponse([]string{"col_name", "data_type", "comment"}, rows), nil

	case strings.HasPrefix(stmt, "ALTER TABLE ") && strings.Contains(stmt, " ADD COLUMNS ("):
		rest := strings.TrimPrefix(stmt, "ALTER TABLE ")
		idx := strings.Index(rest, " ADD COLUMNS (")
		ref := normalizeMockUCRef(rest[:idx])
		if _, ok := uc.tables[ref]; !ok {
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] Table or view not found")
		}
		cols, ok := uc.columns[ref]
		if !ok {
			cols = []describedColumn{{name: "id", dataType: "bigint"}}
		}
		defs := strings.TrimSuffix(rest[idx+len(" ADD COLUMNS ("):], ")")
		for _, def := range strings.Split(defs, ", ") {
			parts := strings.SplitN(def, " ", 2)
			cols = append(cols, describedColumn{name: strings.Trim(parts[0], "`"), dataType: parts[1]})
		}
		uc.columns[ref] = cols
		return dbsql.StatementResponse{}, nil

	case strings.HasPrefix(stmt, "CREATE VIEW IF NOT EXISTS "):
		rest := strings.TrimPrefix(stmt, "CREATE VIEW IF NOT EXISTS ")
		idx := strings.Index(rest, " AS SELECT '")
		viewRef := normalizeMockUCRef(rest[:idx])
		ownerPart := rest[idx+len(" AS SELECT '"):]
		owner := ownerPart[:strings.Index(ownerPart, "' AS owner_id")]
		if _, exists := uc.views[viewRef]; !exists {
			uc.views[viewRef] = owner
		}
		return dbsql.StatementResponse{}, nil

	case strings.HasPrefix(stmt, "SELECT owner_id FROM "):
		viewRef := normalizeMockUCRef(strings.TrimPrefix(stmt, "SELECT owner_id FROM "))
		owner, ok := uc.views[viewRef]
		if !ok {
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_NOT_FOUND] View not found")
		}
		return makeQueryResponse([]string{"owner_id"}, [][]string{{owner}}), nil

	case strings.HasPrefix(stmt, "DROP VIEW IF EXISTS "):
		viewRef := normalizeMockUCRef(strings.TrimPrefix(stmt, "DROP VIEW IF EXISTS "))
		delete(uc.views, viewRef)
		return dbsql.StatementResponse{}, nil

	case strings.HasPrefix(stmt, "CREATE TABLE IF NOT EXISTS `"):
		rest := strings.TrimPrefix(stmt, "CREATE TABLE IF NOT EXISTS ")
		idx := strings.Index(rest, " (")
		if idx < 0 {
			idx = len(rest)
		}
		ref := normalizeMockUCRef(rest[:idx])
		if _, exists := uc.tables[ref]; !exists {
			uc.tables[ref] = mockUCTable{id: "uuid-created-" + strconv.Itoa(len(uc.tables)+1), rows: 0}
		}
		return dbsql.StatementResponse{}, nil

	case strings.HasPrefix(stmt, "CREATE TABLE `") && !strings.HasPrefix(stmt, "CREATE TABLE IF NOT EXISTS"):
		rest := strings.TrimPrefix(stmt, "CREATE TABLE ")
		idx := strings.Index(rest, " (")
		if idx < 0 {
			idx = strings.Index(rest, " AS SELECT ")
		}
		if idx < 0 {
			idx = len(rest)
		}
		ref := normalizeMockUCRef(rest[:idx])
		if _, exists := uc.tables[ref]; exists {
			return dbsql.StatementResponse{}, errors.New("[TABLE_OR_VIEW_ALREADY_EXISTS] Table already exists")
		}
		uc.tables[ref] = mockUCTable{id: "uuid-created-" + strconv.Itoa(len(uc.tables)+1), rows: 0}
		return dbsql.StatementResponse{}, nil

	default:
		return dbsql.StatementResponse{}, nil
	}
}

func TestMergeCDCTablesAtomically_SingleAtomicBatchAndRollbackOnFailure(t *testing.T) {
	ctx := context.Background()
	uc := newMockUCState(map[string]mockUCTable{
		"`main`.`analytics`.`orders`":    {id: "uuid-orders-1", rows: 25},
		"`main`.`analytics`.`customers`": {id: "uuid-customers-1", rows: 12},
		"`main`.`analytics`.`audit_log`": {id: "uuid-audit-1", rows: 5},
	})
	dest, stmts := newTestDatabricksDestination(t, uc.handle)

	ordersInc := destination.CDCTargetKey("main", "analytics", "orders", "uuid-orders-1")
	customersInc := destination.CDCTargetKey("main", "analytics", "customers", "uuid-customers-1")
	auditInc := destination.CDCTargetKey("main", "analytics", "audit_log", "uuid-audit-1")
	cdcCols := []string{"id", "val", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn}

	// 1. Conflicting incarnations for case-variant references to the same Unity Catalog table fail before any SQL.
	err := dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
		{
			Options: destination.MergeOptions{
				TargetTable:            "analytics.orders",
				StagingTable:           "ingestr_staging.stg_orders_1",
				PrimaryKeys:            []string{"id"},
				CDCExpectedIncarnation: ordersInc,
				Columns:                cdcCols,
			},
		},
		{
			Options: destination.MergeOptions{
				TargetTable:            "Analytics.Orders",
				StagingTable:           "ingestr_staging.stg_orders_2",
				PrimaryKeys:            []string{"id"},
				CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "orders", "other-id"),
				Columns:                cdcCols,
			},
		},
	})
	require.ErrorContains(t, err, "conflicting expected incarnations")
	assert.Empty(t, *stmts)

	// 2. Mid-batch failure rolls back ALL tables (including Truncate: true on earlier table) and restores all guards.
	err = dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
		{
			Truncate: true,
			Options: destination.MergeOptions{
				TargetTable:            "analytics.orders",
				StagingTable:           "ingestr_staging.stg_orders",
				PrimaryKeys:            []string{"id"},
				CDCExpectedIncarnation: ordersInc,
				Columns:                cdcCols,
			},
		},
		{
			Options: destination.MergeOptions{
				TargetTable:            "analytics.customers",
				StagingTable:           "ingestr_staging.fail_staging",
				PrimaryKeys:            []string{"id"},
				CDCExpectedIncarnation: customersInc,
				Columns:                cdcCols,
			},
		},
	})
	require.ErrorContains(t, err, "simulated mid-batch merge failure")

	ordersTbl, ok := uc.getTable("`main`.`analytics`.`orders`")
	require.True(t, ok)
	assert.Equal(t, mockUCTable{id: "uuid-orders-1", rows: 25, comment: cdcIncarnationCommentMarker + "uuid-orders-1"}, ordersTbl, "truncated table must roll back to original 25 rows")

	customersTbl, ok := uc.getTable("`main`.`analytics`.`customers`")
	require.True(t, ok)
	assert.Equal(t, mockUCTable{id: "uuid-customers-1", rows: 12, comment: cdcIncarnationCommentMarker + "uuid-customers-1"}, customersTbl)

	// 3. Single-table Truncate: true + failed MERGE also wraps DELETE + MERGE in BEGIN ATOMIC and preserves rows.
	err = dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{{
		Truncate: true,
		Options: destination.MergeOptions{
			TargetTable:            "analytics.orders",
			StagingTable:           "ingestr_staging.fail_staging",
			PrimaryKeys:            []string{"id"},
			CDCExpectedIncarnation: ordersInc,
			Columns:                cdcCols,
		},
	}})
	require.ErrorContains(t, err, "simulated mid-batch merge failure")
	ordersTbl, ok = uc.getTable("`main`.`analytics`.`orders`")
	require.True(t, ok)
	assert.Equal(t, mockUCTable{id: "uuid-orders-1", rows: 25, comment: cdcIncarnationCommentMarker + "uuid-orders-1"}, ordersTbl, "single-table truncate+merge failure must not leave target empty")

	// 4. Successful multi-table batch (CDC + non-CDC, with and without Truncate) executes in ONE BEGIN ATOMIC block.
	*stmts = nil
	require.NoError(t, dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
		{
			Truncate: true,
			Options: destination.MergeOptions{
				TargetTable:            "analytics.orders",
				StagingTable:           "ingestr_staging.stg_orders",
				PrimaryKeys:            []string{"id"},
				CDCExpectedIncarnation: ordersInc,
				Columns:                cdcCols,
			},
		},
		{
			Options: destination.MergeOptions{
				TargetTable:            "Analytics.Customers",
				StagingTable:           "ingestr_staging.stg_customers",
				PrimaryKeys:            []string{"id"},
				CDCExpectedIncarnation: customersInc,
				Columns:                cdcCols,
			},
		},
		{
			Options: destination.MergeOptions{
				TargetTable:            "analytics.audit_log",
				StagingTable:           "ingestr_staging.stg_audit",
				PrimaryKeys:            []string{"id"},
				CDCExpectedIncarnation: auditInc,
				Columns:                []string{"id", "payload"},
			},
		},
	}))

	atomicBlocks := 0
	for _, s := range *stmts {
		if strings.HasPrefix(s, "BEGIN ATOMIC\n") {
			atomicBlocks++
		}
		assert.False(t, strings.HasPrefix(s, "TRUNCATE TABLE "), "must use DML DELETE inside BEGIN ATOMIC rather than standalone TRUNCATE")
	}
	assert.Equal(t, 1, atomicBlocks, "entire multi-table batch must run in a single BEGIN ATOMIC transaction")

	ordersTbl, ok = uc.getTable("`main`.`analytics`.`orders`")
	require.True(t, ok)
	assert.Equal(t, mockUCTable{id: "uuid-orders-1", rows: 10, comment: cdcIncarnationCommentMarker + "uuid-orders-1"}, ordersTbl)

	customersTbl, ok = uc.getTable("`main`.`analytics`.`customers`")
	require.True(t, ok)
	assert.Equal(t, mockUCTable{id: "uuid-customers-1", rows: 22, comment: cdcIncarnationCommentMarker + "uuid-customers-1"}, customersTbl)

	auditTbl, ok := uc.getTable("`main`.`analytics`.`audit_log`")
	require.True(t, ok)
	assert.Equal(t, mockUCTable{id: "uuid-audit-1", rows: 15, comment: cdcIncarnationCommentMarker + "uuid-audit-1"}, auditTbl)
}

func newMockUCWriteDestination(t *testing.T, uc *mockUCState) (*DatabricksDestination, func() []string, *atomic.Bool) {
	t.Helper()
	var (
		mu       sync.Mutex
		sqlStmts []string
		deleted  atomic.Bool
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/2.0/fs/files/"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/2.0/fs/files/"):
			deleted.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/2.0/sql/statements":
			var req dbsql.ExecuteStatementRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			sqlStmts = append(sqlStmts, req.Statement)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			resp, err := uc.handle(req.Statement, req)
			if err != nil {
				resp = dbsql.StatementResponse{
					StatementId: "stmt-err",
					Status: &dbsql.StatementStatus{
						State: dbsql.StatementStateFailed,
						Error: &dbsql.ServiceError{Message: err.Error()},
					},
				}
			} else if resp.Status == nil {
				resp.Status = &dbsql.StatementStatus{State: dbsql.StatementStateSucceeded}
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)

	client, err := databricks.NewWorkspaceClient(&databricks.Config{
		Host: server.URL, Token: "test-token", AuthType: "pat",
	})
	require.NoError(t, err)
	dest := &DatabricksDestination{
		client: client, catalog: "main", schemaName: "analytics", httpPath: "/sql/1.0/warehouses/test",
	}
	return dest, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sqlStmts...)
	}, &deleted
}

func singleRowBatch() <-chan source.RecordBatchResult {
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

const liveOrdersRef = "`main`.`analytics`.`orders`"

func assertLiveTargetNeverRenamed(t *testing.T, stmts []string) {
	t.Helper()
	for _, s := range stmts {
		assert.False(t, strings.HasPrefix(s, "ALTER TABLE "+liveOrdersRef+" RENAME TO "), "live target must never be renamed away: %s", s)
		assert.False(t, strings.HasPrefix(s, "TRUNCATE TABLE "), "fenced truncate must use guarded DELETE inside BEGIN ATOMIC: %s", s)
	}
}

// installLivenessProbe records every statement (top-level or inside BEGIN
// ATOMIC) that ran while the live target name was missing.
func installLivenessProbe(uc *mockUCState) *[]string {
	var missing []string
	prevStmt := uc.onStmt
	prevSub := uc.onAtomicSub
	uc.onStmt = func(stmt string, state *mockUCState) error {
		if _, ok := state.tables[normalizeMockUCRef(liveOrdersRef)]; !ok {
			missing = append(missing, stmt)
		}
		if prevStmt != nil {
			return prevStmt(stmt, state)
		}
		return nil
	}
	uc.onAtomicSub = func(sub string, state *mockUCState) {
		if _, ok := state.tables[normalizeMockUCRef(liveOrdersRef)]; !ok {
			missing = append(missing, sub)
		}
		if prevSub != nil {
			prevSub(sub, state)
		}
	}
	return &missing
}

func countAtomicBlocks(stmts []string) int {
	n := 0
	for _, s := range stmts {
		if strings.HasPrefix(s, "BEGIN ATOMIC\n") {
			n++
		}
	}
	return n
}

func TestBuildFencedAtomicSQLGuardsBeforeAndAfterDML(t *testing.T) {
	t.Parallel()
	fence := &cdcFence{
		catalog:         "Main",
		schemaName:      "Analytics",
		tableName:       "Orders",
		targetFull:      quoteTableParts("Main", "Analytics", "Orders"),
		createdMicros:   "1700000000123456",
		expectedComment: "User's table __ingestr_cdc_incarnation=uuid-orders-1",
	}
	guard := "IF NOT EXISTS (SELECT 1 FROM `Main`.`information_schema`.`tables` WHERE lower(table_catalog) = 'main' AND lower(table_schema) = 'analytics' AND lower(table_name) = 'orders' AND unix_micros(created) = 1700000000123456 AND coalesce(comment, '') = 'User''s table __ingestr_cdc_incarnation=uuid-orders-1') THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'INGESTR_CDC_INCARNATION_CHANGED: `Main`.`Analytics`.`Orders`'; END IF"
	assert.Equal(t, guard, fence.guardSQL())

	got := buildFencedAtomicSQL([]*cdcFence{fence}, []string{"DELETE FROM `Main`.`Analytics`.`Orders`;", "MERGE INTO `Main`.`Analytics`.`Orders` AS target\nUSING s AS source\nON x"})
	want := "BEGIN ATOMIC\n  " + guard + ";\n  DELETE FROM `Main`.`Analytics`.`Orders`;\n  MERGE INTO `Main`.`Analytics`.`Orders` AS target\nUSING s AS source\nON x;\n  " + guard + ";\nEND;"
	assert.Equal(t, want, got)
	assert.NotContains(t, got, "RENAME")
}

func TestTruncateCDCTableIfIncarnation_ProtectsReplacementTable(t *testing.T) {
	ctx := context.Background()
	expected := destination.CDCTargetKey("main", "analytics", "orders", "uuid-orig")

	t.Run("matching incarnation deletes rows in place without hiding the live table", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-orig", rows: 10}})
		missing := installLivenessProbe(uc)
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, "analytics.orders", expected))
		tbl, ok := uc.getTable(liveOrdersRef)
		require.True(t, ok)
		assert.Equal(t, mockUCTable{id: "uuid-orig", rows: 0, comment: cdcIncarnationCommentMarker + "uuid-orig"}, tbl)
		assert.Empty(t, *missing, "live target must stay visible during the truncate")
		assertLiveTargetNeverRenamed(t, *stmts)
		assert.Equal(t, 1, countAtomicBlocks(*stmts))
		joined := strings.Join(*stmts, "\n")
		assert.Contains(t, joined, "DELETE FROM "+liveOrdersRef)
		assert.Equal(t, 2, strings.Count(joined, cdcIncarnationGuardMarker), "guard must run before and after DELETE")
	})

	t.Run("stale incarnation fails before any DML", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-replacement", rows: 77}})
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		err := dest.TruncateCDCTableIfIncarnation(ctx, "analytics.orders", expected)
		require.ErrorContains(t, err, "physical incarnation changed")
		assert.Zero(t, countAtomicBlocks(*stmts))
		tbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-replacement", rows: 77}, tbl)
	})

	t.Run("replacement between identity reads is detected before any DML", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-orig", rows: 10}})
		reads := 0
		uc.onStmt = func(stmt string, state *mockUCState) error {
			if isMockInfoSchemaQuery(stmt) {
				reads++
				if reads == 2 {
					state.tables[normalizeMockUCRef(liveOrdersRef)] = mockUCTable{id: "uuid-replacement", rows: 77}
				}
			}
			return nil
		}
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		err := dest.TruncateCDCTableIfIncarnation(ctx, "analytics.orders", expected)
		require.ErrorContains(t, err, "physical incarnation changed")
		assert.Zero(t, countAtomicBlocks(*stmts))
		tbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-replacement", rows: 77}, tbl)
	})

	t.Run("replacement before the atomic block is rejected by the leading guard", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-orig", rows: 10}})
		uc.onStmt = func(stmt string, state *mockUCState) error {
			if strings.HasPrefix(stmt, "BEGIN ATOMIC\n") {
				state.tables[normalizeMockUCRef(liveOrdersRef)] = mockUCTable{id: "uuid-replacement", rows: 77}
			}
			return nil
		}
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		err := dest.TruncateCDCTableIfIncarnation(ctx, "analytics.orders", expected)
		require.ErrorContains(t, err, "physical incarnation changed")
		require.ErrorContains(t, err, cdcIncarnationGuardMarker)
		tbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-replacement", rows: 77}, tbl)
		assertLiveTargetNeverRenamed(t, *stmts)
	})

	t.Run("replacement after the leading guard is rolled back by the trailing guard", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-orig", rows: 10}})
		replaced := false
		uc.onAtomicSub = func(sub string, state *mockUCState) {
			if !replaced && strings.HasPrefix(sub, "DELETE FROM ") {
				replaced = true
				state.tables[normalizeMockUCRef(liveOrdersRef)] = mockUCTable{id: "uuid-concurrent-new", rows: 99}
			}
		}
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		err := dest.TruncateCDCTableIfIncarnation(ctx, "analytics.orders", expected)
		require.ErrorContains(t, err, "physical incarnation changed")
		require.True(t, replaced)
		tbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-concurrent-new", rows: 99}, tbl, "DELETE on the replacement must be rolled back")
		assertLiveTargetNeverRenamed(t, *stmts)
	})
}

func TestMergeAndSwapAndWrite_ProtectReplacementTables(t *testing.T) {
	ctx := context.Background()
	cdcCols := []string{"id", "val", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn}

	t.Run("MergeTable merges the live table in place", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-orig", rows: 30}})
		missing := installLivenessProbe(uc)
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
			TargetTable:            "analytics.orders",
			StagingTable:           "ingestr_staging.stg_orders",
			PrimaryKeys:            []string{"id"},
			CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "orders", "uuid-orig"),
			Columns:                cdcCols,
		}))
		tbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-orig", rows: 40, comment: cdcIncarnationCommentMarker + "uuid-orig"}, tbl)
		assert.Empty(t, *missing)
		assertLiveTargetNeverRenamed(t, *stmts)
		assert.Contains(t, strings.Join(*stmts, "\n"), "MERGE INTO "+liveOrdersRef+" AS target")
	})

	t.Run("MergeTable rolls back a merge that hit a replacement table", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-orig", rows: 30}})
		replaced := false
		uc.onAtomicSub = func(sub string, state *mockUCState) {
			if !replaced && strings.HasPrefix(sub, "MERGE INTO ") {
				replaced = true
				state.tables[normalizeMockUCRef(liveOrdersRef)] = mockUCTable{id: "uuid-replaced", rows: 88}
			}
		}
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		err := dest.MergeTable(ctx, destination.MergeOptions{
			TargetTable:            "analytics.orders",
			StagingTable:           "ingestr_staging.stg_orders",
			PrimaryKeys:            []string{"id"},
			CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "orders", "uuid-orig"),
			Columns:                cdcCols,
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		tbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-replaced", rows: 88}, tbl)
		assertLiveTargetNeverRenamed(t, *stmts)
	})

	t.Run("MergeCDCTablesAtomically rolls back every table when one target is replaced mid-transaction", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{
			"`main`.`analytics`.`customers`": {id: "uuid-cust", rows: 15},
			liveOrdersRef:                    {id: "uuid-ord", rows: 40},
		})
		replaced := false
		uc.onAtomicSub = func(sub string, state *mockUCState) {
			if !replaced && strings.HasPrefix(sub, "MERGE INTO "+liveOrdersRef) {
				replaced = true
				state.tables[normalizeMockUCRef(liveOrdersRef)] = mockUCTable{id: "uuid-ord-replaced", rows: 120}
			}
		}
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		err := dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            "analytics.customers",
					StagingTable:           "ingestr_staging.stg_customers",
					PrimaryKeys:            []string{"id"},
					CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "customers", "uuid-cust"),
					Columns:                cdcCols,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            "analytics.orders",
					StagingTable:           "ingestr_staging.stg_orders",
					PrimaryKeys:            []string{"id"},
					CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "orders", "uuid-ord"),
					Columns:                cdcCols,
				},
			},
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.True(t, replaced)
		assert.Equal(t, 1, countAtomicBlocks(*stmts))
		custTbl, _ := uc.getTable("`main`.`analytics`.`customers`")
		assert.Equal(t, mockUCTable{id: "uuid-cust", rows: 15, comment: cdcIncarnationCommentMarker + "uuid-cust"}, custTbl, "earlier DELETE+MERGE must roll back")
		ordTbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-ord-replaced", rows: 120}, ordTbl, "replacement must be left untouched")
		for _, s := range *stmts {
			assert.NotContains(t, s, "RENAME TO")
		}
	})

	t.Run("MergeCDCTablesAtomically with a stale incarnation runs no transaction", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{
			"`main`.`analytics`.`items`":     {id: "uuid-item", rows: 8},
			"`main`.`analytics`.`customers`": {id: "uuid-cust", rows: 15},
			liveOrdersRef:                    {id: "uuid-ord-replaced", rows: 120},
		})
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		err := dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{Options: destination.MergeOptions{
				TargetTable: "analytics.items", StagingTable: "ingestr_staging.stg_items", PrimaryKeys: []string{"id"},
				CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "items", "uuid-item"), Columns: cdcCols,
			}},
			{Truncate: true, Options: destination.MergeOptions{
				TargetTable: "analytics.customers", StagingTable: "ingestr_staging.stg_customers", PrimaryKeys: []string{"id"},
				CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "customers", "uuid-cust"), Columns: cdcCols,
			}},
			{Options: destination.MergeOptions{
				TargetTable: "analytics.orders", StagingTable: "ingestr_staging.stg_orders", PrimaryKeys: []string{"id"},
				CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "orders", "uuid-ord"), Columns: cdcCols,
			}},
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		assert.Zero(t, countAtomicBlocks(*stmts))
		for _, s := range *stmts {
			assert.NotContains(t, s, "COMMENT ON TABLE")
		}
		itemTbl, _ := uc.getTable("`main`.`analytics`.`items`")
		assert.Equal(t, mockUCTable{id: "uuid-item", rows: 8}, itemTbl)
		custTbl, _ := uc.getTable("`main`.`analytics`.`customers`")
		assert.Equal(t, mockUCTable{id: "uuid-cust", rows: 15}, custTbl)
	})

	t.Run("SwapTable restores replacement target without dropping it or renaming staging", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{
			"`main`.`analytics`.`orders`":           {id: "uuid-orig", rows: 10},
			"`main`.`ingestr_staging`.`stg_orders`": {id: "uuid-stg", rows: 50},
		})
		replaced := false
		uc.onStmt = func(stmt string, state *mockUCState) error {
			if !replaced && strings.HasPrefix(stmt, "ALTER TABLE `main`.`analytics`.`orders` RENAME TO ") {
				replaced = true
				state.tables["`main`.`analytics`.`orders`"] = mockUCTable{id: "uuid-outside", rows: 999}
			}
			return nil
		}
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		err := dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   "analytics.orders",
			StagingTable:                  "ingestr_staging.stg_orders",
			CDCExpectedIncarnation:        destination.CDCTargetKey("main", "analytics", "orders", "uuid-orig"),
			CDCExpectedStagingIncarnation: destination.CDCTargetKey("main", "ingestr_staging", "stg_orders", "uuid-stg"),
			CDCExpectedResultIncarnation:  destination.CDCTargetKey("main", "analytics", "orders", "uuid-stg"),
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		for _, s := range *stmts {
			assert.False(t, strings.HasPrefix(s, "DROP TABLE "), "outside replacement table must never be dropped")
		}
		targetTbl, ok := uc.getTable("`main`.`analytics`.`orders`")
		require.True(t, ok)
		assert.Equal(t, mockUCTable{id: "uuid-outside", rows: 999}, targetTbl)

		stgTbl, ok := uc.getTable("`main`.`ingestr_staging`.`stg_orders`")
		require.True(t, ok)
		assert.Equal(t, mockUCTable{id: "uuid-stg", rows: 50}, stgTbl)
	})

	t.Run("SwapTable rolls back and preserves old target if staging was replaced before rename", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{
			"`main`.`analytics`.`orders`":           {id: "uuid-orig", rows: 10},
			"`main`.`ingestr_staging`.`stg_orders`": {id: "uuid-stg", rows: 50},
		})
		uc.onStmt = func(stmt string, state *mockUCState) error {
			if strings.HasPrefix(stmt, "ALTER TABLE `main`.`ingestr_staging`.`stg_orders` RENAME TO ") {
				state.tables["`main`.`ingestr_staging`.`stg_orders`"] = mockUCTable{id: "uuid-rogue-stg", rows: 1}
			}
			return nil
		}
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		err := dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   "analytics.orders",
			StagingTable:                  "ingestr_staging.stg_orders",
			CDCExpectedIncarnation:        destination.CDCTargetKey("main", "analytics", "orders", "uuid-orig"),
			CDCExpectedStagingIncarnation: destination.CDCTargetKey("main", "ingestr_staging", "stg_orders", "uuid-stg"),
			CDCExpectedResultIncarnation:  destination.CDCTargetKey("main", "analytics", "orders", "uuid-stg"),
		})
		require.ErrorContains(t, err, "physical incarnation changed after swap")
		for _, s := range *stmts {
			assert.False(t, strings.HasPrefix(s, "DROP TABLE "), "original target backup must never be dropped on result mismatch")
		}
		targetTbl, ok := uc.getTable("`main`.`analytics`.`orders`")
		require.True(t, ok)
		assert.Equal(t, mockUCTable{id: "uuid-orig", rows: 10}, targetTbl)
	})

	t.Run("SwapTable rejects target created after empty-target precheck without overwriting it", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{
			"`main`.`ingestr_staging`.`stg_orders`": {id: "uuid-stg", rows: 50},
		})
		uc.onStmt = func(stmt string, state *mockUCState) error {
			if strings.HasPrefix(stmt, "ALTER TABLE `main`.`ingestr_staging`.`stg_orders` RENAME TO ") {
				state.tables["`main`.`analytics`.`orders`"] = mockUCTable{id: "uuid-created-outside", rows: 33}
			}
			return nil
		}
		dest, _ := newTestDatabricksDestination(t, uc.handle)

		err := dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   "analytics.orders",
			StagingTable:                  "ingestr_staging.stg_orders",
			CDCExpectedIncarnation:        "",
			CDCExpectedStagingIncarnation: destination.CDCTargetKey("main", "ingestr_staging", "stg_orders", "uuid-stg"),
			CDCExpectedResultIncarnation:  destination.CDCTargetKey("main", "analytics", "orders", "uuid-stg"),
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		targetTbl, ok := uc.getTable("`main`.`analytics`.`orders`")
		require.True(t, ok)
		assert.Equal(t, mockUCTable{id: "uuid-created-outside", rows: 33}, targetTbl)
	})

	t.Run("WriteParallel appends through a private load table without hiding the live table", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-orig", rows: 20}})
		missing := installLivenessProbe(uc)
		dest, stmts, deleted := newMockUCWriteDestination(t, uc)

		require.NoError(t, dest.WriteParallel(ctx, singleRowBatch(), destination.WriteOptions{
			Table:                  "analytics.orders",
			CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "orders", "uuid-orig"),
		}))
		tbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-orig", rows: 21, comment: cdcIncarnationCommentMarker + "uuid-orig"}, tbl)
		assert.True(t, deleted.Load())
		assert.Empty(t, *missing)
		all := stmts()
		assertLiveTargetNeverRenamed(t, all)
		for _, s := range all {
			assert.False(t, strings.HasPrefix(s, "COPY INTO "+liveOrdersRef), "COPY INTO must not target the live table directly")
		}
		for ref := range uc.tables {
			assert.NotContains(t, ref, "__ingestr_load_", "private load table must be dropped")
		}
	})

	t.Run("WriteParallel never appends to a replacement table and cleans up", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-orig", rows: 20}})
		replaced := false
		uc.onAtomicSub = func(sub string, state *mockUCState) {
			if !replaced && strings.HasPrefix(sub, "INSERT INTO ") {
				replaced = true
				state.tables[normalizeMockUCRef(liveOrdersRef)] = mockUCTable{id: "uuid-after-upload", rows: 64}
			}
		}
		dest, stmts, deleted := newMockUCWriteDestination(t, uc)

		err := dest.WriteParallel(ctx, singleRowBatch(), destination.WriteOptions{
			Table:                  "analytics.orders",
			CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "orders", "uuid-orig"),
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.True(t, replaced)
		assert.True(t, deleted.Load(), "uploaded volume file must be cleaned up on fence failure")
		targetTbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-after-upload", rows: 64}, targetTbl)
		assertLiveTargetNeverRenamed(t, stmts())
		for ref := range uc.tables {
			assert.NotContains(t, ref, "__ingestr_load_")
		}
	})

	t.Run("WriteParallel rejects missing target columns without running unfenced ALTER TABLE", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-orig", rows: 20}})
		uc.loadColumns = []describedColumn{{name: "id", dataType: "bigint"}, {name: "new_col", dataType: "string"}}
		dest, stmts, deleted := newMockUCWriteDestination(t, uc)

		err := dest.WriteParallel(ctx, singleRowBatch(), destination.WriteOptions{
			Table:                  "analytics.orders",
			CDCExpectedIncarnation: destination.CDCTargetKey("main", "analytics", "orders", "uuid-orig"),
		})
		require.ErrorContains(t, err, "missing columns required by fenced write: new_col")
		assert.True(t, deleted.Load(), "uploaded volume file must be cleaned up")
		for _, s := range stmts() {
			assert.False(t, strings.HasPrefix(s, "ALTER TABLE "), "fenced write must never emit unfenced ALTER TABLE DDL: %s", s)
			assert.False(t, strings.HasPrefix(s, "BEGIN ATOMIC"), "fenced write must abort before BEGIN ATOMIC when schema is missing columns")
		}
		tbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-orig", rows: 20, comment: cdcIncarnationCommentMarker + "uuid-orig"}, tbl)
		for ref := range uc.tables {
			assert.NotContains(t, ref, "__ingestr_load_")
		}
	})
}

func TestSwapTableRecoversInterruptedSwap(t *testing.T) {
	ctx := context.Background()
	oldRef := quoteTableParts("main", "analytics", swapOldTableName("main", "analytics", "orders"))

	t.Run("CDCTargetIncarnation restores a target stranded between swap renames", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{oldRef: {id: "uuid-orig", rows: 10}})
		dest, _ := newTestDatabricksDestination(t, uc.handle)

		inc, exists, err := dest.CDCTargetIncarnation(ctx, "analytics.orders")
		require.NoError(t, err)
		assert.True(t, exists)
		assert.Equal(t, destination.CDCTargetKey("main", "analytics", "orders", "uuid-orig"), inc)
		tbl, ok := uc.getTable(liveOrdersRef)
		require.True(t, ok)
		assert.Equal(t, mockUCTable{id: "uuid-orig", rows: 10}, tbl)
		_, ok = uc.getTable(oldRef)
		assert.False(t, ok)
	})

	t.Run("in-progress swap in this process is not undone", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{oldRef: {id: "uuid-orig", rows: 10}})
		dest, _ := newTestDatabricksDestination(t, uc.handle)
		release := dest.markSwapActive("main", "analytics", "orders")
		defer release()

		_, exists, err := dest.CDCTargetIncarnation(ctx, "analytics.orders")
		require.NoError(t, err)
		assert.False(t, exists)
		_, ok := uc.getTable(oldRef)
		assert.True(t, ok)
	})

	t.Run("SwapTable restores a stranded target before a fenced swap", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{
			oldRef:                                  {id: "uuid-orig", rows: 10},
			"`main`.`ingestr_staging`.`stg_orders`": {id: "uuid-stg", rows: 50},
		})
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   "analytics.orders",
			StagingTable:                  "ingestr_staging.stg_orders",
			CDCExpectedIncarnation:        destination.CDCTargetKey("main", "analytics", "orders", "uuid-orig"),
			CDCExpectedStagingIncarnation: destination.CDCTargetKey("main", "ingestr_staging", "stg_orders", "uuid-stg"),
			CDCExpectedResultIncarnation:  destination.CDCTargetKey("main", "analytics", "orders", "uuid-stg"),
		}))
		tbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-stg", rows: 50, comment: cdcIncarnationCommentMarker + "uuid-stg"}, tbl)
		_, ok := uc.getTable(oldRef)
		assert.False(t, ok)
		assert.Contains(t, strings.Join(*stmts, "\n"), "ALTER TABLE "+liveOrdersRef+" RENAME TO "+oldRef)
	})

	t.Run("SwapTable preserves a leftover from an earlier swap under an orphan name and swaps normally", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{
			liveOrdersRef:                           {id: "uuid-current", rows: 5},
			oldRef:                                  {id: "uuid-previous", rows: 3},
			"`main`.`ingestr_staging`.`stg_orders`": {id: "uuid-stg", rows: 50},
		})
		dest, _ := newTestDatabricksDestination(t, uc.handle)

		require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:  "analytics.orders",
			StagingTable: "ingestr_staging.stg_orders",
		}))
		tbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-stg", rows: 50}, tbl)
		_, ok := uc.getTable(oldRef)
		assert.False(t, ok)
		var orphans []mockUCTable
		for ref, t := range uc.tables {
			if strings.Contains(ref, "`__ingestr_swap_orphan_orders_") {
				orphans = append(orphans, t)
			}
		}
		assert.Equal(t, []mockUCTable{{id: "uuid-previous", rows: 3}}, orphans, "leftover data must never be dropped")
	})

	t.Run("GetTableSchema recovers a target stranded by an interrupted swap", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{oldRef: {id: "uuid-orig", rows: 42}})
		uc.columns[normalizeMockUCRef(oldRef)] = []describedColumn{
			{name: "id", dataType: "bigint"},
			{name: "amount", dataType: "double"},
		}
		// Copy column metadata across RENAME TO so DESCRIBE TABLE sees restored columns.
		uc.onStmt = func(stmt string, state *mockUCState) error {
			if strings.HasPrefix(stmt, "ALTER TABLE ") && strings.Contains(stmt, " RENAME TO ") {
				rest := strings.TrimPrefix(stmt, "ALTER TABLE ")
				parts := strings.SplitN(rest, " RENAME TO ", 2)
				fromRef := normalizeMockUCRef(parts[0])
				toRef := normalizeMockUCRef(parts[1])
				if cols, ok := state.columns[fromRef]; ok {
					state.columns[toRef] = cols
					delete(state.columns, fromRef)
				}
			}
			return nil
		}
		dest, _ := newTestDatabricksDestination(t, uc.handle)

		tblSchema, err := dest.GetTableSchema(ctx, "analytics.orders")
		require.NoError(t, err)
		require.NotNil(t, tblSchema)
		require.Len(t, tblSchema.Columns, 2)
		assert.Equal(t, "id", tblSchema.Columns[0].Name)
		assert.Equal(t, "amount", tblSchema.Columns[1].Name)

		tbl, ok := uc.getTable(liveOrdersRef)
		require.True(t, ok)
		assert.Equal(t, mockUCTable{id: "uuid-orig", rows: 42}, tbl)
		_, ok = uc.getTable(oldRef)
		assert.False(t, ok)
	})

	t.Run("PrepareTable without DropFirst recovers a target stranded by an interrupted swap before CREATE TABLE IF NOT EXISTS", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{oldRef: {id: "uuid-orig", rows: 42}})
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:     "analytics.orders",
			DropFirst: false,
			Schema: &schema.TableSchema{
				Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}},
			},
		}))

		tbl, ok := uc.getTable(liveOrdersRef)
		require.True(t, ok)
		assert.Equal(t, mockUCTable{id: "uuid-orig", rows: 42}, tbl, "restored table and its rows must not be replaced by an empty table")
		_, ok = uc.getTable(oldRef)
		assert.False(t, ok)

		renameIdx, createIdx := -1, -1
		for i, stmt := range *stmts {
			if strings.HasPrefix(stmt, "ALTER TABLE "+oldRef+" RENAME TO "+liveOrdersRef) {
				renameIdx = i
			}
			if strings.HasPrefix(stmt, "CREATE TABLE IF NOT EXISTS "+liveOrdersRef) {
				createIdx = i
			}
		}
		require.NotEqual(t, -1, renameIdx, "must rename stranded swap table back to live target")
		require.NotEqual(t, -1, createIdx, "must still execute CREATE TABLE IF NOT EXISTS")
		assert.Less(t, renameIdx, createIdx, "recovery rename must happen before CREATE TABLE IF NOT EXISTS")
	})
}

func TestEnsureCDCTargetIncarnationAndCreateOrReplaceProtection(t *testing.T) {
	ctx := context.Background()
	cdcCols := []string{"id", "val", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn}
	origCreated := mockCreatedMicros("uuid-orig")
	expected := destination.CDCTargetKey("main", "analytics", "orders", "uuid-orig")

	t.Run("EnsureCDCTargetIncarnation returns false on missing table, preserves user comment prefix, and is idempotent", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{
			liveOrdersRef: {id: "uuid-orig", rows: 5, comment: "Owner's orders table"},
		})
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		missingInc, exists, err := dest.EnsureCDCTargetIncarnation(ctx, "analytics.does_not_exist")
		require.NoError(t, err)
		assert.False(t, exists)
		assert.Empty(t, missingInc)

		inc, exists, err := dest.EnsureCDCTargetIncarnation(ctx, "analytics.orders")
		require.NoError(t, err)
		assert.True(t, exists)
		assert.Equal(t, expected, inc)

		tbl, ok := uc.getTable(liveOrdersRef)
		require.True(t, ok)
		assert.Equal(t, "Owner's orders table __ingestr_cdc_incarnation=uuid-orig", tbl.comment)

		// Second call is a no-op (no COMMENT ON TABLE emitted).
		*stmts = nil
		inc2, exists, err := dest.EnsureCDCTargetIncarnation(ctx, "analytics.orders")
		require.NoError(t, err)
		assert.True(t, exists)
		assert.Equal(t, expected, inc2)
		for _, s := range *stmts {
			assert.False(t, strings.HasPrefix(s, "COMMENT ON TABLE "), "already-stamped table must not be re-commented: %s", s)
		}

		// Subsequent fenced merge also emits zero COMMENT ON TABLE statements and guards with full preserved comment.
		*stmts = nil
		require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
			TargetTable:            "analytics.orders",
			StagingTable:           "ingestr_staging.stg_orders",
			PrimaryKeys:            []string{"id"},
			CDCExpectedIncarnation: expected,
			Columns:                cdcCols,
		}))
		for _, s := range *stmts {
			assert.False(t, strings.HasPrefix(s, "COMMENT ON TABLE "), "steady-state fenced merge must not run COMMENT ON TABLE")
		}
		assert.Contains(t, strings.Join(*stmts, "\n"), "coalesce(comment, '') = 'Owner''s orders table __ingestr_cdc_incarnation=uuid-orig'")
	})

	t.Run("COMMENT ON TABLE racing with an outside replacement is rejected by post-comment DESCRIBE DETAIL check", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-orig", rows: 10}})
		uc.onStmt = func(stmt string, state *mockUCState) error {
			if strings.HasPrefix(stmt, "COMMENT ON TABLE ") {
				state.tables[normalizeMockUCRef(liveOrdersRef)] = mockUCTable{
					id:            "uuid-cor-race",
					rows:          88,
					createdMicros: origCreated,
					comment:       "",
				}
			}
			return nil
		}
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		err := dest.TruncateCDCTableIfIncarnation(ctx, "analytics.orders", expected)
		require.ErrorContains(t, err, "physical incarnation changed")
		assert.Zero(t, countAtomicBlocks(*stmts), "BEGIN ATOMIC must not execute when post-comment check detects replacement")
		tbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, 88, tbl.rows)
	})

	t.Run("external CREATE OR REPLACE TABLE keeping the same created timestamp is rejected by leading guard", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-orig", rows: 10}})
		dest, stmts := newTestDatabricksDestination(t, uc.handle)

		_, exists, err := dest.EnsureCDCTargetIncarnation(ctx, "analytics.orders")
		require.NoError(t, err)
		require.True(t, exists)

		// Simulate CREATE OR REPLACE TABLE right before BEGIN ATOMIC: same createdMicros, new Delta id, cleared comment.
		uc.onStmt = func(stmt string, state *mockUCState) error {
			if strings.HasPrefix(stmt, "BEGIN ATOMIC\n") {
				state.tables[normalizeMockUCRef(liveOrdersRef)] = mockUCTable{
					id:            "uuid-cor-new",
					rows:          77,
					createdMicros: origCreated,
					comment:       "",
				}
			}
			return nil
		}

		err = dest.TruncateCDCTableIfIncarnation(ctx, "analytics.orders", expected)
		require.ErrorContains(t, err, "physical incarnation changed")
		require.ErrorContains(t, err, cdcIncarnationGuardMarker)
		tbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-cor-new", rows: 77, createdMicros: origCreated, comment: ""}, tbl)
		assertLiveTargetNeverRenamed(t, *stmts)
	})

	t.Run("external CREATE OR REPLACE TABLE mid-batch rolls back all tables via trailing guard", func(t *testing.T) {
		custExpected := destination.CDCTargetKey("main", "analytics", "customers", "uuid-cust")
		uc := newMockUCState(map[string]mockUCTable{
			"`main`.`analytics`.`customers`": {id: "uuid-cust", rows: 15},
			liveOrdersRef:                    {id: "uuid-orig", rows: 40},
		})
		dest, _ := newTestDatabricksDestination(t, uc.handle)
		_, _, err := dest.EnsureCDCTargetIncarnation(ctx, "analytics.customers")
		require.NoError(t, err)
		_, _, err = dest.EnsureCDCTargetIncarnation(ctx, "analytics.orders")
		require.NoError(t, err)

		replaced := false
		uc.onAtomicSub = func(sub string, state *mockUCState) {
			if !replaced && strings.HasPrefix(sub, "MERGE INTO "+liveOrdersRef) {
				replaced = true
				// Simulate CREATE OR REPLACE TABLE preserving Unity Catalog created timestamp!
				state.tables[normalizeMockUCRef(liveOrdersRef)] = mockUCTable{
					id:            "uuid-cor-mid",
					rows:          120,
					createdMicros: origCreated,
					comment:       "",
				}
			}
		}

		err = dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            "analytics.customers",
					StagingTable:           "ingestr_staging.stg_customers",
					PrimaryKeys:            []string{"id"},
					CDCExpectedIncarnation: custExpected,
					Columns:                cdcCols,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            "analytics.orders",
					StagingTable:           "ingestr_staging.stg_orders",
					PrimaryKeys:            []string{"id"},
					CDCExpectedIncarnation: expected,
					Columns:                cdcCols,
				},
			},
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.ErrorContains(t, err, cdcIncarnationGuardMarker)
		require.True(t, replaced)

		custTbl, _ := uc.getTable("`main`.`analytics`.`customers`")
		assert.Equal(t, 15, custTbl.rows, "earlier DELETE+MERGE on customers must roll back")
		ordTbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-cor-mid", rows: 120, createdMicros: origCreated, comment: ""}, ordTbl, "CREATE OR REPLACE replacement must remain untouched")
	})

	t.Run("WriteParallel rolls back when external CREATE OR REPLACE TABLE keeps created timestamp", func(t *testing.T) {
		uc := newMockUCState(map[string]mockUCTable{liveOrdersRef: {id: "uuid-orig", rows: 20}})
		dest, _, deleted := newMockUCWriteDestination(t, uc)
		_, _, err := dest.EnsureCDCTargetIncarnation(ctx, "analytics.orders")
		require.NoError(t, err)

		replaced := false
		uc.onAtomicSub = func(sub string, state *mockUCState) {
			if !replaced && strings.HasPrefix(sub, "INSERT INTO ") {
				replaced = true
				state.tables[normalizeMockUCRef(liveOrdersRef)] = mockUCTable{
					id:            "uuid-cor-write",
					rows:          64,
					createdMicros: origCreated,
					comment:       "replacement user comment",
				}
			}
		}

		err = dest.WriteParallel(ctx, singleRowBatch(), destination.WriteOptions{
			Table:                  "analytics.orders",
			CDCExpectedIncarnation: expected,
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.ErrorContains(t, err, cdcIncarnationGuardMarker)
		require.True(t, replaced)
		assert.True(t, deleted.Load())

		ordTbl, _ := uc.getTable(liveOrdersRef)
		assert.Equal(t, mockUCTable{id: "uuid-cor-write", rows: 64, createdMicros: origCreated, comment: "replacement user comment"}, ordTbl)
	})
}

func TestCaseInsensitiveUnityCatalogTargetIdentityAndClaims(t *testing.T) {
	ctx := context.Background()
	uc := newMockUCState(map[string]mockUCTable{
		"`main_cat`.`analytics`.`orders`":            {id: "UUID-MixedCase-123", rows: 5},
		"`other.cat`.`ingestr_staging`.`orders.stg`": {id: "uuid-dotted-stg", rows: 3},
	})
	dest, stmts := newTestDatabricksDestination(t, func(stmt string, req dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error) {
		if strings.HasPrefix(stmt, "SHOW TABLES IN ") {
			return makeQueryResponse([]string{"database", "tableName", "isTemporary"}, [][]string{
				{"ingestr_staging", "analytics__orders_cdc_staging_11111111111111111111111111111111", "false"},
			}), nil
		}
		return uc.handle(stmt, req)
	})
	dest.catalog = "Main_Cat"
	dest.schemaName = "Analytics"

	// 1. ManagedCDCStateCatalog folds catalog to lowercase so connectorIDs and run leases match across catalog casing.
	assert.Equal(t, "main_cat", dest.ManagedCDCStateCatalog())

	// 2. CanonicalCDCTarget rejects unqualified 1-part names, and folds catalog/schema/table case while keeping exact physical ID.
	_, err := dest.CanonicalCDCTarget(ctx, "orders")
	require.Error(t, err)

	expectedCanon := destination.CDCTargetKey("main_cat", "analytics", "orders")
	expectedInc := destination.CDCTargetKey("main_cat", "analytics", "orders", "UUID-MixedCase-123")
	for _, variant := range []string{
		"analytics.orders",
		"Analytics.Orders",
		"`Analytics`.`Orders`",
		"MAIN_CAT.ANALYTICS.ORDERS",
		"`Main_Cat`.`Analytics`.`Orders`",
	} {
		canon, err := dest.CanonicalCDCTarget(ctx, variant)
		require.NoError(t, err, "variant %s", variant)
		assert.Equal(t, expectedCanon, canon, "variant %s", variant)

		inc, exists, err := dest.CDCTargetIncarnation(ctx, variant)
		require.NoError(t, err, "variant %s", variant)
		assert.True(t, exists, "variant %s", variant)
		assert.Equal(t, expectedInc, inc, "variant %s", variant)
	}

	// 3. Claiming `analytics.orders` blocks a second connector from claiming `Analytics.Orders` or `MAIN_CAT.ANALYTICS.ORDERS`.
	require.NoError(t, dest.ClaimCDCTarget(ctx, "ingestr_staging.cdc_targets", destination.CDCTargetClaim{
		DestinationTable: "analytics.orders",
		ConnectorID:      "connector-a",
		SourceTable:      "db.orders",
	}))
	// Idempotent re-claim by the same owner with different casing succeeds.
	require.NoError(t, dest.ClaimCDCTarget(ctx, "ingestr_staging.cdc_targets", destination.CDCTargetClaim{
		DestinationTable: "Analytics.Orders",
		ConnectorID:      "connector-a",
		SourceTable:      "db.orders",
	}))
	// Competing connector using different casing is rejected by the shared marker view.
	err = dest.ClaimCDCTarget(ctx, "ingestr_staging.cdc_targets", destination.CDCTargetClaim{
		DestinationTable: "MAIN_CAT.Analytics.Orders",
		ConnectorID:      "connector-b",
		SourceTable:      "db.orders",
	})
	require.ErrorContains(t, err, "already claimed by CDC connector")

	// 4. ClaimAndPrepareEmptyCDCTarget matches case-variant claim.DestinationTable and opts.Table.
	createdInc, err := dest.ClaimAndPrepareEmptyCDCTarget(
		ctx,
		"ingestr_staging.cdc_targets",
		destination.CDCTargetClaim{
			DestinationTable: "analytics.new_items",
			ConnectorID:      "connector-a",
			SourceTable:      "db.new_items",
		},
		destination.PrepareOptions{
			Table: "Analytics.New_Items",
			Schema: &schema.TableSchema{
				Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}},
			},
		},
	)
	require.NoError(t, err)
	assert.Contains(t, createdInc, destination.CDCTargetKey("main_cat", "analytics", "new_items"))

	// 5. ListStagingTables matches mixed-case prefix against Unity Catalog's lowercase table names.
	staged, err := dest.ListStagingTables(ctx, "ingestr_staging", "Analytics__Orders_cdc_staging_")
	require.NoError(t, err)
	assert.Equal(t, []string{"Analytics__Orders_cdc_staging_11111111111111111111111111111111"}, staged)

	// 6. Cross-catalog swap with dotted identifiers preserves backtick quoting and canonical lowercase keys.
	uc.setTable("`cat.dest`.`sch.dest`.`tbl.dest`", mockUCTable{id: "uuid-dotted-dest", rows: 2})
	*stmts = nil
	stgInc, resInc, err := dest.CDCConditionalSwapIncarnations(ctx, "`Cat.Dest`.`Sch.Dest`.`Tbl.Dest`", "`other.cat`.`ingestr_staging`.`orders.stg`")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(stgInc, destination.CDCTargetKey("cat.dest", "sch.dest")))
	assert.True(t, strings.HasPrefix(resInc, destination.CDCTargetKey("cat.dest", "sch.dest", "tbl.dest")))

	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		TargetTable:                   "`cat.dest`.`sch.dest`.`tbl.dest`",
		StagingTable:                  "`OTHER.CAT`.`ingestr_staging`.`orders.stg`",
		CDCExpectedIncarnation:        destination.CDCTargetKey("cat.dest", "sch.dest", "tbl.dest", "uuid-dotted-dest"),
		CDCExpectedStagingIncarnation: stgInc,
		CDCExpectedResultIncarnation:  resInc,
	}))

	// 7. Synchronous overdue lease check in Err() surfaces ErrConnectorLeaseLost without waiting on monitor tick.
	overdueLease := &databricksCDCRunLease{
		d:       dest,
		catalog: "main_cat",
		schema:  stagingSchema,
		name:    "cdc_lease_overdue",
		done:    make(chan struct{}),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	overdueLease.lastHeartbeat.Store(time.Now().Add(-cdcLeaseTTL).UnixNano())
	require.ErrorIs(t, overdueLease.Err(), source.ErrConnectorLeaseLost)
}

func TestClaimCDCTargetMarkerIsSharedAcrossURICatalogs(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	markers := map[string]string{}
	ownerRe := regexp.MustCompile(`^CREATE VIEW IF NOT EXISTS (\S+) AS SELECT '([^']*)' AS owner_id$`)
	handler := func(stmt string, _ dbsql.ExecuteStatementRequest) (dbsql.StatementResponse, error) {
		mu.Lock()
		defer mu.Unlock()
		if m := ownerRe.FindStringSubmatch(stmt); m != nil {
			if _, ok := markers[m[1]]; !ok {
				markers[m[1]] = m[2]
			}
			return dbsql.StatementResponse{}, nil
		}
		if ref, ok := strings.CutPrefix(stmt, "SELECT owner_id FROM "); ok {
			owner, exists := markers[ref]
			if !exists {
				return dbsql.StatementResponse{}, errors.New("TABLE_OR_VIEW_NOT_FOUND")
			}
			return makeQueryResponse([]string{"owner_id"}, [][]string{{owner}}), nil
		}
		return dbsql.StatementResponse{}, nil
	}

	destA, stmtsA := newTestDatabricksDestination(t, handler)
	destA.catalog = "cat_a"
	destB, _ := newTestDatabricksDestination(t, handler)
	destB.catalog = "cat_b"

	require.NoError(t, destA.ClaimCDCTarget(ctx, "ingestr_staging.cdc_targets", destination.CDCTargetClaim{
		DestinationTable: "shared.analytics.orders",
		ConnectorID:      "connector-a",
		SourceTable:      "db.orders",
	}))
	err := destB.ClaimCDCTarget(ctx, "ingestr_staging.cdc_targets", destination.CDCTargetClaim{
		DestinationTable: "SHARED.Analytics.Orders",
		ConnectorID:      "connector-b",
		SourceTable:      "db.orders",
	})
	require.ErrorContains(t, err, "already claimed by CDC connector")

	require.Len(t, markers, 1)
	for ref := range markers {
		assert.True(t, strings.HasPrefix(ref, "`shared`.`ingestr_staging`.`cdc_claim_"), ref)
	}
	assert.Contains(t, *stmtsA, "CREATE SCHEMA IF NOT EXISTS `shared`.`ingestr_staging`")
}
