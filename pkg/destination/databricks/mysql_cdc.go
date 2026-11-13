package databricks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
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

type cdcFenceSpec struct {
	table               string
	expectedIncarnation string
}

func newGuardTableName(tableName, op string) (string, error) {
	var suffixBytes [8]byte
	if _, err := rand.Read(suffixBytes[:]); err != nil {
		return "", fmt.Errorf("generate Databricks guard table suffix: %w", err)
	}
	suffix := hex.EncodeToString(suffixBytes[:])
	candidate := fmt.Sprintf("__ingestr_%s_%s_%s", op, tableName, suffix)
	return destination.ShortenIdentifier(candidate, candidate, destination.MaxIdentifierLength("databricks")), nil
}

func (d *DatabricksDestination) withFencedCDCTarget(ctx context.Context, table, expectedIncarnation, op string, fn func(guardFull string) error) error {
	if expectedIncarnation == "" {
		return errors.New("databricks conditional CDC fence requires an expected incarnation")
	}
	cat, sch, tbl := d.parseTableParts(table)
	canonKey := canonicalTargetKey(cat, sch, tbl)
	return d.withFencedCDCTargets(ctx, []cdcFenceSpec{{
		table:               table,
		expectedIncarnation: expectedIncarnation,
	}}, op, func(guardedRefs map[string]string) error {
		return fn(guardedRefs[canonKey])
	})
}

func (d *DatabricksDestination) withFencedCDCTargets(ctx context.Context, specs []cdcFenceSpec, op string, fn func(guardedRefs map[string]string) error) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if len(specs) == 0 {
		return fn(nil)
	}

	d.fenceMu.Lock()
	defer d.fenceMu.Unlock()

	type targetFence struct {
		canonKey            string
		rawTable            string
		catalog             string
		schemaName          string
		tableName           string
		targetFull          string
		guardFull           string
		expectedIncarnation string
	}

	byCanon := make(map[string]*targetFence, len(specs))
	for _, spec := range specs {
		if spec.expectedIncarnation == "" {
			return errors.New("databricks conditional CDC fence requires an expected incarnation")
		}
		cat, sch, tbl := d.parseTableParts(spec.table)
		canonKey := canonicalTargetKey(cat, sch, tbl)
		if existing, ok := byCanon[canonKey]; ok {
			if existing.expectedIncarnation != spec.expectedIncarnation {
				return fmt.Errorf("conflicting expected incarnations for Databricks CDC target %q", spec.table)
			}
			continue
		}
		guardName, err := newGuardTableName(tbl, op)
		if err != nil {
			return err
		}
		byCanon[canonKey] = &targetFence{
			canonKey:            canonKey,
			rawTable:            spec.table,
			catalog:             cat,
			schemaName:          sch,
			tableName:           tbl,
			targetFull:          quoteTableParts(cat, sch, tbl),
			guardFull:           quoteTableParts(cat, sch, guardName),
			expectedIncarnation: spec.expectedIncarnation,
		}
	}

	ordered := make([]*targetFence, 0, len(byCanon))
	for _, f := range byCanon {
		ordered = append(ordered, f)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].canonKey < ordered[j].canonKey
	})

	for _, f := range ordered {
		if err := d.checkCDCIncarnation(ctx, f.rawTable, f.expectedIncarnation); err != nil {
			return err
		}
	}

	moved := make([]*targetFence, 0, len(ordered))
	restoreMoved := func() error {
		restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		var restoreErr error
		for i := len(moved) - 1; i >= 0; i-- {
			m := moved[i]
			renameBackSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", m.guardFull, m.targetFull)
			if err := d.executeStatement(restoreCtx, renameBackSQL); err != nil {
				config.LogFailedQuery(renameBackSQL, err)
				restoreErr = errors.Join(restoreErr, fmt.Errorf("restore Databricks CDC target %s: %w", m.rawTable, err))
			}
		}
		return restoreErr
	}

	for _, f := range ordered {
		renameToGuardSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", f.targetFull, f.guardFull)
		if err := d.executeStatement(ctx, renameToGuardSQL); err != nil {
			config.LogFailedQuery(renameToGuardSQL, err)
			restoreErr := restoreMoved()
			if isDatabricksMissingError(err) {
				return errors.Join(fmt.Errorf("databricks CDC target %q physical incarnation changed: %w", f.rawTable, err), restoreErr)
			}
			return errors.Join(fmt.Errorf("lock Databricks CDC target %q: %w", f.rawTable, err), restoreErr)
		}
		moved = append(moved, f)

		_, _, _, guardID, exists, idErr := d.tableIdentity(ctx, f.guardFull)
		movedInc := canonicalIncarnationKey(f.catalog, f.schemaName, f.tableName, guardID)
		if idErr != nil || !exists || movedInc != f.expectedIncarnation {
			restoreErr := restoreMoved()
			if idErr != nil {
				return errors.Join(idErr, restoreErr)
			}
			return errors.Join(fmt.Errorf("databricks CDC target %q physical incarnation changed", f.rawTable), restoreErr)
		}
	}

	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return errors.Join(err, restoreMoved())
	}

	guardedRefs := make(map[string]string, len(ordered))
	for _, f := range ordered {
		guardedRefs[f.canonKey] = f.guardFull
	}

	if err := fn(guardedRefs); err != nil {
		return errors.Join(err, restoreMoved())
	}

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var publishErr error
	for i := len(moved) - 1; i >= 0; i-- {
		m := moved[i]
		renameBackSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", m.guardFull, m.targetFull)
		if err := d.executeStatement(publishCtx, renameBackSQL); err != nil {
			config.LogFailedQuery(renameBackSQL, err)
			if isDatabricksAlreadyExistsError(err) {
				publishErr = errors.Join(publishErr, fmt.Errorf("databricks CDC target %q physical incarnation changed: %w", m.rawTable, err))
			} else {
				publishErr = errors.Join(publishErr, fmt.Errorf("publish Databricks CDC target %q: %w", m.rawTable, err))
			}
		}
	}
	if publishErr != nil {
		return publishErr
	}

	for _, f := range ordered {
		if err := d.checkCDCIncarnation(publishCtx, f.rawTable, f.expectedIncarnation); err != nil {
			return err
		}
	}
	return nil
}

func buildAtomicStatementsSQL(statements []string) string {
	if len(statements) == 1 {
		return statements[0]
	}
	var b strings.Builder
	b.WriteString("BEGIN ATOMIC\n")
	for _, stmt := range statements {
		trimmed := strings.TrimRight(strings.TrimSpace(stmt), ";")
		b.WriteString("  ")
		b.WriteString(trimmed)
		b.WriteString(";\n")
	}
	b.WriteString("END;")
	return b.String()
}

func (d *DatabricksDestination) executeFencedMerges(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if len(merges) == 0 {
		return nil
	}

	expectedByCanon := make(map[string]string, len(merges))
	fenceSpecs := make([]cdcFenceSpec, 0, len(merges))
	for _, m := range merges {
		if len(m.Options.PrimaryKeys) == 0 {
			if destination.HasCDCDeletedColumn(m.Options.Columns) {
				return errors.New("databricks CDC merge requires at least one primary key")
			}
			return errors.New("merge requires at least one primary key")
		}
		if m.Options.CDCExpectedIncarnation != "" {
			cat, sch, tbl := d.parseTableParts(m.Options.TargetTable)
			canonKey := canonicalTargetKey(cat, sch, tbl)
			if prev, ok := expectedByCanon[canonKey]; ok && prev != m.Options.CDCExpectedIncarnation {
				return fmt.Errorf("conflicting expected incarnations for Databricks CDC target %q", m.Options.TargetTable)
			}
			expectedByCanon[canonKey] = m.Options.CDCExpectedIncarnation
			fenceSpecs = append(fenceSpecs, cdcFenceSpec{
				table:               m.Options.TargetTable,
				expectedIncarnation: m.Options.CDCExpectedIncarnation,
			})
		}
	}

	return d.withFencedCDCTargets(ctx, fenceSpecs, "merge", func(guardedRefs map[string]string) error {
		statements := make([]string, 0, len(merges)*2)
		for _, m := range merges {
			cat, sch, tbl := d.parseTableParts(m.Options.TargetTable)
			canonKey := canonicalTargetKey(cat, sch, tbl)
			targetRef := quoteTableParts(cat, sch, tbl)
			if guardedRefs != nil {
				if guardedRef, ok := guardedRefs[canonKey]; ok {
					targetRef = guardedRef
				}
			}
			if m.Truncate {
				statements = append(statements, fmt.Sprintf("DELETE FROM %s", targetRef))
			}
			if destination.HasCDCDeletedColumn(m.Options.Columns) {
				statements = append(statements, d.buildCDCMergeSQLForTarget(m.Options, targetRef))
			} else {
				statements = append(statements, d.buildNonCDCMergeSQLForTarget(m.Options, targetRef))
			}
		}

		sql := buildAtomicStatementsSQL(statements)
		config.Debug("[DATABRICKS] Executing merge SQL: %s", sql)
		if err := d.executeStatement(ctx, sql); err != nil {
			config.LogFailedQuery(sql, err)
			if len(statements) == 1 {
				if destination.HasCDCDeletedColumn(merges[0].Options.Columns) {
					return fmt.Errorf("failed to execute CDC merge: %w", err)
				}
				return fmt.Errorf("failed to execute merge: %w", err)
			}
			return fmt.Errorf("failed to execute atomic CDC merge batch: %w", err)
		}
		return nil
	})
}

func (d *DatabricksDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	if expectedIncarnation == "" {
		return errors.New("databricks conditional CDC truncate requires an expected incarnation")
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	return d.withFencedCDCTarget(ctx, table, expectedIncarnation, "trunc", func(guardFull string) error {
		truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", guardFull)
		if err := d.executeStatement(ctx, truncateSQL); err != nil {
			config.LogFailedQuery(truncateSQL, err)
			return fmt.Errorf("truncate Databricks CDC target: %w", err)
		}
		return nil
	})
}

func (d *DatabricksDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	return d.executeFencedMerges(ctx, merges)
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

	marker := d.claimMarker(claimTable, target)
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

	incarnation, exists, err := d.CDCTargetIncarnation(ctx, opts.Table)
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
	swapKey := d.canonicalSwapKey(stagingTable)
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
	copyCandidate := fmt.Sprintf("%s_swap_%d", targetName, time.Now().UnixNano())
	copyName := destination.ShortenIdentifier(copyCandidate, copyCandidate, destination.MaxIdentifierLength("databricks"))
	copyTable := quoteTableParts(targetCat, targetSchema, copyName)
	copySQL := fmt.Sprintf("CREATE TABLE %s AS SELECT * FROM %s", copyTable, d.quoteTableRef(stagingTable))
	if err := d.executeStatement(ctx, copySQL); err != nil {
		config.LogFailedQuery(copySQL, err)
		return "", fmt.Errorf("copy Databricks swap staging %s: %w", stagingTable, err)
	}
	return copyTable, nil
}

func (d *DatabricksDestination) canonicalSwapKey(table string) string {
	cat, sch, tbl := d.parseTableParts(table)
	return canonicalTargetKey(cat, sch, tbl)
}

func (d *DatabricksDestination) takePreparedSwap(stagingTable string) (string, bool) {
	swapKey := d.canonicalSwapKey(stagingTable)
	d.swapMu.Lock()
	defer d.swapMu.Unlock()
	copied, ok := d.preparedSwaps[swapKey]
	delete(d.preparedSwaps, swapKey)
	return copied, ok
}

func (d *DatabricksDestination) forgetSwapCopy(table string) {
	swapKey := d.canonicalSwapKey(table)
	d.swapMu.Lock()
	defer d.swapMu.Unlock()
	if d.keptSwaps == nil {
		d.keptSwaps = map[string]bool{}
	}
	d.keptSwaps[swapKey] = true
}

func (d *DatabricksDestination) dropQuietly(ctx context.Context, table string) {
	swapKey := d.canonicalSwapKey(table)
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

func (l *databricksCDCRunLease) tableNameOf(name string) string {
	return quoteTableParts(l.catalog, l.schema, name)
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
	l.d.dropQuietly(ctx, l.tableNameOf(dead))
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

func (l *databricksCDCRunLease) Done() <-chan struct{} {
	if !l.stopping.Load() && l.renewalOverdue() {
		l.markLost(errors.New("the lease could not be renewed in time"))
	}
	return l.done
}

func (l *databricksCDCRunLease) Err() error {
	if !l.stopping.Load() && l.renewalOverdue() {
		l.markLost(errors.New("the lease could not be renewed in time"))
	}
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
		if err := l.d.DropTable(ctx, l.tableNameOf(released)); err != nil {
			l.releaseErr = fmt.Errorf("drop released Databricks CDC run lease: %w", err)
		}
	})
	return l.releaseErr
}
