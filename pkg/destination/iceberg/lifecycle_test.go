package iceberg

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergio "github.com/apache/iceberg-go/io"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestManagedTableDropPurgesPhysicalFiles(t *testing.T) {
	for _, catalogType := range []string{"hadoop", "sqlite"} {
		t.Run(catalogType, func(t *testing.T) {
			ctx := context.Background()
			warehouse := t.TempDir()
			dest := NewDestination()
			var uri string
			if catalogType == "hadoop" {
				uri = "iceberg+hadoop://?warehouse=" + url.QueryEscape(warehouse)
			} else {
				uri = "iceberg+sqlite://" + filepath.Join(t.TempDir(), "catalog.db") +
					"?warehouse_path=" + url.QueryEscape(warehouse)
			}
			require.NoError(t, dest.Connect(ctx, uri))
			t.Cleanup(func() { require.NoError(t, dest.Close(ctx)) })

			tableName := "lifecycle.purge_staging"
			tableSchema := lifecycleTestSchema()
			require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
				Table:        tableName,
				Schema:       tableSchema,
				DropFirst:    true,
				ExpiresAfter: time.Hour,
			}))
			require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1)), destination.WriteOptions{
				Table:  tableName,
				Schema: tableSchema,
			}))

			tbl, err := dest.loadIcebergTable(ctx, tableName)
			require.NoError(t, err)
			require.Equal(t, "true", tbl.Properties()[managedTableProperty])
			require.Equal(t, managedTableKindStaging, tbl.Properties()[managedTableKindProperty])
			require.Equal(t, "true", tbl.Properties()[gcEnabledProperty])
			_, err = time.Parse(time.RFC3339Nano, tbl.Properties()[managedTableExpiresAt])
			require.NoError(t, err)

			location, ok := localFilesystemPath(tbl.Location())
			require.True(t, ok)
			require.NotEmpty(t, regularFilesUnder(t, location))

			require.NoError(t, dest.DropTable(ctx, tableName))
			exists, err := dest.tableExists(ctx, icebergcatalog.ToIdentifier(tableName))
			require.NoError(t, err)
			require.False(t, exists)
			require.Empty(t, regularFilesUnder(t, location))
		})
	}
}

func TestCleanupExpiredManagedTablesOnlyPurgesExpiredOwnedTables(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableSchema := lifecycleTestSchema()

	writeLifecycleTable(t, dest, "lifecycle.expired", tableSchema, time.Hour)
	writeLifecycleTable(t, dest, "lifecycle.live", tableSchema, 48*time.Hour)
	writeLifecycleTable(t, dest, "lifecycle.user_table", tableSchema, 0)

	expired, err := dest.loadIcebergTable(ctx, "lifecycle.expired")
	require.NoError(t, err)
	expiredLocation, ok := localFilesystemPath(expired.Location())
	require.True(t, ok)

	result, err := dest.cleanupExpiredManagedTables(
		ctx,
		icebergcatalog.ToIdentifier("lifecycle"),
		time.Now().Add(2*time.Hour),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"lifecycle.expired"}, result.Purged)
	require.Empty(t, regularFilesUnder(t, expiredLocation))

	for _, tableName := range []string{"lifecycle.live", "lifecycle.user_table"} {
		exists, err := dest.tableExists(ctx, icebergcatalog.ToIdentifier(tableName))
		require.NoError(t, err)
		require.True(t, exists, tableName)
	}
}

func TestPrepareTableRecreatesExpiredManagedTable(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableSchema := lifecycleTestSchema()
	writeLifecycleTable(t, dest, "lifecycle.reused", tableSchema, time.Hour)

	tbl, err := dest.loadIcebergTable(ctx, "lifecycle.reused")
	require.NoError(t, err)
	txn := tbl.NewTransaction()
	require.NoError(t, txn.SetProperties(map[string]string{
		managedTableExpiresAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:        "lifecycle.reused",
		Schema:       tableSchema,
		DropFirst:    true,
		ExpiresAfter: time.Hour,
	}))
	recreated, err := dest.loadIcebergTable(ctx, "lifecycle.reused")
	require.NoError(t, err)
	require.Nil(t, recreated.CurrentSnapshot())
	expiresAt, managed, err := managedTableExpiration(recreated.Properties())
	require.NoError(t, err)
	require.True(t, managed)
	require.True(t, expiresAt.After(time.Now()))
}

func TestManagedWriteHeartbeatsLeaseWhileSourceIsBlocked(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	dest.leaseHeartbeatInterval = 5 * time.Millisecond
	tableName := "lifecycle.heartbeat"
	tableSchema := lifecycleTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:        tableName,
		Schema:       tableSchema,
		DropFirst:    true,
		ExpiresAfter: time.Minute,
	}))

	before, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	initialExpiration, managed, err := managedTableExpiration(before.Properties())
	require.NoError(t, err)
	require.True(t, managed)

	records := make(chan source.RecordBatchResult)
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- dest.WriteParallel(ctx, records, destination.WriteOptions{
			Table:  tableName,
			Schema: tableSchema,
		})
	}()

	require.Eventually(t, func() bool {
		current, loadErr := dest.loadIcebergTable(ctx, tableName)
		if loadErr != nil {
			return false
		}
		expiresAt, _, expirationErr := managedTableExpiration(current.Properties())
		return expirationErr == nil && expiresAt.After(initialExpiration)
	}, 2*time.Second, 5*time.Millisecond)

	close(records)
	require.NoError(t, <-writeDone)
}

func TestManagedAppendHeartbeatsLeaseWhileSourceIsBlocked(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	dest.leaseHeartbeatInterval = 5 * time.Millisecond
	tableName := "lifecycle.append_heartbeat"
	tableSchema := lifecycleTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: tableName, Schema: tableSchema, ExpiresAfter: time.Minute,
	}))

	before, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	initialExpiration, managed, err := managedTableExpiration(before.Properties())
	require.NoError(t, err)
	require.True(t, managed)

	records := make(chan source.RecordBatchResult)
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- dest.WriteParallel(ctx, records, destination.WriteOptions{Table: tableName, Schema: tableSchema})
	}()
	require.Eventually(t, func() bool {
		current, loadErr := dest.loadIcebergTable(ctx, tableName)
		if loadErr != nil {
			return false
		}
		expiresAt, _, expirationErr := managedTableExpiration(current.Properties())
		return expirationErr == nil && expiresAt.After(initialExpiration)
	}, 2*time.Second, 5*time.Millisecond)
	close(records)
	require.NoError(t, <-writeDone)
}

func TestManagedAppendAbortsWhenFinalLeaseRefreshFails(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	dest.leaseHeartbeatInterval = time.Hour
	tableName := "lifecycle.append_final_refresh"
	tableSchema := lifecycleTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: tableName, Schema: tableSchema, ExpiresAfter: time.Minute,
	}))

	leaseErr := errors.New("injected final lease refresh failure")
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, beforeCommitErrs: []error{leaseErr}}
	err := dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1)), destination.WriteOptions{
		Table: tableName, Schema: tableSchema,
	})
	require.ErrorIs(t, err, leaseErr)
	require.EqualValues(t, 0, icebergRowCount(ctx, t, dest, tableName))
}

func TestManagedWriteSynchronouslyRenewsLeaseBeforeReadingSource(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	dest.leaseHeartbeatInterval = time.Hour
	tableName := "lifecycle.synchronous_lease_refresh"
	tableSchema := lifecycleTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: tableName, Schema: tableSchema, ExpiresAfter: time.Minute,
	}))
	setManagedExpiration(t, dest, tableName, time.Now().Add(-time.Minute))

	records := make(chan source.RecordBatchResult)
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- dest.WriteParallel(ctx, records, destination.WriteOptions{Table: tableName, Schema: tableSchema})
	}()
	require.Eventually(t, func() bool {
		current, err := dest.loadIcebergTable(ctx, tableName)
		if err != nil {
			return false
		}
		expiresAt, _, err := managedTableExpiration(current.Properties())
		return err == nil && expiresAt.After(time.Now())
	}, time.Second, 5*time.Millisecond)
	close(records)
	require.NoError(t, <-writeDone)
}

func TestManagedWriteRejectsPurgeClaimBeforeReadingSource(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableName := "lifecycle.claim_before_read"
	tableSchema := lifecycleTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: tableName, Schema: tableSchema, ExpiresAfter: time.Minute,
	}))
	setLifecycleTableProperty(t, dest, tableName, managedTablePurgeClaim, time.Now().UTC().Format(time.RFC3339Nano))
	records := make(chan source.RecordBatchResult)
	err := dest.WriteParallel(ctx, records, destination.WriteOptions{Table: tableName, Schema: tableSchema})
	require.ErrorContains(t, err, "claimed for purge")
}

func TestS3TablesManagedPurgeDoesNotRequireFilesystemJournal(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "lifecycle.s3tables_server_purge"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	wrapped := &missingRecoveryCatalog{Catalog: dest.catalog, catalogType: icebergcatalog.REST, target: ident, failed: true}
	dest.catalog = wrapped
	documented, err := parseIcebergConfig("iceberg+s3tables://?region=us-east-1&warehouse=" +
		url.QueryEscape("arn:aws:s3tables:us-east-1:123456789012:bucket/analytics"))
	require.NoError(t, err)
	dest.cfg = documented
	require.True(t, dest.usesServerManagedPurge())

	require.NoError(t, dest.DropTable(ctx, tableName))
	require.True(t, wrapped.failed, "server-managed PurgeTable path must be invoked")
	_, err = dest.purgeJournalPath(ident)
	require.ErrorContains(t, err, "requires a filesystem warehouse")
	require.NoError(t, dest.DropTable(ctx, tableName), "missing-table retry must reconcile server-managed purge")
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: tableName, Schema: lifecycleTestSchema()}),
		"idle server-managed ownership must allow later recreation")
}

func TestS3TablesLostPurgeResponseCannotDeleteRecreatedUUID(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "lifecycle.s3tables_recreated_uuid"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	base := dest.catalog
	wrapped := &missingRecoveryCatalog{Catalog: base, catalogType: icebergcatalog.REST, target: ident}
	dest.catalog = wrapped
	documented, err := parseIcebergConfig("iceberg+s3tables://?region=us-east-1&warehouse=" +
		url.QueryEscape("arn:aws:s3tables:us-east-1:123456789012:bucket/analytics"))
	require.NoError(t, err)
	dest.cfg = documented

	err = dest.DropTable(ctx, tableName)
	require.ErrorIs(t, err, errInjectedMissingRecoveryOutcome)
	err = dest.PrepareTable(ctx, destination.PrepareOptions{Table: tableName, Schema: lifecycleTestSchema()})
	require.ErrorContains(t, err, "must be reconciled before recreation")

	iceSchema, err := icebergSchemaFromTableSchema(lifecycleTestSchema())
	require.NoError(t, err)
	properties, err := lifecyclePropertiesForCreate(nil, time.Hour)
	require.NoError(t, err)
	recreated, err := base.CreateTable(ctx, ident, iceSchema, icebergcatalog.WithProperties(properties))
	require.NoError(t, err, "simulate recreation by an external catalog client that bypasses ingestr's guard")
	recreatedUUID := recreated.Metadata().TableUUID()

	err = dest.DropTable(ctx, tableName)
	require.ErrorContains(t, err, "refused stale purge for recreated table")
	live, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)
	require.Equal(t, recreatedUUID, live.Metadata().TableUUID())

	require.NoError(t, dest.DropTable(ctx, tableName), "a new explicit drop may delete the reconciled replacement")
}

func TestS3TablesCreationUsesCatalogGuard(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	dest.catalog = &missingRecoveryCatalog{Catalog: dest.catalog, catalogType: icebergcatalog.REST}
	documented, err := parseIcebergConfig("iceberg+s3tables://?region=us-east-1&warehouse=" +
		url.QueryEscape("arn:aws:s3tables:us-east-1:123456789012:bucket/analytics"))
	require.NoError(t, err)
	dest.cfg = documented
	tableName := "s3guard.created"
	lockIdent := purgeLockIdentifier(icebergcatalog.ToIdentifier(tableName))
	require.NoError(t, validateS3TablesName("table", lockIdent[len(lockIdent)-1]))
	require.NoError(t, dest.catalog.CreateNamespace(ctx, icebergcatalog.ToIdentifier("s3guard"), iceberggo.Properties{}))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: tableName, Schema: lifecycleTestSchema()}))
	idleLock, err := dest.catalog.LoadTable(ctx, lockIdent)
	require.NoError(t, err)
	require.Equal(t, purgeLockModeIdle, idleLock.Properties()[purgeLockModeKey])
}

func TestPrepareTableRejectsExternalCreateRace(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "lifecycle.external_create_race"
	ident := icebergcatalog.ToIdentifier(tableName)
	wrapper := &externalCreateRaceCatalog{Catalog: dest.catalog, target: ident}
	dest.catalog = wrapper

	err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: tableName, Schema: lifecycleTestSchema(), DropFirst: true,
	})
	require.ErrorContains(t, err, "refused concurrent external creation")
	live, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)
	require.Equal(t, wrapper.externalUUID, live.Metadata().TableUUID())
	dest.mu.Lock()
	_, prepared := dest.prepared[tableName]
	dest.mu.Unlock()
	require.False(t, prepared)
}

func TestS3TablesRevalidatesUUIDImmediatelyBeforePurge(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "lifecycle.s3tables_pre_purge_fence"
	tableSchema := lifecycleTestSchema()
	writeLifecycleTable(t, dest, tableName, tableSchema, time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	iceSchema, err := icebergSchemaFromTableSchema(tableSchema)
	require.NoError(t, err)
	properties, err := lifecyclePropertiesForCreate(nil, time.Hour)
	require.NoError(t, err)
	wrapper := &replaceBeforePurgeCatalog{
		Catalog: dest.catalog, target: ident, schema: iceSchema, properties: properties,
	}
	dest.catalog = wrapper
	documented, err := parseIcebergConfig("iceberg+s3tables://?region=us-east-1&warehouse=" +
		url.QueryEscape("arn:aws:s3tables:us-east-1:123456789012:bucket/analytics"))
	require.NoError(t, err)
	dest.cfg = documented

	err = dest.DropTable(ctx, tableName)
	require.ErrorContains(t, err, "refused server-managed purge for recreated table")
	require.Zero(t, wrapper.purgeCalls)
	live, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)
	require.Equal(t, wrapper.recreatedUUID, live.Metadata().TableUUID())
}

func TestDeletionFenceDoesNotDropReplacementCreatedAtRename(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "lifecycle.rename_fenced_replacement"
	tableSchema := lifecycleTestSchema()
	writeLifecycleTable(t, dest, tableName, tableSchema, time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	iceSchema, err := icebergSchemaFromTableSchema(tableSchema)
	require.NoError(t, err)
	properties, err := lifecyclePropertiesForCreate(nil, time.Hour)
	require.NoError(t, err)
	wrapper := &replaceDuringDeletionFenceCatalog{
		Catalog: dest.catalog,
		target:  ident,
		schema:  iceSchema,
		props:   properties,
	}
	dest.catalog = wrapper

	err = dest.DropTable(ctx, tableName)
	require.ErrorContains(t, err, "UUID changed")
	live, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)
	require.Equal(t, wrapper.replacementUUID, live.Metadata().TableUUID())
	exists, err := dest.catalog.CheckTableExists(ctx, wrapper.fencedIdent)
	require.NoError(t, err)
	require.False(t, exists, "mismatched replacement must be restored, never deleted")
}

func TestDeletionFenceFailsClosedBeforeGlueRename(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "lifecycle.glue_non_atomic_rename"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	wrapper := &nonAtomicRenameCatalog{Catalog: dest.catalog}
	dest.catalog = wrapper

	err := dest.DropTable(ctx, tableName)
	require.ErrorContains(t, err, "cannot provide an atomic deletion fence")
	require.Zero(t, wrapper.renameCalls)
	_, err = dest.catalog.LoadTable(ctx, icebergcatalog.ToIdentifier(tableName))
	require.NoError(t, err)
}

func TestS3TablesHeartbeatFailureCancelsBlockingPurgeBeforeTakeover(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "lifecycle.s3tables_cancel_failed_heartbeat"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	dest.catalogLockHeartbeatInterval = 5 * time.Millisecond
	ident := icebergcatalog.ToIdentifier(tableName)
	wrapper := &blockingPurgeHeartbeatFailureCatalog{
		Catalog: dest.catalog, target: ident, purgeStarted: make(chan struct{}), purgeCanceled: make(chan struct{}), failHeartbeat: true,
	}
	dest.catalog = wrapper
	documented, err := parseIcebergConfig("iceberg+s3tables://?region=us-east-1&warehouse=" +
		url.QueryEscape("arn:aws:s3tables:us-east-1:123456789012:bucket/analytics"))
	require.NoError(t, err)
	dest.cfg = documented

	err = dest.DropTable(ctx, tableName)
	require.ErrorIs(t, err, errInjectedPurgeHeartbeatFailure)
	select {
	case <-wrapper.purgeCanceled:
	default:
		t.Fatal("blocking PurgeTable must terminate from cancellation before DropTable returns")
	}
	wrapper.failHeartbeat = false
	lock, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	require.NoError(t, err)
	txn := lock.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		purgeLockExpiresAtKey: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)
	_, err = dest.claimPurgeResume(ctx, ident, lock.Properties()[purgeLockTableUUIDKey])
	require.NoError(t, err, "takeover is safe only after the original purge request has terminated")
}

func TestCreateGuardStopDoesNotReportCanceledInFlightHeartbeat(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	ident := icebergcatalog.ToIdentifier("lifecycle.create_guard_stop_heartbeat")
	require.NoError(t, dest.ensureNamespace(ctx, icebergcatalog.NamespaceFromIdent(ident)))
	wrapper := &cancelOnHeartbeatStopCatalog{
		Catalog:       dest.catalog,
		target:        ident,
		lock:          purgeLockIdentifier(ident),
		renewStarted:  make(chan struct{}),
		renewCanceled: make(chan struct{}),
	}
	wrapper.blockRenew.Store(true)
	dest.catalog = wrapper
	dest.catalogLockHeartbeatInterval = time.Millisecond

	require.NoError(t, dest.createTable(ctx, ident, destination.PrepareOptions{
		Table:        strings.Join(ident, "."),
		Schema:       lifecycleTestSchema(),
		ExpiresAfter: time.Hour,
	}))
	require.NoError(t, ctx.Err())
	select {
	case <-wrapper.renewCanceled:
	default:
		t.Fatal("create guard must join the canceled in-flight heartbeat before returning")
	}
	exists, err := dest.catalog.CheckTableExists(ctx, ident)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestDropUnmanagedTableDoesNotEnableGCOrPurgeDataFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dest := NewDestination()
	uri := "iceberg+sqlite://" + filepath.Join(root, "catalog.db") +
		"?warehouse_path=" + url.QueryEscape(filepath.Join(root, "warehouse"))
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { require.NoError(t, dest.Close(ctx)) })
	table := "lifecycle.unmanaged_drop"
	writeTableRows(t, dest, table, lifecycleTestSchema(), false, [][]any{{int64(1)}})
	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	location, ok := localFilesystemPath(tbl.Location())
	require.True(t, ok)
	filesBefore := regularFilesUnder(t, location)
	require.NotEmpty(t, filesBefore)

	require.NoError(t, dest.DropTable(ctx, table))
	exists, err := dest.tableExists(ctx, icebergcatalog.ToIdentifier(table))
	require.NoError(t, err)
	require.False(t, exists)
	for _, file := range filesBefore {
		require.FileExists(t, file)
	}
}

func TestDropUnmanagedHadoopTableRefusesPhysicalDeletion(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "lifecycle.unmanaged_hadoop_drop"
	writeTableRows(t, dest, table, lifecycleTestSchema(), false, [][]any{{int64(1)}})
	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	location, ok := localFilesystemPath(tbl.Location())
	require.True(t, ok)
	filesBefore := regularFilesUnder(t, location)
	require.NotEmpty(t, filesBefore)

	err = dest.DropTable(ctx, table)
	require.ErrorContains(t, err, "cannot drop unmanaged Hadoop table")
	exists, existsErr := dest.tableExists(ctx, icebergcatalog.ToIdentifier(table))
	require.NoError(t, existsErr)
	require.True(t, exists)
	for _, file := range filesBefore {
		require.FileExists(t, file)
	}
}

func TestExpiredCleanupAbortsWhenLeaseRefreshWinsClaim(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableName := "lifecycle.refresh_race"
	tableSchema := lifecycleTestSchema()
	writeLifecycleTable(t, dest, tableName, tableSchema, time.Hour)

	ident := icebergcatalog.ToIdentifier(tableName)
	now := time.Now().UTC()
	tbl, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)
	txn := tbl.NewTransaction()
	require.NoError(t, txn.SetProperties(map[string]string{
		managedTableExpiresAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)

	base := dest.catalog
	dest.catalog = &refreshBeforePurgeClaimCatalog{
		Catalog: base,
		target:  ident,
		now:     now,
	}
	t.Cleanup(func() { dest.catalog = base })

	result, err := dest.cleanupExpiredManagedTables(ctx, icebergcatalog.ToIdentifier("lifecycle"), now)
	require.NoError(t, err)
	require.Empty(t, result.Purged)
	current, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)
	expiresAt, _, err := managedTableExpiration(current.Properties())
	require.NoError(t, err)
	require.True(t, expiresAt.After(now))
	require.Empty(t, current.Properties()[managedTablePurgeClaim])
}

func TestPurgeClaimPreventsSubsequentLeaseRefresh(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableName := "lifecycle.claimed"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	now := time.Now().UTC().Add(2 * time.Hour)

	claimed, eligible, err := dest.claimExpiredManagedTable(ctx, ident, now)
	require.NoError(t, err)
	require.True(t, eligible)
	require.NotEmpty(t, claimed.Properties()[managedTablePurgeClaim])
	_, eligible, err = dest.claimExpiredManagedTable(ctx, ident, now)
	require.NoError(t, err)
	require.False(t, eligible)
	_, err = dest.renewManagedTableLease(ctx, ident, now.Add(time.Second))
	require.ErrorContains(t, err, "claimed for purge")
	reclaimed, eligible, err := dest.claimExpiredManagedTable(ctx, ident, now.Add(purgeResumeClaimTTL+time.Second))
	require.NoError(t, err)
	require.True(t, eligible, "a crashed purge claimant must be reclaimable after its lease")
	require.NotEqual(t, claimed.Properties()[managedTablePurgeClaim], reclaimed.Properties()[managedTablePurgeClaim])
}

func TestPrepareTableRejectsLegacyExternalFilePathProperty(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableName := "lifecycle.legacy_external_path"
	tableSchema := lifecycleTestSchema()
	writeTableRows(t, dest, tableName, tableSchema, false, [][]any{{int64(1)}})
	setLifecycleTableProperty(t, dest, tableName, icebergtable.WriteDataPathKey, t.TempDir())

	err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:  tableName,
		Schema: tableSchema,
	})
	require.ErrorContains(t, err, icebergtable.WriteDataPathKey)
	require.ErrorContains(t, err, "cannot be isolated")
}

func TestPurgeRejectsAnotherTableWithLegacyExternalFilePath(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableSchema := lifecycleTestSchema()
	target := "lifecycle.safe_drop_target"
	other := "lifecycle.legacy_external_neighbor"
	writeLifecycleTable(t, dest, target, tableSchema, time.Hour)
	writeTableRows(t, dest, other, tableSchema, false, [][]any{{int64(2)}})
	targetTable, err := dest.loadIcebergTable(ctx, target)
	require.NoError(t, err)
	setLifecycleTableProperty(t, dest, other, icebergtable.WriteDataPathKey, targetTable.Location()+"/foreign-data")

	err = dest.DropTable(ctx, target)
	require.ErrorContains(t, err, "cannot verify orphan-cleanup isolation")
	for _, tableName := range []string{target, other} {
		exists, existsErr := dest.tableExists(ctx, icebergcatalog.ToIdentifier(tableName))
		require.NoError(t, existsErr)
		require.True(t, exists, tableName)
	}
}

func TestConfiguredMaintenanceDetachesExpirationCleanupFromCallerCancellation(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableSchema := lifecycleTestSchema()
	activeTable := "lifecycle.cleanup_trigger"
	expiredTable := "lifecycle.cleanup_detached"
	writeLifecycleTable(t, dest, activeTable, tableSchema, 0)
	writeLifecycleTable(t, dest, expiredTable, tableSchema, time.Hour)

	expired, err := dest.loadIcebergTable(ctx, expiredTable)
	require.NoError(t, err)
	txn := expired.NewTransaction()
	require.NoError(t, txn.SetProperties(map[string]string{
		managedTableExpiresAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)

	canceledCtx, cancel := context.WithCancel(context.Background())
	base := dest.catalog
	dest.catalog = &cancelAfterTableLoadCatalog{
		Catalog: base,
		target:  icebergcatalog.ToIdentifier(activeTable),
		cancel:  cancel,
	}
	dest.expirationScans = make(map[string]int64)
	t.Cleanup(func() { dest.catalog = base })

	dest.runConfiguredMaintenance(canceledCtx, activeTable)
	require.ErrorIs(t, canceledCtx.Err(), context.Canceled)
	exists, err := dest.tableExists(context.Background(), icebergcatalog.ToIdentifier(expiredTable))
	require.NoError(t, err)
	require.False(t, exists)
}

func TestPrepareTableRejectsNegativeExpiration(t *testing.T) {
	dest := newHadoopDestination(t)
	err := dest.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:        "lifecycle.bad_ttl",
		Schema:       lifecycleTestSchema(),
		ExpiresAfter: -time.Second,
	})
	require.ErrorContains(t, err, "table expiration must not be negative")
}

func TestPrepareTableRefusesToClaimUnmanagedTableAsStaging(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lifecycle.unmanaged_collision"
	tableSchema := lifecycleTestSchema()
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(7)}})

	err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:        table,
		Schema:       tableSchema,
		DropFirst:    true,
		ExpiresAfter: time.Hour,
	})
	require.ErrorContains(t, err, "refusing to claim existing unmanaged table")

	tbl, loadErr := dest.loadIcebergTable(ctx, table)
	require.NoError(t, loadErr)
	require.Empty(t, tbl.Properties()[managedTableProperty])
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, table))
	require.Equal(t, int64(7), readTableRows(t, dest, table).Rows[0][0])
}

func TestManagedDropDoesNotFallbackAfterUnknownPurge(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	writeLifecycleTable(t, dest, "lifecycle.unknown_purge", lifecycleTestSchema(), time.Hour)

	base := dest.catalog
	failing := &unknownPurgeCatalog{Catalog: base}
	dest.catalog = failing
	err := dest.DropTable(ctx, "lifecycle.unknown_purge")
	require.ErrorContains(t, err, "unknown purge status")
	require.Zero(t, failing.dropCalls)

	dest.catalog = base
	exists, err := dest.tableExists(ctx, icebergcatalog.ToIdentifier("lifecycle.unknown_purge"))
	require.NoError(t, err)
	require.True(t, exists)
}

func TestClientSidePurgePropagatesPhysicalFailureAfterConfirmedDrop(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	writeLifecycleTable(t, dest, "lifecycle.failed_physical_purge", lifecycleTestSchema(), time.Hour)

	base := dest.catalog
	injected := errors.New("injected physical purge failure")
	catalog := &clientSidePurgeCatalog{Catalog: base, purgeErr: injected}
	dest.catalog = catalog
	t.Cleanup(func() { dest.catalog = base })

	err := dest.DropTable(ctx, "lifecycle.failed_physical_purge")
	require.ErrorContains(t, err, "dropped managed table lifecycle.failed_physical_purge")
	require.ErrorIs(t, err, injected)
	require.True(t, catalog.dropConfirmed)
	require.Equal(t, 1, catalog.dropCalls)
	require.Positive(t, catalog.removeCalls)
	require.False(t, catalog.removeBeforeDrop)
}

func TestClientSidePurgeFinishesAfterCallerContextIsCanceledPostDrop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dest := newHadoopDestination(t)
	writeLifecycleTable(t, dest, "lifecycle.detached_physical_purge", lifecycleTestSchema(), time.Hour)

	base := dest.catalog
	catalog := &clientSidePurgeCatalog{Catalog: base, afterDrop: cancel}
	dest.catalog = catalog
	t.Cleanup(func() { dest.catalog = base })

	require.NoError(t, dest.DropTable(ctx, "lifecycle.detached_physical_purge"))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.True(t, catalog.dropConfirmed)
	require.Positive(t, catalog.removeCalls)
	require.False(t, catalog.removeBeforeDrop)
}

func TestClientSidePurgeResumesAfterPartialPhysicalDeletion(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "lifecycle.partial_physical_purge"
	writeLifecycleTable(t, dest, table, lifecycleTestSchema(), time.Hour)

	loaded, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	location, ok := localFilesystemPath(loaded.Location())
	require.True(t, ok)
	require.NotEmpty(t, regularFilesUnder(t, location))

	base := dest.catalog
	catalog := &partialDeleteCatalog{Catalog: base}
	dest.catalog = catalog
	t.Cleanup(func() { dest.catalog = base })

	require.NoError(t, dest.DropTable(ctx, table))
	require.True(t, catalog.fs.failed)
	require.Greater(t, catalog.fs.removeCalls, 1)
	require.Empty(t, regularFilesUnder(t, location))
}

func TestClientSidePurgeDoesNotDeleteFilesAfterUnknownDrop(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	writeLifecycleTable(t, dest, "lifecycle.unknown_drop", lifecycleTestSchema(), time.Hour)

	base := dest.catalog
	unknown := errors.New("unknown catalog drop outcome")
	catalog := &clientSidePurgeCatalog{
		Catalog:  base,
		dropErr:  unknown,
		purgeErr: errors.New("must not be reached"),
	}
	dest.catalog = catalog
	t.Cleanup(func() { dest.catalog = base })

	err := dest.DropTable(ctx, "lifecycle.unknown_drop")
	require.ErrorContains(t, err, "before physical purge")
	require.ErrorIs(t, err, unknown)
	require.False(t, catalog.dropConfirmed)
	require.Equal(t, 1, catalog.dropCalls)
	require.Zero(t, catalog.removeCalls)

	dest.catalog = base
	exists, err := dest.tableExists(ctx, icebergcatalog.ToIdentifier("lifecycle.unknown_drop"))
	require.NoError(t, err)
	require.True(t, exists)
}

type unknownPurgeCatalog struct {
	icebergcatalog.Catalog
	dropCalls int
}

type refreshBeforePurgeClaimCatalog struct {
	icebergcatalog.Catalog
	target      icebergtable.Identifier
	now         time.Time
	targetLoads int
}

type cancelAfterTableLoadCatalog struct {
	icebergcatalog.Catalog
	target   icebergtable.Identifier
	cancel   context.CancelFunc
	canceled bool
}

func (c *cancelAfterTableLoadCatalog) LoadTable(
	ctx context.Context,
	ident icebergtable.Identifier,
) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, ident)
	if err == nil && !c.canceled && slices.Equal(ident, c.target) {
		c.canceled = true
		c.cancel()
	}
	return tbl, err
}

func (c *refreshBeforePurgeClaimCatalog) LoadTable(
	ctx context.Context,
	ident icebergtable.Identifier,
) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, ident)
	if err != nil || !slices.Equal(ident, c.target) {
		return tbl, err
	}
	c.targetLoads++
	if c.targetLoads != 2 {
		return tbl, nil
	}
	_, ttl, _, err := managedTableLease(tbl.Properties())
	if err != nil {
		return nil, err
	}
	return commitManagedTableLease(ctx, tbl, c.now, ttl)
}

func (c *unknownPurgeCatalog) CatalogType() icebergcatalog.Type {
	return icebergcatalog.REST
}

func (c *unknownPurgeCatalog) PurgeTable(context.Context, icebergtable.Identifier) error {
	return errors.New("unknown purge status")
}

func (c *unknownPurgeCatalog) DropTable(ctx context.Context, ident icebergtable.Identifier) error {
	if !strings.HasPrefix(ident[len(ident)-1], purgeLockTablePrefix) {
		c.dropCalls++
	}
	return c.Catalog.DropTable(ctx, ident)
}

type clientSidePurgeCatalog struct {
	icebergcatalog.Catalog
	purgeErr         error
	dropErr          error
	dropCalls        int
	removeCalls      int
	dropConfirmed    bool
	removeBeforeDrop bool
	afterDrop        func()
}

type replaceBeforePurgeCatalog struct {
	icebergcatalog.Catalog
	target        icebergtable.Identifier
	schema        *iceberggo.Schema
	properties    iceberggo.Properties
	targetLoads   int
	purgeCalls    int
	recreatedUUID uuid.UUID
}

type replaceDuringDeletionFenceCatalog struct {
	icebergcatalog.Catalog
	target          icebergtable.Identifier
	schema          *iceberggo.Schema
	props           iceberggo.Properties
	replaced        bool
	replacementUUID uuid.UUID
	fencedIdent     icebergtable.Identifier
}

type nonAtomicRenameCatalog struct {
	icebergcatalog.Catalog
	renameCalls int
}

func (c *nonAtomicRenameCatalog) CatalogType() icebergcatalog.Type { return icebergcatalog.Glue }

func (c *nonAtomicRenameCatalog) RenameTable(
	ctx context.Context,
	from, to icebergtable.Identifier,
) (*icebergtable.Table, error) {
	c.renameCalls++
	return c.Catalog.RenameTable(ctx, from, to)
}

func (c *replaceDuringDeletionFenceCatalog) RenameTable(
	ctx context.Context,
	from, to icebergtable.Identifier,
) (*icebergtable.Table, error) {
	if slices.Equal(from, c.target) && !c.replaced {
		c.replaced = true
		if err := c.DropTable(ctx, from); err != nil {
			return nil, err
		}
		replacement, err := c.CreateTable(ctx, from, c.schema, icebergcatalog.WithProperties(c.props))
		if err != nil {
			return nil, err
		}
		c.replacementUUID = replacement.Metadata().TableUUID()
		c.fencedIdent = append(icebergtable.Identifier(nil), to...)
	}
	return c.Catalog.RenameTable(ctx, from, to)
}

type externalCreateRaceCatalog struct {
	icebergcatalog.Catalog
	target       icebergtable.Identifier
	externalUUID uuid.UUID
	created      bool
}

func (c *externalCreateRaceCatalog) CheckTableExists(ctx context.Context, ident icebergtable.Identifier) (bool, error) {
	if slices.Equal(ident, c.target) && !c.created {
		return false, nil
	}
	return c.Catalog.CheckTableExists(ctx, ident)
}

func (c *externalCreateRaceCatalog) CreateTable(
	ctx context.Context,
	ident icebergtable.Identifier,
	tableSchema *iceberggo.Schema,
	opts ...icebergcatalog.CreateTableOpt,
) (*icebergtable.Table, error) {
	if slices.Equal(ident, c.target) && !c.created {
		c.created = true
		external, err := c.Catalog.CreateTable(ctx, ident, tableSchema, opts...)
		if err != nil {
			return nil, err
		}
		c.externalUUID = external.Metadata().TableUUID()
		return nil, icebergcatalog.ErrTableAlreadyExists
	}
	return c.Catalog.CreateTable(ctx, ident, tableSchema, opts...)
}

var errInjectedPurgeHeartbeatFailure = errors.New("injected purge lock heartbeat failure")

type blockingPurgeHeartbeatFailureCatalog struct {
	icebergcatalog.Catalog
	target        icebergtable.Identifier
	purgeStarted  chan struct{}
	purgeCanceled chan struct{}
	failHeartbeat bool
}

type cancelOnHeartbeatStopCatalog struct {
	icebergcatalog.Catalog
	target        icebergtable.Identifier
	lock          icebergtable.Identifier
	guardCreated  atomic.Bool
	blockRenew    atomic.Bool
	renewStarted  chan struct{}
	renewCanceled chan struct{}
}

func (c *cancelOnHeartbeatStopCatalog) LoadTable(
	ctx context.Context,
	ident icebergtable.Identifier,
) (*icebergtable.Table, error) {
	if slices.Equal(ident, c.lock) && c.guardCreated.Load() && c.blockRenew.CompareAndSwap(true, false) {
		close(c.renewStarted)
		<-ctx.Done()
		close(c.renewCanceled)
		return nil, ctx.Err()
	}
	return c.Catalog.LoadTable(ctx, ident)
}

func (c *cancelOnHeartbeatStopCatalog) CreateTable(
	ctx context.Context,
	ident icebergtable.Identifier,
	tableSchema *iceberggo.Schema,
	opts ...icebergcatalog.CreateTableOpt,
) (*icebergtable.Table, error) {
	if slices.Equal(ident, c.target) {
		<-c.renewStarted
	}
	tbl, err := c.Catalog.CreateTable(ctx, ident, tableSchema, opts...)
	if err == nil && slices.Equal(ident, c.lock) {
		c.guardCreated.Store(true)
	}
	return tbl, err
}

func (c *blockingPurgeHeartbeatFailureCatalog) CatalogType() icebergcatalog.Type {
	return icebergcatalog.REST
}

func (c *blockingPurgeHeartbeatFailureCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, ident)
	if err != nil {
		return nil, err
	}
	fsFactory := func(ctx context.Context) (icebergio.IO, error) { return tbl.FS(ctx) }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *blockingPurgeHeartbeatFailureCatalog) CommitTable(
	ctx context.Context,
	ident icebergtable.Identifier,
	requirements []icebergtable.Requirement,
	updates []icebergtable.Update,
) (icebergtable.Metadata, string, error) {
	select {
	case <-c.purgeStarted:
		if c.failHeartbeat && strings.HasPrefix(ident[len(ident)-1], purgeLockTablePrefix) {
			return nil, "", errInjectedPurgeHeartbeatFailure
		}
	default:
	}
	return c.Catalog.CommitTable(ctx, ident, requirements, updates)
}

func (c *blockingPurgeHeartbeatFailureCatalog) PurgeTable(ctx context.Context, ident icebergtable.Identifier) error {
	if !slices.Equal(ident, c.target) && !isDeletionFenceForTestTarget(ident, c.target) {
		return c.DropTable(ctx, ident)
	}
	close(c.purgeStarted)
	<-ctx.Done()
	close(c.purgeCanceled)
	return ctx.Err()
}

func (c *replaceBeforePurgeCatalog) CatalogType() icebergcatalog.Type { return icebergcatalog.REST }

func (c *replaceBeforePurgeCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	if slices.Equal(ident, c.target) {
		c.targetLoads++
		if c.targetLoads == 2 {
			if err := c.DropTable(ctx, ident); err != nil {
				return nil, err
			}
			recreated, err := c.CreateTable(ctx, ident, c.schema, icebergcatalog.WithProperties(c.properties))
			if err != nil {
				return nil, err
			}
			c.recreatedUUID = recreated.Metadata().TableUUID()
			return recreated, nil
		}
	}
	return c.Catalog.LoadTable(ctx, ident)
}

func (c *replaceBeforePurgeCatalog) PurgeTable(ctx context.Context, ident icebergtable.Identifier) error {
	c.purgeCalls++
	return c.DropTable(ctx, ident)
}

type partialDeleteCatalog struct {
	icebergcatalog.Catalog
	fs      *partialDeleteIO
	dropped bool
}

func (c *partialDeleteCatalog) CatalogType() icebergcatalog.Type {
	return icebergcatalog.SQL
}

func (c *partialDeleteCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	if c.dropped {
		return nil, icebergcatalog.ErrNoSuchTable
	}
	tbl, err := c.Catalog.LoadTable(ctx, ident)
	if err != nil {
		return nil, err
	}
	tableFS, err := tbl.FS(ctx)
	if err != nil {
		return nil, err
	}
	listable, ok := tableFS.(icebergio.ListableIO)
	if !ok {
		return nil, errors.New("test table filesystem is not listable")
	}
	if c.fs == nil {
		c.fs = &partialDeleteIO{ListableIO: listable}
	}
	fsFactory := func(context.Context) (icebergio.IO, error) { return c.fs, nil }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *partialDeleteCatalog) DropTable(context.Context, icebergtable.Identifier) error {
	c.dropped = true
	return nil
}

type partialDeleteIO struct {
	icebergio.ListableIO
	failed      bool
	removeCalls int
}

func (f *partialDeleteIO) Remove(path string) error {
	f.removeCalls++
	err := f.ListableIO.Remove(path)
	if !f.failed {
		f.failed = true
		return errors.Join(err, errors.New("injected failure after deleting one file"))
	}
	return err
}

func (c *clientSidePurgeCatalog) CatalogType() icebergcatalog.Type {
	return icebergcatalog.SQL
}

func (c *clientSidePurgeCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	if c.dropConfirmed {
		return nil, icebergcatalog.ErrNoSuchTable
	}
	tbl, err := c.Catalog.LoadTable(ctx, ident)
	if err != nil {
		return nil, err
	}
	fs, err := tbl.FS(ctx)
	if err != nil {
		return nil, err
	}
	listable, ok := fs.(icebergio.ListableIO)
	if !ok {
		return nil, errors.New("test table filesystem is not listable")
	}
	fsFactory := func(context.Context) (icebergio.IO, error) {
		return &failingPurgeIO{ListableIO: listable, catalog: c}, nil
	}
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *clientSidePurgeCatalog) DropTable(ctx context.Context, ident icebergtable.Identifier) error {
	if strings.HasPrefix(ident[len(ident)-1], purgeLockTablePrefix) {
		return c.Catalog.DropTable(ctx, ident)
	}
	c.dropCalls++
	if c.dropErr != nil {
		return c.dropErr
	}
	c.dropConfirmed = true
	if c.afterDrop != nil {
		c.afterDrop()
	}
	return nil
}

type failingPurgeIO struct {
	icebergio.ListableIO
	catalog *clientSidePurgeCatalog
}

func (f *failingPurgeIO) Remove(path string) error {
	f.catalog.removeCalls++
	if !f.catalog.dropConfirmed {
		f.catalog.removeBeforeDrop = true
	}
	if f.catalog.purgeErr != nil {
		return f.catalog.purgeErr
	}
	return f.ListableIO.Remove(path)
}

func lifecycleTestSchema() *schema.TableSchema {
	return &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
}

func writeLifecycleTable(
	t *testing.T,
	dest *Destination,
	tableName string,
	tableSchema *schema.TableSchema,
	ttl time.Duration,
) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:        tableName,
		Schema:       tableSchema,
		DropFirst:    true,
		ExpiresAfter: ttl,
	}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1)), destination.WriteOptions{
		Table:  tableName,
		Schema: tableSchema,
	}))
}

func setLifecycleTableProperty(t *testing.T, dest *Destination, tableName, key, value string) {
	t.Helper()
	tbl, err := dest.catalog.LoadTable(context.Background(), icebergcatalog.ToIdentifier(tableName))
	require.NoError(t, err)
	txn := tbl.NewTransaction()
	require.NoError(t, txn.SetProperties(map[string]string{key: value}))
	_, err = txn.Commit(context.Background())
	require.NoError(t, err)
}

func regularFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return fs.SkipDir
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	return files
}
