package starrocks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	mysqldriver "github.com/go-sql-driver/mysql"
)

// MySQL CDC needs the destination to serialize connector runs and to fence
// target mutations by physical table identity. StarRocks has neither advisory
// locks nor multi-statement transactions, so:
//
//   - the run lease is a table whose exclusive CREATE and RENAME are atomic in
//     the FE catalog, kept alive by heartbeats and checked every second;
//   - conditional merges, truncates, and swaps check the table identity
//     immediately before and after their single atomic statement while the
//     lease is held, so a replaced target fails the run before state is saved;
//   - multi-table merges apply per table in order. A crash leaves state
//     uncommitted and the next run replays the batch; LSN-ordered merges make
//     the replay idempotent.
const (
	cdcLeaseDatabase    = "_bruin_staging"
	cdcLeaseTTL         = 30 * time.Second
	cdcLeaseHeartbeat   = 5 * time.Second
	cdcLeaseCheck       = time.Second
	cdcLeaseCommentHead = "ingestr-cdc-lease:"
)

var (
	_ destination.ManagedCDCRunLeaser        = (*StarRocksDestination)(nil)
	_ destination.CDCConditionalMergeCapable = (*StarRocksDestination)(nil)
	_ destination.CDCConditionalTruncater    = (*StarRocksDestination)(nil)
	_ destination.CDCMultiTableAtomicMerger  = (*StarRocksDestination)(nil)
	_ destination.CDCLateTargetClaimPreparer = (*StarRocksDestination)(nil)
	_ destination.CDCConditionalSwapCapable  = (*StarRocksDestination)(nil)
	_ destination.CDCConditionalSwapPlanner  = (*StarRocksDestination)(nil)
)

func (d *StarRocksDestination) SupportsCDCConditionalMerge() bool { return true }
func (d *StarRocksDestination) SupportsCDCConditionalSwap() bool  { return true }

func (d *StarRocksDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	if expectedIncarnation == "" {
		return errors.New("StarRocks conditional CDC truncate requires an expected incarnation")
	}
	if err := d.checkCDCIncarnation(ctx, table, expectedIncarnation); err != nil {
		return err
	}
	// TRUNCATE keeps TABLE_ID, so the identity is unchanged afterwards.
	if err := d.TruncateTable(ctx, table); err != nil {
		return fmt.Errorf("truncate StarRocks CDC target: %w", err)
	}
	return d.checkCDCIncarnation(ctx, table, expectedIncarnation)
}

func (d *StarRocksDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	for _, merge := range merges {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if merge.Truncate {
			if merge.Options.CDCExpectedIncarnation != "" {
				if err := d.TruncateCDCTableIfIncarnation(ctx, merge.Options.TargetTable, merge.Options.CDCExpectedIncarnation); err != nil {
					return err
				}
			} else if err := d.TruncateTable(ctx, merge.Options.TargetTable); err != nil {
				return fmt.Errorf("truncate StarRocks CDC target: %w", err)
			}
		}
		if err := d.MergeTable(ctx, merge.Options); err != nil {
			return err
		}
	}
	return nil
}

func (d *StarRocksDestination) ClaimAndPrepareEmptyCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim, opts destination.PrepareOptions) (string, error) {
	if opts.Schema == nil {
		return "", errors.New("cannot create an empty managed CDC target without a schema")
	}
	target, err := d.CanonicalCDCTarget(ctx, opts.Table)
	if err != nil {
		return "", err
	}
	claimed, err := d.CanonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return "", err
	}
	if claimed != target {
		return "", fmt.Errorf("CDC target claim %q does not match prepared table %q", claim.DestinationTable, opts.Table)
	}
	if _, exists, err := d.CDCTargetIncarnation(ctx, opts.Table); err != nil {
		return "", err
	} else if exists {
		return "", fmt.Errorf("destination table %q already exists", opts.Table)
	}
	marker := d.claimMarker(claimTable, target)
	var existing string
	claimExisted := d.db.QueryRowContext(ctx, "SELECT owner_id FROM "+marker).Scan(&existing) == nil
	if err := d.ClaimCDCTarget(ctx, claimTable, claim); err != nil {
		return "", err
	}
	opts.DropFirst = false
	// Without IF NOT EXISTS, CREATE TABLE fails if another process created the
	// target after the existence check, so the returned identity is ours.
	if err := d.prepareTable(ctx, opts, true); err != nil {
		if !claimExisted {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			owner, _ := claim.OwnerID()
			_, dropErr := d.db.ExecContext(cleanupCtx, "DROP VIEW IF EXISTS "+marker)
			_, deleteErr := d.db.ExecContext(cleanupCtx, "DELETE FROM "+d.tableRef(claimTable)+" WHERE destination_table = ? AND connector_id = ?", destination.CDCTargetKeyDigest(target), owner)
			return "", errors.Join(err, dropErr, deleteErr)
		}
		return "", err
	}
	incarnation, exists, err := d.CDCTargetIncarnation(ctx, opts.Table)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("claimed StarRocks CDC target %q disappeared after creation", opts.Table)
	}
	return incarnation, nil
}

// CDCConditionalSwapIncarnations returns the identity SwapTable will check
// for the replacement and the identity the target will have afterwards:
// SWAP WITH and RENAME keep TABLE_ID with the physical table. StarRocks swaps
// only within a database, so cross-database staging is copied into the
// target database here and SwapTable publishes that copy.
func (d *StarRocksDestination) CDCConditionalSwapIncarnations(ctx context.Context, targetTable, stagingTable string) (string, string, error) {
	targetDB, targetName := d.tableParts(targetTable)
	stageDB, _ := d.tableParts(stagingTable)
	physical := stagingTable
	if stageDB != targetDB {
		copied, err := d.copySwapStaging(ctx, stagingTable, targetDB, targetName)
		if err != nil {
			return "", "", err
		}
		d.swapMu.Lock()
		if d.preparedSwaps == nil {
			d.preparedSwaps = map[string]string{}
		}
		previous, hadPrevious := d.preparedSwaps[stagingTable]
		d.preparedSwaps[stagingTable] = copied
		d.swapMu.Unlock()
		if hadPrevious {
			d.dropQuietly(ctx, previous)
		}
		physical = copied
	}
	physicalDB, physicalName := d.tableParts(physical)
	id, exists, err := d.tableID(ctx, physicalDB, physicalName)
	if err == nil && !exists {
		err = fmt.Errorf("StarRocks swap staging table %q does not exist", physical)
	}
	if err != nil {
		if physical != stagingTable {
			if copied, ok := d.takePreparedSwap(stagingTable); ok {
				d.dropQuietly(ctx, copied)
			}
		}
		return "", "", err
	}
	return destination.CDCTargetKey(physicalDB, physicalName, id), destination.CDCTargetKey(targetDB, targetName, id), nil
}

func (d *StarRocksDestination) takePreparedSwap(stagingTable string) (string, bool) {
	d.swapMu.Lock()
	defer d.swapMu.Unlock()
	copied, ok := d.preparedSwaps[stagingTable]
	delete(d.preparedSwaps, stagingTable)
	return copied, ok
}

// forgetSwapCopy keeps a swapped-out table from being dropped by the
// deferred cleanup of the copy that replaced it.
func (d *StarRocksDestination) forgetSwapCopy(table string) {
	d.swapMu.Lock()
	defer d.swapMu.Unlock()
	if d.keptSwaps == nil {
		d.keptSwaps = map[string]bool{}
	}
	d.keptSwaps[table] = true
}

func (d *StarRocksDestination) tableID(ctx context.Context, db, name string) (string, bool, error) {
	var id string
	err := d.db.QueryRowContext(ctx, `SELECT TABLE_ID FROM information_schema.tables_config WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND TABLE_ENGINE IN ('OLAP', 'CLOUD_NATIVE')`, db, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read StarRocks table identity: %w", err)
	}
	return id, true, nil
}

func (d *StarRocksDestination) dropQuietly(ctx context.Context, table string) {
	d.swapMu.Lock()
	kept := d.keptSwaps[table]
	delete(d.keptSwaps, table)
	d.swapMu.Unlock()
	if kept {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := d.DropTable(cleanupCtx, table); err != nil {
		config.Debug("[STARROCKS] Failed to drop %s: %v", table, err)
	}
}

type starRocksCDCRunLease struct {
	d       *StarRocksDestination
	db      string
	name    string
	comment string
	token   string

	// lastHeartbeat (unix nanos) lets every lease check, not only the
	// monitor tick, notice that renewals stopped, e.g. after a long pause.
	lastHeartbeat atomic.Int64
	stopping      atomic.Bool

	done        chan struct{}
	stop        chan struct{}
	stopped     chan struct{}
	doneOnce    sync.Once
	releaseOnce sync.Once
	mu          sync.Mutex
	err         error
	releaseErr  error
}

func (d *StarRocksDestination) AcquireManagedCDCRunLease(ctx context.Context, connectorID string) (source.ConnectorLease, error) {
	if connectorID == "" {
		return nil, errors.New("managed CDC connector ID is empty")
	}
	sum := sha256.Sum256([]byte(connectorID))
	tokenBytes := make([]byte, 12)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)
	lease := &starRocksCDCRunLease{
		d:       d,
		db:      cdcLeaseDatabase,
		name:    "cdc_lease_" + hex.EncodeToString(sum[:10]),
		comment: cdcLeaseCommentHead + token,
		token:   token,
		done:    make(chan struct{}),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	if _, err := d.db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+quoteColumn(lease.db)); err != nil {
		return nil, fmt.Errorf("ensure StarRocks CDC lease database: %w", err)
	}
	// A crashed holder stops heartbeating; wait long enough for its lease to
	// expire so a restart after a crash succeeds without operator action.
	deadline := time.Now().Add(cdcLeaseTTL + 15*time.Second)
	for {
		acquired, err := lease.tryAcquire(ctx)
		if err != nil {
			return nil, err
		}
		if acquired {
			lease.lastHeartbeat.Store(time.Now().UnixNano())
			go lease.monitor()
			return lease, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another MySQL CDC run already owns connector %s (StarRocks lease %s.%s)", connectorID, lease.db, lease.name)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (l *starRocksCDCRunLease) ref() string {
	return quoteColumn(l.db) + "." + quoteColumn(l.name)
}

func (l *starRocksCDCRunLease) tryAcquire(ctx context.Context) (bool, error) {
	create := fmt.Sprintf("CREATE TABLE %s (owner VARCHAR(64) NOT NULL, heartbeat DATETIME NOT NULL) PRIMARY KEY (owner) COMMENT %s DISTRIBUTED BY HASH (owner) BUCKETS 1",
		l.ref(), sqlString(l.comment))
	if l.d.replicationNum != "" {
		create += fmt.Sprintf(" PROPERTIES (\"replication_num\" = \"%s\")", l.d.replicationNum)
	}
	_, err := l.d.db.ExecContext(ctx, create)
	if err == nil {
		if err := l.heartbeat(ctx); err != nil {
			return false, fmt.Errorf("record StarRocks CDC lease heartbeat: %w", err)
		}
		owner, err := l.owner(ctx)
		if err != nil {
			return false, err
		}
		return owner == l.comment, nil
	}
	if !isTableExistsError(err) {
		return false, fmt.Errorf("acquire StarRocks CDC run lease: %w", err)
	}
	staleOwner, err := l.staleHolder(ctx)
	if err != nil || staleOwner == "" {
		return false, err
	}
	// RENAME is the compare-and-swap: only one contender can move the lease
	// aside. It does not compare the owner, so verify the moved table is the
	// stale one before dropping it, and put back a live lease that a slower
	// contender moved by mistake.
	dead := l.name + "_expired_" + l.token
	if _, err := l.d.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME %s", l.ref(), quoteColumn(dead))); err != nil {
		return false, nil
	}
	if moved, err := l.commentOf(ctx, dead); err != nil || moved != staleOwner {
		if _, renameErr := l.d.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME %s", quoteColumn(l.db)+"."+quoteColumn(dead), quoteColumn(l.name))); renameErr != nil {
			config.Debug("[STARROCKS] Could not restore CDC run lease %s moved during takeover: %v", l.ref(), renameErr)
		}
		return false, nil
	}
	config.Debug("[STARROCKS] Took over expired CDC run lease %s", l.ref())
	l.d.dropQuietly(ctx, l.db+"."+dead)
	return false, nil
}

// isTableExistsError matches StarRocks' CREATE TABLE conflict errors, which
// arrive as 1050 or, for concurrent creates, 1005.
func isTableExistsError(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	return mysqlErr.Number == 1050 || mysqlErr.Number == 1005 && strings.Contains(strings.ToLower(mysqlErr.Message), "already exists")
}

func (l *starRocksCDCRunLease) commentOf(ctx context.Context, table string) (string, error) {
	var comment string
	err := l.d.db.QueryRowContext(ctx, "SELECT TABLE_COMMENT FROM information_schema.tables WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", l.db, table).Scan(&comment)
	return comment, err
}

// staleHolder returns the lease comment of a holder that has not heartbeated
// within the TTL, or "" if the lease is live or gone. Time is read from the
// FE clock on both sides.
func (l *starRocksCDCRunLease) staleHolder(ctx context.Context) (string, error) {
	owner, err := l.owner(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	holder := strings.TrimPrefix(owner, cdcLeaseCommentHead)
	var age sql.NullInt64
	err = l.d.db.QueryRowContext(ctx, "SELECT TIMESTAMPDIFF(SECOND, MAX(heartbeat), NOW()) FROM "+l.ref()+" WHERE owner = ?", holder).Scan(&age)
	if err != nil {
		return "", fmt.Errorf("read StarRocks CDC lease heartbeat: %w", err)
	}
	if !age.Valid {
		// The holder died between CREATE and its first heartbeat.
		err = l.d.db.QueryRowContext(ctx, "SELECT TIMESTAMPDIFF(SECOND, CREATE_TIME, NOW()) FROM information_schema.tables WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", l.db, l.name).Scan(&age)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", nil
			}
			return "", fmt.Errorf("read StarRocks CDC lease creation time: %w", err)
		}
	}
	if age.Valid && time.Duration(age.Int64)*time.Second > cdcLeaseTTL {
		return owner, nil
	}
	return "", nil
}

func (l *starRocksCDCRunLease) owner(ctx context.Context) (string, error) {
	var comment string
	err := l.d.db.QueryRowContext(ctx, "SELECT TABLE_COMMENT FROM information_schema.tables WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", l.db, l.name).Scan(&comment)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		return "", fmt.Errorf("read StarRocks CDC lease owner: %w", err)
	}
	return comment, nil
}

func (l *starRocksCDCRunLease) heartbeat(ctx context.Context) error {
	_, err := l.d.db.ExecContext(ctx, "INSERT INTO "+l.ref()+" (owner, heartbeat) VALUES (?, NOW())", l.token)
	return err
}

func (l *starRocksCDCRunLease) monitor() {
	defer close(l.stopped)
	ticker := time.NewTicker(cdcLeaseCheck)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
		}
		var lost error
		// Checked before any query: a process resuming from a long stall must
		// stop writing at once, as other runs may already own the lease.
		if l.renewalOverdue() {
			l.markLost(errors.New("the lease could not be renewed in time"))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		owner, err := l.owner(ctx)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			lost = errors.New("the lease table no longer exists")
		case err != nil:
			config.Debug("[STARROCKS] CDC lease check failed: %v", err)
		case owner != l.comment:
			lost = errors.New("the lease was taken over by another run")
		case time.Since(time.Unix(0, l.lastHeartbeat.Load())) >= cdcLeaseHeartbeat:
			if err := l.heartbeat(ctx); err != nil {
				config.Debug("[STARROCKS] CDC lease heartbeat failed: %v", err)
			} else {
				l.lastHeartbeat.Store(time.Now().UnixNano())
			}
		}
		cancel()
		// Give up well before other runs may consider the lease expired.
		if lost == nil && l.renewalOverdue() {
			lost = errors.New("the lease could not be renewed in time")
		}
		if lost == nil {
			continue
		}
		l.markLost(lost)
		return
	}
}

func (l *starRocksCDCRunLease) markLost(cause error) {
	l.mu.Lock()
	l.err = errors.Join(source.ErrConnectorLeaseLost, fmt.Errorf("StarRocks CDC run lease lost: %w", cause))
	l.mu.Unlock()
	l.doneOnce.Do(func() { close(l.done) })
}

func (l *starRocksCDCRunLease) renewalOverdue() bool {
	return time.Since(time.Unix(0, l.lastHeartbeat.Load())) > cdcLeaseTTL/2
}

// Done also marks the lease lost when renewals are overdue, so the
// ConnectorLeaseLoss check before each write stops a process that resumed
// from a long pause before its monitor goroutine runs.
func (l *starRocksCDCRunLease) Done() <-chan struct{} {
	if !l.stopping.Load() && l.renewalOverdue() {
		l.markLost(errors.New("the lease could not be renewed in time"))
	}
	return l.done
}

func (l *starRocksCDCRunLease) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

func (l *starRocksCDCRunLease) Release() error {
	l.releaseOnce.Do(func() {
		l.stopping.Store(true)
		close(l.stop)
		<-l.stopped
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		owner, err := l.owner(ctx)
		if err != nil || owner != l.comment {
			if err == nil || errors.Is(err, sql.ErrNoRows) {
				err = errors.New("lease was not owned during release")
			}
			l.releaseErr = fmt.Errorf("release StarRocks CDC run lease: %w", err)
			return
		}
		released := l.name + "_released_" + l.token
		if _, err := l.d.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME %s", l.ref(), quoteColumn(released))); err != nil {
			l.releaseErr = fmt.Errorf("release StarRocks CDC run lease: %w", err)
			return
		}
		if moved, err := l.commentOf(ctx, released); err != nil || moved != l.comment {
			_, _ = l.d.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME %s", quoteColumn(l.db)+"."+quoteColumn(released), quoteColumn(l.name)))
			l.releaseErr = errors.New("release StarRocks CDC run lease: the lease was taken over before release")
			return
		}
		if err := l.d.DropTable(ctx, l.db+"."+released); err != nil {
			l.releaseErr = fmt.Errorf("drop released StarRocks CDC run lease: %w", err)
		}
	})
	return l.releaseErr
}
