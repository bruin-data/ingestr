package iceberg

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergio "github.com/apache/iceberg-go/io"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/google/uuid"
)

const (
	managedTableProperty       = "ingestr.managed"
	managedTableKindProperty   = "ingestr.managed-kind"
	managedTableKindStaging    = "staging"
	managedTableExpiresAt      = "ingestr.expires-at"
	managedTableExpiresAfterMS = "ingestr.expires-after-ms"
	managedTablePurgeClaim     = "ingestr.purge-claim"
	gcEnabledProperty          = "gc.enabled"
	deletionFenceTablePrefix   = "ingestr_deleting_"

	clientSidePurgeTimeout     = 30 * time.Minute
	clientSidePurgeMaxAttempts = 6
)

// ExpiredTableCleanupResult reports catalog entries that were physically
// purged by CleanupExpiredManagedTables. A table is eligible only when it was
// explicitly marked as managed by ingestr and its persisted deadline passed.
type ExpiredTableCleanupResult struct {
	Purged []string
}

func lifecycleProperties(base iceberggo.Properties, expiresAfter time.Duration, now time.Time) (iceberggo.Properties, error) {
	props := maps.Clone(base)
	if props == nil {
		props = iceberggo.Properties{}
	}
	if expiresAfter < 0 {
		return nil, fmt.Errorf("iceberg: table expiration must not be negative: %s", expiresAfter)
	}
	if expiresAfter == 0 {
		return props, nil
	}

	props[managedTableProperty] = "true"
	props[managedTableKindProperty] = managedTableKindStaging
	props[managedTableExpiresAt] = now.UTC().Add(expiresAfter).Format(time.RFC3339Nano)
	props[managedTableExpiresAfterMS] = strconv.FormatInt(expiresAfter.Milliseconds(), 10)
	delete(props, managedTablePurgeClaim)
	// ExpiresAfter means the caller explicitly handed lifecycle ownership to
	// ingestr. Force GC on so dropping the managed table cannot silently leave
	// its data and metadata behind because of a generic URI table property.
	props[gcEnabledProperty] = "true"
	return props, nil
}

func lifecyclePropertiesForCreate(base iceberggo.Properties, expiresAfter time.Duration) (iceberggo.Properties, error) {
	return lifecycleProperties(base, expiresAfter, time.Now())
}

func managedTableExpiration(props iceberggo.Properties) (time.Time, bool, error) {
	managed, err := strconv.ParseBool(props.Get(managedTableProperty, "false"))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("invalid %s property %q: %w", managedTableProperty, props[managedTableProperty], err)
	}
	if !managed {
		return time.Time{}, false, nil
	}

	raw := strings.TrimSpace(props[managedTableExpiresAt])
	if raw == "" {
		return time.Time{}, false, fmt.Errorf("managed table is missing %s", managedTableExpiresAt)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("invalid %s property %q: %w", managedTableExpiresAt, raw, err)
	}
	return expiresAt, true, nil
}

// prepareExistingTableLifecycle purges a stale managed table before it can be
// reused and refreshes the persisted lease on a live managed table. It returns
// true when an expired table was removed and must be recreated.
func (d *Destination) prepareExistingTableLifecycle(
	ctx context.Context,
	ident icebergtable.Identifier,
	expiresAfter time.Duration,
	now time.Time,
) (bool, error) {
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		if isMissingTableOrNamespace(err) {
			return false, nil
		}
		return false, fmt.Errorf("iceberg: failed to load table %s for lifecycle check: %w", strings.Join(ident, "."), err)
	}
	if err := validateIsolatedTableFilePaths(tbl.Properties()); err != nil {
		return false, fmt.Errorf("iceberg: table %s: %w", strings.Join(ident, "."), err)
	}

	expiresAt, managed, err := managedTableExpiration(tbl.Properties())
	if err != nil {
		return false, fmt.Errorf("iceberg: table %s: %w", strings.Join(ident, "."), err)
	}
	if managed && !now.Before(expiresAt) {
		purged, err := d.purgeExpiredManagedTable(ctx, ident, now)
		if err != nil {
			return false, err
		}
		return purged, nil
	}
	if expiresAfter == 0 {
		return false, nil
	}
	if !managed {
		return false, fmt.Errorf(
			"iceberg: refusing to claim existing unmanaged table %s as managed staging data",
			strings.Join(ident, "."),
		)
	}

	desired, err := lifecycleProperties(nil, expiresAfter, now)
	if err != nil {
		return false, err
	}
	updates := iceberggo.Properties{}
	for key, value := range desired {
		if tbl.Properties()[key] != value {
			updates[key] = value
		}
	}
	if len(updates) == 0 {
		return false, nil
	}
	txn := tbl.NewTransaction()
	if err := txn.SetProperties(updates); err != nil {
		return false, fmt.Errorf("iceberg: failed to stage lifecycle properties for table %s: %w", strings.Join(ident, "."), err)
	}
	if _, err := txn.Commit(ctx); err != nil {
		return false, fmt.Errorf("iceberg: failed to persist lifecycle properties for table %s: %w", strings.Join(ident, "."), err)
	}
	return false, nil
}

func (d *Destination) prepareExistingTableLifecycleNow(
	ctx context.Context,
	ident icebergtable.Identifier,
	expiresAfter time.Duration,
) (bool, error) {
	return d.prepareExistingTableLifecycle(ctx, ident, expiresAfter, time.Now())
}

func (d *Destination) refreshManagedTableLease(ctx context.Context, tbl *icebergtable.Table, now time.Time) error {
	expiresAt, ttl, managed, err := managedTableLease(tbl.Properties())
	if err != nil || !managed {
		return err
	}
	if claim := strings.TrimSpace(tbl.Properties()[managedTablePurgeClaim]); claim != "" {
		return fmt.Errorf("managed table %s has been claimed for purge at %s", strings.Join(tbl.Identifier(), "."), claim)
	}
	if now.Add(ttl / 2).Before(expiresAt) {
		return nil
	}
	_, err = commitManagedTableLease(ctx, tbl, now, ttl)
	return err
}

func managedTableLease(props iceberggo.Properties) (time.Time, time.Duration, bool, error) {
	expiresAt, managed, err := managedTableExpiration(props)
	if err != nil || !managed {
		return expiresAt, 0, managed, err
	}
	rawTTL := strings.TrimSpace(props[managedTableExpiresAfterMS])
	ttlMS, err := strconv.ParseInt(rawTTL, 10, 64)
	if err != nil {
		return time.Time{}, 0, false, fmt.Errorf("invalid %s property %q: %w", managedTableExpiresAfterMS, rawTTL, err)
	}
	if ttlMS <= 0 {
		return time.Time{}, 0, false, fmt.Errorf("invalid %s property %q: must be positive", managedTableExpiresAfterMS, rawTTL)
	}
	const maxDurationMilliseconds = int64(1<<63-1) / int64(time.Millisecond)
	if ttlMS > maxDurationMilliseconds {
		return time.Time{}, 0, false, fmt.Errorf("invalid %s property %q: duration overflows", managedTableExpiresAfterMS, rawTTL)
	}
	return expiresAt, time.Duration(ttlMS) * time.Millisecond, true, nil
}

func commitManagedTableLease(
	ctx context.Context,
	tbl *icebergtable.Table,
	now time.Time,
	ttl time.Duration,
) (*icebergtable.Table, error) {
	txn := tbl.NewTransaction()
	if err := txn.SetProperties(iceberggo.Properties{
		managedTableExpiresAt: now.UTC().Add(ttl).Format(time.RFC3339Nano),
	}); err != nil {
		return nil, err
	}
	return txn.Commit(ctx)
}

func (d *Destination) renewManagedTableLease(
	ctx context.Context,
	ident icebergtable.Identifier,
	now time.Time,
) (*icebergtable.Table, error) {
	const maxAttempts = 5
	var commitErr error
	for range maxAttempts {
		tbl, err := d.catalog.LoadTable(ctx, ident)
		if err != nil {
			return nil, err
		}
		_, ttl, managed, err := managedTableLease(tbl.Properties())
		if err != nil {
			return nil, err
		}
		if !managed {
			return tbl, nil
		}
		if claim := strings.TrimSpace(tbl.Properties()[managedTablePurgeClaim]); claim != "" {
			return nil, fmt.Errorf("managed table %s has been claimed for purge at %s", strings.Join(ident, "."), claim)
		}
		committed, err := commitManagedTableLease(ctx, tbl, now, ttl)
		if err == nil {
			return committed, nil
		}
		commitErr = err
		if !errors.Is(err, icebergtable.ErrCommitFailed) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("failed to refresh managed table %s lease after concurrent commits: %w", strings.Join(ident, "."), commitErr)
}

type managedLeaseHeartbeat struct {
	destination *Destination
	ident       icebergtable.Identifier
	cancel      context.CancelFunc
	done        chan struct{}
	stopOnce    sync.Once
	errMu       sync.Mutex
	err         error
}

func (d *Destination) startManagedTableLeaseHeartbeat(
	ctx context.Context,
	tbl *icebergtable.Table,
) (*managedLeaseHeartbeat, error) {
	return d.startManagedTableLeaseHeartbeatWithCancel(ctx, tbl, nil)
}

func (d *Destination) startManagedTableLeaseHeartbeatWithCancel(
	ctx context.Context,
	tbl *icebergtable.Table,
	cancelOwner context.CancelFunc,
) (*managedLeaseHeartbeat, error) {
	_, _, managed, err := managedTableLease(tbl.Properties())
	if err != nil || !managed {
		return nil, err
	}
	tbl, err = d.renewManagedTableLease(ctx, tbl.Identifier(), time.Now())
	if err != nil {
		return nil, err
	}
	_, ttl, _, err := managedTableLease(tbl.Properties())
	if err != nil {
		return nil, err
	}
	interval := ttl / 4
	if d.leaseHeartbeatInterval > 0 {
		interval = d.leaseHeartbeatInterval
	}
	if interval < time.Millisecond {
		interval = time.Millisecond
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)
	heartbeat := &managedLeaseHeartbeat{
		destination: d,
		ident:       slices.Clone(tbl.Identifier()),
		cancel:      cancel,
		done:        make(chan struct{}),
	}
	go func() {
		defer close(heartbeat.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case now := <-ticker.C:
				if _, err := d.renewManagedTableLease(heartbeatCtx, heartbeat.ident, now); err != nil && heartbeatCtx.Err() == nil {
					heartbeat.errMu.Lock()
					heartbeat.err = err
					heartbeat.errMu.Unlock()
					if cancelOwner != nil {
						cancelOwner()
					}
					return
				}
			}
		}
	}()
	return heartbeat, nil
}

func (h *managedLeaseHeartbeat) stop() {
	if h == nil {
		return
	}
	h.stopOnce.Do(func() {
		h.cancel()
		<-h.done
	})
}

func (h *managedLeaseHeartbeat) stopAndRefresh(ctx context.Context) (*icebergtable.Table, error) {
	h.stop()
	h.errMu.Lock()
	err := h.err
	h.errMu.Unlock()
	if err != nil {
		return nil, err
	}
	return h.destination.renewManagedTableLease(ctx, h.ident, time.Now())
}

func (d *Destination) dropTableWithLifecycle(ctx context.Context, ident icebergtable.Identifier) (retErr error) {
	return d.dropTableWithLifecycleExpected(ctx, ident, "", nil)
}

func (d *Destination) dropTableWithLifecycleExpected(
	ctx context.Context,
	ident icebergtable.Identifier,
	expectedUUID string,
	validate func(*icebergtable.Table) error,
) (retErr error) {
	defer func() {
		if retErr == nil {
			d.forgetPreparedTable(ident)
		}
	}()
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		if isMissingTableOrNamespace(err) {
			if d.usesServerManagedPurge() {
				return d.reconcileMissingServerManagedPurge(ctx, ident)
			}
			return d.resumePurgeJournal(ctx, ident)
		}
		return fmt.Errorf("iceberg: failed to load table %s before drop: %w", strings.Join(ident, "."), err)
	}
	if expectedUUID != "" && tbl.Metadata().TableUUID().String() != expectedUUID {
		return fmt.Errorf("iceberg: refused to drop replaced table %s: UUID changed from %s to %s", strings.Join(ident, "."), expectedUUID, tbl.Metadata().TableUUID())
	}
	if validate != nil {
		if err := validate(tbl); err != nil {
			return err
		}
	}

	_, managed, err := managedTableExpiration(tbl.Properties())
	if err != nil {
		return fmt.Errorf("iceberg: table %s: %w", strings.Join(ident, "."), err)
	}
	if !managed {
		if d.catalog.CatalogType() == icebergcatalog.Hadoop {
			return fmt.Errorf("iceberg: cannot drop unmanaged Hadoop table %s without deleting its physical table root", strings.Join(ident, "."))
		}
		claimed, err := d.claimTableForDeletion(ctx, tbl, expectedUUID, validate)
		if err != nil {
			return err
		}
		if err := d.catalog.DropTable(ctx, claimed.Identifier()); err != nil && !isMissingTableOrNamespace(err) {
			restoreErr := d.restoreDeletionClaim(context.WithoutCancel(ctx), claimed, ident)
			return errors.Join(fmt.Errorf("iceberg: failed to drop unmanaged table %s: %w", strings.Join(ident, "."), err), restoreErr)
		}
		return nil
	}
	if !strings.EqualFold(tbl.Properties().Get(gcEnabledProperty, "true"), "true") {
		txn := tbl.NewTransaction()
		if err := txn.SetProperties(iceberggo.Properties{gcEnabledProperty: "true"}); err != nil {
			return fmt.Errorf("iceberg: failed to enable physical purge for table %s: %w", strings.Join(ident, "."), err)
		}
		if _, err := txn.Commit(ctx); err != nil {
			return fmt.Errorf("iceberg: failed to commit physical purge setting for table %s: %w", strings.Join(ident, "."), err)
		}
	}
	if err := d.validateOrphanCleanupIsolation(ctx, tbl); err != nil {
		return fmt.Errorf("iceberg: refused to purge managed table %s: %w", strings.Join(ident, "."), err)
	}
	if expectedUUID == "" {
		expectedUUID = tbl.Metadata().TableUUID().String()
	}
	return d.purgeLoadedTableExpected(ctx, tbl, expectedUUID, validate)
}

func (d *Destination) forgetPreparedTable(ident icebergtable.Identifier) {
	d.mu.Lock()
	delete(d.prepared, strings.Join(ident, "."))
	d.mu.Unlock()
}

func deletionFenceIdentifier(ident icebergtable.Identifier) icebergtable.Identifier {
	fenced := append(icebergtable.Identifier(nil), ident...)
	fenced[len(fenced)-1] = deletionFenceTablePrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	return fenced
}

func (d *Destination) claimTableForDeletion(
	ctx context.Context,
	tbl *icebergtable.Table,
	expectedUUID string,
	validate func(*icebergtable.Table) error,
) (*icebergtable.Table, error) {
	return d.claimTableForDeletionAt(ctx, tbl, expectedUUID, validate, deletionFenceIdentifier(tbl.Identifier()))
}

func (d *Destination) claimTableForDeletionAt(
	ctx context.Context,
	tbl *icebergtable.Table,
	expectedUUID string,
	validate func(*icebergtable.Table) error,
	fencedIdent icebergtable.Identifier,
) (*icebergtable.Table, error) {
	ident := tbl.Identifier()
	if expectedUUID == "" {
		expectedUUID = tbl.Metadata().TableUUID().String()
	}
	if d.cfg.Properties.Get("type", "") == "hadoop" {
		return d.claimHadoopTableForDeletion(ctx, tbl, fencedIdent, expectedUUID, validate)
	}
	switch d.catalog.CatalogType() {
	case icebergcatalog.SQL, icebergcatalog.REST, icebergcatalog.Hive, icebergcatalog.Hadoop:
	default:
		return nil, fmt.Errorf("iceberg: catalog type %s cannot provide an atomic deletion fence for %s; refusing deletion", d.catalog.CatalogType(), strings.Join(ident, "."))
	}
	_, renameErr := d.catalog.RenameTable(ctx, ident, fencedIdent)
	var claimed *icebergtable.Table
	if renameErr != nil {
		// A remote rename can commit despite a lost response. Only accept that
		// outcome when the isolated identifier resolves to the expected UUID.
		var loadErr error
		claimed, loadErr = d.catalog.LoadTable(context.WithoutCancel(ctx), fencedIdent)
		if loadErr != nil || claimed.Metadata().TableUUID().String() != expectedUUID {
			return nil, errors.Join(
				fmt.Errorf("iceberg: catalog cannot provide a server-enforced deletion fence for %s: %w", strings.Join(ident, "."), renameErr),
				loadErr,
			)
		}
	} else {
		// Reload through the active catalog wrapper so filesystem/authentication
		// behavior remains identical to a normal table load.
		claimed, renameErr = d.catalog.LoadTable(ctx, fencedIdent)
		if renameErr != nil {
			return nil, fmt.Errorf("iceberg: failed to load fenced table %s after catalog rename: %w", strings.Join(fencedIdent, "."), renameErr)
		}
	}
	if claimed == nil || claimed.Metadata().TableUUID().String() != expectedUUID {
		actual := "<unknown>"
		if claimed != nil {
			actual = claimed.Metadata().TableUUID().String()
		}
		restoreErr := d.restoreDeletionClaim(context.WithoutCancel(ctx), claimed, ident)
		return nil, errors.Join(
			fmt.Errorf("iceberg: refused to delete replaced table %s: UUID changed from %s to %s", strings.Join(ident, "."), expectedUUID, actual),
			restoreErr,
		)
	}
	if validate != nil {
		if err := validate(claimed); err != nil {
			return nil, errors.Join(err, d.restoreDeletionClaim(context.WithoutCancel(ctx), claimed, ident))
		}
	}
	return claimed, nil
}

func (d *Destination) claimHadoopTableForDeletion(
	ctx context.Context,
	tbl *icebergtable.Table,
	fencedIdent icebergtable.Identifier,
	expectedUUID string,
	validate func(*icebergtable.Table) error,
) (*icebergtable.Table, error) {
	originalPath, ok := localFilesystemPath(tbl.Location())
	if !ok {
		return nil, fmt.Errorf("iceberg: Hadoop catalog cannot provide a server-enforced deletion fence for non-local table %s", strings.Join(tbl.Identifier(), "."))
	}
	fencedPath := filepath.Join(filepath.Dir(originalPath), fencedIdent[len(fencedIdent)-1])
	if err := os.Rename(originalPath, fencedPath); err != nil {
		return nil, fmt.Errorf("iceberg: failed to atomically fence Hadoop table %s for deletion: %w", strings.Join(tbl.Identifier(), "."), err)
	}
	claimed, err := d.catalog.LoadTable(ctx, fencedIdent)
	if err == nil && claimed.Metadata().TableUUID().String() == expectedUUID {
		if validate == nil {
			return claimed, nil
		}
		if validationErr := validate(claimed); validationErr == nil {
			return claimed, nil
		} else {
			err = validationErr
		}
	} else if err == nil {
		err = fmt.Errorf("UUID changed from %s to %s", expectedUUID, claimed.Metadata().TableUUID())
	}
	restoreErr := os.Rename(fencedPath, originalPath)
	return nil, errors.Join(fmt.Errorf("iceberg: refused fenced Hadoop deletion for %s: %w", strings.Join(tbl.Identifier(), "."), err), restoreErr)
}

func (d *Destination) restoreDeletionClaim(
	ctx context.Context,
	claimed *icebergtable.Table,
	original icebergtable.Identifier,
) error {
	if claimed == nil {
		return nil
	}
	if d.cfg.Properties.Get("type", "") == "hadoop" {
		originalPath, ok := localFilesystemPath(claimed.Location())
		if !ok {
			return fmt.Errorf("iceberg: cannot restore non-local Hadoop deletion fence for %s", strings.Join(original, "."))
		}
		fencedPath := filepath.Join(filepath.Dir(originalPath), claimed.Identifier()[len(claimed.Identifier())-1])
		if err := os.Rename(fencedPath, originalPath); err != nil {
			return fmt.Errorf("iceberg: failed to restore Hadoop deletion fence for %s: %w", strings.Join(original, "."), err)
		}
		return nil
	}
	current, err := d.catalog.LoadTable(ctx, claimed.Identifier())
	if err != nil {
		return fmt.Errorf("iceberg: failed to inspect fenced table before restoring %s: %w", strings.Join(original, "."), err)
	}
	if current.Metadata().TableUUID() != claimed.Metadata().TableUUID() {
		return fmt.Errorf("iceberg: refused to restore deletion fence for %s because fenced UUID changed", strings.Join(original, "."))
	}
	if _, err := d.catalog.RenameTable(ctx, claimed.Identifier(), original); err != nil {
		return fmt.Errorf("iceberg: failed to restore deletion fence for %s: %w", strings.Join(original, "."), err)
	}
	return nil
}

func (d *Destination) applyConfiguredTableProperties(ctx context.Context, tbl *icebergtable.Table) (*icebergtable.Table, error) {
	updates := iceberggo.Properties{}
	for key, value := range d.cfg.TableProperties {
		if key == icebergtable.PropertyFormatVersion {
			continue
		}
		if tbl.Properties()[key] != value {
			updates[key] = value
		}
	}
	if len(updates) == 0 {
		return tbl, nil
	}
	txn := tbl.NewTransaction()
	if err := txn.SetProperties(updates); err != nil {
		return nil, fmt.Errorf("iceberg: failed to stage table properties for %s: %w", strings.Join(tbl.Identifier(), "."), err)
	}
	updated, err := txn.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("iceberg: failed to commit table properties for %s: %w", strings.Join(tbl.Identifier(), "."), err)
	}
	return updated, nil
}

func (d *Destination) purgeExpiredManagedTable(
	ctx context.Context,
	ident icebergtable.Identifier,
	now time.Time,
) (bool, error) {
	tableName := strings.Join(ident, ".")
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		if isMissingTableOrNamespace(err) {
			return false, nil
		}
		return false, fmt.Errorf("iceberg: failed to load managed table %s for expiration check: %w", tableName, err)
	}
	expiresAt, managed, err := managedTableExpiration(tbl.Properties())
	if err != nil {
		return false, fmt.Errorf("iceberg: table %s: %w", tableName, err)
	}
	if !managed || now.Before(expiresAt) {
		return false, nil
	}
	if err := d.validateOrphanCleanupIsolation(ctx, tbl); err != nil {
		return false, fmt.Errorf("iceberg: refused to purge managed table %s: %w", tableName, err)
	}

	claimed, eligible, err := d.claimExpiredManagedTable(ctx, ident, now)
	if err != nil || !eligible {
		return false, err
	}
	if err := d.purgeLoadedTable(ctx, claimed); err != nil {
		return false, err
	}
	d.forgetPreparedTable(ident)
	return true, nil
}

func (d *Destination) claimExpiredManagedTable(
	ctx context.Context,
	ident icebergtable.Identifier,
	now time.Time,
) (*icebergtable.Table, bool, error) {
	const maxAttempts = 5
	var commitErr error
	for range maxAttempts {
		tbl, err := d.catalog.LoadTable(ctx, ident)
		if err != nil {
			if isMissingTableOrNamespace(err) {
				return nil, false, nil
			}
			return nil, false, err
		}
		expiresAt, managed, err := managedTableExpiration(tbl.Properties())
		if err != nil {
			return nil, false, err
		}
		if !managed || now.Before(expiresAt) {
			return nil, false, nil
		}
		if rawClaim := strings.TrimSpace(tbl.Properties()[managedTablePurgeClaim]); rawClaim != "" {
			claimedAt, parseErr := time.Parse(time.RFC3339Nano, rawClaim)
			if parseErr != nil || now.Before(claimedAt.Add(purgeResumeClaimTTL)) {
				return tbl, false, nil
			}
		}

		txn := tbl.NewTransaction()
		if err := txn.SetProperties(iceberggo.Properties{
			managedTablePurgeClaim: now.UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return nil, false, err
		}
		claimed, err := txn.Commit(ctx)
		if err == nil {
			return claimed, true, nil
		}
		commitErr = err
		if !errors.Is(err, icebergtable.ErrCommitFailed) {
			return nil, false, err
		}
	}
	return nil, false, fmt.Errorf("failed to claim managed table %s for purge after concurrent commits: %w", strings.Join(ident, "."), commitErr)
}

func (d *Destination) purgeLoadedTable(ctx context.Context, tbl *icebergtable.Table) error {
	return d.purgeLoadedTableExpected(ctx, tbl, tbl.Metadata().TableUUID().String(), nil)
}

func (d *Destination) purgeLoadedTableExpected(
	ctx context.Context,
	tbl *icebergtable.Table,
	expectedUUID string,
	validate func(*icebergtable.Table) error,
) error {
	ident := tbl.Identifier()
	tableName := strings.Join(ident, ".")
	if d.usesServerManagedPurge() {
		return d.purgeServerManagedTable(ctx, tbl, expectedUUID, validate)
	}
	if err := d.resumePurgeJournal(ctx, ident); err != nil {
		return fmt.Errorf("iceberg: cannot begin purge for table %s while prior purge recovery is active: %w", tableName, err)
	}
	journal, tableFS, journalFS, journalPath, lockToken, err := d.createPurgeJournal(ctx, tbl)
	if err != nil {
		return fmt.Errorf("iceberg: failed to prepare durable physical purge for table %s: %w", tableName, err)
	}
	claimed, err := d.claimTableForDeletionAt(ctx, tbl, expectedUUID, validate, journal.DeletionIdentifiers[0])
	if err != nil {
		cleanupErr := d.releasePurgeLockOwned(context.WithoutCancel(ctx), ident, journal.TableUUID, lockToken)
		if cleanupErr == nil {
			cleanupErr = removePurgeJournal(journalFS, journalPath)
		}
		return errors.Join(
			fmt.Errorf("iceberg: failed to claim table %s for fenced catalog deletion", tableName),
			err,
			cleanupErr,
		)
	}
	deleteIdent := claimed.Identifier()

	if d.catalog.CatalogType() == icebergcatalog.REST {
		purger, ok := d.catalog.(icebergcatalog.PurgeableTable)
		if !ok {
			return fmt.Errorf("iceberg: REST catalog cannot safely purge managed table %s", tableName)
		}
		if err := purger.PurgeTable(ctx, deleteIdent); err != nil && !isMissingTableOrNamespace(err) {
			// Do not retry with DropTable here. A remote purge may have committed
			// despite a lost response.
			return d.reconcileFailedCatalogPurge(ctx, tbl, claimed, journalFS, journalPath, lockToken,
				fmt.Errorf("iceberg: failed to purge managed table %s: %w", tableName, err))
		}
	} else if d.catalog.CatalogType() == icebergcatalog.Hadoop {
		if err := d.catalog.DropTable(ctx, deleteIdent); err != nil && !isMissingTableOrNamespace(err) {
			return d.reconcileFailedCatalogPurge(ctx, tbl, claimed, journalFS, journalPath, lockToken,
				fmt.Errorf("iceberg: failed to drop managed table %s before physical purge: %w", tableName, err))
		}
	} else if err := d.catalog.DropTable(ctx, deleteIdent); err != nil {
		if !isMissingTableOrNamespace(err) {
			// The catalog mutation may have an unknown outcome. Deleting files in
			// that state can corrupt a table whose catalog entry still exists.
			return d.reconcileFailedCatalogPurge(ctx, tbl, claimed, journalFS, journalPath, lockToken,
				fmt.Errorf("iceberg: failed to drop managed table %s before physical purge: %w", tableName, err))
		}
	}

	if err := executePurgeJournal(ctx, d, tableFS, journalFS, journalPath, journal, lockToken); err != nil {
		if claimErr := d.relinquishPurgeClaim(context.WithoutCancel(ctx), ident, journal.TableUUID, lockToken); claimErr != nil {
			return errors.Join(
				fmt.Errorf("iceberg: dropped managed table %s but failed to purge physical files: %w", tableName, err),
				fmt.Errorf("iceberg: failed to relinquish purge recovery claim: %w", claimErr),
			)
		}
		return fmt.Errorf("iceberg: dropped managed table %s but failed to purge physical files: %w", tableName, err)
	}
	return nil
}

func (d *Destination) purgeServerManagedTable(
	ctx context.Context,
	tbl *icebergtable.Table,
	expectedUUID string,
	validate func(*icebergtable.Table) error,
) error {
	ident := tbl.Identifier()
	tableName := strings.Join(ident, ".")
	tableUUID := tbl.Metadata().TableUUID().String()
	lockToken, err := d.acquirePurgeLock(ctx, ident, tableUUID)
	if err != nil {
		lock, loadErr := d.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
		if loadErr != nil {
			return fmt.Errorf("iceberg: failed to inspect prior server-managed purge for table %s: %w", tableName, err)
		}
		lockedUUID := lock.Properties()[purgeLockTableUUIDKey]
		if validationErr := validatePurgeLock(lock, ident, lockedUUID); validationErr != nil {
			return validationErr
		}
		lockToken, loadErr = d.claimPurgeResume(ctx, ident, lockedUUID)
		if loadErr != nil {
			return fmt.Errorf("iceberg: failed to claim prior server-managed purge for table %s: %w", tableName, loadErr)
		}
		if lockedUUID != tableUUID {
			if releaseErr := d.releasePurgeLockOwned(ctx, ident, lockedUUID, lockToken); releaseErr != nil {
				return fmt.Errorf("iceberg: refused stale purge for recreated table %s and failed to release its old ownership lock: %w", tableName, releaseErr)
			}
			return fmt.Errorf("iceberg: refused stale purge for recreated table %s: table UUID changed from %s to %s", tableName, lockedUUID, tableUUID)
		}
	}
	purger, ok := d.catalog.(icebergcatalog.PurgeableTable)
	if !ok {
		_ = d.releasePurgeLockOwned(context.WithoutCancel(ctx), ident, tableUUID, lockToken)
		return fmt.Errorf("iceberg: managed REST catalog cannot safely purge table %s", tableName)
	}
	if err := d.renewPurgeLock(ctx, ident, tableUUID, lockToken); err != nil {
		return fmt.Errorf("iceberg: failed to renew server-managed purge fence for table %s: %w", tableName, err)
	}
	liveBeforePurge, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		if isMissingTableOrNamespace(err) {
			if releaseErr := d.releasePurgeLockOwned(context.WithoutCancel(ctx), ident, tableUUID, lockToken); releaseErr != nil {
				return releaseErr
			}
			return nil
		}
		return fmt.Errorf("iceberg: failed to revalidate table %s immediately before purge: %w", tableName, err)
	}
	if liveBeforePurge.Metadata().TableUUID().String() != tableUUID {
		if releaseErr := d.releasePurgeLockOwned(context.WithoutCancel(ctx), ident, tableUUID, lockToken); releaseErr != nil {
			return errors.Join(fmt.Errorf("iceberg: refused server-managed purge for recreated table %s", tableName), releaseErr)
		}
		return fmt.Errorf("iceberg: refused server-managed purge for recreated table %s: table UUID changed from %s to %s", tableName, tableUUID, liveBeforePurge.Metadata().TableUUID())
	}
	claimed, err := d.claimTableForDeletion(ctx, liveBeforePurge, expectedUUID, validate)
	if err != nil {
		if releaseErr := d.releasePurgeLockOwned(context.WithoutCancel(ctx), ident, tableUUID, lockToken); releaseErr != nil {
			return errors.Join(err, releaseErr)
		}
		return err
	}
	purgeCtx, cancelPurge := context.WithCancel(ctx)
	heartbeat := d.startCatalogLockHeartbeat(purgeCtx, ident, purgeLockModePurge, tableUUID, lockToken, purgeResumeClaimTTL, cancelPurge)
	purgeErr := purger.PurgeTable(purgeCtx, claimed.Identifier())
	heartbeatErr := heartbeat.stop()
	cancelPurge()
	if heartbeatErr != nil {
		var restoreErr error
		if live, loadErr := d.catalog.LoadTable(context.WithoutCancel(ctx), claimed.Identifier()); loadErr == nil &&
			live.Metadata().TableUUID().String() == tableUUID {
			restoreErr = d.restoreDeletionClaim(context.WithoutCancel(ctx), live, ident)
		}
		return errors.Join(fmt.Errorf("iceberg: server-managed purge lock heartbeat failed for table %s", tableName), heartbeatErr, purgeErr, restoreErr)
	}
	if purgeErr == nil || isMissingTableOrNamespace(purgeErr) {
		if err := d.releasePurgeLockOwned(context.WithoutCancel(ctx), ident, tableUUID, lockToken); err != nil {
			return fmt.Errorf("iceberg: server-managed purge completed for table %s but its ownership lock could not be released: %w", tableName, err)
		}
		return nil
	}
	live, loadErr := d.catalog.LoadTable(ctx, claimed.Identifier())
	if loadErr == nil && live.Metadata().TableUUID().String() == tableUUID {
		if restoreErr := d.restoreDeletionClaim(context.WithoutCancel(ctx), live, ident); restoreErr != nil {
			return errors.Join(purgeErr, fmt.Errorf("failed to restore table after failed server-managed purge: %w", restoreErr))
		}
		if err := d.releasePurgeLockOwned(context.WithoutCancel(ctx), ident, tableUUID, lockToken); err != nil {
			return errors.Join(purgeErr, fmt.Errorf("failed to release failed server-managed purge lock: %w", err))
		}
	} else if err := d.relinquishPurgeClaim(context.WithoutCancel(ctx), ident, tableUUID, lockToken); err != nil {
		return errors.Join(purgeErr, fmt.Errorf("failed to relinquish uncertain server-managed purge claim: %w", err))
	}
	return fmt.Errorf("iceberg: failed to purge managed table %s: %w", tableName, purgeErr)
}

func (d *Destination) reconcileMissingServerManagedPurge(ctx context.Context, ident icebergtable.Identifier) error {
	lock, err := d.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	if isMissingTableOrNamespace(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("iceberg: failed to inspect server-managed purge ownership for %s: %w", strings.Join(ident, "."), err)
	}
	if lock.Properties()[purgeLockModeKey] == purgeLockModeIdle {
		return nil
	}
	tableUUID := lock.Properties()[purgeLockTableUUIDKey]
	if err := validatePurgeLock(lock, ident, tableUUID); err != nil {
		return err
	}
	token, err := d.claimPurgeResume(ctx, ident, tableUUID)
	if err != nil {
		return err
	}
	if lock.Properties()[purgeLockModeKey] == purgeLockModeIdle {
		return nil
	}
	if err := d.releasePurgeLockOwned(ctx, ident, tableUUID, token); err != nil {
		return fmt.Errorf("iceberg: failed to release completed server-managed purge ownership for %s: %w", strings.Join(ident, "."), err)
	}
	return nil
}

func (d *Destination) ensureNoServerManagedPurgeLock(ctx context.Context, ident icebergtable.Identifier) error {
	lock, err := d.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	if isMissingTableOrNamespace(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if lock.Properties()[purgeLockModeKey] == purgeLockModeIdle {
		return nil
	}
	return fmt.Errorf(
		"iceberg: server-managed purge for %s with original table UUID %s must be reconciled before recreation",
		strings.Join(ident, "."), lock.Properties()[purgeLockTableUUIDKey],
	)
}

func (d *Destination) reconcileFailedCatalogPurge(
	ctx context.Context,
	original *icebergtable.Table,
	claimed *icebergtable.Table,
	journalFS icebergio.IO,
	journalPath string,
	lockToken string,
	purgeErr error,
) error {
	live, err := d.catalog.LoadTable(ctx, claimed.Identifier())
	if err != nil || live.Metadata().TableUUID() != original.Metadata().TableUUID() {
		if claimErr := d.relinquishPurgeClaim(context.WithoutCancel(ctx), original.Identifier(), original.Metadata().TableUUID().String(), lockToken); claimErr != nil {
			return errors.Join(purgeErr, fmt.Errorf("failed to relinquish purge recovery claim: %w", claimErr))
		}
		return purgeErr
	}
	if err := d.restoreDeletionClaim(context.WithoutCancel(ctx), live, original.Identifier()); err != nil {
		return errors.Join(purgeErr, fmt.Errorf("failed to restore table after failed fenced deletion: %w", err))
	}
	if err := d.releasePurgeLockOwned(context.WithoutCancel(ctx), original.Identifier(), original.Metadata().TableUUID().String(), lockToken); err != nil {
		return errors.Join(purgeErr, fmt.Errorf("failed to release abandoned purge lock: %w", err))
	}
	if err := removePurgeJournal(journalFS, journalPath); err != nil {
		return errors.Join(purgeErr, fmt.Errorf("failed to remove abandoned purge journal: %w", err))
	}
	return purgeErr
}

func (d *Destination) usesServerManagedPurge() bool {
	return d.catalog != nil && d.catalog.CatalogType() == icebergcatalog.REST &&
		strings.EqualFold(d.cfg.Properties.Get("rest.signing-name", ""), "s3tables")
}

func filesUnderTableLocation(ctx context.Context, tableFS icebergio.ListableIO, location string) ([]string, error) {
	var files []string
	err := tableFS.WalkDir(location, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if isObjectNotFound(walkErr) {
				return nil
			}
			return walkErr
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if isObjectNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list table location %s: %w", location, err)
	}
	return files, nil
}

func removeTableFiles(ctx context.Context, tableFS icebergio.IO, files []string) error {
	if bulk, ok := tableFS.(icebergio.BulkRemovableIO); ok {
		_, err := bulk.DeleteFiles(ctx, files)
		return err
	}

	var removeErr error
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return errors.Join(removeErr, err)
		}
		if err := tableFS.Remove(path); err != nil && !isObjectNotFound(err) {
			removeErr = errors.Join(removeErr, fmt.Errorf("failed to remove %s: %w", path, err))
		}
	}
	return removeErr
}

// CleanupExpiredManagedTables enforces persisted ExpiresAfter deadlines for
// one namespace. It never touches an unmarked table and never falls back to a
// catalog-only drop when physical purge is unavailable or has unknown status.
func (d *Destination) CleanupExpiredManagedTables(ctx context.Context, namespace string) (ExpiredTableCleanupResult, error) {
	if d.catalog == nil {
		return ExpiredTableCleanupResult{}, errors.New("iceberg destination not connected")
	}

	var ident icebergtable.Identifier
	if strings.TrimSpace(namespace) != "" {
		var err error
		ident, err = parseIdentifier(namespace + ".placeholder")
		if err != nil {
			return ExpiredTableCleanupResult{}, err
		}
		ident = icebergcatalog.NamespaceFromIdent(ident)
	}
	if err := d.sweepPurgeJournals(ctx, ident); err != nil {
		return ExpiredTableCleanupResult{}, err
	}
	return d.cleanupExpiredManagedTables(ctx, ident, time.Now())
}

func (d *Destination) cleanupExpiredManagedTables(
	ctx context.Context,
	namespace icebergtable.Identifier,
	now time.Time,
) (ExpiredTableCleanupResult, error) {
	return d.cleanupExpiredManagedTablesExcept(ctx, namespace, nil, now)
}

func (d *Destination) cleanupExpiredManagedTablesExcept(
	ctx context.Context,
	namespace icebergtable.Identifier,
	exclude icebergtable.Identifier,
	now time.Time,
) (ExpiredTableCleanupResult, error) {
	result := ExpiredTableCleanupResult{}
	for ident, listErr := range d.catalog.ListTables(ctx, namespace) {
		if listErr != nil {
			return result, fmt.Errorf("iceberg: failed to list tables in namespace %s: %w", strings.Join(namespace, "."), listErr)
		}
		if slices.Equal(ident, exclude) {
			continue
		}
		purged, err := d.purgeExpiredManagedTable(ctx, ident, now)
		if err != nil {
			return result, err
		}
		if purged {
			result.Purged = append(result.Purged, strings.Join(ident, "."))
		}
	}
	slices.Sort(result.Purged)
	return result, nil
}
