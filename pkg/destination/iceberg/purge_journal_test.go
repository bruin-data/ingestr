package iceberg

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergio "github.com/apache/iceberg-go/io"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gocloud.dev/blob"
	"gocloud.dev/blob/memblob"
)

func TestPurgeJournalObjectStorePersistenceAndNotExist(t *testing.T) {
	ctx := context.Background()
	objectFS, err := icebergio.LoadFS(ctx, map[string]string{}, "mem://purge-journal-tests/warehouse")
	require.NoError(t, err)
	require.Implements(t, (*icebergio.WriteFileIO)(nil), objectFS)
	require.Implements(t, (*icebergio.ListableIO)(nil), objectFS)

	ident := icebergcatalog.ToIdentifier("journal.object_store")
	journal := &purgeJournal{
		Version: purgeJournalVersion, Identifier: ident,
		TableUUID:     "9ed4d13f-a67f-485f-b65d-9dfc260ee765",
		TableLocation: "mem://purge-journal-tests/warehouse/journal/object_store",
		Files:         []string{"mem://purge-journal-tests/warehouse/journal/object_store/data/a.parquet"},
	}
	journalPath := "mem://purge-journal-tests/warehouse/.ingestr/purge-journals/object.json"
	require.NoError(t, writePurgeJournal(objectFS, journalPath, journal))
	read, err := readPurgeJournal(objectFS, journalPath, ident)
	require.NoError(t, err)
	require.Equal(t, journal, read)
	require.NoError(t, removePurgeJournal(objectFS, journalPath))
	require.NoError(t, removePurgeJournal(objectFS, journalPath), "object-store NotExist must be idempotent")
}

func TestRemovePurgeJournalNormalizesGoCloudProviderNotFound(t *testing.T) {
	ctx := context.Background()
	base, err := icebergio.LoadFS(ctx, map[string]string{}, "mem://purge-gocloud-adapter/base")
	require.NoError(t, err)
	bucket := memblob.OpenBucket(nil)
	t.Cleanup(func() { require.NoError(t, bucket.Close()) })
	adapter := &goCloudNotFoundIO{IO: base, bucket: bucket}
	require.NoError(t, removePurgeJournal(adapter, "gs://provider-bucket/missing-journal.json"))
	_, err = readPurgeJournal(adapter, "gs://provider-bucket/missing-journal.json", icebergcatalog.ToIdentifier("missing.table"))
	require.True(t, isObjectNotFound(err))
}

func TestClientSidePurgeJournalResumesAfterRestart(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.resume_after_restart"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)

	base := dest.catalog
	failing := &durableFailureCatalog{Catalog: base}
	dest.catalog = failing
	err := dest.DropTable(ctx, tableName)
	require.ErrorContains(t, err, "failed to purge physical files")
	require.True(t, failing.dropped)

	ident := icebergcatalog.ToIdentifier(tableName)
	journalPath, err := dest.purgeJournalPath(ident)
	require.NoError(t, err)
	localJournal, ok := localFilesystemPath(journalPath)
	require.True(t, ok)
	require.FileExists(t, localJournal)

	dest.catalog = base
	require.NoError(t, dest.DropTable(ctx, tableName), "a retry after restart must resume the durable journal")
	require.NoFileExists(t, localJournal)
	location, ok := dest.localTableLocation(ident)
	require.True(t, ok)
	require.Empty(t, regularFilesUnder(t, location))
}

func TestMissingManagedTableResumesDurablePurgeForRESTAndHadoopCatalogs(t *testing.T) {
	for _, catalogType := range []icebergcatalog.Type{icebergcatalog.REST, icebergcatalog.Hadoop} {
		t.Run(string(catalogType), func(t *testing.T) {
			ctx := context.Background()
			dest := newJournalTestDestination(t)
			tableName := "journal.missing_recovery_" + strings.ToLower(string(catalogType))
			writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
			ident := icebergcatalog.ToIdentifier(tableName)
			tbl, err := dest.loadIcebergTable(ctx, tableName)
			require.NoError(t, err)
			location, ok := localFilesystemPath(tbl.Location())
			require.True(t, ok)
			require.NotEmpty(t, regularFilesUnder(t, location))

			base := dest.catalog
			wrapped := &missingRecoveryCatalog{Catalog: base, catalogType: catalogType, target: ident}
			dest.catalog = wrapped
			err = dest.DropTable(ctx, tableName)
			require.ErrorIs(t, err, errInjectedMissingRecoveryOutcome)
			exists, existsErr := dest.tableExists(ctx, ident)
			require.NoError(t, existsErr)
			require.False(t, exists)
			journalPath, pathErr := dest.purgeJournalPath(ident)
			require.NoError(t, pathErr)
			localJournal, ok := localFilesystemPath(journalPath)
			require.True(t, ok)
			require.FileExists(t, localJournal)

			require.NoError(t, dest.DropTable(ctx, tableName))
			require.NoFileExists(t, localJournal)
			require.Empty(t, regularFilesUnder(t, location))
		})
	}
}

func TestRESTAndHadoopStartupSweepResumeDurablePurge(t *testing.T) {
	for _, catalogType := range []icebergcatalog.Type{icebergcatalog.REST, icebergcatalog.Hadoop} {
		t.Run(string(catalogType), func(t *testing.T) {
			ctx := context.Background()
			dest := newJournalTestDestination(t)
			tableName := "journal.startup_sweep_" + strings.ToLower(string(catalogType))
			writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
			ident := icebergcatalog.ToIdentifier(tableName)
			tbl, err := dest.loadIcebergTable(ctx, tableName)
			require.NoError(t, err)
			location, ok := localFilesystemPath(tbl.Location())
			require.True(t, ok)

			wrapped := &missingRecoveryCatalog{Catalog: dest.catalog, catalogType: catalogType, target: ident}
			dest.catalog = wrapped
			err = dest.DropTable(ctx, tableName)
			require.ErrorIs(t, err, errInjectedMissingRecoveryOutcome)
			require.NoError(t, dest.sweepPurgeJournals(ctx, nil))
			require.Empty(t, regularFilesUnder(t, location))
			journalPath, pathErr := dest.purgeJournalPath(ident)
			require.NoError(t, pathErr)
			localJournal, ok := localFilesystemPath(journalPath)
			require.True(t, ok)
			require.NoFileExists(t, localJournal)
		})
	}
}

func TestFailedRESTAndHadoopDeletionReleasesJournalAndLockForRetry(t *testing.T) {
	for _, catalogType := range []icebergcatalog.Type{icebergcatalog.REST, icebergcatalog.Hadoop} {
		t.Run(string(catalogType), func(t *testing.T) {
			ctx := context.Background()
			dest := newJournalTestDestination(t)
			tableName := "journal.live_delete_failure_" + strings.ToLower(string(catalogType))
			writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
			ident := icebergcatalog.ToIdentifier(tableName)
			wrapped := &liveDeleteFailureCatalog{
				Catalog: dest.catalog, catalogType: catalogType, target: ident, fail: true,
			}
			dest.catalog = wrapped

			err := dest.DropTable(ctx, tableName)
			require.ErrorIs(t, err, errInjectedLiveDeleteFailure)
			_, err = dest.catalog.LoadTable(ctx, ident)
			require.NoError(t, err, "same table UUID must remain live after the injected failure")
			idleLock, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
			require.NoError(t, err)
			require.Equal(t, purgeLockModeIdle, idleLock.Properties()[purgeLockModeKey])
			journalPath, pathErr := dest.purgeJournalPath(ident)
			require.NoError(t, pathErr)
			localJournal, ok := localFilesystemPath(journalPath)
			require.True(t, ok)
			require.NoFileExists(t, localJournal)

			wrapped.fail = false
			require.NoError(t, dest.DropTable(ctx, tableName), "retry must not be wedged by the failed deletion")
			_, err = dest.catalog.LoadTable(ctx, ident)
			require.Error(t, err)
		})
	}
}

func TestInitialPurgeOwnerBlocksRecoveryUntilClaimExpiry(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.initial_owner_exclusive"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	tbl, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)
	_, _, _, _, ownerToken, err := dest.createPurgeJournal(ctx, tbl)
	require.NoError(t, err)
	require.NotEmpty(t, ownerToken)
	require.NoError(t, dest.catalog.DropTable(ctx, ident))

	err = dest.resumePurgeJournal(ctx, ident)
	require.ErrorContains(t, err, "already claimed")
	lock, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	require.NoError(t, err)
	txn := lock.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		purgeLockExpiresAtKey: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)
	require.NoError(t, dest.resumePurgeJournal(ctx, ident))
}

func TestExpiredJournalLessPurgeLockIsReclaimedAfterLiveUUIDCheck(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.expired_without_journal"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	tbl, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)
	_, err = dest.acquirePurgeLock(ctx, ident, tbl.Metadata().TableUUID().String())
	require.NoError(t, err)
	lock, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	require.NoError(t, err)
	txn := lock.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		purgeLockExpiresAtKey: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)

	require.NoError(t, dest.DropTable(ctx, tableName))
	_, err = dest.catalog.LoadTable(ctx, ident)
	require.Error(t, err)
}

func TestDropNamespaceRemovesFencedIdleLifecycleLocks(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "namespace_idle_lock.events"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	require.NoError(t, dest.DropTable(ctx, tableName))
	lockIdent := purgeLockIdentifier(icebergcatalog.ToIdentifier(tableName))
	lock, err := dest.catalog.LoadTable(ctx, lockIdent)
	require.NoError(t, err)
	require.Equal(t, purgeLockModeIdle, lock.Properties()[purgeLockModeKey])

	require.NoError(t, dest.DropNamespace(ctx, "namespace_idle_lock"))
	_, err = dest.catalog.LoadNamespaceProperties(ctx, icebergcatalog.ToIdentifier("namespace_idle_lock"))
	require.Error(t, err)
}

func TestDropNamespaceCleansIdleLocksWithoutDependingOnCatalogErrorText(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "namespace_opaque_error.events"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	require.NoError(t, dest.DropTable(ctx, tableName))
	dest.catalog = &opaqueNamespaceNotEmptyCatalog{Catalog: dest.catalog}

	require.NoError(t, dest.DropNamespace(ctx, "namespace_opaque_error"))
}

func TestDropNamespaceReclaimsExpiredCleanupLock(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	namespace := icebergcatalog.ToIdentifier("namespace_expired_cleanup")
	require.NoError(t, dest.catalog.CreateNamespace(ctx, namespace, iceberggo.Properties{}))
	target := append(append(icebergtable.Identifier(nil), namespace...), "target")
	token, err := dest.acquireCreateGuard(ctx, target)
	require.NoError(t, err)
	require.NoError(t, dest.releaseCreateGuard(ctx, target, token))
	lockIdent := purgeLockIdentifier(target)
	lock, err := dest.catalog.LoadTable(ctx, lockIdent)
	require.NoError(t, err)
	txn := lock.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		purgeLockModeKey:      purgeLockModeCleanup,
		purgeLockTokenKey:     "crashed-owner",
		purgeLockExpiresAtKey: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)

	require.NoError(t, dest.DropNamespace(ctx, strings.Join(namespace, ".")))
	exists, err := dest.catalog.CheckNamespaceExists(ctx, namespace)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestDropNamespaceRollsFailedCleanupFenceBackToIdle(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	namespace := icebergcatalog.ToIdentifier("namespace_cleanup_rollback")
	require.NoError(t, dest.catalog.CreateNamespace(ctx, namespace, iceberggo.Properties{}))
	target := append(append(icebergtable.Identifier(nil), namespace...), "target")
	token, err := dest.acquireCreateGuard(ctx, target)
	require.NoError(t, err)
	require.NoError(t, dest.releaseCreateGuard(ctx, target, token))
	lockIdent := purgeLockIdentifier(target)
	dest.catalog = &failLifecycleLockDropOnceCatalog{Catalog: dest.catalog, target: lockIdent}

	err = dest.DropNamespace(ctx, strings.Join(namespace, "."))
	require.ErrorContains(t, err, "injected lifecycle lock drop failure")
	lock, loadErr := dest.catalog.LoadTable(ctx, lockIdent)
	require.NoError(t, loadErr)
	require.Equal(t, purgeLockModeIdle, lock.Properties()[purgeLockModeKey])
	require.Empty(t, lock.Properties()[purgeLockTokenKey])
	require.Empty(t, lock.Properties()[purgeLockExpiresAtKey])
	require.NoError(t, dest.DropNamespace(ctx, strings.Join(namespace, ".")))
}

func TestCreateGuardReclaimsExpiredCleanupLockButNotLiveCleanup(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	namespace := icebergcatalog.ToIdentifier("namespace_create_cleanup")
	require.NoError(t, dest.catalog.CreateNamespace(ctx, namespace, iceberggo.Properties{}))
	target := append(append(icebergtable.Identifier(nil), namespace...), "target")
	token, err := dest.acquireCreateGuard(ctx, target)
	require.NoError(t, err)
	require.NoError(t, dest.releaseCreateGuard(ctx, target, token))
	lockIdent := purgeLockIdentifier(target)
	lock, err := dest.catalog.LoadTable(ctx, lockIdent)
	require.NoError(t, err)
	txn := lock.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		purgeLockModeKey:      purgeLockModeCleanup,
		purgeLockTokenKey:     "live-cleaner",
		purgeLockExpiresAtKey: time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)
	_, err = dest.acquireCreateGuard(ctx, target)
	require.ErrorContains(t, err, "durable cleanup guard exists")

	lock, err = dest.catalog.LoadTable(ctx, lockIdent)
	require.NoError(t, err)
	txn = lock.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		purgeLockExpiresAtKey: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)
	newToken, err := dest.acquireCreateGuard(ctx, target)
	require.NoError(t, err)
	require.NotEqual(t, "live-cleaner", newToken)
	require.NoError(t, dest.releaseCreateGuard(ctx, target, newToken))
}

func TestExpiredInitialPurgeOwnerWithLiveTableIsAbandonedForRetry(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.expired_live_initial_owner"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	tbl, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)
	_, _, _, journalPath, ownerToken, err := dest.createPurgeJournal(ctx, tbl)
	require.NoError(t, err)
	require.NotEmpty(t, ownerToken)

	err = dest.resumePurgeJournal(ctx, ident)
	require.ErrorContains(t, err, "already claimed")
	lock, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	require.NoError(t, err)
	txn := lock.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		purgeLockExpiresAtKey: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)
	require.NoError(t, dest.resumePurgeJournal(ctx, ident))
	_, err = dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err, "abandoning the crashed pre-deletion attempt must retain the live table")
	idleLock, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	require.NoError(t, err)
	require.Equal(t, purgeLockModeIdle, idleLock.Properties()[purgeLockModeKey])
	localJournal, ok := localFilesystemPath(journalPath)
	require.True(t, ok)
	require.NoFileExists(t, localJournal)
	require.NoError(t, dest.DropTable(ctx, tableName))
}

func TestConnectSweepsDurablePurgeJournal(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	uri := journalTestURI(root)
	dest := NewDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	tableName := "journal.connect_sweep"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)

	base := dest.catalog
	failing := &durableFailureCatalog{Catalog: base}
	dest.catalog = failing
	require.Error(t, dest.DropTable(ctx, tableName))
	dest.catalog = base
	require.NoError(t, dest.Close(ctx))

	restarted := NewDestination()
	require.NoError(t, restarted.Connect(ctx, uri), "Connect must sweep confirmed-absent purge journals")
	t.Cleanup(func() { require.NoError(t, restarted.Close(context.Background())) })
	location, ok := restarted.localTableLocation(icebergcatalog.ToIdentifier(tableName))
	require.True(t, ok)
	require.Empty(t, regularFilesUnder(t, location))
	journalPath, err := restarted.purgeJournalPath(icebergcatalog.ToIdentifier(tableName))
	require.NoError(t, err)
	localJournal, ok := localFilesystemPath(journalPath)
	require.True(t, ok)
	require.NoFileExists(t, localJournal)
}

func TestClientSidePurgeJournalAbandonsCleanupWhenIdentifierIsReused(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.reused_identifier"
	tableSchema := lifecycleTestSchema()
	writeLifecycleTable(t, dest, tableName, tableSchema, time.Hour)

	base := dest.catalog
	failing := &durableFailureCatalog{Catalog: base}
	dest.catalog = failing
	require.Error(t, dest.DropTable(ctx, tableName))

	dest.catalog = base
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: tableName, Schema: tableSchema}))
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 99)), destination.WriteOptions{
		Table: tableName, Schema: tableSchema,
	}))
	recreated, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	require.NotEqual(t, failing.tableUUID, recreated.Metadata().TableUUID().String())

	require.NoError(t, dest.resumePurgeJournal(ctx, icebergcatalog.ToIdentifier(tableName)))
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, tableName))
	require.Equal(t, int64(99), readTableRows(t, dest, tableName).Rows[0][0])
	journalPath, err := dest.purgeJournalPath(icebergcatalog.ToIdentifier(tableName))
	require.NoError(t, err)
	localJournal, ok := localFilesystemPath(journalPath)
	require.True(t, ok)
	require.NoFileExists(t, localJournal)
}

func TestValidatePurgeJournalRejectsInvalidUUIDAndEscapingPaths(t *testing.T) {
	ident := icebergcatalog.ToIdentifier("journal.validation")
	journal := &purgeJournal{
		Version:       purgeJournalVersion,
		Identifier:    ident,
		TableUUID:     "not-a-uuid",
		TableLocation: "/warehouse/journal/validation",
		Files:         []string{"/warehouse/journal/validation/data/file.parquet"},
	}
	require.ErrorContains(t, validatePurgeJournal(journal, ident), "invalid table UUID")

	journal.TableUUID = "9ed4d13f-a67f-485f-b65d-9dfc260ee765"
	journal.Files = []string{"/warehouse/journal/validation-neighbor/data/file.parquet"}
	require.ErrorContains(t, validatePurgeJournal(journal, ident), "outside table location")

	journal.TableLocation = "s3://bucket/warehouse/table"
	journal.Files = []string{"s3://other-bucket/warehouse/table/data/file.parquet"}
	require.ErrorContains(t, validatePurgeJournal(journal, ident), "outside table location")

	journal.TableLocation = "file:/warehouse/journal/validation"
	journal.Files = []string{"/warehouse/journal/validation/data/file.parquet"}
	require.NoError(t, validatePurgeJournal(journal, ident))
	journal.Files = []string{"/warehouse/journal/validation-neighbor/data/file.parquet"}
	require.ErrorContains(t, validatePurgeJournal(journal, ident), "outside table location")
}

func TestResumePurgeJournalRefusesSameUUIDAndRetainsJournal(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.same_uuid"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	tbl, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	journal, _, journalFS, journalPath, _, err := dest.createPurgeJournal(ctx, tbl)
	require.NoError(t, err)

	err = dest.resumePurgeJournal(ctx, icebergcatalog.ToIdentifier(tableName))
	require.ErrorContains(t, err, "still exists")
	_, err = readPurgeJournal(journalFS, journalPath, journal.Identifier)
	require.NoError(t, err)
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, tableName))
}

func TestResumePurgeJournalRetainsCorruptJournalWithoutDeletingLiveTable(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.corrupt_live"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	journalPath, err := dest.purgeJournalPath(ident)
	require.NoError(t, err)
	localPath, ok := localFilesystemPath(journalPath)
	require.True(t, ok)
	require.NoError(t, ensureJournalDirectory(localPath))
	require.NoError(t, os.WriteFile(localPath, []byte("not json"), 0o600))

	err = dest.resumePurgeJournal(ctx, ident)
	require.ErrorContains(t, err, "exists; refusing corrupt")
	require.FileExists(t, localPath)
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, tableName))
}

func TestPurgeJournalRetriesTransientDeleteAndRemovesLateResidue(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.transient_and_residue"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)

	base := dest.catalog
	wrapped := &transientResidueCatalog{Catalog: base}
	dest.catalog = wrapped
	require.NoError(t, dest.DropTable(ctx, tableName))
	require.True(t, wrapped.failedOnce)
	require.True(t, wrapped.wroteResidue)
	location, ok := localFilesystemPath(wrapped.location)
	require.True(t, ok)
	require.Empty(t, regularFilesUnder(t, location))
}

func TestResumePurgeJournalRecoversCrashAfterDeletionFenceRename(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.crash_after_deletion_fence"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	tbl, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)
	journal, _, _, _, token, err := dest.createPurgeJournal(ctx, tbl)
	require.NoError(t, err)
	claimed, err := dest.claimTableForDeletionAt(ctx, tbl, journal.TableUUID, nil, journal.DeletionIdentifiers[0])
	require.NoError(t, err)
	require.NoError(t, dest.relinquishPurgeClaim(ctx, ident, journal.TableUUID, token))

	require.NoError(t, dest.resumePurgeJournal(ctx, ident))
	for _, deletionIdent := range journal.DeletionIdentifiers {
		exists, checkErr := dest.catalog.CheckTableExists(ctx, deletionIdent)
		require.NoError(t, checkErr)
		require.False(t, exists)
	}
	exists, err := dest.catalog.CheckTableExists(ctx, claimed.Identifier())
	require.NoError(t, err)
	require.False(t, exists)
	for listed, listErr := range dest.catalog.ListTables(ctx, icebergcatalog.NamespaceFromIdent(ident)) {
		require.NoError(t, listErr)
		require.NotContains(t, listed[len(listed)-1], deletionFenceTablePrefix)
	}
	journalPath, err := dest.purgeJournalPath(ident)
	require.NoError(t, err)
	localJournalPath, ok := localFilesystemPath(journalPath)
	require.True(t, ok)
	require.NoFileExists(t, localJournalPath)
}

func TestPurgeJournalMissingWarehouseIsReported(t *testing.T) {
	dest := &Destination{cfg: icebergConfig{CatalogName: "test"}}
	_, err := dest.purgeJournalPath(icebergcatalog.ToIdentifier("journal.missing_warehouse"))
	require.ErrorContains(t, err, "requires a filesystem warehouse")
}

func TestCatalogPurgeLockBlocksRecreationUntilReleased(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.locked_recreation"
	tableSchema := lifecycleTestSchema()
	writeLifecycleTable(t, dest, tableName, tableSchema, time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	tbl, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	tableUUID := tbl.Metadata().TableUUID().String()
	lockToken, err := dest.acquirePurgeLock(ctx, ident, tableUUID)
	require.NoError(t, err)
	_, err = dest.acquirePurgeLock(ctx, ident, tableUUID)
	require.ErrorContains(t, err, "already held")
	require.NoError(t, dest.catalog.DropTable(ctx, ident))

	err = dest.PrepareTable(ctx, destination.PrepareOptions{Table: tableName, Schema: tableSchema})
	require.ErrorContains(t, err, "durable purge lock")
	require.NoError(t, dest.releasePurgeLockOwned(ctx, ident, tableUUID, lockToken))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: tableName, Schema: tableSchema}))
}

func TestPurgeRecoveryClaimIsExclusiveAndCanTakeOverAfterExpiry(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.exclusive_recovery_claim"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	tbl, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	tableUUID := tbl.Metadata().TableUUID().String()
	firstToken, err := dest.acquirePurgeLock(ctx, ident, tableUUID)
	require.NoError(t, err)
	require.NotEmpty(t, firstToken)
	_, err = dest.claimPurgeResume(ctx, ident, tableUUID)
	require.ErrorContains(t, err, "already claimed")

	lock, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	require.NoError(t, err)
	txn := lock.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		purgeLockExpiresAtKey: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)
	secondToken, err := dest.claimPurgeResume(ctx, ident, tableUUID)
	require.NoError(t, err)
	require.NotEqual(t, firstToken, secondToken)
	require.ErrorContains(t, dest.releasePurgeLockOwned(ctx, ident, tableUUID, firstToken), "ownership changed")
	require.NoError(t, dest.releasePurgeLockOwned(ctx, ident, tableUUID, secondToken))
}

func TestExpiredCreateGuardCanBeReclaimed(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	ident := icebergcatalog.ToIdentifier("journal.expired_create_guard")
	require.NoError(t, dest.catalog.CreateNamespace(ctx, icebergcatalog.ToIdentifier("journal"), iceberggo.Properties{}))
	firstToken, err := dest.acquireCreateGuard(ctx, ident)
	require.NoError(t, err)
	lock, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	require.NoError(t, err)
	txn := lock.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		purgeLockExpiresAtKey: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)

	secondToken, err := dest.acquireCreateGuard(ctx, ident)
	require.NoError(t, err)
	require.NotEqual(t, firstToken, secondToken)
	require.ErrorContains(t, dest.releaseCreateGuard(ctx, ident, firstToken), "ownership changed")
	require.NoError(t, dest.releaseCreateGuard(ctx, ident, secondToken))
}

func TestCreateGuardOldOwnerCannotReleaseReclaimedGuard(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	ident := icebergcatalog.ToIdentifier("journal.create_guard_release_fence")
	require.NoError(t, dest.catalog.CreateNamespace(ctx, icebergcatalog.ToIdentifier("journal"), iceberggo.Properties{}))
	oldToken, err := dest.acquireCreateGuard(ctx, ident)
	require.NoError(t, err)
	lock, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	require.NoError(t, err)
	txn := lock.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		purgeLockExpiresAtKey: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)
	wrapped := &conflictFirstOfTwoCatalog{
		Catalog: dest.catalog, firstReady: make(chan struct{}), secondCommitted: make(chan struct{}),
	}
	dest.catalog = wrapped
	oldRelease := make(chan error, 1)
	go func() { oldRelease <- dest.releaseCreateGuard(ctx, ident, oldToken) }()
	<-wrapped.firstReady
	newToken, err := dest.acquireCreateGuard(ctx, ident)
	require.NoError(t, err)
	require.ErrorIs(t, <-oldRelease, icebergtable.ErrCommitFailed)
	current, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	require.NoError(t, err)
	require.Equal(t, newToken, current.Properties()[purgeLockTokenKey])
	require.NoError(t, dest.releaseCreateGuard(ctx, ident, newToken))
}

func TestPurgeLockOldOwnerCannotReleaseReclaimedLock(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.purge_release_fence"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	tbl, err := dest.catalog.LoadTable(ctx, ident)
	require.NoError(t, err)
	tableUUID := tbl.Metadata().TableUUID().String()
	oldToken, err := dest.acquirePurgeLock(ctx, ident, tableUUID)
	require.NoError(t, err)
	lock, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	require.NoError(t, err)
	txn := lock.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		purgeLockExpiresAtKey: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)
	wrapped := &conflictFirstOfTwoCatalog{
		Catalog: dest.catalog, firstReady: make(chan struct{}), secondCommitted: make(chan struct{}),
	}
	dest.catalog = wrapped
	oldRelease := make(chan error, 1)
	go func() { oldRelease <- dest.releasePurgeLockOwned(ctx, ident, tableUUID, oldToken) }()
	<-wrapped.firstReady
	newToken, err := dest.claimPurgeResume(ctx, ident, tableUUID)
	require.NoError(t, err)
	require.ErrorIs(t, <-oldRelease, icebergtable.ErrCommitFailed)
	current, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	require.NoError(t, err)
	require.Equal(t, newToken, current.Properties()[purgeLockTokenKey])
	require.NoError(t, dest.releasePurgeLockOwned(ctx, ident, tableUUID, newToken))
}

func TestExpiredCreateGuardIsNotReclaimedWhilePurgeJournalExists(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	ident := icebergcatalog.ToIdentifier("journal.guarded_by_purge_journal")
	require.NoError(t, dest.catalog.CreateNamespace(ctx, icebergcatalog.ToIdentifier("journal"), iceberggo.Properties{}))
	_, err := dest.acquireCreateGuard(ctx, ident)
	require.NoError(t, err)
	lock, err := dest.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	require.NoError(t, err)
	txn := lock.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		purgeLockExpiresAtKey: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(ctx)
	require.NoError(t, err)
	journalPath, err := dest.purgeJournalPath(ident)
	require.NoError(t, err)
	journalFS, err := icebergio.LoadFS(ctx, dest.cfg.Properties, journalPath)
	require.NoError(t, err)
	require.NoError(t, writePurgeJournal(journalFS, journalPath, &purgeJournal{
		Version: purgeJournalVersion, Identifier: ident, TableUUID: uuid.NewString(),
		TableLocation: dest.configuredPurgeWarehouse() + "/journal/guarded_by_purge_journal",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}))

	_, err = dest.acquireCreateGuard(ctx, ident)
	require.ErrorContains(t, err, "durable purge journal exists")
}

func TestCatalogCreateGuardBlocksPurgeUntilReleased(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	tableName := "journal.create_guard_blocks_purge"
	writeLifecycleTable(t, dest, tableName, lifecycleTestSchema(), time.Hour)
	ident := icebergcatalog.ToIdentifier(tableName)
	token, err := dest.acquireCreateGuard(ctx, ident)
	require.NoError(t, err)
	tbl, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	_, _, _, _, _, err = dest.createPurgeJournal(ctx, tbl)
	require.ErrorContains(t, err, "conflicting purge lock")
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, tableName))
	require.NoError(t, dest.releaseCreateGuard(ctx, ident, token))
}

func TestSweepRejectsJournalFilenameHashMismatch(t *testing.T) {
	ctx := context.Background()
	dest := newJournalTestDestination(t)
	ident := icebergcatalog.ToIdentifier("journal.hash_mismatch")
	root, err := dest.purgeJournalRoot(ident)
	require.NoError(t, err)
	wrongPath := appendLocationPath(root, "wrong.json", false)
	journalFS, err := icebergio.LoadFS(ctx, dest.cfg.Properties, wrongPath)
	require.NoError(t, err)
	journal := &purgeJournal{
		Version: purgeJournalVersion, Identifier: ident,
		TableUUID:     "9ed4d13f-a67f-485f-b65d-9dfc260ee765",
		TableLocation: filepath.Join(dest.cfg.Properties["warehouse"], "journal", "hash_mismatch"),
	}
	require.NoError(t, writePurgeJournal(journalFS, wrongPath, journal))
	err = dest.sweepPurgeJournals(ctx, nil)
	require.ErrorContains(t, err, "filename/hash mismatch")
	localPath, ok := localFilesystemPath(wrongPath)
	require.True(t, ok)
	require.FileExists(t, localPath)
}

func TestValidatePurgeJournalRejectsLocationOutsideConfiguredWarehouse(t *testing.T) {
	dest := &Destination{cfg: icebergConfig{Properties: map[string]string{"warehouse": "/safe/warehouse"}}}
	journal := &purgeJournal{
		Identifier:    icebergcatalog.ToIdentifier("journal.forged"),
		TableLocation: "/attacker/root",
	}
	require.ErrorContains(t, dest.validatePurgeJournalLocation(journal), "outside configured warehouse")
	journal.TableLocation = "/safe/warehouse"
	require.ErrorContains(t, dest.validatePurgeJournalLocation(journal), "outside configured warehouse")
}

func TestValidatePurgeJournalRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	warehouse := filepath.Join(root, "warehouse")
	outside := filepath.Join(root, "outside")
	require.NoError(t, os.MkdirAll(warehouse, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "table"), 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(warehouse, "escape")))
	dest := &Destination{cfg: icebergConfig{Properties: map[string]string{"warehouse": warehouse}}}
	journal := &purgeJournal{TableLocation: filepath.Join(warehouse, "escape", "table")}
	require.ErrorContains(t, dest.validatePurgeJournalLocation(journal), "escapes warehouse")
}

func TestReadBoundedIOFileRejectsOversizedJournal(t *testing.T) {
	ctx := context.Background()
	objectFS, err := icebergio.LoadFS(ctx, map[string]string{}, "mem://purge-bounds/root")
	require.NoError(t, err)
	writer := objectFS.(icebergio.WriteFileIO)
	path := "mem://purge-bounds/root/oversized.json"
	require.NoError(t, writer.WriteFile(path, []byte("01234567890")))
	_, err = readBoundedIOFile(objectFS, path, 10)
	require.ErrorContains(t, err, "size limit")
}

func newJournalTestDestination(t *testing.T) *Destination {
	t.Helper()
	root := t.TempDir()
	dest := NewDestination()
	uri := journalTestURI(root)
	require.NoError(t, dest.Connect(context.Background(), uri))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
	return dest
}

func journalTestURI(root string) string {
	return "iceberg+sqlite://" + filepath.Join(root, "catalog.db") +
		"?warehouse_path=" + url.QueryEscape(filepath.Join(root, "warehouse"))
}

type durableFailureCatalog struct {
	icebergcatalog.Catalog
	dropped   bool
	tableUUID string
}

type opaqueNamespaceNotEmptyCatalog struct {
	icebergcatalog.Catalog
	failed bool
}

type failLifecycleLockDropOnceCatalog struct {
	icebergcatalog.Catalog
	target icebergtable.Identifier
	failed bool
}

func (c *failLifecycleLockDropOnceCatalog) DropTable(ctx context.Context, ident icebergtable.Identifier) error {
	if slices.Equal(ident, c.target) && !c.failed {
		c.failed = true
		return errors.New("injected lifecycle lock drop failure")
	}
	return c.Catalog.DropTable(ctx, ident)
}

func (c *opaqueNamespaceNotEmptyCatalog) DropNamespace(ctx context.Context, ident icebergtable.Identifier) error {
	if !c.failed {
		c.failed = true
		return errors.New("opaque catalog failure")
	}
	return c.Catalog.DropNamespace(ctx, ident)
}

var errInjectedMissingRecoveryOutcome = errors.New("injected lost purge response after catalog removal")

var errInjectedLiveDeleteFailure = errors.New("injected catalog deletion failure while table remains live")

type missingRecoveryCatalog struct {
	icebergcatalog.Catalog
	catalogType icebergcatalog.Type
	target      icebergtable.Identifier
	failed      bool
}

type liveDeleteFailureCatalog struct {
	icebergcatalog.Catalog
	catalogType icebergcatalog.Type
	target      icebergtable.Identifier
	fail        bool
}

func (c *liveDeleteFailureCatalog) CatalogType() icebergcatalog.Type { return c.catalogType }

func (c *liveDeleteFailureCatalog) PurgeTable(ctx context.Context, ident icebergtable.Identifier) error {
	if c.fail && (slices.Equal(ident, c.target) || isDeletionFenceForTestTarget(ident, c.target)) {
		return errInjectedLiveDeleteFailure
	}
	return c.Catalog.DropTable(ctx, ident)
}

func (c *liveDeleteFailureCatalog) DropTable(ctx context.Context, ident icebergtable.Identifier) error {
	if c.fail && (slices.Equal(ident, c.target) || isDeletionFenceForTestTarget(ident, c.target)) {
		return errInjectedLiveDeleteFailure
	}
	return c.Catalog.DropTable(ctx, ident)
}

func (c *missingRecoveryCatalog) CatalogType() icebergcatalog.Type { return c.catalogType }

func (c *missingRecoveryCatalog) PurgeTable(ctx context.Context, ident icebergtable.Identifier) error {
	return c.dropTargetWithUnknownOutcome(ctx, ident)
}

func (c *missingRecoveryCatalog) DropTable(ctx context.Context, ident icebergtable.Identifier) error {
	if c.catalogType == icebergcatalog.Hadoop && (slices.Equal(ident, c.target) || isDeletionFenceForTestTarget(ident, c.target)) {
		return c.dropTargetWithUnknownOutcome(ctx, ident)
	}
	return c.Catalog.DropTable(ctx, ident)
}

func (c *missingRecoveryCatalog) dropTargetWithUnknownOutcome(ctx context.Context, ident icebergtable.Identifier) error {
	if err := c.Catalog.DropTable(ctx, ident); err != nil {
		return err
	}
	if !c.failed && (slices.Equal(ident, c.target) || isDeletionFenceForTestTarget(ident, c.target)) {
		c.failed = true
		return errInjectedMissingRecoveryOutcome
	}
	return nil
}

func isDeletionFenceForTestTarget(ident, target icebergtable.Identifier) bool {
	return len(ident) == len(target) && slices.Equal(icebergcatalog.NamespaceFromIdent(ident), icebergcatalog.NamespaceFromIdent(target)) &&
		strings.HasPrefix(ident[len(ident)-1], deletionFenceTablePrefix)
}

func (c *durableFailureCatalog) CatalogType() icebergcatalog.Type {
	return icebergcatalog.SQL
}

func (c *durableFailureCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, ident)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(ident[len(ident)-1], purgeLockTablePrefix) {
		return tbl, nil
	}
	c.tableUUID = tbl.Metadata().TableUUID().String()
	tableFS, err := tbl.FS(ctx)
	if err != nil {
		return nil, err
	}
	listable, ok := tableFS.(icebergio.ListableIO)
	if !ok {
		return nil, errors.New("test filesystem is not listable")
	}
	failing := &alwaysFailRemoveIO{ListableIO: listable}
	fsFactory := func(context.Context) (icebergio.IO, error) { return failing, nil }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *durableFailureCatalog) DropTable(ctx context.Context, ident icebergtable.Identifier) error {
	if err := c.Catalog.DropTable(ctx, ident); err != nil {
		return err
	}
	c.dropped = true
	return nil
}

type alwaysFailRemoveIO struct {
	icebergio.ListableIO
}

type goCloudNotFoundIO struct {
	icebergio.IO
	bucket *blob.Bucket
}

func (g *goCloudNotFoundIO) Remove(string) error {
	return g.bucket.Delete(context.Background(), "missing")
}

func (g *goCloudNotFoundIO) Open(string) (icebergio.File, error) {
	_, err := g.bucket.Attributes(context.Background(), "missing")
	return nil, err
}

type transientResidueCatalog struct {
	icebergcatalog.Catalog
	fs           icebergio.ListableIO
	location     string
	failedOnce   bool
	wroteResidue bool
	target       icebergtable.Identifier
}

func (c *transientResidueCatalog) CatalogType() icebergcatalog.Type { return icebergcatalog.SQL }

func (c *transientResidueCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, ident)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(ident[len(ident)-1], purgeLockTablePrefix) {
		return tbl, nil
	}
	tableFS, err := tbl.FS(ctx)
	if err != nil {
		return nil, err
	}
	listable, ok := tableFS.(icebergio.ListableIO)
	if !ok {
		return nil, errors.New("test filesystem is not listable")
	}
	c.fs = listable
	c.location = tbl.Location()
	if len(c.target) == 0 {
		c.target = append(icebergtable.Identifier(nil), ident...)
	}
	wrapped := &transientRemoveIO{ListableIO: listable, catalog: c}
	fsFactory := func(context.Context) (icebergio.IO, error) { return wrapped, nil }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *transientResidueCatalog) DropTable(ctx context.Context, ident icebergtable.Identifier) error {
	if err := c.Catalog.DropTable(ctx, ident); err != nil {
		return err
	}
	if !slices.Equal(ident, c.target) && !isDeletionFenceForTestTarget(ident, c.target) {
		return nil
	}
	writer, ok := c.fs.(icebergio.WriteFileIO)
	if !ok {
		return errors.New("test filesystem is not writable")
	}
	c.wroteResidue = true
	return writer.WriteFile(appendLocationPath(c.location, "data/late.parquet", false), []byte("late"))
}

type transientRemoveIO struct {
	icebergio.ListableIO
	catalog *transientResidueCatalog
}

func (f *transientRemoveIO) Remove(filePath string) error {
	if !f.catalog.failedOnce {
		f.catalog.failedOnce = true
		return errors.New("injected transient delete failure")
	}
	return f.ListableIO.Remove(filePath)
}

func (f *alwaysFailRemoveIO) Remove(path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return err
	}
	return errors.New("injected durable cleanup interruption")
}
