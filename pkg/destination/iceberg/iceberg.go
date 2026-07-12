package iceberg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	_ "github.com/apache/iceberg-go/catalog/glue"
	_ "github.com/apache/iceberg-go/catalog/hadoop"
	_ "github.com/apache/iceberg-go/catalog/hive"
	_ "github.com/apache/iceberg-go/catalog/sql"
	_ "github.com/apache/iceberg-go/io/gocloud"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

const prepareOwnershipProperty = "ingestr.prepare-owner"

const (
	defaultManagedStagingNamespace  = "_bruin_staging"
	s3TablesManagedStagingNamespace = "bruin_staging"
)

type preparedTable struct {
	schema           *schema.TableSchema
	replace          bool
	preserveMetadata bool
	evolveSchema     bool
	partitionBy      string
	clusterBy        []string
}

type Destination struct {
	cfg                          icebergConfig
	catalog                      icebergcatalog.Catalog
	leaseHeartbeatInterval       time.Duration
	catalogLockHeartbeatInterval time.Duration

	mu       sync.Mutex
	prepared map[string]preparedTable
	// expirationScans throttles best-effort catalog scans that reclaim
	// abandoned managed staging tables. It is guarded by mu.
	expirationScans map[string]int64
}

func (*Destination) SupportsIdempotentCommitTokenWrites() bool {
	return true
}

func NewDestination() *Destination {
	return &Destination{}
}

func (d *Destination) Schemes() []string {
	return []string{"iceberg", "iceberg+rest", "iceberg+nessie", "iceberg+polaris", "iceberg+s3tables", "iceberg+glue", "iceberg+hive", "iceberg+hadoop", "iceberg+sql", "iceberg+sqlite", "iceberg+postgres"}
}

func (d *Destination) ManagedStagingPolicy() destination.ReplaceStagingPolicy {
	namespace := defaultManagedStagingNamespace
	if d.cfg.Properties.Get("rest.signing-name", "") == "s3tables" {
		namespace = s3TablesManagedStagingNamespace
	}
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingManagedSchema,
		DefaultManagedSchema: namespace,
	}
}

func (d *Destination) Connect(ctx context.Context, rawURI string) error {
	cfg, err := parseIcebergConfig(rawURI)
	if err != nil {
		return err
	}
	if err := validateMaintenanceConfiguration(cfg.TableProperties); err != nil {
		return err
	}
	if err := validateMaintenanceTableLocation(cfg); err != nil {
		return err
	}

	cat, err := loadIcebergCatalog(ctx, cfg)
	if err != nil {
		return fmt.Errorf("iceberg: failed to load catalog: %w", err)
	}
	if err := validateIsolatedTableFilePaths(cfg.TableProperties); err != nil {
		_ = closeIcebergCatalog(cat)
		return fmt.Errorf("iceberg uri: %w", err)
	}
	if d.catalog != nil {
		if err := closeIcebergCatalog(d.catalog); err != nil {
			_ = closeIcebergCatalog(cat)
			return fmt.Errorf("iceberg: failed to close previous catalog: %w", err)
		}
	}

	d.cfg = cfg
	d.catalog = cat
	d.prepared = make(map[string]preparedTable)
	d.expirationScans = make(map[string]int64)
	if err := d.sweepPurgeJournals(ctx, nil); err != nil {
		d.catalog = nil
		_ = closeIcebergCatalog(cat)
		return fmt.Errorf("iceberg: failed to resume durable purge journals: %w", err)
	}
	config.Debug("[ICEBERG] Connected catalog type=%s name=%s", cat.CatalogType(), cfg.CatalogName)
	return nil
}

func (d *Destination) Close(ctx context.Context) error {
	cat := d.catalog
	d.catalog = nil
	d.prepared = nil
	d.expirationScans = nil
	if err := closeIcebergCatalog(cat); err != nil {
		return fmt.Errorf("iceberg: failed to close catalog: %w", err)
	}
	return nil
}

func closeIcebergCatalog(cat icebergcatalog.Catalog) error {
	if closer, ok := cat.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (d *Destination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	if opts.Schema == nil {
		return errors.New("iceberg destination requires schema")
	}
	tableSchema := tableSchemaWithPrimaryKeys(opts.Schema, opts.PrimaryKeys)
	if err := validateIcebergTableSchema(tableSchema); err != nil {
		return err
	}
	partitionBy := opts.PartitionBy
	if d.cfg.PartitionSpec != "" {
		partitionBy = d.cfg.PartitionSpec
	}
	clusterBy := append([]string(nil), opts.ClusterBy...)

	ident, err := parseIdentifier(opts.Table)
	if err != nil {
		return err
	}
	if err := validateS3TablesIdentifier(d.cfg, ident, tableSchema); err != nil {
		return err
	}
	namespace := icebergcatalog.NamespaceFromIdent(ident)
	if err := d.ensureNamespace(ctx, namespace); err != nil {
		return err
	}

	exists, err := d.tableExists(ctx, ident)
	if err != nil {
		return err
	}
	if !exists {
		var recoveryErr error
		if d.usesServerManagedPurge() {
			recoveryErr = d.ensureNoServerManagedPurgeLock(ctx, ident)
		} else {
			recoveryErr = d.resumePurgeJournal(ctx, ident)
		}
		if recoveryErr != nil {
			return fmt.Errorf("iceberg: cannot prepare table %s while durable purge recovery is incomplete: %w", opts.Table, recoveryErr)
		}
		exists, err = d.tableExists(ctx, ident)
		if err != nil {
			return err
		}
	}
	if exists {
		expired, err := d.prepareExistingTableLifecycleNow(ctx, ident, opts.ExpiresAfter)
		if err != nil {
			return err
		}
		exists = !expired
	}
	if exists {
		tbl, err := d.catalog.LoadTable(ctx, ident)
		if err != nil {
			return fmt.Errorf("iceberg: failed to load table %s: %w", opts.Table, err)
		}
		if opts.OwnershipToken != "" && tbl.Properties()[prepareOwnershipProperty] != opts.OwnershipToken {
			return fmt.Errorf("iceberg: table %s was created by another owner", opts.Table)
		}
		if opts.DropFirst {
			if err := validateIdentifierFieldsForEvolution(tbl.Schema(), tableSchema, true); err != nil {
				return err
			}
		}
		if opts.PreserveExistingLayout {
			partitionBy = ""
			clusterBy = nil
		} else if !opts.DropFirst && len(clusterBy) == 0 && tbl.SortOrder().Len() > 0 {
			inherited, ok := identitySortColumns(tbl)
			if ok && preparedLayoutColumnsExist(tableSchema, "", inherited) {
				clusterBy = inherited
			} else if _, err := d.ensureSortOrder(ctx, tbl, nil); err != nil {
				return err
			}
		}
		// A replace stages schema, partition, properties, and rows in the same
		// Iceberg transaction. Applying layout metadata here would leave the old
		// rows behind with new metadata if extraction or file writing later fails.
		if !opts.DropFirst && !opts.PreserveExistingLayout {
			tbl, err = d.applyConfiguredTableProperties(ctx, tbl)
			if err != nil {
				return err
			}
			if partitionBy != "" && layoutColumnsExist(tbl.Schema(), partitionBy, nil) {
				if err := d.updateExistingPartitionSpec(ctx, tbl, partitionBy); err != nil {
					return err
				}
				tbl, err = d.catalog.LoadTable(ctx, ident)
				if err != nil {
					return fmt.Errorf("iceberg: failed to reload table %s after partition evolution: %w", opts.Table, err)
				}
			}
			if len(clusterBy) > 0 && layoutColumnsExist(tbl.Schema(), "", clusterBy) {
				if _, err := d.ensureSortOrder(ctx, tbl, clusterBy); err != nil {
					return err
				}
			}
		}
	} else {
		if err := validatePreparedLayoutColumns(tableSchema, partitionBy, clusterBy); err != nil {
			return err
		}
		createOpts := opts
		createOpts.Schema = tableSchema
		if err := d.createTable(ctx, ident, createOpts); err != nil {
			return err
		}
	}

	d.mu.Lock()
	d.prepared[opts.Table] = preparedTable{
		schema:      tableSchema,
		replace:     opts.DropFirst,
		partitionBy: partitionBy,
		clusterBy:   clusterBy,
	}
	d.mu.Unlock()
	return nil
}

func (d *Destination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *Destination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	input := newRecordBatchInput(records)
	defer input.Close()

	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(opts.Table)
	if err != nil {
		return err
	}
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		return fmt.Errorf("iceberg: failed to load table %s: %w", opts.Table, err)
	}
	if err := validateIsolatedTableFilePaths(tbl.Properties()); err != nil {
		return fmt.Errorf("iceberg: table %s: %w", opts.Table, err)
	}
	if err := d.validateExpectedIncarnation(ctx, tbl, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	prepared := d.lookupPrepared(opts.Table)
	metadata := newCommitMetadata(opts.CommitToken, opts.CDCResumeLSN).withExpectedIncarnation(opts.CDCExpectedIncarnation)
	if opts.SkipCDCResume {
		metadata.cdcResumeLSN = ""
		metadata.resetCDCResume = !opts.StagingTable
	}
	if prepared.replace && metadata.token == "" {
		metadata = newCommitMetadata("replace-run:"+uuid.NewString(), "")
	}
	if tableHasCommitToken(tbl, metadata.token) {
		if err := input.Drain(ctx); err != nil {
			return fmt.Errorf("iceberg: failed to drain already-committed write for table %s: %w", opts.Table, err)
		}
		if opts.SkipCDCResume && !opts.StagingTable {
			if err := d.ensureManagedCDCResumeResetExpected(ctx, tbl, metadata.token, opts.CDCExpectedIncarnation); err != nil {
				return fmt.Errorf("iceberg: failed to clear legacy CDC resume state for table %s: %w", opts.Table, err)
			}
		}
		if prepared.replace {
			if _, err := d.ensureSortOrderExpected(ctx, tbl, prepared.clusterBy, opts.CDCExpectedIncarnation); err != nil {
				return fmt.Errorf("iceberg: data replacement for table %s is committed but its sort order is not: %w", opts.Table, err)
			}
		}
		return nil
	}
	if err := validateCDCResumeAdvance(tbl, metadata); err != nil {
		return err
	}

	writeSchema := opts.Schema
	if writeSchema == nil {
		writeSchema = prepared.schema
	}
	if writeSchema == nil {
		return errors.New("iceberg destination requires schema for write")
	}
	if err := validateIcebergTableSchema(writeSchema); err != nil {
		return err
	}
	leaseHeartbeat, err := d.startManagedTableLeaseHeartbeat(ctx, tbl)
	if err != nil {
		return fmt.Errorf("iceberg: failed to start managed-table lease heartbeat for %s: %w", opts.Table, err)
	}
	if leaseHeartbeat != nil {
		defer leaseHeartbeat.stop()
	}

	operation := "append"
	if prepared.replace {
		operation = "replace"
	}
	props := snapshotProps(operation, metadata)
	var observe func(arrow.RecordBatch)
	if metadata.cdcResumeLSN == "" && !opts.SkipCDCResume {
		observe = func(batch arrow.RecordBatch) { observeCDCResumeLSN(props, batch, metadata.token != "") }
	}

	arrowSchema := icebergWriteArrowSchema(writeSchema, tbl.Schema())
	if prepared.replace && prepared.schema != nil {
		preparedIcebergSchema, schemaErr := icebergSchemaFromTableSchema(prepared.schema)
		if schemaErr != nil {
			return schemaErr
		}
		arrowSchema = icebergWriteArrowSchema(writeSchema, preparedIcebergSchema)
	}
	batchReader := input.RecordReader(ctx, arrowSchema)
	batchReader.observe = observe
	var reader array.RecordReader = batchReader
	var transformedReaders []func()
	defer func() {
		for i := len(transformedReaders) - 1; i >= 0; i-- {
			transformedReaders[i]()
		}
	}()
	if prepared.replace && opts.DeduplicatePrimaryKeys && len(prepared.schema.PrimaryKeys) > 0 {
		var cleanup func()
		reader, cleanup, err = deduplicateRecordReader(reader, prepared.schema.PrimaryKeys, opts.IncrementalKey)
		if err != nil {
			return fmt.Errorf("iceberg: failed to deduplicate replace for table %s: %w", opts.Table, err)
		}
		transformedReaders = append(transformedReaders, cleanup)
	}
	if len(prepared.clusterBy) > 0 {
		var cleanup func()
		reader, cleanup, err = clusterRecordReader(ctx, reader, prepared.clusterBy)
		if err != nil {
			return fmt.Errorf("iceberg: failed to cluster write for table %s: %w", opts.Table, err)
		}
		transformedReaders = append(transformedReaders, cleanup)
	}
	if prepared.replace {
		openReplay, cleanupReplay, err := spoolReplayableRecordReader(reader)
		if err != nil {
			return fmt.Errorf("iceberg: failed to spool replacement for table %s: %w", opts.Table, err)
		}
		defer cleanupReplay()
		var committed *icebergtable.Table
		current := tbl
		var writeErr error
		const maxAttempts = 5
		for attempt := 0; attempt < maxAttempts; attempt++ {
			replay, release, replayErr := openReplay()
			if replayErr != nil {
				return replayErr
			}
			committed, writeErr = d.overwritePrepared(ctx, current, replay, props, prepared, opts.Parallelism, leaseHeartbeat, false, opts.CDCExpectedIncarnation)
			release()
			if writeErr == nil {
				break
			}
			if !errors.Is(writeErr, icebergtable.ErrCommitFailed) {
				if reconciled := d.reconcileCommit(ctx, opts.Table, props[snapshotCommitTokenKey], opts.CDCExpectedIncarnation, writeErr); reconciled != nil {
					return fmt.Errorf("iceberg: failed to write table %s: %w", opts.Table, reconciled)
				}
				committed, err = d.loadIcebergTable(ctx, opts.Table)
				if err != nil {
					return fmt.Errorf("iceberg: replacement for table %s committed but could not be reloaded: %w", opts.Table, err)
				}
				if err := d.validateExpectedIncarnation(ctx, committed, opts.CDCExpectedIncarnation); err != nil {
					return err
				}
				break
			}
			current, err = d.loadIcebergTable(ctx, opts.Table)
			if err != nil {
				return err
			}
			if err := d.validateExpectedIncarnation(ctx, current, opts.CDCExpectedIncarnation); err != nil {
				return err
			}
			if err := waitForCommitRetry(ctx, attempt); err != nil {
				return err
			}
		}
		if committed == nil {
			return fmt.Errorf("iceberg: replacement for table %s failed after retries: %w", opts.Table, writeErr)
		}
		if _, err := d.ensureSortOrderExpected(ctx, committed, prepared.clusterBy, opts.CDCExpectedIncarnation); err != nil {
			return fmt.Errorf("iceberg: data replacement for table %s is committed but its sort order is not: %w", opts.Table, err)
		}
	} else {
		if leaseHeartbeat != nil {
			spooled, cleanup, spoolErr := spoolRecordReader(reader)
			if spoolErr != nil {
				return fmt.Errorf("iceberg: failed to spool managed append for table %s: %w", opts.Table, spoolErr)
			}
			defer cleanup()
			reader = spooled
			tbl, err = leaseHeartbeat.stopAndRefresh(ctx)
			if err != nil {
				return fmt.Errorf("iceberg: failed to finalize managed-table lease before append: %w", err)
			}
			expiresAt, ttl, _, leaseErr := managedTableLease(tbl.Properties())
			if leaseErr != nil {
				return leaseErr
			}
			fileWriteDeadline := expiresAt.Add(-ttl / 4)
			if !time.Now().Before(fileWriteDeadline) {
				return errors.New("iceberg: managed-table lease has insufficient time remaining for append")
			}
			var cancelLeaseDeadline context.CancelFunc
			ctx, cancelLeaseDeadline = context.WithDeadline(ctx, fileWriteDeadline)
			defer cancelLeaseDeadline()
		}
		parallelism := opts.Parallelism
		if len(prepared.clusterBy) > 0 {
			parallelism = 1
		}
		err = d.appendRecordBatches(ctx, tbl, reader, props, parallelism, opts.CDCExpectedIncarnation)
	}
	if !prepared.replace && err != nil {
		if incarnationErr := d.validateExpectedIncarnation(ctx, tbl, opts.CDCExpectedIncarnation); incarnationErr != nil {
			return incarnationErr
		}
		if reconciled := d.reconcileCommit(ctx, opts.Table, props[snapshotCommitTokenKey], opts.CDCExpectedIncarnation, err); reconciled != nil {
			return fmt.Errorf("iceberg: failed to write table %s: %w", opts.Table, reconciled)
		}
	}
	d.afterSuccessfulCommitExpected(ctx, opts.Table, opts.CDCExpectedIncarnation)
	return nil
}

func (d *Destination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	return nil
}

func (d *Destination) DropTable(ctx context.Context, table string) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return err
	}
	return d.dropTableWithLifecycle(ctx, ident)
}

func (d *Destination) DropTableIfOwned(ctx context.Context, table, ownershipToken string) error {
	if strings.TrimSpace(ownershipToken) == "" {
		return errors.New("iceberg: conditional table drop requires an ownership token")
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return err
	}
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if isMissingTableOrNamespace(err) {
		return nil
	}
	if err != nil {
		return err
	}
	expectedUUID := tbl.Metadata().TableUUID().String()
	return d.dropTableWithLifecycleExpected(ctx, ident, expectedUUID, func(current *icebergtable.Table) error {
		if current.Properties()[prepareOwnershipProperty] != ownershipToken {
			return fmt.Errorf("iceberg: refused to drop table %s because preparation ownership changed", table)
		}
		return nil
	})
}

var _ destination.OwnedTableDropper = (*Destination)(nil)

// DropNamespace removes an empty Iceberg namespace using the destination's
// configured catalog client, including URI-scoped authentication.
func (d *Destination) DropNamespace(ctx context.Context, namespace string) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(namespace + ".placeholder")
	if err != nil {
		return err
	}
	namespaceIdent := icebergcatalog.NamespaceFromIdent(ident)
	err = d.catalog.DropNamespace(ctx, namespaceIdent)
	if isMissingTableOrNamespace(err) {
		return nil
	}
	if err == nil {
		return nil
	}
	originalErr := err
	cleaned, cleanupErr := d.dropIdleLifecycleLocks(ctx, namespaceIdent)
	if cleanupErr != nil {
		return errors.Join(originalErr, cleanupErr)
	}
	if !cleaned {
		return originalErr
	}
	err = d.catalog.DropNamespace(ctx, namespaceIdent)
	if isMissingTableOrNamespace(err) {
		return nil
	}
	return err
}

func (d *Destination) dropIdleLifecycleLocks(ctx context.Context, namespace icebergtable.Identifier) (bool, error) {
	cleaned := false
	for ident, listErr := range d.catalog.ListTables(ctx, namespace) {
		if isMissingTableOrNamespace(listErr) {
			return cleaned, nil
		}
		if listErr != nil {
			return cleaned, fmt.Errorf("iceberg: failed to list namespace lifecycle locks: %w", listErr)
		}
		if len(ident) == 0 || !strings.HasPrefix(ident[len(ident)-1], purgeLockTablePrefix) {
			continue
		}
		lock, err := d.catalog.LoadTable(ctx, ident)
		if err != nil {
			return cleaned, fmt.Errorf("iceberg: failed to inspect namespace lifecycle lock %s: %w", strings.Join(ident, "."), err)
		}
		mode := lock.Properties()[purgeLockModeKey]
		if mode != purgeLockModeIdle && mode != purgeLockModeCleanup {
			return cleaned, fmt.Errorf("iceberg: namespace lifecycle lock %s is still active", strings.Join(ident, "."))
		}
		if mode == purgeLockModeCleanup {
			expiresAt, parseErr := time.Parse(time.RFC3339Nano, lock.Properties()[purgeLockExpiresAtKey])
			if parseErr != nil {
				return cleaned, fmt.Errorf("iceberg: namespace lifecycle lock %s has an invalid cleanup expiry: %w", strings.Join(ident, "."), parseErr)
			}
			if time.Now().UTC().Before(expiresAt) {
				return cleaned, fmt.Errorf("iceberg: namespace lifecycle lock %s cleanup is still active", strings.Join(ident, "."))
			}
		}
		token := uuid.NewString()
		txn := lock.NewTransaction()
		if err := txn.SetProperties(iceberggo.Properties{
			purgeLockModeKey: purgeLockModeCleanup, purgeLockTokenKey: token,
			purgeLockExpiresAtKey: time.Now().UTC().Add(createGuardTTL).Format(time.RFC3339Nano),
		}); err != nil {
			return cleaned, err
		}
		if _, err := txn.Commit(ctx); err != nil {
			return cleaned, fmt.Errorf("iceberg: failed to fence namespace lifecycle lock %s for cleanup: %w", strings.Join(ident, "."), err)
		}
		current, err := d.catalog.LoadTable(ctx, ident)
		if err != nil {
			return cleaned, err
		}
		if current.Properties()[purgeLockModeKey] != purgeLockModeCleanup || current.Properties()[purgeLockTokenKey] != token {
			return cleaned, fmt.Errorf("iceberg: namespace lifecycle lock %s cleanup ownership changed", strings.Join(ident, "."))
		}
		if err := d.catalog.DropTable(ctx, ident); err != nil && !isMissingTableOrNamespace(err) {
			rollbackErr := d.rollbackCleanupLock(context.WithoutCancel(ctx), ident, token)
			return cleaned, errors.Join(
				fmt.Errorf("iceberg: failed to remove idle namespace lifecycle lock %s: %w", strings.Join(ident, "."), err),
				rollbackErr,
			)
		}
		cleaned = true
	}
	return cleaned, nil
}

func (d *Destination) rollbackCleanupLock(ctx context.Context, ident icebergtable.Identifier, token string) error {
	lock, err := d.catalog.LoadTable(ctx, ident)
	if isMissingTableOrNamespace(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("iceberg: failed to inspect cleanup lock rollback for %s: %w", strings.Join(ident, "."), err)
	}
	if lock.Properties()[purgeLockModeKey] != purgeLockModeCleanup || lock.Properties()[purgeLockTokenKey] != token {
		return fmt.Errorf("iceberg: refused cleanup lock rollback for %s because ownership changed", strings.Join(ident, "."))
	}
	if err := commitLockIdle(ctx, lock); err != nil {
		return fmt.Errorf("iceberg: failed to roll cleanup lock %s back to idle: %w", strings.Join(ident, "."), err)
	}
	return nil
}

func (d *Destination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return errors.New("iceberg destination does not support SQL execution")
}

func (d *Destination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return nil, errors.New("iceberg destination does not support SQL transactions")
}

func (d *Destination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	if d.catalog == nil {
		return nil, errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return nil, err
	}
	exists, err := d.tableExists(ctx, ident)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		if isMissingTableOrNamespace(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("iceberg: failed to load table %s: %w", table, err)
	}
	return tableSchemaFromIceberg(table, tbl.Schema())
}

func (d *Destination) GetScheme() string {
	return "iceberg"
}

func (d *Destination) SupportsReplaceStrategy() bool            { return true }
func (d *Destination) SupportsAppendStrategy() bool             { return true }
func (d *Destination) SupportsMergeStrategy() bool              { return true }
func (d *Destination) SupportsDeleteInsertStrategy() bool       { return true }
func (d *Destination) SupportsSCD2Strategy() bool               { return true }
func (d *Destination) SupportsAtomicSwap() bool                 { return false }
func (d *Destination) SupportsDirectReplaceDeduplication() bool { return true }
func (d *Destination) SupportsCDCMerge() bool                   { return true }
func (d *Destination) SupportsCDCUnchangedCols() bool           { return true }

func (d *Destination) MaxConcurrentFlushes() int { return 4 }

func (d *Destination) createTable(ctx context.Context, ident icebergtable.Identifier, opts destination.PrepareOptions) error {
	if (d.catalog.CatalogType() == icebergcatalog.REST && !d.usesServerManagedPurge()) ||
		d.catalog.CatalogType() == icebergcatalog.Hadoop {
		return d.createTableGuarded(ctx, ident, opts)
	}
	token, err := d.acquireCreateGuard(ctx, ident)
	if err != nil {
		return err
	}
	createCtx, cancelCreate := context.WithCancel(ctx)
	heartbeat := d.startCatalogLockHeartbeat(createCtx, ident, purgeLockModeCreate, "", token, createGuardTTL, cancelCreate)
	createErr := d.createTableGuarded(createCtx, ident, opts)
	heartbeatErr := heartbeat.stop()
	cancelCreate()
	releaseErr := d.releaseCreateGuard(context.WithoutCancel(ctx), ident, token)
	return errors.Join(createErr, heartbeatErr, releaseErr)
}

func (d *Destination) createTableGuarded(ctx context.Context, ident icebergtable.Identifier, opts destination.PrepareOptions) error {
	iceSchema, err := icebergSchemaFromTableSchema(opts.Schema)
	if err != nil {
		return err
	}

	createOpts := []icebergcatalog.CreateTableOpt{}
	requestedProperties := maps.Clone(d.cfg.TableProperties)
	maps.Copy(requestedProperties, opts.TableProperties)
	if opts.OwnershipToken != "" {
		requestedProperties[prepareOwnershipProperty] = opts.OwnershipToken
	}
	tableProperties, err := lifecyclePropertiesForCreate(requestedProperties, opts.ExpiresAfter)
	if err != nil {
		return err
	}
	if len(tableProperties) > 0 {
		createOpts = append(createOpts, icebergcatalog.WithProperties(tableProperties))
	}
	if d.cfg.TableLocation != "" {
		createOpts = append(createOpts, icebergcatalog.WithLocation(renderTableLocation(d.cfg.TableLocation, ident)))
	}
	partitionBy := opts.PartitionBy
	if d.cfg.PartitionSpec != "" {
		partitionBy = d.cfg.PartitionSpec
	}
	if partitionBy != "" {
		spec, err := buildPartitionSpec(iceSchema, partitionBy)
		if err != nil {
			return err
		}
		createOpts = append(createOpts, icebergcatalog.WithPartitionSpec(&spec))
	}
	if len(opts.ClusterBy) > 0 {
		order, err := sortOrderForColumns(iceSchema, opts.ClusterBy, icebergtable.InitialSortOrderID)
		if err != nil {
			return err
		}
		createOpts = append(createOpts, icebergcatalog.WithSortOrder(order))
	}

	if err := d.ensureLocalTableDirs(ident); err != nil {
		return err
	}
	if _, err := d.catalog.CreateTable(ctx, ident, iceSchema, createOpts...); err != nil {
		if errors.Is(err, icebergcatalog.ErrTableAlreadyExists) {
			return fmt.Errorf("iceberg: refused concurrent external creation of table %s: %w", strings.Join(ident, "."), err)
		}
		return fmt.Errorf("iceberg: failed to create table %s: %w", strings.Join(ident, "."), err)
	}
	return nil
}

func (d *Destination) stageTableSchemaUpdate(
	txn *icebergtable.Transaction,
	tbl *icebergtable.Table,
	desired *schema.TableSchema,
	reset bool,
	allowIncompatible bool,
) (bool, error) {
	if err := validateIdentifierFieldsForEvolution(tbl.Schema(), desired, reset); err != nil {
		return false, err
	}
	update := txn.UpdateSchema(true, allowIncompatible, icebergtable.WithNameMapping(tbl.NameMapping()))
	changed := false

	desiredColumns := make(map[string]struct{}, len(desired.Columns))
	for _, col := range desired.Columns {
		desiredColumns[col.Name] = struct{}{}
	}
	if reset {
		for _, field := range tbl.Schema().Fields() {
			if _, ok := desiredColumns[field.Name]; ok {
				continue
			}
			update.DeleteColumn([]string{field.Name})
			changed = true
		}
	}
	if !reset {
		for _, field := range tbl.Schema().Fields() {
			if _, ok := desiredColumns[field.Name]; ok || !field.Required {
				continue
			}
			if slices.Contains(tbl.Schema().IdentifierFieldIDs, field.ID) {
				return false, fmt.Errorf("iceberg: cannot omit required identifier column %q", field.Name)
			}
			update.UpdateColumn([]string{field.Name}, icebergtable.ColumnUpdate{
				Required: iceberggo.Optional[bool]{Valid: true, Val: false},
			})
			changed = true
		}
	}

	for _, col := range desired.Columns {
		targetType, err := icebergTypeForColumn(col)
		if err != nil {
			return false, fmt.Errorf("iceberg: failed to map column %q type: %w", col.Name, err)
		}
		field, ok := tbl.Schema().FindFieldByName(col.Name)
		if !ok {
			update.AddColumn([]string{col.Name}, targetType, "", allowIncompatible && !col.Nullable, nil)
			changed = true
			continue
		}

		if !icebergTypesEquivalent(field.Type, targetType) {
			if nestedColumn, nestedCurrent := nestedIcebergType(targetType), nestedIcebergType(field.Type); nestedColumn && nestedCurrent {
				current, err := columnFromIcebergField(field)
				if err != nil {
					return false, fmt.Errorf("iceberg: failed to inspect nested column %q: %w", col.Name, err)
				}
				comparison, err := schemaevolution.Compare(
					&schema.TableSchema{Columns: []schema.Column{col}},
					&schema.TableSchema{Columns: []schema.Column{current}},
					nil,
				)
				if err != nil {
					return false, err
				}
				if allowIncompatible {
					comparison = restoreDesiredNestedRequiredness(comparison, desired)
				}
				nestedChanged, err := stageSchemaComparisonChanges(update, tbl, comparison, allowIncompatible)
				if err != nil {
					return false, err
				}
				changed = changed || nestedChanged
				continue
			}
			if !reset {
				if _, err := iceberggo.PromoteType(field.Type, targetType); err != nil {
					return false, fmt.Errorf("iceberg: column %q type change from %s to %s is not supported: %w", col.Name, field.Type, targetType, err)
				}
			}
			update.UpdateColumn([]string{col.Name}, icebergtable.ColumnUpdate{
				FieldType: iceberggo.Optional[iceberggo.Type]{Valid: true, Val: targetType},
			})
			changed = true
		}
		if field.Required && col.Nullable {
			update.UpdateColumn([]string{col.Name}, icebergtable.ColumnUpdate{
				Required: iceberggo.Optional[bool]{Valid: true, Val: false},
			})
			changed = true
		}
		if allowIncompatible && !field.Required && !col.Nullable {
			update.UpdateColumn([]string{col.Name}, icebergtable.ColumnUpdate{
				Required: iceberggo.Optional[bool]{Valid: true, Val: true},
			})
			changed = true
		}
	}
	if !identifierFieldsEqual(tbl.Schema(), desired.PrimaryKeys, reset) {
		paths := make([][]string, 0, len(desired.PrimaryKeys))
		for _, pk := range desired.PrimaryKeys {
			paths = append(paths, []string{pk})
		}
		update.SetIdentifierField(paths)
		changed = true
	}

	if !changed {
		return false, nil
	}
	if err := update.Commit(); err != nil {
		return false, fmt.Errorf("iceberg: failed to update table schema: %w", err)
	}
	return true, nil
}

func nestedIcebergType(t iceberggo.Type) bool {
	switch t.(type) {
	case *iceberggo.StructType, *iceberggo.ListType, *iceberggo.MapType:
		return true
	default:
		return false
	}
}

func restoreDesiredNestedRequiredness(comparison *schemaevolution.SchemaComparison, desired *schema.TableSchema) *schemaevolution.SchemaComparison {
	if comparison == nil {
		return nil
	}
	result := *comparison
	result.Changes = append([]schemaevolution.SchemaChange(nil), comparison.Changes...)
	for i := range result.Changes {
		change := &result.Changes[i]
		if change.Type != schemaevolution.ChangeAddColumn || len(change.ColumnPath) == 0 {
			continue
		}
		if col, ok := nestedSchemaColumnAtPath(desired.Columns, change.ColumnPath); ok {
			change.NewColumn.Nullable = col.Nullable
		}
	}
	return &result
}

func nestedSchemaColumnAtPath(columns []schema.Column, path []string) (schema.Column, bool) {
	if len(path) == 0 {
		return schema.Column{}, false
	}
	for _, col := range columns {
		if !strings.EqualFold(col.Name, path[0]) {
			continue
		}
		if len(path) == 1 {
			return col, true
		}
		switch col.DataType {
		case schema.TypeStruct:
			if col.StructFields != nil {
				return nestedSchemaColumnAtPath(col.StructFields.Columns, path[1:])
			}
		case schema.TypeArray:
			if path[1] == "element" && col.Element != nil {
				return nestedSchemaColumnAtPath([]schema.Column{*col.Element}, append([]string{col.Element.Name}, path[2:]...))
			}
		case schema.TypeMap:
			if path[1] == "value" && col.MapValue != nil {
				return nestedSchemaColumnAtPath([]schema.Column{*col.MapValue}, append([]string{col.MapValue.Name}, path[2:]...))
			}
		}
	}
	return schema.Column{}, false
}

func (d *Destination) stagePartitionSpecUpdate(txn *icebergtable.Transaction, tbl *icebergtable.Table, partitionBy string) (bool, error) {
	if partitionExpressionMatches(tbl, partitionBy) {
		return false, nil
	}
	terms, err := parsePartitionExpression(partitionBy)
	if err != nil {
		return false, err
	}
	update := txn.UpdateSpec(true)
	spec := tbl.Metadata().PartitionSpec()
	for _, field := range spec.Fields() {
		update.RemoveField(field.Name)
	}
	for _, term := range terms {
		update.AddField(term.source, term.transform, term.name)
	}
	if err := update.Commit(); err != nil {
		return false, fmt.Errorf("iceberg: failed to update partition spec: %w", err)
	}
	return true, nil
}

func (d *Destination) overwritePrepared(
	ctx context.Context,
	tbl *icebergtable.Table,
	reader array.RecordReader,
	props iceberggo.Properties,
	prepared preparedTable,
	parallelism int,
	leaseHeartbeat *managedLeaseHeartbeat,
	readerAlreadySpooled bool,
	expectedIncarnation string,
) (committedTable *icebergtable.Table, retErr error) {
	var err error
	if !readerAlreadySpooled {
		spooled, cleanup, err := spoolRecordReader(reader)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		reader = spooled
	}
	if leaseHeartbeat != nil {
		tbl, err = leaseHeartbeat.stopAndRefresh(ctx)
		if err != nil {
			return nil, fmt.Errorf("iceberg: failed to finalize managed-table lease before overwrite: %w", err)
		}
		expiresAt, ttl, _, err := managedTableLease(tbl.Properties())
		if err != nil {
			return nil, err
		}
		fileWriteDeadline := expiresAt.Add(-ttl / 4)
		if !time.Now().Before(fileWriteDeadline) {
			return nil, fmt.Errorf("iceberg: managed-table lease has insufficient time remaining for overwrite")
		}
		var cancelLeaseDeadline context.CancelFunc
		ctx, cancelLeaseDeadline = context.WithDeadline(ctx, fileWriteDeadline)
		defer cancelLeaseDeadline()
	}
	dataFilesToDelete, deleteFilesToRemove, err := liveFilesForReplace(ctx, tbl)
	if err != nil {
		return nil, err
	}
	tableFS, err := tbl.FS(ctx)
	if err != nil {
		return nil, err
	}
	writeID := uuid.New()
	generatedPaths := make([]string, 0)
	committed, cleanupSafe := false, true
	defer func() {
		if committed || !cleanupSafe {
			return
		}
		if err := removeGeneratedMergeFiles(tableFS, tbl.Location(), writeID.String(), generatedPaths); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()
	txn := tbl.NewTransaction()
	if err := stageResetCommitTokenLedger(txn, props[snapshotCommitTokenKey]); err != nil {
		return nil, err
	}
	if err := stageCDCResumeState(txn, props); err != nil {
		return nil, err
	}
	if prepared.evolveSchema && prepared.schema != nil {
		if _, err := d.stageTableSchemaUpdate(txn, tbl, prepared.schema, false, true); err != nil {
			return nil, err
		}
	}
	if !prepared.preserveMetadata {
		mutableProperties := maps.Clone(d.cfg.TableProperties)
		delete(mutableProperties, icebergtable.PropertyFormatVersion)
		if err := txn.SetProperties(mutableProperties); err != nil {
			return nil, fmt.Errorf("iceberg: failed to stage configured table properties: %w", err)
		}
		if prepared.schema != nil && !prepared.evolveSchema {
			if _, err := d.stageTableSchemaUpdate(txn, tbl, prepared.schema, true, true); err != nil {
				return nil, err
			}
		}
		if _, err := d.stagePartitionSpecUpdate(txn, tbl, prepared.partitionBy); err != nil {
			return nil, err
		}
	}
	staged, err := txn.StagedTable()
	if err != nil {
		return nil, fmt.Errorf("iceberg: failed to construct staged overwrite table: %w", err)
	}
	stagedSortOrderMatches := sortOrderMatchesColumns(staged.Table, prepared.clusterBy)
	writeOptions := make([]icebergtable.WriteRecordOption, 0, 1)
	writeOptions = append(writeOptions, icebergtable.WithWriteUUID(writeID))
	if parallelism > 0 {
		if len(prepared.clusterBy) > 0 {
			parallelism = 1
		}
		writeOptions = append(writeOptions, icebergtable.WithMaxWriteWorkers(parallelism))
	}
	var dataFiles []iceberggo.DataFile
	for dataFile, writeErr := range icebergtable.WriteRecords(ctx, staged.Table, reader.Schema(), retainedRecordIterator(reader), writeOptions...) {
		if writeErr != nil {
			return nil, writeErr
		}
		if !stagedSortOrderMatches {
			dataFile, err = withDataFileSortOrderID(dataFile, staged.Table, icebergtable.UnsortedSortOrderID)
			if err != nil {
				return nil, err
			}
		}
		dataFiles = append(dataFiles, dataFile)
		generatedPaths = append(generatedPaths, dataFile.FilePath())
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}
	if staged.Table.Metadata().Version() >= 3 {
		nextRowID := staged.Table.Metadata().NextRowID()
		for i, file := range dataFiles {
			dataFiles[i], err = withDataFileFirstRowID(file, staged.Table, nextRowID)
			if err != nil {
				return nil, err
			}
			nextRowID += file.Count()
		}
	}
	if len(dataFilesToDelete) == 0 && len(deleteFilesToRemove) == 0 && len(dataFiles) == 0 {
		empty, emptyErr := array.NewRecordReader(reader.Schema(), nil)
		if emptyErr != nil {
			return nil, fmt.Errorf("iceberg: failed to create empty replace reader: %w", emptyErr)
		}
		defer empty.Release()
		if err := txn.Append(ctx, empty, props); err != nil {
			return nil, err
		}
	} else if err := txn.ReplaceFiles(
		ctx,
		dataFilesToDelete,
		dataFiles,
		deleteFilesToRemove,
		props,
		icebergtable.WithoutDuplicateCheck(),
	); err != nil {
		return nil, err
	}
	if err := d.validateExpectedIncarnation(ctx, tbl, expectedIncarnation); err != nil {
		return nil, err
	}
	committedTable, err = txn.Commit(ctx)
	if err != nil {
		cleanupSafe = errors.Is(err, icebergtable.ErrCommitFailed)
		return nil, err
	}
	committed = true
	return committedTable, nil
}

func liveFilesForReplace(ctx context.Context, tbl *icebergtable.Table) ([]iceberggo.DataFile, []iceberggo.DataFile, error) {
	snapshot := tbl.CurrentSnapshot()
	if snapshot == nil {
		return nil, nil, nil
	}
	fs, err := tbl.FS(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("iceberg: failed to load table file IO for replace: %w", err)
	}
	manifests, err := snapshot.Manifests(fs)
	if err != nil {
		return nil, nil, fmt.Errorf("iceberg: failed to load current manifests for replace: %w", err)
	}
	dataFiles := make([]iceberggo.DataFile, 0)
	deleteFiles := make([]iceberggo.DataFile, 0)
	seen := make(map[string]struct{})
	for _, manifest := range manifests {
		for entry, entryErr := range manifest.Entries(fs, true) {
			if entryErr != nil {
				return nil, nil, fmt.Errorf("iceberg: failed to read current manifest for replace: %w", entryErr)
			}
			file := entry.DataFile()
			if _, ok := seen[file.FilePath()]; ok {
				continue
			}
			seen[file.FilePath()] = struct{}{}
			if file.ContentType() == iceberggo.EntryContentData {
				dataFiles = append(dataFiles, file)
			} else {
				deleteFiles = append(deleteFiles, file)
			}
		}
	}
	return dataFiles, deleteFiles, nil
}

func (d *Destination) updateExistingPartitionSpec(ctx context.Context, tbl *icebergtable.Table, partitionBy string) error {
	if partitionExpressionMatches(tbl, partitionBy) {
		return nil
	}
	txn := tbl.NewTransaction()
	if _, err := d.stagePartitionSpecUpdate(txn, tbl, partitionBy); err != nil {
		return err
	}
	if _, err := txn.Commit(ctx); err != nil {
		return fmt.Errorf("iceberg: failed to commit partition spec update: %w", err)
	}
	return nil
}

func (d *Destination) ensureNamespace(ctx context.Context, namespace icebergtable.Identifier) error {
	if len(namespace) == 0 {
		return nil
	}
	if d.cfg.Properties.Get("type", "") == "hive" && len(namespace) != 1 {
		return fmt.Errorf("iceberg: Hive catalog requires a single-level namespace, got %s", strings.Join(namespace, "."))
	}
	if !d.cfg.CreateNamespace {
		return nil
	}
	for i := 1; i <= len(namespace); i++ {
		current := namespace[:i]
		exists, err := d.catalog.CheckNamespaceExists(ctx, current)
		if err != nil && !errors.Is(err, icebergcatalog.ErrNoSuchNamespace) {
			return fmt.Errorf("iceberg: failed to check namespace %s: %w", strings.Join(current, "."), err)
		}
		if exists {
			continue
		}
		properties := iceberggo.Properties{}
		if d.cfg.Properties.Get("type", "") == "hive" {
			if warehouse := d.cfg.Properties.Get("warehouse", ""); warehouse != "" {
				properties["location"] = appendLocationPath(warehouse, strings.Join(current, "/"), false)
			}
		}
		if err := d.catalog.CreateNamespace(ctx, current, properties); err != nil && !errors.Is(err, icebergcatalog.ErrNamespaceAlreadyExists) {
			return fmt.Errorf("iceberg: failed to create namespace %s: %w", strings.Join(current, "."), err)
		}
	}
	return nil
}

func (d *Destination) tableExists(ctx context.Context, ident icebergtable.Identifier) (bool, error) {
	exists, err := d.catalog.CheckTableExists(ctx, ident)
	if err != nil {
		if isMissingTableOrNamespace(err) {
			return false, nil
		}
		return false, fmt.Errorf("iceberg: failed to check table %s: %w", strings.Join(ident, "."), err)
	}
	return exists, nil
}

func (d *Destination) lookupPrepared(table string) preparedTable {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.prepared[table]
}

func parseIdentifier(table string) (icebergtable.Identifier, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		return nil, errors.New("iceberg table identifier is required")
	}
	ident := icebergcatalog.ToIdentifier(table)
	for _, part := range ident {
		if part == "" {
			return nil, fmt.Errorf("iceberg table identifier %q contains an empty component", table)
		}
	}
	return ident, nil
}

func isMissingTableOrNamespace(err error) bool {
	return errors.Is(err, icebergcatalog.ErrNoSuchTable) || errors.Is(err, icebergcatalog.ErrNoSuchNamespace)
}

func renderTableLocation(template string, ident icebergtable.Identifier) string {
	namespaceParts := ident[:len(ident)-1]
	tableName := ident[len(ident)-1]
	replacer := strings.NewReplacer(
		"{namespace}", strings.Join(namespaceParts, "/"),
		"{namespace_dot}", strings.Join(namespaceParts, "."),
		"{table}", tableName,
		"{identifier}", strings.Join(ident, "/"),
		"{identifier_dot}", strings.Join(ident, "."),
	)
	return replacer.Replace(template)
}

func (d *Destination) ensureLocalTableDirs(ident icebergtable.Identifier) error {
	location, ok := d.localTableLocation(ident)
	if !ok {
		return nil
	}
	mode := fs.FileMode(0o755)
	forceMode := false
	if d.cfg.Properties.Get("type", "") == "rest" {
		// A local REST warehouse is shared by the catalog server and this process,
		// which may run as different UIDs.
		mode = 0o777
		forceMode = true
	}
	for _, dir := range []string{location, filepath.Join(location, "data"), filepath.Join(location, "metadata")} {
		if err := os.MkdirAll(dir, mode); err != nil {
			return fmt.Errorf("iceberg: failed to create local table directory %s: %w", dir, err)
		}
		if forceMode {
			if err := os.Chmod(dir, mode); err != nil {
				return fmt.Errorf("iceberg: failed to set local table directory permissions %s: %w", dir, err)
			}
		}
	}
	return nil
}

func (d *Destination) localTableLocation(ident icebergtable.Identifier) (string, bool) {
	if d.cfg.TableLocation != "" {
		return localFilesystemPath(renderTableLocation(d.cfg.TableLocation, ident))
	}
	warehouse, ok := localFilesystemPath(d.cfg.Properties.Get("warehouse", ""))
	if !ok || warehouse == "" {
		return "", false
	}
	parts := append([]string{warehouse}, ident...)
	return filepath.Join(parts...), true
}

func localFilesystemPath(location string) (string, bool) {
	if location == "" {
		return "", false
	}
	if strings.HasPrefix(strings.ToLower(location), "file:") {
		parsed, err := url.Parse(location)
		if err != nil || parsed.Scheme != "file" || parsed.Path == "" {
			return "", false
		}
		return parsed.Path, true
	}
	if !strings.Contains(location, "://") {
		return location, true
	}
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme != "file" {
		return "", false
	}
	return parsed.Path, parsed.Path != ""
}

func tableSchemaWithPrimaryKeys(s *schema.TableSchema, primaryKeys []string) *schema.TableSchema {
	if len(primaryKeys) == 0 {
		return s
	}
	out := *s
	out.PrimaryKeys = append([]string(nil), primaryKeys...)
	return &out
}

func validateIdentifierFieldsForEvolution(current *iceberggo.Schema, desired *schema.TableSchema, reset bool) error {
	if len(desired.PrimaryKeys) == 0 {
		return nil
	}
	if _, err := icebergSchemaFromTableSchema(desired); err != nil {
		return err
	}
	if reset {
		return nil
	}

	currentFields := make(map[string]iceberggo.NestedField, current.NumFields())
	for _, field := range current.Fields() {
		currentFields[field.Name] = field
	}
	for _, pk := range desired.PrimaryKeys {
		field, ok := currentFields[pk]
		if !ok {
			return fmt.Errorf("primary key %q cannot be set on a new column without replace mode", pk)
		}
		if err := validateIdentifierField(pk, field); err != nil {
			return err
		}
	}
	return nil
}

func identifierFieldsEqual(iceSchema *iceberggo.Schema, primaryKeys []string, allowClear bool) bool {
	if len(primaryKeys) == 0 && !allowClear {
		return true
	}
	if len(iceSchema.IdentifierFieldIDs) != len(primaryKeys) {
		return false
	}
	current := make(map[string]struct{}, len(iceSchema.IdentifierFieldIDs))
	for _, id := range iceSchema.IdentifierFieldIDs {
		if name, ok := iceSchema.FindColumnName(id); ok {
			current[name] = struct{}{}
		}
	}
	for _, pk := range primaryKeys {
		if _, ok := current[pk]; !ok {
			return false
		}
	}
	return true
}
