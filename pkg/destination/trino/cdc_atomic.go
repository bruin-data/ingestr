package trino

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
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
	trinoCDCInternalTablePrefix       = "__ingestr_cdc_"
	trinoCDCBatchJournalTable         = "__ingestr_cdc_batch_journal"
	defaultTrinoAtomicJournalTTL      = 30 * time.Second
	defaultTrinoAtomicJournalRenewIv  = 8 * time.Second
	minTrinoAtomicOrphanMaxAge        = 5 * time.Minute
	trinoAtomicJournalConflictRetries = 6
)

type atomicRecoveryCtxKey struct{}

type trinoAtomicTargetPlan struct {
	index               int
	canonicalKey        string
	table               string
	catalog             string
	schemaName          string
	tableName           string
	fqn                 string
	expectedIncarnation string
	targetMarker        string
	incarnationPred     string
	cols                []trinoSwapMergeCol
	colNames            []string
	hasRefs             bool
	hasPreSnapshot      bool
	preSnapshotID       int64
	postSnapshotID      *int64
	backupName          string
	backupFQN           string
	backupMarker        string
	backupPred          string
	backupCreated       bool
	shadowName          string
	shadowFQN           string
	shadowMarker        string
	shadowPred          string
	shadowCreated       bool
}

type trinoAtomicJournalEntry struct {
	batchID             string
	stepOrd             int64
	ownerToken          string
	targetTable         string
	targetCatalog       string
	targetSchema        string
	targetName          string
	expectedIncarnation string
	targetMarker        string
	hasRefs             bool
	hasPreSnapshot      bool
	preSnapshotID       int64
	backupName          string
	backupMarker        string
	shadowName          string

	// Set only for in-process rollback: restore only while main still points at
	// the snapshot this batch published, so newer writes are never undone.
	publishedSnapshotID *int64
}

var errTrinoAtomicRollbackSuperseded = errors.New("target changed after this batch published")

type trinoAtomicJournalHeartbeat struct {
	d             *TrinoDestination
	journalFQN    string
	batchID       string
	ownerToken    string
	ttl           time.Duration
	renewIv       time.Duration
	lastRenewNano atomic.Int64
	stopCh        chan struct{}
	stoppedCh     chan struct{}
	stopOnce      sync.Once
}

func (d *TrinoDestination) maybeRecoverInterruptedAtomicBatches(ctx context.Context, tableHints ...string) (context.Context, error) {
	if !d.atomicRecoveryEnabled || d.db == nil {
		return ctx, nil
	}
	if ctx.Value(atomicRecoveryCtxKey{}) != nil {
		return ctx, nil
	}
	if err := ctx.Err(); err != nil {
		return ctx, err
	}

	catalogs := d.collectAtomicRecoveryCatalogs(tableHints...)
	if len(catalogs) == 0 {
		return ctx, nil
	}

	subCtx := context.WithValue(ctx, atomicRecoveryCtxKey{}, true)
	for _, cat := range catalogs {
		if err := d.recoverInterruptedAtomicBatchesForCatalog(subCtx, cat); err != nil {
			return ctx, err
		}
	}
	return subCtx, nil
}

// maybeRecoverInterruptedAtomicBatchesForLoad lets ordinary loads proceed when
// the account cannot read CDC bookkeeping it never uses; CDC work stays strict.
func (d *TrinoDestination) maybeRecoverInterruptedAtomicBatchesForLoad(ctx context.Context, cdc bool, tableHints ...string) (context.Context, error) {
	recoveredCtx, err := d.maybeRecoverInterruptedAtomicBatches(ctx, tableHints...)
	if err == nil || cdc || !isTrinoAccessDenied(err) {
		return recoveredCtx, err
	}
	config.Debug("[TRINO] Skipping CDC atomic batch recovery for non-CDC operation: %v", err)
	return context.WithValue(ctx, atomicRecoveryCtxKey{}, true), nil
}

func (d *TrinoDestination) collectAtomicRecoveryCatalogs(tableHints ...string) []string {
	var catalogs []string
	addCatalog := func(cat string) {
		cat = strings.TrimSpace(cat)
		if cat == "" {
			return
		}
		if slices.ContainsFunc(catalogs, func(existing string) bool {
			return strings.EqualFold(existing, cat)
		}) {
			return
		}
		catalogs = append(catalogs, cat)
	}

	addCatalog(d.catalog)
	for _, hint := range tableHints {
		if strings.TrimSpace(hint) == "" {
			continue
		}
		cat, _, _ := d.parseTableName(hint)
		addCatalog(cat)
	}
	return catalogs
}

func (d *TrinoDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	if len(merges) == 0 {
		return nil
	}
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	targetHints := make([]string, len(merges))
	for i, merge := range merges {
		targetHints[i] = merge.Options.TargetTable
	}
	var err error
	if ctx, err = d.maybeRecoverInterruptedAtomicBatches(ctx, targetHints...); err != nil {
		return err
	}

	for _, merge := range merges {
		if len(merge.Options.PrimaryKeys) == 0 {
			return fmt.Errorf("trino atomic CDC merge for %q requires primary keys", merge.Options.TargetTable)
		}
	}

	if len(merges) == 1 && !merges[0].Truncate {
		return d.MergeTable(ctx, merges[0].Options)
	}

	plans := make([]*trinoAtomicTargetPlan, 0, len(merges))
	planByKey := make(map[string]*trinoAtomicTargetPlan, len(merges))
	lockTables := make([]string, 0, len(merges))
	var batchCatalog string

	for _, merge := range merges {
		catalog, schemaName, tableName, resolveErr := d.resolveTableName(merge.Options.TargetTable)
		if resolveErr != nil {
			return resolveErr
		}
		if batchCatalog == "" {
			batchCatalog = catalog
		} else if !strings.EqualFold(catalog, batchCatalog) {
			return fmt.Errorf(
				"trino atomic CDC merge requires all target tables in the same catalog (%s vs %s)",
				batchCatalog,
				catalog,
			)
		}
		key := destination.CDCTargetKey(strings.ToLower(catalog), strings.ToLower(schemaName), strings.ToLower(tableName))
		if existing, exists := planByKey[key]; exists {
			if merge.Options.CDCExpectedIncarnation != "" {
				if existing.expectedIncarnation != "" && existing.expectedIncarnation != merge.Options.CDCExpectedIncarnation {
					return fmt.Errorf("trino atomic CDC merge has conflicting expected incarnations for %q", merge.Options.TargetTable)
				}
				existing.expectedIncarnation = merge.Options.CDCExpectedIncarnation
			}
			continue
		}
		plan := &trinoAtomicTargetPlan{
			index:               len(plans),
			canonicalKey:        key,
			table:               merge.Options.TargetTable,
			catalog:             catalog,
			schemaName:          schemaName,
			tableName:           tableName,
			fqn:                 qualifyTrinoTable(catalog, schemaName, tableName),
			expectedIncarnation: merge.Options.CDCExpectedIncarnation,
		}
		plans = append(plans, plan)
		planByKey[key] = plan
		lockTables = append(lockTables, merge.Options.TargetTable)
	}

	releaseLocks, verifyLocks, err := d.acquireCDCTargetLocks(ctx, lockTables...)
	if err != nil {
		return err
	}
	defer func() { _ = releaseLocks() }()

	if err := d.ensureSchemaExists(ctx, batchCatalog, trinoCDCLeaseSchema); err != nil {
		return fmt.Errorf("ensure Trino CDC staging schema: %w", err)
	}

	batchID, err := generateTrinoAtomicHex(8)
	if err != nil {
		return err
	}
	ownerToken, err := generateTrinoAtomicHex(12)
	if err != nil {
		return err
	}
	createdAtMS := time.Now().UnixMilli()

	retainBackups := false
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		for _, p := range plans {
			if p.shadowCreated {
				_, _ = d.db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+p.shadowFQN)
			}
			if !retainBackups && p.backupCreated {
				_, _ = d.db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+p.backupFQN)
			}
		}
	}()

	for _, plan := range plans {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := verifyLocks(); err != nil {
			return err
		}

		if plan.expectedIncarnation != "" {
			meta, marker, incErr := d.ensureCheckedCDCIncarnation(ctx, plan.table, plan.expectedIncarnation)
			if incErr != nil {
				return incErr
			}
			plan.catalog = meta.catalog
			plan.schemaName = meta.schemaName
			plan.tableName = meta.tableName
			plan.fqn = meta.fqn
			plan.targetMarker = marker
			plan.incarnationPred = trinoIncarnationExistsPredicate(meta.catalog, meta.schemaName, meta.tableName, marker)
		} else {
			meta, exists, metaErr := d.trinoTargetMetadata(ctx, plan.table)
			if metaErr != nil {
				return metaErr
			}
			if !exists {
				return fmt.Errorf("trino atomic CDC target table %q does not exist", plan.table)
			}
			plan.catalog = meta.catalog
			plan.schemaName = meta.schemaName
			plan.tableName = meta.tableName
			plan.fqn = meta.fqn
			if marker := trinoIncarnationMarker(meta.tableComment); marker != "" {
				plan.targetMarker = marker
				plan.incarnationPred = trinoIncarnationExistsPredicate(meta.catalog, meta.schemaName, meta.tableName, marker)
			}
		}

		targetSpecs, colsErr := d.readTrinoSwapColumnSpecs(ctx, plan.catalog, plan.schemaName, plan.tableName)
		if colsErr != nil {
			return colsErr
		}
		if len(targetSpecs) == 0 {
			if plan.expectedIncarnation != "" {
				return fmt.Errorf("trino CDC target %q physical incarnation changed", plan.table)
			}
			return fmt.Errorf("trino atomic CDC target table %q does not exist", plan.table)
		}
		plan.cols = make([]trinoSwapMergeCol, len(targetSpecs))
		plan.colNames = make([]string, len(targetSpecs))
		for i, spec := range targetSpecs {
			plan.cols[i] = trinoSwapMergeCol{name: spec.name}
			plan.colNames[i] = spec.name
		}
		selectList := strings.Join(quoteColumns(plan.colNames), ", ")

		hasRefs, hasSnap, snapID, snapErr := d.readTrinoMainSnapshotRef(ctx, plan.catalog, plan.schemaName, plan.tableName)
		if snapErr != nil {
			return snapErr
		}
		plan.hasRefs = hasRefs
		plan.hasPreSnapshot = hasSnap
		plan.preSnapshotID = snapID

		backupMarker, markerErr := generateTrinoIncarnationMarker()
		if markerErr != nil {
			return markerErr
		}
		shadowMarker, markerErr := generateTrinoIncarnationMarker()
		if markerErr != nil {
			return markerErr
		}
		plan.backupName = formatTrinoAtomicStagingTableName("bak", createdAtMS, batchID, plan.index)
		plan.backupFQN = qualifyTrinoTable(batchCatalog, trinoCDCLeaseSchema, plan.backupName)
		plan.backupMarker = backupMarker
		plan.backupPred = trinoIncarnationExistsPredicate(batchCatalog, trinoCDCLeaseSchema, plan.backupName, backupMarker)

		plan.shadowName = formatTrinoAtomicStagingTableName("shd", createdAtMS, batchID, plan.index)
		plan.shadowFQN = qualifyTrinoTable(batchCatalog, trinoCDCLeaseSchema, plan.shadowName)
		plan.shadowMarker = shadowMarker
		plan.shadowPred = trinoIncarnationExistsPredicate(batchCatalog, trinoCDCLeaseSchema, plan.shadowName, shadowMarker)

		cloneGuards := make([]string, 0, 2)
		if plan.incarnationPred != "" {
			cloneGuards = append(cloneGuards, plan.incarnationPred)
		}
		if refPred := trinoSnapshotRefPredicate(plan.catalog, plan.schemaName, plan.tableName, plan.hasRefs, plan.hasPreSnapshot, plan.preSnapshotID); refPred != "" {
			cloneGuards = append(cloneGuards, refPred)
		}
		createBackupSQL := buildCreateTableFromStagingSQL(
			plan.backupFQN,
			plan.fqn,
			selectList,
			d.jsonType,
			trinoIncarnationCommentPrefix+plan.backupMarker,
			strings.Join(cloneGuards, " AND "),
		)
		plan.backupCreated = true
		if _, execErr := d.db.ExecContext(ctx, createBackupSQL); execErr != nil {
			config.LogFailedQuery(createBackupSQL, execErr)
			if plan.expectedIncarnation != "" {
				if incErr := d.checkCDCIncarnation(ctx, plan.table, plan.expectedIncarnation); incErr != nil {
					return incErr
				}
			}
			if strings.Contains(execErr.Error(), trinoCDCIncarnationGuardSentinel) {
				return fmt.Errorf("trino CDC target %q snapshot or physical incarnation changed: %w", plan.table, execErr)
			}
			return fmt.Errorf("create Trino CDC pre-image backup for %s: %w", plan.table, execErr)
		}

		createShadowSQL := buildCreateTableFromStagingSQL(
			plan.shadowFQN,
			plan.backupFQN,
			selectList,
			d.jsonType,
			trinoIncarnationCommentPrefix+plan.shadowMarker,
			plan.backupPred,
		)
		plan.shadowCreated = true
		if _, execErr := d.db.ExecContext(ctx, createShadowSQL); execErr != nil {
			config.LogFailedQuery(createShadowSQL, execErr)
			return fmt.Errorf("create Trino CDC shadow table for %s: %w", plan.table, execErr)
		}

		if err := d.verifyAtomicPlanUnchanged(ctx, plan); err != nil {
			return err
		}
	}

	for _, merge := range merges {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := verifyLocks(); err != nil {
			return err
		}
		catalog, schemaName, tableName, _ := d.resolveTableName(merge.Options.TargetTable)
		key := destination.CDCTargetKey(strings.ToLower(catalog), strings.ToLower(schemaName), strings.ToLower(tableName))
		plan := planByKey[key]

		if merge.Truncate {
			if err := d.truncateCDCTableIfIncarnationLocked(ctx, plan.table, plan.shadowFQN, plan.shadowPred, verifyLocks); err != nil {
				return err
			}
		}

		shadowOpts := merge.Options
		shadowOpts.TargetTable = plan.shadowFQN
		shadowOpts.CDCExpectedIncarnation = ""
		if destination.HasCDCDeletedColumn(shadowOpts.Columns) {
			if err := d.mergeCDCLocked(ctx, shadowOpts, plan.shadowPred, verifyLocks); err != nil {
				return err
			}
		} else {
			if err := d.mergeTableLocked(ctx, shadowOpts, plan.shadowPred, verifyLocks); err != nil {
				return err
			}
		}
	}

	journalFQN := qualifyTrinoTable(batchCatalog, trinoCDCLeaseSchema, trinoCDCBatchJournalTable)
	journalTTL := d.effectiveAtomicJournalTTL()
	journalRenewIv := d.effectiveAtomicJournalRenewIv(journalTTL)
	var hb *trinoAtomicJournalHeartbeat
	defer func() {
		if hb != nil {
			hb.stop()
		}
	}()
	journalWritten := false

	if len(plans) > 1 {
		if err := d.ensureAtomicBatchJournalTable(ctx, journalFQN); err != nil {
			return err
		}
	}

	var batchErr error
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		batchErr = err
	} else if err := verifyLocks(); err != nil {
		batchErr = err
	}
	if batchErr == nil {
		for _, plan := range plans {
			if err := d.verifyAtomicPlanUnchanged(ctx, plan); err != nil {
				batchErr = err
				break
			}
		}
	}

	mutatedPlans := make([]*trinoAtomicTargetPlan, 0, len(plans))
	var unmutatedJournalSteps []int
	if batchErr == nil {
		for i, plan := range plans {
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				batchErr = err
				break
			}
			if err := verifyLocks(); err != nil {
				batchErr = err
				break
			}
			if hb != nil {
				if err := hb.verifyHealthy(); err != nil {
					batchErr = err
					break
				}
			}
			if d.beforeAtomicPublishStep != nil {
				if err := d.beforeAtomicPublishStep(i, plan.table); err != nil {
					batchErr = err
					break
				}
			}

			if len(plans) > 1 {
				if err := d.insertAtomicBatchJournalRows(ctx, journalFQN, batchID, ownerToken, journalTTL.Milliseconds(), []*trinoAtomicTargetPlan{plan}); err != nil {
					batchErr = err
					break
				}
				journalWritten = true
				if hb == nil {
					hb = d.startAtomicJournalHeartbeat(journalFQN, batchID, ownerToken, journalTTL, journalRenewIv)
				}
			}

			publishGuards := make([]string, 0, 3)
			if plan.incarnationPred != "" {
				publishGuards = append(publishGuards, plan.incarnationPred)
			}
			publishGuards = append(publishGuards, plan.shadowPred)
			if refPred := trinoSnapshotRefPredicate(plan.catalog, plan.schemaName, plan.tableName, plan.hasRefs, plan.hasPreSnapshot, plan.preSnapshotID); refPred != "" {
				publishGuards = append(publishGuards, refPred)
			}
			publishSQL := buildConditionalSwapMergeSQL(
				plan.fqn,
				plan.shadowFQN,
				plan.cols,
				plan.colNames,
				strings.Join(publishGuards, " AND "),
			)
			if _, execErr := d.db.ExecContext(ctx, publishSQL); execErr != nil {
				config.LogFailedQuery(publishSQL, execErr)
				if isIndeterminateTrinoPublishError(ctx, execErr) {
					mutatedPlans = append(mutatedPlans, plan)
				} else if len(plans) > 1 {
					unmutatedJournalSteps = append(unmutatedJournalSteps, plan.index)
				}
				if plan.expectedIncarnation != "" {
					if incErr := d.checkCDCIncarnation(ctx, plan.table, plan.expectedIncarnation); incErr != nil {
						batchErr = incErr
						break
					}
				}
				if strings.Contains(execErr.Error(), trinoCDCIncarnationGuardSentinel) {
					batchErr = fmt.Errorf("trino CDC target %q snapshot or physical incarnation changed: %w", plan.table, execErr)
					break
				}
				batchErr = fmt.Errorf("publish Trino atomic CDC target %s: %w", plan.table, execErr)
				break
			}
			mutatedPlans = append(mutatedPlans, plan)
			if plan.hasRefs {
				_, hasSnap, snapID, snapErr := d.readTrinoMainSnapshotRef(ctx, plan.catalog, plan.schemaName, plan.tableName)
				if snapErr == nil && hasSnap && verifyLocks() == nil {
					plan.postSnapshotID = &snapID
				}
			}

			if plan.expectedIncarnation != "" {
				if err := d.checkCDCIncarnation(ctx, plan.table, plan.expectedIncarnation); err != nil {
					batchErr = err
					break
				}
			}
			if err := verifyLocks(); err != nil {
				batchErr = err
				break
			}
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				batchErr = err
				break
			}
		}
	}

	if hb != nil {
		hb.stop()
	}

	if batchErr == nil && journalWritten {
		commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		batchErr = d.deleteAtomicBatchJournalRows(commitCtx, journalFQN, batchID)
		cancel()
		if batchErr == nil {
			journalWritten = false
		}
	}

	if batchErr != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()

		lockLost := verifyLocks() != nil
		var rbErrs []error
		var skippedTables []string
		var skippedSteps []int
		skip := func(p *trinoAtomicTargetPlan) {
			skippedTables = append(skippedTables, p.table)
			skippedSteps = append(skippedSteps, p.index)
		}
		restorePlans := make([]*trinoAtomicTargetPlan, 0, len(mutatedPlans))
		for i := len(mutatedPlans) - 1; i >= 0; i-- {
			p := mutatedPlans[i]
			if lockLost && p.postSnapshotID == nil {
				skip(p)
				continue
			}
			entry := trinoAtomicJournalEntry{
				batchID:             batchID,
				stepOrd:             int64(p.index),
				ownerToken:          ownerToken,
				targetTable:         p.table,
				targetCatalog:       p.catalog,
				targetSchema:        p.schemaName,
				targetName:          p.tableName,
				expectedIncarnation: p.expectedIncarnation,
				targetMarker:        p.targetMarker,
				hasRefs:             p.hasRefs,
				hasPreSnapshot:      p.hasPreSnapshot,
				preSnapshotID:       p.preSnapshotID,
				backupName:          p.backupName,
				backupMarker:        p.backupMarker,
				shadowName:          p.shadowName,
				publishedSnapshotID: p.postSnapshotID,
			}
			if rbErr := d.restoreAtomicTargetEntry(cleanupCtx, batchCatalog, entry); rbErr != nil {
				if errors.Is(rbErr, errTrinoAtomicRollbackSuperseded) {
					skip(p)
					continue
				}
				rbErrs = append(rbErrs, rbErr)
				restorePlans = append(restorePlans, p)
			}
		}
		if len(skippedTables) > 0 {
			// The checkpoint has not advanced, so replaying the batch converges
			// these targets without undoing another writer's commits.
			batchErr = errors.Join(batchErr, fmt.Errorf(
				"trino atomic CDC rollback skipped for %s: target lock lost or target changed after publish",
				strings.Join(skippedTables, ", "),
			))
		}
		rollbackErr := errors.Join(rbErrs...)
		if rollbackErr == nil {
			if journalWritten {
				if delErr := d.deleteAtomicBatchJournalRows(cleanupCtx, journalFQN, batchID); delErr != nil {
					retainBackups = true
					_ = d.expireAtomicBatchJournalRows(cleanupCtx, journalFQN, batchID, ownerToken)
					return errors.Join(batchErr, delErr)
				}
			}
			return batchErr
		}

		retainBackups = true
		if journalWritten {
			for _, stepOrd := range slices.Concat(unmutatedJournalSteps, skippedSteps) {
				_ = d.deleteAtomicBatchJournalStepRow(cleanupCtx, journalFQN, batchID, stepOrd)
			}
			_ = d.expireAtomicBatchJournalRows(cleanupCtx, journalFQN, batchID, ownerToken)
		} else if len(restorePlans) > 0 {
			if tblErr := d.ensureAtomicBatchJournalTable(cleanupCtx, journalFQN); tblErr == nil {
				_ = d.insertAtomicBatchJournalRows(cleanupCtx, journalFQN, batchID, ownerToken, 0, restorePlans)
			}
		}
		return errors.Join(batchErr, rollbackErr)
	}

	return nil
}

func isIndeterminateTrinoPublishError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), trinoCDCIncarnationGuardSentinel) ||
		isTrinoCommitConflict(err) ||
		isTrinoMissingObject(err) {
		return false
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "client.timeout")
}

func formatTrinoAtomicStagingTableName(kind string, createdAtMS int64, batchID string, index int) string {
	return fmt.Sprintf("%s%s_%d_%s_%d", trinoCDCInternalTablePrefix, kind, createdAtMS, batchID, index)
}

func parseTrinoAtomicStagingTableName(name string) (createdAtMS int64, batchID string, ok bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	var rest string
	switch {
	case strings.HasPrefix(lower, trinoCDCInternalTablePrefix+"bak_"):
		rest = lower[len(trinoCDCInternalTablePrefix+"bak_"):]
	case strings.HasPrefix(lower, trinoCDCInternalTablePrefix+"shd_"):
		rest = lower[len(trinoCDCInternalTablePrefix+"shd_"):]
	default:
		return 0, "", false
	}
	parts := strings.Split(rest, "_")
	if len(parts) != 3 || parts[1] == "" {
		return 0, "", false
	}
	ms, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || ms <= 0 {
		return 0, "", false
	}
	if _, err := strconv.Atoi(parts[2]); err != nil {
		return 0, "", false
	}
	return ms, parts[1], true
}

func (d *TrinoDestination) verifyAtomicPlanUnchanged(ctx context.Context, plan *trinoAtomicTargetPlan) error {
	if plan.expectedIncarnation != "" {
		if err := d.checkCDCIncarnation(ctx, plan.table, plan.expectedIncarnation); err != nil {
			return err
		}
	} else if plan.targetMarker != "" {
		meta, exists, err := d.trinoTargetMetadata(ctx, plan.table)
		if err != nil {
			return err
		}
		if !exists || trinoIncarnationMarker(meta.tableComment) != plan.targetMarker {
			return fmt.Errorf("trino CDC target %q physical incarnation changed", plan.table)
		}
	}
	if !plan.hasRefs {
		return nil
	}
	hasRefs, hasSnap, snapID, err := d.readTrinoMainSnapshotRef(ctx, plan.catalog, plan.schemaName, plan.tableName)
	if err != nil {
		return err
	}
	if !hasRefs || hasSnap != plan.hasPreSnapshot || (hasSnap && snapID != plan.preSnapshotID) {
		return fmt.Errorf("trino CDC target %q snapshot changed concurrently before atomic commit", plan.table)
	}
	return nil
}

func (d *TrinoDestination) readTrinoMainSnapshotRef(
	ctx context.Context,
	catalog, schemaName, tableName string,
) (hasRefs, hasSnapshot bool, snapshotID int64, err error) {
	refsFQN := qualifyTrinoTable(catalog, schemaName, tableName+"$refs")
	query := fmt.Sprintf("SELECT snapshot_id FROM %s WHERE name = 'main'", refsFQN)
	var id sql.NullInt64
	err = d.db.QueryRowContext(ctx, query).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return true, false, 0, nil
	}
	if err != nil {
		if isTrinoUnsupportedSnapshots(err) {
			return false, false, 0, nil
		}
		config.LogFailedQuery(query, err)
		return false, false, 0, fmt.Errorf("read Trino snapshot ref for %s.%s.%s: %w", catalog, schemaName, tableName, err)
	}
	if !id.Valid {
		return true, false, 0, nil
	}
	return true, true, id.Int64, nil
}

func isTrinoUnsupportedSnapshots(err error) bool {
	if isTrinoMissingObject(err) {
		return true
	}
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not supported") || strings.Contains(msg, "unsupported")
}

func trinoSnapshotRefPredicate(catalog, schemaName, tableName string, hasRefs, hasSnapshot bool, snapshotID int64) string {
	if !hasRefs {
		return ""
	}
	refsFQN := qualifyTrinoTable(catalog, schemaName, tableName+"$refs")
	if !hasSnapshot {
		return fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s WHERE name = 'main')", refsFQN)
	}
	return fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE name = 'main' AND snapshot_id = %d)", refsFQN, snapshotID)
}

func (d *TrinoDestination) effectiveAtomicJournalTTL() time.Duration {
	if d.atomicJournalTTL > 0 {
		return d.atomicJournalTTL
	}
	return defaultTrinoAtomicJournalTTL
}

func (d *TrinoDestination) effectiveAtomicJournalRenewIv(ttl time.Duration) time.Duration {
	if d.atomicJournalRenewIv > 0 {
		return d.atomicJournalRenewIv
	}
	if third := ttl / 3; third > 0 && third < defaultTrinoAtomicJournalRenewIv {
		return third
	}
	return defaultTrinoAtomicJournalRenewIv
}

func (d *TrinoDestination) effectiveAtomicOrphanMaxAge() time.Duration {
	if d.atomicOrphanMaxAge > 0 {
		return d.atomicOrphanMaxAge
	}
	return max(2*d.effectiveAtomicJournalTTL(), minTrinoAtomicOrphanMaxAge)
}

func (d *TrinoDestination) ensureAtomicBatchJournalTable(ctx context.Context, journalFQN string) error {
	createSQL := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (
  "batch_id" VARCHAR,
  "step_ord" BIGINT,
  "owner_token" VARCHAR,
  "expires_at_ms" BIGINT,
  "target_table" VARCHAR,
  "target_catalog" VARCHAR,
  "target_schema" VARCHAR,
  "target_name" VARCHAR,
  "expected_incarnation" VARCHAR,
  "target_marker" VARCHAR,
  "has_refs" BOOLEAN,
  "has_pre_snapshot" BOOLEAN,
  "pre_snapshot_id" BIGINT,
  "backup_name" VARCHAR,
  "backup_marker" VARCHAR,
  "shadow_name" VARCHAR
)`,
		journalFQN,
	)
	for attempt := 0; attempt < trinoAtomicJournalConflictRetries; attempt++ {
		_, err := d.db.ExecContext(ctx, createSQL)
		if err == nil || isTrinoAlreadyExists(err) {
			return nil
		}
		if !isTrinoCommitConflict(err) || attempt == trinoAtomicJournalConflictRetries-1 {
			config.LogFailedQuery(createSQL, err)
			return fmt.Errorf("ensure Trino CDC atomic batch journal: %w", err)
		}
		if err := sleepWithContext(ctx, time.Duration(attempt+1)*50*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

func (d *TrinoDestination) insertAtomicBatchJournalRows(
	ctx context.Context,
	journalFQN, batchID, ownerToken string,
	ttlMillis int64,
	plans []*trinoAtomicTargetPlan,
) error {
	if len(plans) == 0 {
		return nil
	}
	expiresExpr := "BIGINT '0'"
	if ttlMillis > 0 {
		expiresExpr = fmt.Sprintf("CAST(to_unixtime(CURRENT_TIMESTAMP) * 1000 AS BIGINT) + BIGINT '%d'", ttlMillis)
	}
	rows := make([]string, len(plans))
	stepOrds := make([]string, len(plans))
	for i, p := range plans {
		stepOrds[i] = fmt.Sprintf("BIGINT '%d'", p.index)
		rows[i] = fmt.Sprintf(
			"(CAST(%s AS VARCHAR), BIGINT '%d', CAST(%s AS VARCHAR), %s, CAST(%s AS VARCHAR), CAST(%s AS VARCHAR), CAST(%s AS VARCHAR), CAST(%s AS VARCHAR), CAST(%s AS VARCHAR), CAST(%s AS VARCHAR), %s, %s, BIGINT '%d', CAST(%s AS VARCHAR), CAST(%s AS VARCHAR), CAST(%s AS VARCHAR))",
			escapeString(batchID),
			p.index,
			escapeString(ownerToken),
			expiresExpr,
			escapeString(p.table),
			escapeString(p.catalog),
			escapeString(p.schemaName),
			escapeString(p.tableName),
			escapeString(p.expectedIncarnation),
			escapeString(p.targetMarker),
			formatTrinoBoolLiteral(p.hasRefs),
			formatTrinoBoolLiteral(p.hasPreSnapshot),
			p.preSnapshotID,
			escapeString(p.backupName),
			escapeString(p.backupMarker),
			escapeString(p.shadowName),
		)
	}
	insertSQL := fmt.Sprintf(
		`INSERT INTO %s (
  "batch_id", "step_ord", "owner_token", "expires_at_ms",
  "target_table", "target_catalog", "target_schema", "target_name",
  "expected_incarnation", "target_marker", "has_refs", "has_pre_snapshot",
  "pre_snapshot_id", "backup_name", "backup_marker", "shadow_name"
) VALUES %s`,
		journalFQN,
		strings.Join(rows, ", "),
	)
	checkSQL := fmt.Sprintf(
		`SELECT COUNT(DISTINCT "step_ord") FROM %s WHERE "batch_id" = %s AND "step_ord" IN (%s)`,
		journalFQN,
		escapeString(batchID),
		strings.Join(stepOrds, ", "),
	)

	for attempt := 0; attempt < trinoAtomicJournalConflictRetries; attempt++ {
		_, err := d.db.ExecContext(ctx, insertSQL)
		if err == nil {
			return nil
		}
		if !isTrinoCommitConflict(err) || attempt == trinoAtomicJournalConflictRetries-1 {
			config.LogFailedQuery(insertSQL, err)
			return fmt.Errorf("record Trino CDC atomic batch journal %s: %w", batchID, err)
		}
		var existing int64
		if scanErr := d.db.QueryRowContext(ctx, checkSQL).Scan(&existing); scanErr == nil && existing >= int64(len(plans)) {
			return nil
		}
		if err := sleepWithContext(ctx, time.Duration(attempt+1)*50*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

func (d *TrinoDestination) updateAtomicBatchJournalExpiry(
	ctx context.Context,
	journalFQN, batchID, ownerToken string,
	ttlMillis int64,
) error {
	expiresExpr := "BIGINT '0'"
	if ttlMillis > 0 {
		expiresExpr = fmt.Sprintf("CAST(to_unixtime(CURRENT_TIMESTAMP) * 1000 AS BIGINT) + BIGINT '%d'", ttlMillis)
	}
	updateSQL := fmt.Sprintf(
		`UPDATE %s SET "expires_at_ms" = %s WHERE "batch_id" = %s AND "owner_token" = %s`,
		journalFQN,
		expiresExpr,
		escapeString(batchID),
		escapeString(ownerToken),
	)
	for attempt := 0; attempt < trinoAtomicJournalConflictRetries; attempt++ {
		_, err := d.db.ExecContext(ctx, updateSQL)
		if err == nil {
			return nil
		}
		if !isTrinoCommitConflict(err) || attempt == trinoAtomicJournalConflictRetries-1 {
			config.LogFailedQuery(updateSQL, err)
			return fmt.Errorf("update Trino CDC atomic batch journal %s: %w", batchID, err)
		}
		if err := sleepWithContext(ctx, time.Duration(attempt+1)*50*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

func (d *TrinoDestination) expireAtomicBatchJournalRows(ctx context.Context, journalFQN, batchID, ownerToken string) error {
	return d.updateAtomicBatchJournalExpiry(ctx, journalFQN, batchID, ownerToken, 0)
}

func (d *TrinoDestination) deleteAtomicBatchJournalStepRow(ctx context.Context, journalFQN, batchID string, stepOrd int) error {
	deleteSQL := fmt.Sprintf(
		`DELETE FROM %s WHERE "batch_id" = %s AND "step_ord" = BIGINT '%d'`,
		journalFQN,
		escapeString(batchID),
		stepOrd,
	)
	for attempt := 0; attempt < trinoAtomicJournalConflictRetries; attempt++ {
		_, err := d.db.ExecContext(ctx, deleteSQL)
		if err == nil || isTrinoMissingObject(err) {
			return nil
		}
		if !isTrinoCommitConflict(err) || attempt == trinoAtomicJournalConflictRetries-1 {
			config.LogFailedQuery(deleteSQL, err)
			return fmt.Errorf("delete Trino CDC atomic batch journal step %s/%d: %w", batchID, stepOrd, err)
		}
		if err := sleepWithContext(ctx, time.Duration(attempt+1)*50*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

func (d *TrinoDestination) deleteAtomicBatchJournalRows(ctx context.Context, journalFQN, batchID string) error {
	deleteSQL := fmt.Sprintf(`DELETE FROM %s WHERE "batch_id" = %s`, journalFQN, escapeString(batchID))
	for attempt := 0; attempt < trinoAtomicJournalConflictRetries; attempt++ {
		_, err := d.db.ExecContext(ctx, deleteSQL)
		if err == nil || isTrinoMissingObject(err) {
			return nil
		}
		if !isTrinoCommitConflict(err) || attempt == trinoAtomicJournalConflictRetries-1 {
			config.LogFailedQuery(deleteSQL, err)
			return fmt.Errorf("delete Trino CDC atomic batch journal %s: %w", batchID, err)
		}
		if err := sleepWithContext(ctx, time.Duration(attempt+1)*50*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

func (d *TrinoDestination) startAtomicJournalHeartbeat(
	journalFQN, batchID, ownerToken string,
	ttl, renewIv time.Duration,
) *trinoAtomicJournalHeartbeat {
	hb := &trinoAtomicJournalHeartbeat{
		d:          d,
		journalFQN: journalFQN,
		batchID:    batchID,
		ownerToken: ownerToken,
		ttl:        ttl,
		renewIv:    renewIv,
		stopCh:     make(chan struct{}),
		stoppedCh:  make(chan struct{}),
	}
	hb.lastRenewNano.Store(time.Now().UnixNano())
	go hb.loop()
	return hb
}

func (hb *trinoAtomicJournalHeartbeat) loop() {
	defer close(hb.stoppedCh)
	ticker := time.NewTicker(hb.renewIv)
	defer ticker.Stop()

	for {
		select {
		case <-hb.stopCh:
			return
		case <-ticker.C:
		}
		timeout := min(hb.ttl/2, 10*time.Second)
		if timeout <= 0 {
			timeout = time.Second
		}
		renewCtx, cancel := context.WithTimeout(context.Background(), timeout)
		err := hb.d.updateAtomicBatchJournalExpiry(renewCtx, hb.journalFQN, hb.batchID, hb.ownerToken, hb.ttl.Milliseconds())
		cancel()
		if err == nil {
			hb.lastRenewNano.Store(time.Now().UnixNano())
		} else {
			config.Debug("[TRINO] CDC batch journal heartbeat failed for %s: %v", hb.batchID, err)
		}
	}
}

func (hb *trinoAtomicJournalHeartbeat) verifyHealthy() error {
	last := time.Unix(0, hb.lastRenewNano.Load())
	maxAge := hb.ttl * 3 / 4
	if maxAge <= 0 {
		maxAge = hb.ttl
	}
	if time.Since(last) >= maxAge {
		return fmt.Errorf("trino CDC atomic batch journal %s lease could not be renewed before expiry", hb.batchID)
	}
	return nil
}

func (hb *trinoAtomicJournalHeartbeat) stop() {
	hb.stopOnce.Do(func() {
		close(hb.stopCh)
	})
	<-hb.stoppedCh
}

func (d *TrinoDestination) recoverInterruptedAtomicBatchesForCatalog(ctx context.Context, batchCatalog string) error {
	journalFQN := qualifyTrinoTable(batchCatalog, trinoCDCLeaseSchema, trinoCDCBatchJournalTable)
	expiredSQL := fmt.Sprintf(
		`SELECT DISTINCT "batch_id" FROM %s WHERE "expires_at_ms" <= CAST(to_unixtime(CURRENT_TIMESTAMP) * 1000 AS BIGINT)`,
		journalFQN,
	)
	rows, err := d.db.QueryContext(ctx, expiredSQL)
	if err != nil {
		if !isTrinoMissingObject(err) {
			config.LogFailedQuery(expiredSQL, err)
			return fmt.Errorf("query expired Trino CDC atomic batches in %s: %w", batchCatalog, err)
		}
	} else {
		var batchIDs []string
		for rows.Next() {
			var id string
			if scanErr := rows.Scan(&id); scanErr != nil {
				_ = rows.Close()
				return fmt.Errorf("scan expired Trino CDC atomic batch ID: %w", scanErr)
			}
			batchIDs = append(batchIDs, id)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate expired Trino CDC atomic batches: %w", rowsErr)
		}
		_ = rows.Close()

		for _, batchID := range batchIDs {
			if err := d.recoverSingleExpiredAtomicBatch(ctx, batchCatalog, journalFQN, batchID); err != nil {
				return err
			}
		}
	}

	return d.sweepStaleOrphanAtomicStagingTables(ctx, batchCatalog, journalFQN)
}

func (d *TrinoDestination) sweepStaleOrphanAtomicStagingTables(ctx context.Context, batchCatalog, journalFQN string) error {
	listSQL := fmt.Sprintf(
		`SELECT table_name FROM %s.information_schema.tables
		WHERE lower(table_schema) = lower(%s)
		  AND table_type = 'BASE TABLE'
		  AND (lower(table_name) LIKE '__ingestr_cdc_bak_%%' OR lower(table_name) LIKE '__ingestr_cdc_shd_%%')`,
		quoteIdentifier(batchCatalog),
		escapeString(trinoCDCLeaseSchema),
	)
	rows, err := d.db.QueryContext(ctx, listSQL)
	if err != nil {
		if isTrinoMissingObject(err) {
			return nil
		}
		config.LogFailedQuery(listSQL, err)
		return fmt.Errorf("list Trino CDC atomic staging tables in %s: %w", batchCatalog, err)
	}
	type staleCandidate struct {
		tableName string
		batchID   string
	}
	maxAge := d.effectiveAtomicOrphanMaxAge()
	nowMS := time.Now().UnixMilli()
	var candidates []staleCandidate
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			_ = rows.Close()
			return fmt.Errorf("scan Trino CDC atomic staging table name: %w", scanErr)
		}
		createdAtMS, batchID, ok := parseTrinoAtomicStagingTableName(name)
		if !ok {
			continue
		}
		if nowMS-createdAtMS > maxAge.Milliseconds() {
			candidates = append(candidates, staleCandidate{tableName: name, batchID: batchID})
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate Trino CDC atomic staging tables: %w", rowsErr)
	}
	_ = rows.Close()

	if len(candidates) == 0 {
		return nil
	}

	journalBatches := make(map[string]struct{})
	activeSQL := fmt.Sprintf(`SELECT DISTINCT lower("batch_id") FROM %s`, journalFQN)
	jRows, jErr := d.db.QueryContext(ctx, activeSQL)
	if jErr != nil {
		if !isTrinoMissingObject(jErr) {
			config.LogFailedQuery(activeSQL, jErr)
			return fmt.Errorf("read Trino CDC atomic journal batch IDs in %s: %w", batchCatalog, jErr)
		}
	} else {
		for jRows.Next() {
			var id string
			if scanErr := jRows.Scan(&id); scanErr != nil {
				_ = jRows.Close()
				return fmt.Errorf("scan Trino CDC atomic journal batch ID: %w", scanErr)
			}
			journalBatches[id] = struct{}{}
		}
		if rowsErr := jRows.Err(); rowsErr != nil {
			_ = jRows.Close()
			return fmt.Errorf("iterate Trino CDC atomic journal batch IDs: %w", rowsErr)
		}
		_ = jRows.Close()
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	for _, cand := range candidates {
		if _, referenced := journalBatches[cand.batchID]; referenced {
			continue
		}
		_, _ = d.db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+qualifyTrinoTable(batchCatalog, trinoCDCLeaseSchema, cand.tableName))
	}
	return nil
}

func (d *TrinoDestination) recoverSingleExpiredAtomicBatch(ctx context.Context, batchCatalog, journalFQN, batchID string) error {
	entries, err := d.loadAtomicBatchJournalEntries(ctx, journalFQN, batchID)
	if err != nil || len(entries) == 0 {
		return err
	}

	lockTables := make([]string, len(entries))
	for i, e := range entries {
		lockTables[i] = qualifyTrinoTable(e.targetCatalog, e.targetSchema, e.targetName)
	}
	releaseLocks, _, err := d.acquireCDCTargetLocks(ctx, lockTables...)
	if err != nil {
		return err
	}
	defer func() { _ = releaseLocks() }()

	checkSQL := fmt.Sprintf(
		`SELECT COUNT(*), COALESCE(MAX("expires_at_ms"), BIGINT '0') <= CAST(to_unixtime(CURRENT_TIMESTAMP) * 1000 AS BIGINT)
		FROM %s WHERE "batch_id" = %s`,
		journalFQN,
		escapeString(batchID),
	)
	var rowCount int64
	var stillExpired bool
	if err := d.db.QueryRowContext(ctx, checkSQL).Scan(&rowCount, &stillExpired); err != nil {
		if isTrinoMissingObject(err) {
			return nil
		}
		config.LogFailedQuery(checkSQL, err)
		return fmt.Errorf("verify expired Trino CDC atomic batch %s: %w", batchID, err)
	}
	if rowCount == 0 || !stillExpired {
		return nil
	}

	entries, err = d.loadAtomicBatchJournalEntries(ctx, journalFQN, batchID)
	if err != nil || len(entries) == 0 {
		return err
	}

	for _, entry := range entries {
		if err := d.restoreAtomicTargetEntry(ctx, batchCatalog, entry); err != nil {
			return err
		}
	}

	if err := d.deleteAtomicBatchJournalRows(ctx, journalFQN, batchID); err != nil {
		return err
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	for _, entry := range entries {
		if entry.backupName != "" {
			_, _ = d.db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+qualifyTrinoTable(batchCatalog, trinoCDCLeaseSchema, entry.backupName))
		}
		if entry.shadowName != "" {
			_, _ = d.db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+qualifyTrinoTable(batchCatalog, trinoCDCLeaseSchema, entry.shadowName))
		}
	}
	return nil
}

func (d *TrinoDestination) loadAtomicBatchJournalEntries(
	ctx context.Context,
	journalFQN, batchID string,
) ([]trinoAtomicJournalEntry, error) {
	query := fmt.Sprintf(
		`SELECT
  "batch_id", "step_ord", "owner_token",
  "target_table", "target_catalog", "target_schema", "target_name",
  "expected_incarnation", "target_marker",
  "has_refs", "has_pre_snapshot", "pre_snapshot_id",
  "backup_name", "backup_marker", "shadow_name"
FROM %s
WHERE "batch_id" = %s
ORDER BY "step_ord" DESC`,
		journalFQN,
		escapeString(batchID),
	)
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		if isTrinoMissingObject(err) {
			return nil, nil
		}
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("read Trino CDC atomic batch journal %s: %w", batchID, err)
	}
	defer func() { _ = rows.Close() }()

	var entries []trinoAtomicJournalEntry
	for rows.Next() {
		var e trinoAtomicJournalEntry
		if err := rows.Scan(
			&e.batchID,
			&e.stepOrd,
			&e.ownerToken,
			&e.targetTable,
			&e.targetCatalog,
			&e.targetSchema,
			&e.targetName,
			&e.expectedIncarnation,
			&e.targetMarker,
			&e.hasRefs,
			&e.hasPreSnapshot,
			&e.preSnapshotID,
			&e.backupName,
			&e.backupMarker,
			&e.shadowName,
		); err != nil {
			return nil, fmt.Errorf("scan Trino CDC atomic batch journal %s: %w", batchID, err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (d *TrinoDestination) restoreAtomicTargetEntry(
	ctx context.Context,
	batchCatalog string,
	entry trinoAtomicJournalEntry,
) error {
	ctx = context.WithValue(ctx, atomicRecoveryCtxKey{}, true)
	targetFQN := qualifyTrinoTable(entry.targetCatalog, entry.targetSchema, entry.targetName)
	backupFQN := qualifyTrinoTable(batchCatalog, trinoCDCLeaseSchema, entry.backupName)
	backupPred := trinoIncarnationExistsPredicate(batchCatalog, trinoCDCLeaseSchema, entry.backupName, entry.backupMarker)

	for attempt := 0; attempt < trinoAtomicJournalConflictRetries; attempt++ {
		if entry.expectedIncarnation != "" {
			actualInc, exists, incErr := d.CDCTargetIncarnation(ctx, targetFQN)
			if incErr != nil {
				return incErr
			}
			if !exists || actualInc != entry.expectedIncarnation {
				return nil
			}
		} else {
			meta, exists, metaErr := d.trinoTargetMetadata(ctx, targetFQN)
			if metaErr != nil {
				return metaErr
			}
			if !exists {
				return nil
			}
			if entry.targetMarker != "" && trinoIncarnationMarker(meta.tableComment) != entry.targetMarker {
				return nil
			}
		}

		var curHasSnap bool
		var curSnapID int64
		if entry.hasRefs {
			hasRefs, hasSnap, snapID, snapErr := d.readTrinoMainSnapshotRef(ctx, entry.targetCatalog, entry.targetSchema, entry.targetName)
			if snapErr != nil {
				return snapErr
			}
			if hasRefs && hasSnap == entry.hasPreSnapshot && (!hasSnap || snapID == entry.preSnapshotID) {
				return nil
			}
			if entry.publishedSnapshotID != nil && (!hasSnap || snapID != *entry.publishedSnapshotID) {
				return fmt.Errorf("restore Trino CDC target %s: %w", targetFQN, errTrinoAtomicRollbackSuperseded)
			}
			curHasSnap = hasSnap
			curSnapID = snapID
		}

		targetSpecs, err := d.readTrinoSwapColumnSpecs(ctx, entry.targetCatalog, entry.targetSchema, entry.targetName)
		if err != nil {
			return err
		}
		if len(targetSpecs) == 0 {
			return nil
		}
		backupSpecs, err := d.readTrinoSwapColumnSpecs(ctx, batchCatalog, trinoCDCLeaseSchema, entry.backupName)
		if err != nil {
			return err
		}
		if len(backupSpecs) == 0 {
			return fmt.Errorf("trino CDC atomic rollback backup table %s is missing for %s", backupFQN, targetFQN)
		}

		targetByLower := make(map[string]trinoSwapColumnSpec, len(targetSpecs))
		allColNames := make([]string, 0, len(targetSpecs)+len(backupSpecs))
		for _, spec := range targetSpecs {
			targetByLower[strings.ToLower(spec.name)] = spec
			allColNames = append(allColNames, spec.name)
		}
		rbCols := make([]trinoSwapMergeCol, 0, len(backupSpecs))
		for _, bSpec := range backupSpecs {
			tSpec, ok := targetByLower[strings.ToLower(bSpec.name)]
			if !ok {
				allColNames = append(allColNames, bSpec.name)
				addDDL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", targetFQN, quoteIdentifier(bSpec.name), bSpec.dataType)
				if _, ddlErr := d.db.ExecContext(ctx, addDDL); ddlErr != nil {
					return fmt.Errorf("align Trino CDC target %s column %q for rollback: %w", targetFQN, bSpec.name, ddlErr)
				}
				rbCols = append(rbCols, trinoSwapMergeCol{name: bSpec.name})
				continue
			}
			switch classifyTrinoSwapType(tSpec.dataType, bSpec.dataType) {
			case trinoSwapTypeTargetSubsumesStage:
				rbCols = append(rbCols, trinoSwapMergeCol{name: tSpec.name, castToType: tSpec.dataType})
			default:
				rbCols = append(rbCols, trinoSwapMergeCol{name: tSpec.name})
			}
		}

		guards := make([]string, 0, 3)
		if entry.targetMarker != "" {
			guards = append(guards, trinoIncarnationExistsPredicate(entry.targetCatalog, entry.targetSchema, entry.targetName, entry.targetMarker))
		}
		guards = append(guards, backupPred)
		if entry.hasRefs {
			guards = append(guards, trinoSnapshotRefPredicate(entry.targetCatalog, entry.targetSchema, entry.targetName, true, curHasSnap, curSnapID))
		}

		rbSQL := buildConditionalSwapMergeSQL(targetFQN, backupFQN, rbCols, allColNames, strings.Join(guards, " AND "))
		_, execErr := d.db.ExecContext(ctx, rbSQL)
		if execErr == nil {
			return nil
		}
		if entry.expectedIncarnation != "" {
			actualInc, exists, incErr := d.CDCTargetIncarnation(ctx, targetFQN)
			if incErr != nil {
				return incErr
			}
			if !exists || actualInc != entry.expectedIncarnation {
				return nil
			}
		} else if entry.targetMarker != "" {
			meta, exists, metaErr := d.trinoTargetMetadata(ctx, targetFQN)
			if metaErr != nil {
				return metaErr
			}
			if !exists || trinoIncarnationMarker(meta.tableComment) != entry.targetMarker {
				return nil
			}
		}
		retryable := isTrinoCommitConflict(execErr) || (entry.hasRefs && strings.Contains(execErr.Error(), trinoCDCIncarnationGuardSentinel))
		if !retryable || attempt == trinoAtomicJournalConflictRetries-1 {
			config.LogFailedQuery(rbSQL, execErr)
			return fmt.Errorf("restore Trino CDC target %s from backup %s: %w", targetFQN, backupFQN, execErr)
		}
		if err := sleepWithContext(ctx, time.Duration(attempt+1)*50*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

func generateTrinoAtomicHex(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate Trino CDC atomic token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func formatTrinoBoolLiteral(v bool) string {
	if v {
		return "TRUE"
	}
	return "FALSE"
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
