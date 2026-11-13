package databricks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	cdcLeaseTTL         = 30 * time.Second
	cdcLeaseHeartbeat   = 5 * time.Second
	cdcLeaseCheck       = time.Second
	cdcLeaseCommentHead = "ingestr-cdc-lease:"
)

var (
	_ destination.ManagedCDCRunLeaser        = (*DatabricksDestination)(nil)
	_ destination.CDCConditionalMergeCapable = (*DatabricksDestination)(nil)
	_ destination.CDCConditionalTruncater    = (*DatabricksDestination)(nil)
	_ destination.CDCMultiTableAtomicMerger  = (*DatabricksDestination)(nil)
	_ destination.CDCLateTargetClaimPreparer = (*DatabricksDestination)(nil)
	_ destination.CDCConditionalSwapCapable  = (*DatabricksDestination)(nil)
	_ destination.CDCConditionalSwapPlanner  = (*DatabricksDestination)(nil)
)

var errLeaseTableNotFound = errors.New("lease table not found")

func (d *DatabricksDestination) SupportsCDCConditionalMerge() bool { return true }
func (d *DatabricksDestination) SupportsCDCConditionalSwap() bool  { return true }

// executeMerges runs one or more merges (each optionally preceded by a
// truncate) as a single statement. Targets with an expected incarnation are
// fenced; without any fences the batch still runs atomically.
func (d *DatabricksDestination) executeMerges(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if len(merges) == 0 {
		return nil
	}

	statements := make([]string, 0, len(merges)*2)
	for _, m := range merges {
		isCDC := destination.HasCDCDeletedColumn(m.Options.Columns)
		if len(m.Options.PrimaryKeys) == 0 {
			if isCDC {
				return errors.New("databricks CDC merge requires at least one primary key")
			}
			return errors.New("merge requires at least one primary key")
		}
		if m.Truncate {
			statements = append(statements, "DELETE FROM "+d.quoteTableRef(m.Options.TargetTable))
		}
		if isCDC {
			statements = append(statements, d.buildCDCMergeSQL(m.Options))
		} else {
			statements = append(statements, d.buildNonCDCMergeSQL(m.Options))
		}
	}
	fences, err := d.collectCDCFences(merges)
	if err != nil {
		return err
	}

	failure := "failed to execute atomic CDC merge batch"
	if len(merges) == 1 && !merges[0].Truncate {
		failure = "failed to execute merge"
		if destination.HasCDCDeletedColumn(merges[0].Options.Columns) {
			failure = "failed to execute CDC merge"
		}
	}
	return d.runFencedAtomic(ctx, fences, "merge", failure, func() ([]string, error) { return statements, nil })
}

func (d *DatabricksDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	if expectedIncarnation == "" {
		return errors.New("databricks conditional CDC truncate requires an expected incarnation")
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	fence, err := d.newCDCFence(table, expectedIncarnation)
	if err != nil {
		return err
	}
	return d.runFencedAtomic(ctx, []*cdcFence{fence}, "truncate", "truncate Databricks CDC target", func() ([]string, error) {
		return []string{"DELETE FROM " + fence.targetFull}, nil
	})
}

// fencedCopyInto loads a volume file into a private table first, because
// COPY INTO cannot run inside BEGIN ATOMIC, then appends it to the live
// target with a guarded atomic INSERT.
func (d *DatabricksDestination) fencedCopyInto(ctx context.Context, table, expectedIncarnation, volumePath string) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	fence, err := d.newCDCFence(table, expectedIncarnation)
	if err != nil {
		return err
	}

	loadName, err := newPrivateTableName(fence.tableName, "load")
	if err != nil {
		return err
	}
	loadFull := quoteTableParts(d.effectiveCatalog(), stagingSchema, loadName)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		dropSQL := "DROP TABLE IF EXISTS " + loadFull
		if err := d.executeStatement(cleanupCtx, dropSQL); err != nil {
			config.Debug("[DATABRICKS] Failed to drop load table %s: %v", loadFull, err)
		}
	}()

	createSQL := "CREATE TABLE " + loadFull
	if err := d.executeStatement(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("create Databricks load table %s: %w", loadFull, err)
	}
	copySQL := fmt.Sprintf(
		"COPY INTO %s FROM %s FILEFORMAT = PARQUET FORMAT_OPTIONS ('mergeSchema' = 'true') COPY_OPTIONS ('mergeSchema' = 'true')",
		loadFull, sqlString(volumePath),
	)
	if err := d.executeStatement(ctx, copySQL); err != nil {
		config.LogFailedQuery(copySQL, err)
		return fmt.Errorf("COPY INTO failed: %w", err)
	}
	loadCols, err := d.describeColumns(ctx, loadFull)
	if err != nil {
		return err
	}
	if len(loadCols) == 0 {
		return fmt.Errorf("databricks load table %s has no columns", loadFull)
	}

	appendFailure := "append Databricks CDC load into " + fence.targetFull
	return d.runFencedAtomic(ctx, []*cdcFence{fence}, "write", appendFailure, func() ([]string, error) {
		targetCols, err := d.describeColumns(ctx, fence.targetFull)
		if err != nil {
			if isDatabricksMissingError(err) {
				return nil, incarnationChangedError(fence.rawTable)
			}
			return nil, err
		}
		if missing := missingColumns(loadCols, targetCols); len(missing) > 0 {
			names := make([]string, len(missing))
			for i, col := range missing {
				names[i] = col.name
			}
			return nil, fmt.Errorf("databricks CDC target %s is missing columns required by fenced write: %s", fence.targetFull, strings.Join(names, ", "))
		}
		cols := make([]string, len(loadCols))
		for i, col := range loadCols {
			cols[i] = quoteIdentifier(col.name)
		}
		joined := strings.Join(cols, ", ")
		return []string{fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s", fence.targetFull, joined, joined, loadFull)}, nil
	})
}

func (d *DatabricksDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	return d.executeMerges(ctx, merges)
}

func (d *DatabricksDestination) ClaimAndPrepareEmptyCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim, opts destination.PrepareOptions) (string, error) {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return "", err
	}
	if opts.Schema == nil {
		return "", errors.New("cannot create an empty managed CDC target without a schema")
	}
	owner, err := claim.OwnerID()
	if err != nil {
		return "", err
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

	_, marker := d.claimMarker(opts.Table)
	res, markerErr := d.queryStatement(ctx, "SELECT owner_id FROM "+marker)
	claimExisted := markerErr == nil && len(res.rows) > 0 && len(res.rows[0]) > 0 && res.rows[0][0] == owner

	if err := d.ClaimCDCTarget(ctx, claimTable, claim); err != nil {
		return "", err
	}

	opts.DropFirst = false
	if err := d.prepareTable(ctx, opts, true); err != nil {
		if !claimExisted {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			dropErr := d.executeStatement(cleanupCtx, "DROP VIEW IF EXISTS "+marker)
			delErr := d.executeStatement(
				cleanupCtx,
				fmt.Sprintf(
					"DELETE FROM %s WHERE %s = %s AND %s = %s",
					d.quoteTableRef(claimTable),
					quoteIdentifier("destination_table"), sqlString(destination.CDCTargetKeyDigest(target)),
					quoteIdentifier("connector_id"), sqlString(owner),
				),
			)
			return "", errors.Join(err, dropErr, delErr)
		}
		return "", err
	}

	incarnation, exists, err := d.EnsureCDCTargetIncarnation(ctx, opts.Table)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("claimed Databricks CDC target %q disappeared after creation", opts.Table)
	}
	return incarnation, nil
}

func (d *DatabricksDestination) CDCConditionalSwapIncarnations(ctx context.Context, targetTable, stagingTable string) (string, string, error) {
	targetCat, targetSchema, targetName := d.parseTableParts(targetTable)
	stageCat, _, _ := d.parseTableParts(stagingTable)

	physical := stagingTable
	swapKey := d.canonicalKey(stagingTable)
	if !strings.EqualFold(stageCat, targetCat) {
		copied, err := d.copySwapStaging(ctx, stagingTable, targetCat, targetSchema, targetName)
		if err != nil {
			return "", "", err
		}
		d.swapMu.Lock()
		if d.preparedSwaps == nil {
			d.preparedSwaps = map[string]string{}
		}
		previous, hadPrevious := d.preparedSwaps[swapKey]
		d.preparedSwaps[swapKey] = copied
		d.swapMu.Unlock()
		if hadPrevious {
			d.dropQuietly(ctx, previous)
		}
		physical = copied
	}

	physCat, physSchema, physName, id, exists, err := d.tableIdentity(ctx, physical)
	if err == nil && !exists {
		err = fmt.Errorf("databricks swap staging table %q does not exist", physical)
	}
	if err == nil {
		expectedStageInc := canonicalIncarnationKey(physCat, physSchema, physName, id)
		var stampedInc string
		stampedInc, exists, err = d.EnsureCDCTargetIncarnation(ctx, physical)
		if err == nil && (!exists || stampedInc != expectedStageInc) {
			err = fmt.Errorf("databricks swap staging table %q physical incarnation changed", physical)
		}
	}
	if err != nil {
		if physical != stagingTable {
			if copied, ok := d.takePreparedSwap(stagingTable); ok {
				d.dropQuietly(ctx, copied)
			}
		}
		return "", "", err
	}

	return canonicalIncarnationKey(physCat, physSchema, physName, id),
		canonicalIncarnationKey(targetCat, targetSchema, targetName, id),
		nil
}

func (d *DatabricksDestination) copySwapStaging(ctx context.Context, stagingTable, targetCat, targetSchema, targetName string) (string, error) {
	if err := d.ensureSchemaForParts(ctx, targetCat, targetSchema); err != nil {
		return "", fmt.Errorf("ensure Databricks swap schema %s.%s: %w", targetCat, targetSchema, err)
	}
	copyName := shortTableName(fmt.Sprintf("%s_swap_%d", targetName, time.Now().UnixNano()))
	copyTable := quoteTableParts(targetCat, targetSchema, copyName)
	copySQL := fmt.Sprintf("CREATE TABLE %s AS SELECT * FROM %s", copyTable, d.quoteTableRef(stagingTable))
	if err := d.executeStatement(ctx, copySQL); err != nil {
		config.LogFailedQuery(copySQL, err)
		return "", fmt.Errorf("copy Databricks swap staging %s: %w", stagingTable, err)
	}
	return copyTable, nil
}

func (d *DatabricksDestination) canonicalKey(table string) string {
	cat, sch, tbl := d.parseTableParts(table)
	return canonicalTargetKey(cat, sch, tbl)
}

func (d *DatabricksDestination) takePreparedSwap(stagingTable string) (string, bool) {
	swapKey := d.canonicalKey(stagingTable)
	d.swapMu.Lock()
	defer d.swapMu.Unlock()
	copied, ok := d.preparedSwaps[swapKey]
	delete(d.preparedSwaps, swapKey)
	return copied, ok
}

func (d *DatabricksDestination) forgetSwapCopy(table string) {
	swapKey := d.canonicalKey(table)
	d.swapMu.Lock()
	defer d.swapMu.Unlock()
	if d.keptSwaps == nil {
		d.keptSwaps = map[string]bool{}
	}
	d.keptSwaps[swapKey] = true
}

func (d *DatabricksDestination) dropQuietly(ctx context.Context, table string) {
	swapKey := d.canonicalKey(table)
	d.swapMu.Lock()
	kept := d.keptSwaps[swapKey]
	delete(d.keptSwaps, swapKey)
	d.swapMu.Unlock()
	if kept {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := d.DropTable(cleanupCtx, table); err != nil {
		config.Debug("[DATABRICKS] Failed to drop %s: %v", table, err)
	}
}

type databricksCDCRunLease struct {
	d       *DatabricksDestination
	catalog string
	schema  string
	name    string
	comment string
	token   string

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

func (d *DatabricksDestination) AcquireManagedCDCRunLease(ctx context.Context, connectorID string) (source.ConnectorLease, error) {
	if connectorID == "" {
		return nil, errors.New("managed CDC connector ID is empty")
	}
	sum := sha256.Sum256([]byte(connectorID))
	tokenBytes := make([]byte, 12)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)

	lease := &databricksCDCRunLease{
		d:       d,
		catalog: strings.ToLower(d.effectiveCatalog()),
		schema:  stagingSchema,
		name:    "cdc_lease_" + hex.EncodeToString(sum[:10]),
		comment: cdcLeaseCommentHead + token,
		token:   token,
		done:    make(chan struct{}),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}

	if err := d.ensureSchemaForParts(ctx, lease.catalog, lease.schema); err != nil {
		return nil, fmt.Errorf("ensure Databricks CDC lease schema: %w", err)
	}

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
			return nil, fmt.Errorf("another CDC run already owns connector %s (Databricks lease %s.%s.%s)", connectorID, lease.catalog, lease.schema, lease.name)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (l *databricksCDCRunLease) refOf(name string) string {
	return quoteTableParts(l.catalog, l.schema, name)
}

func (l *databricksCDCRunLease) ref() string {
	return l.refOf(l.name)
}

func (l *databricksCDCRunLease) tryAcquire(ctx context.Context) (bool, error) {
	createSQL := fmt.Sprintf("CREATE TABLE %s (`owner` STRING, `heartbeat` TIMESTAMP) COMMENT %s", l.ref(), sqlString(l.comment))
	err := l.d.executeStatement(ctx, createSQL)
	if err == nil {
		if err := l.heartbeat(ctx); err != nil {
			return false, fmt.Errorf("record Databricks CDC lease heartbeat: %w", err)
		}
		owner, err := l.owner(ctx)
		if err != nil {
			return false, err
		}
		return owner == l.comment, nil
	}
	if !isDatabricksAlreadyExistsError(err) {
		return false, fmt.Errorf("acquire Databricks CDC run lease: %w", err)
	}

	staleOwner, err := l.staleHolder(ctx)
	if err != nil || staleOwner == "" {
		return false, err
	}

	dead := l.name + "_expired_" + l.token
	if err := l.d.executeStatement(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", l.ref(), l.refOf(dead))); err != nil {
		return false, nil
	}
	moved, _, err := l.leaseDetail(ctx, dead)
	if err != nil || moved != staleOwner {
		if renameErr := l.d.executeStatement(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", l.refOf(dead), l.ref())); renameErr != nil {
			config.Debug("[DATABRICKS] Could not restore CDC run lease %s moved during takeover: %v", l.ref(), renameErr)
		}
		return false, nil
	}
	config.Debug("[DATABRICKS] Took over expired CDC run lease %s", l.ref())
	l.d.dropQuietly(ctx, l.refOf(dead))
	return false, nil
}

func (l *databricksCDCRunLease) leaseDetail(ctx context.Context, tableName string) (description, createdAt string, err error) {
	res, err := l.d.queryStatement(ctx, "DESCRIBE DETAIL "+l.refOf(tableName))
	if err != nil {
		if isDatabricksMissingError(err) {
			return "", "", errLeaseTableNotFound
		}
		return "", "", fmt.Errorf("read Databricks CDC lease detail: %w", err)
	}
	if len(res.rows) == 0 {
		return "", "", errLeaseTableNotFound
	}
	row := res.rows[0]
	if idx := res.columnIndex("description"); idx >= 0 && idx < len(row) {
		description = row[idx]
	}
	if idx := res.columnIndex("createdAt"); idx >= 0 && idx < len(row) {
		createdAt = row[idx]
	}
	return description, createdAt, nil
}

func (l *databricksCDCRunLease) owner(ctx context.Context) (string, error) {
	owner, _, err := l.leaseDetail(ctx, l.name)
	return owner, err
}

func (l *databricksCDCRunLease) staleHolder(ctx context.Context) (string, error) {
	owner, createdAt, err := l.leaseDetail(ctx, l.name)
	if err != nil {
		if errors.Is(err, errLeaseTableNotFound) {
			return "", nil
		}
		return "", err
	}
	holder := strings.TrimPrefix(owner, cdcLeaseCommentHead)

	ageQuery := fmt.Sprintf(
		"SELECT CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP()) - UNIX_TIMESTAMP(MAX(`heartbeat`)) AS BIGINT) FROM %s WHERE `owner` = %s",
		l.ref(), sqlString(holder),
	)
	res, err := l.d.queryStatement(ctx, ageQuery)
	if err != nil {
		if isDatabricksMissingError(err) {
			return "", nil
		}
		return "", fmt.Errorf("read Databricks CDC lease heartbeat: %w", err)
	}

	var ageStr string
	if len(res.rows) > 0 && len(res.rows[0]) > 0 {
		ageStr = strings.TrimSpace(res.rows[0][0])
	}
	if ageStr == "" {
		if createdAt == "" {
			return "", nil
		}
		createdQuery := fmt.Sprintf(
			"SELECT CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP()) - UNIX_TIMESTAMP(CAST(%s AS TIMESTAMP)) AS BIGINT)",
			sqlString(createdAt),
		)
		res, err = l.d.queryStatement(ctx, createdQuery)
		if err != nil {
			return "", fmt.Errorf("read Databricks CDC lease creation age: %w", err)
		}
		if len(res.rows) > 0 && len(res.rows[0]) > 0 {
			ageStr = strings.TrimSpace(res.rows[0][0])
		}
	}
	if ageStr == "" {
		return "", nil
	}
	ageSeconds, err := strconv.ParseInt(ageStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse Databricks CDC lease age %q: %w", ageStr, err)
	}
	if time.Duration(ageSeconds)*time.Second > cdcLeaseTTL {
		return owner, nil
	}
	return "", nil
}

func (l *databricksCDCRunLease) heartbeat(ctx context.Context) error {
	return l.d.executeStatement(ctx, fmt.Sprintf("INSERT INTO %s (`owner`, `heartbeat`) VALUES (%s, CURRENT_TIMESTAMP())", l.ref(), sqlString(l.token)))
}

func (l *databricksCDCRunLease) monitor() {
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
		if l.renewalOverdue() {
			l.markLost(errors.New("the lease could not be renewed in time"))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		owner, err := l.owner(ctx)
		switch {
		case errors.Is(err, errLeaseTableNotFound):
			lost = errors.New("the lease table no longer exists")
		case err != nil:
			config.Debug("[DATABRICKS] CDC lease check failed: %v", err)
		case owner != l.comment:
			lost = errors.New("the lease was taken over by another run")
		case time.Since(time.Unix(0, l.lastHeartbeat.Load())) >= cdcLeaseHeartbeat:
			if err := l.heartbeat(ctx); err != nil {
				config.Debug("[DATABRICKS] CDC lease heartbeat failed: %v", err)
			} else {
				l.lastHeartbeat.Store(time.Now().UnixNano())
			}
		}
		cancel()
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

func (l *databricksCDCRunLease) markLost(cause error) {
	l.mu.Lock()
	l.err = errors.Join(source.ErrConnectorLeaseLost, fmt.Errorf("databricks CDC run lease lost: %w", cause))
	l.mu.Unlock()
	l.doneOnce.Do(func() { close(l.done) })
}

func (l *databricksCDCRunLease) renewalOverdue() bool {
	return time.Since(time.Unix(0, l.lastHeartbeat.Load())) > cdcLeaseTTL/2
}

// expireIfOverdue marks the lease lost when renewals have fallen behind, so
// callers observe the loss even before the monitor's next tick.
func (l *databricksCDCRunLease) expireIfOverdue() {
	if !l.stopping.Load() && l.renewalOverdue() {
		l.markLost(errors.New("the lease could not be renewed in time"))
	}
}

func (l *databricksCDCRunLease) Done() <-chan struct{} {
	l.expireIfOverdue()
	return l.done
}

func (l *databricksCDCRunLease) Err() error {
	l.expireIfOverdue()
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

func (l *databricksCDCRunLease) Release() error {
	l.releaseOnce.Do(func() {
		l.stopping.Store(true)
		close(l.stop)
		<-l.stopped
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		owner, err := l.owner(ctx)
		if err != nil || owner != l.comment {
			if err == nil || errors.Is(err, errLeaseTableNotFound) {
				err = errors.New("lease was not owned during release")
			}
			l.releaseErr = fmt.Errorf("release Databricks CDC run lease: %w", err)
			return
		}
		released := l.name + "_released_" + l.token
		if err := l.d.executeStatement(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", l.ref(), l.refOf(released))); err != nil {
			l.releaseErr = fmt.Errorf("release Databricks CDC run lease: %w", err)
			return
		}
		moved, _, err := l.leaseDetail(ctx, released)
		if err != nil || moved != l.comment {
			_ = l.d.executeStatement(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", l.refOf(released), l.ref()))
			l.releaseErr = errors.New("release Databricks CDC run lease: the lease was taken over before release")
			return
		}
		if err := l.d.DropTable(ctx, l.refOf(released)); err != nil {
			l.releaseErr = fmt.Errorf("drop released Databricks CDC run lease: %w", err)
		}
	})
	return l.releaseErr
}
