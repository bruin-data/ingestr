package iceberg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/google/uuid"
)

const (
	purgeLockTablePrefix  = "ingestr_purge_lock_"
	purgeLockTargetKey    = "ingestr.purge-lock-target"
	purgeLockTableUUIDKey = "ingestr.purge-lock-table-uuid"
	purgeLockModeKey      = "ingestr.purge-lock-mode"
	purgeLockTokenKey     = "ingestr.purge-lock-token"
	purgeLockExpiresAtKey = "ingestr.purge-lock-expires-at"
	purgeLockModePurge    = "purge"
	purgeLockModeCreate   = "create"
	purgeLockModeIdle     = "idle"
	purgeLockModeCleanup  = "cleanup"
)

const (
	purgeResumeClaimTTL = 45 * time.Minute
	createGuardTTL      = 5 * time.Minute
)

func (d *Destination) acquirePurgeLock(ctx context.Context, ident icebergtable.Identifier, tableUUID string) (string, error) {
	lockIdent := purgeLockIdentifier(ident)
	lockSchema := iceberggo.NewSchema(0, iceberggo.NestedField{
		ID: 1, Name: "locked", Type: iceberggo.PrimitiveTypes.Bool, Required: true,
	})
	for range 5 {
		token := uuid.NewString()
		now := time.Now().UTC()
		existing, err := d.catalog.LoadTable(ctx, lockIdent)
		if isMissingTableOrNamespace(err) {
			_, err = d.catalog.CreateTable(ctx, lockIdent, lockSchema, icebergcatalog.WithProperties(iceberggo.Properties{
				purgeLockTargetKey: strings.Join(ident, "."), purgeLockTableUUIDKey: tableUUID,
				purgeLockModeKey: purgeLockModePurge, purgeLockTokenKey: token,
				purgeLockExpiresAtKey: now.Add(purgeResumeClaimTTL).Format(time.RFC3339Nano),
			}))
			if err == nil {
				return token, nil
			}
			if errors.Is(err, icebergcatalog.ErrTableAlreadyExists) || strings.Contains(strings.ToLower(errString(err)), "already exists") || strings.Contains(strings.ToLower(errString(err)), "unique constraint") {
				continue
			}
			return "", err
		}
		if err != nil {
			return "", err
		}
		if existing.Properties()[purgeLockModeKey] != purgeLockModeIdle {
			if validationErr := validatePurgeLock(existing, ident, tableUUID); validationErr != nil {
				return "", validationErr
			}
			expiresAt, parseErr := time.Parse(time.RFC3339Nano, existing.Properties()[purgeLockExpiresAtKey])
			if parseErr != nil || now.Before(expiresAt) {
				return "", fmt.Errorf("iceberg: purge lock for %s is already held", strings.Join(ident, "."))
			}
			if !d.usesServerManagedPurge() {
				journalExists, journalErr := d.purgeJournalExists(ctx, ident)
				if journalErr != nil {
					return "", fmt.Errorf("iceberg: failed to inspect journal for expired purge lock %s: %w", strings.Join(ident, "."), journalErr)
				}
				if journalExists {
					return "", fmt.Errorf("iceberg: expired purge lock for %s still has a recovery journal", strings.Join(ident, "."))
				}
			}
			live, liveErr := d.catalog.LoadTable(ctx, ident)
			if liveErr != nil {
				return "", fmt.Errorf("iceberg: cannot reclaim expired purge lock for %s without a live UUID check: %w", strings.Join(ident, "."), liveErr)
			}
			if live.Metadata().TableUUID().String() != tableUUID {
				return "", fmt.Errorf("iceberg: cannot reclaim expired purge lock for %s: live table UUID changed", strings.Join(ident, "."))
			}
		}
		txn := existing.NewTransaction()
		if err := txn.SetProperties(iceberggo.Properties{
			purgeLockTargetKey: strings.Join(ident, "."), purgeLockTableUUIDKey: tableUUID,
			purgeLockModeKey: purgeLockModePurge, purgeLockTokenKey: token,
			purgeLockExpiresAtKey: now.Add(purgeResumeClaimTTL).Format(time.RFC3339Nano),
		}); err != nil {
			return "", err
		}
		if _, err := txn.Commit(ctx); err == nil {
			return token, nil
		} else if !errors.Is(err, icebergtable.ErrCommitFailed) {
			return "", err
		}
	}
	return "", fmt.Errorf("iceberg: failed to acquire purge lock for %s after concurrent updates", strings.Join(ident, "."))
}

func (d *Destination) acquireCreateGuard(ctx context.Context, ident icebergtable.Identifier) (string, error) {
	lockIdent := purgeLockIdentifier(ident)
	lockSchema := iceberggo.NewSchema(0, iceberggo.NestedField{
		ID: 1, Name: "locked", Type: iceberggo.PrimitiveTypes.Bool, Required: true,
	})
	for range 5 {
		now := time.Now().UTC()
		token := uuid.NewString()
		existing, err := d.catalog.LoadTable(ctx, lockIdent)
		if isMissingTableOrNamespace(err) {
			_, err = d.catalog.CreateTable(
				ctx, lockIdent, lockSchema,
				icebergcatalog.WithProperties(iceberggo.Properties{
					purgeLockTargetKey:    strings.Join(ident, "."),
					purgeLockModeKey:      purgeLockModeCreate,
					purgeLockTokenKey:     token,
					purgeLockExpiresAtKey: now.Add(createGuardTTL).Format(time.RFC3339Nano),
				}),
			)
			if err == nil {
				return token, nil
			}
			if errors.Is(err, icebergcatalog.ErrTableAlreadyExists) ||
				strings.Contains(strings.ToLower(errString(err)), "unique constraint") ||
				strings.Contains(strings.ToLower(errString(err)), "already exists") {
				continue
			}
			return "", fmt.Errorf("iceberg: failed to acquire create guard for %s: %w", strings.Join(ident, "."), err)
		}
		if err != nil {
			return "", fmt.Errorf("iceberg: failed to inspect create guard for %s: %w", strings.Join(ident, "."), err)
		}
		mode := existing.Properties()[purgeLockModeKey]
		if mode != purgeLockModeCreate && mode != purgeLockModeIdle && mode != purgeLockModeCleanup ||
			existing.Properties()[purgeLockTargetKey] != strings.Join(ident, ".") {
			return "", fmt.Errorf("iceberg: cannot create table %s while a durable purge lock exists", strings.Join(ident, "."))
		}
		if mode == purgeLockModeCreate || mode == purgeLockModeCleanup {
			expiresAt, parseErr := time.Parse(time.RFC3339Nano, existing.Properties()[purgeLockExpiresAtKey])
			if parseErr != nil || now.Before(expiresAt) {
				return "", fmt.Errorf("iceberg: cannot create table %s while a durable %s guard exists", strings.Join(ident, "."), mode)
			}
		}
		if !d.usesServerManagedPurge() {
			journalExists, err := d.purgeJournalExists(ctx, ident)
			if err != nil {
				return "", fmt.Errorf("iceberg: cannot reclaim expired create guard for %s without checking its purge journal: %w", strings.Join(ident, "."), err)
			}
			if journalExists {
				return "", fmt.Errorf("iceberg: cannot reclaim expired create guard for %s while a durable purge journal exists", strings.Join(ident, "."))
			}
		}
		txn := existing.NewTransaction()
		if err := txn.SetProperties(iceberggo.Properties{
			purgeLockModeKey:      purgeLockModeCreate,
			purgeLockTargetKey:    strings.Join(ident, "."),
			purgeLockTableUUIDKey: "",
			purgeLockTokenKey:     token,
			purgeLockExpiresAtKey: now.Add(createGuardTTL).Format(time.RFC3339Nano),
		}); err != nil {
			return "", err
		}
		if _, err := txn.Commit(ctx); err == nil {
			return token, nil
		} else if !errors.Is(err, icebergtable.ErrCommitFailed) {
			return "", fmt.Errorf("iceberg: failed to reclaim expired create guard for %s: %w", strings.Join(ident, "."), err)
		}
	}
	return "", fmt.Errorf("iceberg: failed to acquire create guard for %s after concurrent updates", strings.Join(ident, "."))
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (d *Destination) releaseCreateGuard(ctx context.Context, ident icebergtable.Identifier, token string) error {
	lockIdent := purgeLockIdentifier(ident)
	lock, err := d.catalog.LoadTable(ctx, lockIdent)
	if err != nil {
		return err
	}
	if lock.Properties()[purgeLockModeKey] != purgeLockModeCreate || lock.Properties()[purgeLockTokenKey] != token {
		return fmt.Errorf("iceberg: create guard ownership changed for %s", strings.Join(ident, "."))
	}
	return commitLockIdle(ctx, lock)
}

func (d *Destination) releasePurgeLockOwned(ctx context.Context, ident icebergtable.Identifier, tableUUID, token string) error {
	lockIdent := purgeLockIdentifier(ident)
	lock, err := d.catalog.LoadTable(ctx, lockIdent)
	if isMissingTableOrNamespace(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := validatePurgeLock(lock, ident, tableUUID); err != nil {
		return err
	}
	if lock.Properties()[purgeLockTokenKey] != token {
		return fmt.Errorf("iceberg: purge lock ownership changed for %s", strings.Join(ident, "."))
	}
	return commitLockIdle(ctx, lock)
}

func commitLockIdle(ctx context.Context, lock *icebergtable.Table) error {
	txn := lock.NewTransaction()
	if err := txn.SetProperties(iceberggo.Properties{
		purgeLockModeKey: purgeLockModeIdle, purgeLockTokenKey: "", purgeLockExpiresAtKey: "", purgeLockTableUUIDKey: "",
	}); err != nil {
		return err
	}
	_, err := txn.Commit(ctx)
	return err
}

type catalogLockHeartbeat struct {
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

func (d *Destination) startCatalogLockHeartbeat(
	ctx context.Context,
	ident icebergtable.Identifier,
	mode, tableUUID, token string,
	ttl time.Duration,
	cancelOwner context.CancelFunc,
) *catalogLockHeartbeat {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	heartbeat := &catalogLockHeartbeat{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(heartbeat.done)
		interval := ttl / 4
		if d.catalogLockHeartbeatInterval > 0 {
			interval = d.catalogLockHeartbeatInterval
		}
		if interval < time.Millisecond {
			interval = time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if err := d.renewCatalogLock(heartbeatCtx, ident, mode, tableUUID, token, ttl); err != nil {
					// stop cancels heartbeatCtx before joining this goroutine. A
					// renewal already in flight can therefore return ctx.Err(); that
					// is a clean shutdown, not a lost ownership lease.
					if heartbeatCtx.Err() != nil {
						return
					}
					heartbeat.mu.Lock()
					heartbeat.err = err
					heartbeat.mu.Unlock()
					if cancelOwner != nil {
						cancelOwner()
					}
					return
				}
			}
		}
	}()
	return heartbeat
}

func (h *catalogLockHeartbeat) stop() error {
	h.cancel()
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

func (d *Destination) renewCatalogLock(
	ctx context.Context,
	ident icebergtable.Identifier,
	mode, tableUUID, token string,
	ttl time.Duration,
) error {
	lock, err := d.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	if err != nil {
		return err
	}
	if lock.Properties()[purgeLockModeKey] != mode || lock.Properties()[purgeLockTargetKey] != strings.Join(ident, ".") ||
		lock.Properties()[purgeLockTableUUIDKey] != tableUUID || lock.Properties()[purgeLockTokenKey] != token {
		return fmt.Errorf("iceberg: catalog lock ownership changed for %s", strings.Join(ident, "."))
	}
	txn := lock.NewTransaction()
	if err := txn.SetProperties(iceberggo.Properties{
		purgeLockExpiresAtKey: time.Now().UTC().Add(ttl).Format(time.RFC3339Nano),
	}); err != nil {
		return err
	}
	_, err = txn.Commit(ctx)
	return err
}

func (d *Destination) claimPurgeResume(ctx context.Context, ident icebergtable.Identifier, tableUUID string) (string, error) {
	lock, err := d.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	if err != nil {
		return "", fmt.Errorf("iceberg: failed to load durable purge lock for %s: %w", strings.Join(ident, "."), err)
	}
	if err := validatePurgeLock(lock, ident, tableUUID); err != nil {
		return "", err
	}
	properties := lock.Properties()
	if existing := properties[purgeLockTokenKey]; existing != "" {
		expiresAt, parseErr := time.Parse(time.RFC3339Nano, properties[purgeLockExpiresAtKey])
		if parseErr != nil || time.Now().UTC().Before(expiresAt) {
			return "", fmt.Errorf("iceberg: purge recovery for %s is already claimed", strings.Join(ident, "."))
		}
	}
	token := uuid.NewString()
	txn := lock.NewTransaction()
	if err := txn.SetProperties(iceberggo.Properties{
		purgeLockTokenKey:     token,
		purgeLockExpiresAtKey: time.Now().UTC().Add(purgeResumeClaimTTL).Format(time.RFC3339Nano),
	}); err != nil {
		return "", fmt.Errorf("iceberg: failed to stage purge recovery claim for %s: %w", strings.Join(ident, "."), err)
	}
	if _, err := txn.Commit(ctx); err != nil {
		return "", fmt.Errorf("iceberg: failed to claim purge recovery for %s: %w", strings.Join(ident, "."), err)
	}
	claimed, err := d.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	if err != nil {
		return "", fmt.Errorf("iceberg: failed to verify purge recovery claim for %s: %w", strings.Join(ident, "."), err)
	}
	if claimed.Properties()[purgeLockTokenKey] != token {
		return "", fmt.Errorf("iceberg: purge recovery claim for %s was lost concurrently", strings.Join(ident, "."))
	}
	return token, nil
}

func (d *Destination) relinquishPurgeClaim(ctx context.Context, ident icebergtable.Identifier, tableUUID, token string) error {
	lock, err := d.catalog.LoadTable(ctx, purgeLockIdentifier(ident))
	if err != nil {
		return err
	}
	if err := validatePurgeLock(lock, ident, tableUUID); err != nil {
		return err
	}
	if lock.Properties()[purgeLockTokenKey] != token {
		return fmt.Errorf("iceberg: purge lock ownership changed for %s", strings.Join(ident, "."))
	}
	txn := lock.NewTransaction()
	if err := txn.SetProperties(iceberggo.Properties{
		purgeLockTokenKey:     "",
		purgeLockExpiresAtKey: "",
	}); err != nil {
		return err
	}
	_, err = txn.Commit(ctx)
	return err
}

func (d *Destination) renewPurgeLock(ctx context.Context, ident icebergtable.Identifier, tableUUID, token string) error {
	return d.renewCatalogLock(ctx, ident, purgeLockModePurge, tableUUID, token, purgeResumeClaimTTL)
}

func validatePurgeLock(lock *icebergtable.Table, ident icebergtable.Identifier, tableUUID string) error {
	wantTarget := strings.Join(ident, ".")
	if lock.Properties()[purgeLockModeKey] != purgeLockModePurge ||
		lock.Properties()[purgeLockTargetKey] != wantTarget || lock.Properties()[purgeLockTableUUIDKey] != tableUUID {
		return fmt.Errorf(
			"iceberg: conflicting purge lock %s targets %q with table UUID %q; expected %q and %q",
			strings.Join(lock.Identifier(), "."), lock.Properties()[purgeLockTargetKey],
			lock.Properties()[purgeLockTableUUIDKey], wantTarget, tableUUID,
		)
	}
	return nil
}

func purgeLockIdentifier(ident icebergtable.Identifier) icebergtable.Identifier {
	lockIdent := append(icebergtable.Identifier(nil), icebergcatalog.NamespaceFromIdent(ident)...)
	return append(lockIdent, purgeLockTablePrefix+identifierHash("", ident)[:24])
}
