package iceberg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	atomicSnapshotAttemptProperty = "ingestr.snapshot-attempt"
	atomicSnapshotTargetProperty  = "ingestr.snapshot-target"
	atomicSnapshotTargetUUID      = "ingestr.snapshot-target-uuid"
)

func atomicSnapshotStageIdent(target icebergtable.Identifier, attemptID string) icebergtable.Identifier {
	sum := sha256.Sum256([]byte(strings.Join(target, "\x00") + "\x00" + attemptID))
	stage := append(icebergtable.Identifier(nil), target...)
	stage[len(stage)-1] = "ingestr_snapshot_" + hex.EncodeToString(sum[:8])
	return stage
}

func (d *Destination) BeginAtomicSnapshot(ctx context.Context, opts destination.AtomicSnapshotOptions) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	target, err := parseIdentifier(opts.Table)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.AttemptID) == "" {
		return errors.New("iceberg: atomic snapshot requires an attempt identifier")
	}
	targetTable, err := d.catalog.LoadTable(ctx, target)
	if err != nil {
		return fmt.Errorf("iceberg: failed to load atomic snapshot target %s: %w", strings.Join(target, "."), err)
	}
	if err := d.validateExpectedIncarnation(ctx, targetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	targetUUID := targetTable.Metadata().TableUUID().String()
	stage := atomicSnapshotStageIdent(target, opts.AttemptID)
	if tbl, loadErr := d.catalog.LoadTable(ctx, stage); loadErr == nil {
		if err := validateAtomicSnapshotStageGeneration(tbl, targetTable, opts.AttemptID); err == nil {
			return nil
		} else if !isRecoverablePreOwnershipAtomicSnapshotStage(tbl) {
			return err
		}
		if err := d.dropAtomicSnapshotStage(ctx, tbl, target, opts.AttemptID, true); err != nil {
			return fmt.Errorf("iceberg: failed to recover pre-ownership atomic snapshot staging table %s: %w", strings.Join(stage, "."), err)
		}
	} else if !isMissingTableOrNamespace(loadErr) {
		return fmt.Errorf("iceberg: failed to inspect atomic snapshot staging table %s: %w", strings.Join(stage, "."), loadErr)
	}

	if err := d.PrepareTable(ctx, destination.PrepareOptions{
		Table:        strings.Join(stage, "."),
		Schema:       opts.Schema,
		CDCMode:      true,
		ExpiresAfter: destination.ManagedStagingTTL,
		TableProperties: map[string]string{
			atomicSnapshotAttemptProperty: opts.AttemptID,
			atomicSnapshotTargetProperty:  strings.Join(target, "."),
			atomicSnapshotTargetUUID:      targetUUID,
		},
	}); err != nil {
		return err
	}
	return nil
}

func (d *Destination) WriteAtomicSnapshot(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	target, err := parseIdentifier(opts.Table)
	if err != nil {
		return err
	}
	stageIdent := atomicSnapshotStageIdent(target, opts.AtomicSnapshotAttemptID)
	stage, err := d.catalog.LoadTable(ctx, stageIdent)
	if err != nil {
		return fmt.Errorf("iceberg: failed to load atomic snapshot staging table: %w", err)
	}
	if err := validateAtomicSnapshotStage(stage, target, opts.AtomicSnapshotAttemptID); err != nil {
		return err
	}
	opts.Table = strings.Join(stageIdent, ".")
	if _, err := d.renewManagedTableLease(ctx, stageIdent, time.Now()); err != nil {
		return fmt.Errorf("iceberg: failed to refresh atomic snapshot staging lease for %s: %w", opts.Table, err)
	}
	opts.StagingTable = true
	opts.CDCResumeLSN = ""
	opts.SkipCDCResume = true
	opts.CDCExpectedIncarnation = ""
	return d.WriteParallel(ctx, records, opts)
}

func (d *Destination) EvolveAtomicSnapshot(ctx context.Context, opts destination.AtomicSnapshotOptions) error {
	target, err := parseIdentifier(opts.Table)
	if err != nil {
		return err
	}
	stageIdent := atomicSnapshotStageIdent(target, opts.AttemptID)
	stage, err := d.catalog.LoadTable(ctx, stageIdent)
	if err != nil {
		return fmt.Errorf("iceberg: failed to load atomic snapshot staging table %s for evolution: %w", strings.Join(stageIdent, "."), err)
	}
	if err := validateAtomicSnapshotStage(stage, target, opts.AttemptID); err != nil {
		return err
	}
	txn := stage.NewTransaction()
	changed, err := d.stageTableSchemaUpdate(txn, stage, opts.Schema, false, false)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if _, err := txn.Commit(ctx); err != nil {
		return fmt.Errorf("iceberg: failed to evolve atomic snapshot staging table %s: %w", strings.Join(stageIdent, "."), err)
	}
	return nil
}

func (d *Destination) AbortAtomicSnapshot(ctx context.Context, opts destination.AtomicSnapshotOptions) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	target, err := parseIdentifier(opts.Table)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.AttemptID) == "" {
		return errors.New("iceberg: atomic snapshot abort requires an attempt identifier")
	}
	if opts.CommitToken != nil {
		tbl, loadErr := d.catalog.LoadTable(ctx, target)
		if loadErr != nil {
			return fmt.Errorf("iceberg: cannot verify atomic snapshot publication before abort: %w", loadErr)
		}
		if tableHasCommitToken(tbl, commitTokenID(opts.CommitToken)) {
			return fmt.Errorf("iceberg: refusing to abort published atomic snapshot attempt %s", opts.AttemptID)
		}
	}
	stageIdent := atomicSnapshotStageIdent(target, opts.AttemptID)
	stage, err := d.catalog.LoadTable(ctx, stageIdent)
	if isMissingTableOrNamespace(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("iceberg: failed to inspect atomic snapshot stage before abort: %w", err)
	}
	if err := validateAtomicSnapshotStage(stage, target, opts.AttemptID); err != nil {
		return err
	}
	return d.dropAtomicSnapshotStage(ctx, stage, target, opts.AttemptID, false)
}

func (d *Destination) PublishAtomicSnapshot(ctx context.Context, opts destination.AtomicSnapshotOptions) error {
	if strings.TrimSpace(opts.AttemptID) == "" {
		return errors.New("iceberg: atomic snapshot publish requires an attempt identifier")
	}
	if commitTokenID(opts.CommitToken) == "" {
		opts.CommitToken = "atomic-snapshot:" + opts.AttemptID
	}
	target, err := d.loadIcebergTable(ctx, opts.Table)
	if err != nil {
		return err
	}
	if err := d.validateExpectedIncarnation(ctx, target, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	metadata := newCommitMetadata(opts.CommitToken, opts.CDCResumeLSN).withExpectedIncarnation(opts.CDCExpectedIncarnation)
	if opts.SkipCDCResume {
		metadata.cdcResumeLSN = ""
		metadata.resetCDCResume = true
	}
	stageIdent := atomicSnapshotStageIdent(target.Identifier(), opts.AttemptID)
	stage, loadErr := d.catalog.LoadTable(ctx, stageIdent)
	if isMissingTableOrNamespace(loadErr) {
		if tableHasCommitToken(target, metadata.token) {
			return d.completeAtomicSnapshotPublication(ctx, target, nil, opts, metadata.token)
		}
		return fmt.Errorf("iceberg: atomic snapshot staging table %s is missing", strings.Join(stageIdent, "."))
	}
	if loadErr != nil {
		return fmt.Errorf("iceberg: failed to load atomic snapshot staging table %s: %w", strings.Join(stageIdent, "."), loadErr)
	}
	if err := validateAtomicSnapshotStageGeneration(stage, target, opts.AttemptID); err != nil {
		return err
	}
	targetUUID := target.Metadata().TableUUID().String()
	if err := d.validateExpectedIncarnation(ctx, target, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	if tableHasCommitToken(target, metadata.token) {
		return d.completeAtomicSnapshotPublication(ctx, target, stage, opts, metadata.token)
	}
	if err := validateCDCResumeAdvance(target, metadata); err != nil {
		return err
	}
	stageUUID := stage.Metadata().TableUUID().String()
	publishCtx, cancelPublish := context.WithCancel(ctx)
	defer cancelPublish()
	stageHeartbeat, err := d.startManagedTableLeaseHeartbeatWithCancel(publishCtx, stage, cancelPublish)
	if err != nil {
		return fmt.Errorf("iceberg: failed to start atomic snapshot staging lease heartbeat: %w", err)
	}
	if stageHeartbeat == nil {
		return errors.New("iceberg: atomic snapshot staging table is not managed")
	}
	defer stageHeartbeat.stop()
	ctx = publishCtx

	desired := opts.TargetSchema
	if desired == nil {
		desired = destination.DestinationTableSchema(opts.Schema)
	}
	if desired == nil {
		return errors.New("iceberg: atomic snapshot publication requires schema")
	}
	if err := validateIcebergTableSchema(desired); err != nil {
		return err
	}
	writeSchema := icebergArrowSchema(desired)
	sourceFields := stage.Schema().Fields()
	sourceColumns := make([]string, len(sourceFields))
	for i, field := range sourceFields {
		sourceColumns[i] = field.Name
	}
	openStageReader := func() *chanRecordReader {
		projection := newRowProjection(writeSchema, sourceColumns)
		return streamingReader(writeSchema, func(sink func(batch arrow.RecordBatch) error) error {
			emitter := newBatchEmitter(projection, sink)
			defer emitter.release()
			if err := forEachScannedRow(ctx, stage, iceberggo.AlwaysTrue{}, emitter.add); err != nil {
				return err
			}
			return emitter.flushBatch()
		})
	}
	stageReader := openStageReader()
	openReplay, cleanupReplay, err := spoolReplayableRecordReader(stageReader)
	stageReader.Release()
	if err != nil {
		return fmt.Errorf("iceberg: failed to spool atomic snapshot stage: %w", err)
	}
	defer cleanupReplay()

	props := snapshotProps("snapshot", metadata)
	prepared := preparedTable{
		replace:          true,
		preserveMetadata: true,
		evolveSchema:     true,
		schema:           desired,
		partitionBy:      opts.PartitionBy,
		clusterBy:        opts.ClusterBy,
	}
	current := target
	var committed *icebergtable.Table
	var publishErr error
	for attempt := 0; attempt < 5; attempt++ {
		if err := d.validateExpectedIncarnation(ctx, current, opts.CDCExpectedIncarnation); err != nil {
			return err
		}
		if err := validateAtomicSnapshotTargetGeneration(current, targetUUID); err != nil {
			return err
		}
		if tableHasCommitToken(current, metadata.token) {
			committed = current
			publishErr = nil
			break
		}
		if err := validateCDCResumeAdvance(current, metadata); err != nil {
			return err
		}
		currentStage, leaseErr := d.renewManagedTableLease(ctx, stageIdent, time.Now())
		if leaseErr != nil {
			return fmt.Errorf("iceberg: failed to renew atomic snapshot stage before publish attempt: %w", leaseErr)
		}
		if currentStage.Metadata().TableUUID().String() != stageUUID {
			return fmt.Errorf("iceberg: atomic snapshot staging UUID changed before target commit")
		}
		if err := validateAtomicSnapshotStage(currentStage, target.Identifier(), opts.AttemptID); err != nil {
			return err
		}
		reader, release, replayErr := openReplay()
		if replayErr != nil {
			return replayErr
		}
		committed, publishErr = d.overwritePrepared(ctx, current, reader, props, prepared, opts.Parallelism, nil, true, opts.CDCExpectedIncarnation)
		release()
		if publishErr == nil {
			break
		}
		if !errors.Is(publishErr, icebergtable.ErrCommitFailed) {
			if reconciled := d.reconcileCommit(ctx, opts.Table, metadata.token, opts.CDCExpectedIncarnation, publishErr); reconciled != nil {
				return fmt.Errorf("iceberg: failed to publish atomic snapshot for table %s: %w", opts.Table, reconciled)
			}
			committed, err = d.loadIcebergTable(ctx, opts.Table)
			if err != nil {
				return fmt.Errorf("iceberg: atomic snapshot for table %s committed but could not be reloaded: %w", opts.Table, err)
			}
			if err := validateAtomicSnapshotTargetGeneration(committed, targetUUID); err != nil {
				return err
			}
			publishErr = nil
			break
		}
		current, err = d.loadIcebergTable(ctx, opts.Table)
		if err != nil {
			return fmt.Errorf("iceberg: failed to reload atomic snapshot target %s after commit conflict: %w", opts.Table, err)
		}
		if err := validateAtomicSnapshotTargetGeneration(current, targetUUID); err != nil {
			return err
		}
		if err := waitForCommitRetry(ctx, attempt); err != nil {
			return err
		}
	}
	if committed == nil && publishErr != nil {
		return fmt.Errorf("iceberg: failed to publish atomic snapshot for table %s after retries: %w", opts.Table, publishErr)
	}
	finalStage, err := stageHeartbeat.stopAndRefresh(ctx)
	if err != nil {
		return fmt.Errorf("iceberg: atomic snapshot target committed but staging lease finalization failed: %w", err)
	}
	if finalStage.Metadata().TableUUID().String() != stageUUID {
		return fmt.Errorf("iceberg: atomic snapshot target committed but staging UUID changed")
	}
	if err := validateAtomicSnapshotStage(finalStage, target.Identifier(), opts.AttemptID); err != nil {
		return err
	}
	return d.completeAtomicSnapshotPublication(ctx, committed, stage, opts, metadata.token)
}

func (d *Destination) completeAtomicSnapshotPublication(
	ctx context.Context,
	committed, stage *icebergtable.Table,
	opts destination.AtomicSnapshotOptions,
	commitToken string,
) error {
	if opts.SkipCDCResume {
		if err := d.ensureManagedCDCResumeResetExpected(ctx, committed, commitToken, opts.CDCExpectedIncarnation); err != nil {
			return err
		}
	}
	if _, err := d.ensureSortOrderExpected(ctx, committed, opts.ClusterBy, opts.CDCExpectedIncarnation); err != nil {
		return fmt.Errorf("iceberg: atomic snapshot for table %s committed but its sort order is not: %w", opts.Table, err)
	}
	d.afterSuccessfulCommitExpected(ctx, opts.Table, opts.CDCExpectedIncarnation)
	if stage == nil {
		return nil
	}
	if err := d.dropAtomicSnapshotStage(ctx, stage, committed.Identifier(), opts.AttemptID, false); err != nil {
		return fmt.Errorf("iceberg: snapshot for table %s published but staging cleanup failed: %w", opts.Table, err)
	}
	return nil
}

func (d *Destination) dropAtomicSnapshotStage(
	ctx context.Context,
	stage *icebergtable.Table,
	target icebergtable.Identifier,
	attemptID string,
	preOwnership bool,
) error {
	return d.dropTableWithLifecycleExpected(
		ctx,
		stage.Identifier(),
		stage.Metadata().TableUUID().String(),
		func(current *icebergtable.Table) error {
			if preOwnership {
				if !isRecoverablePreOwnershipAtomicSnapshotStage(current) {
					return fmt.Errorf("iceberg: atomic snapshot staging ownership changed for %s", strings.Join(stage.Identifier(), "."))
				}
				return nil
			}
			return validateAtomicSnapshotStage(current, target, attemptID)
		},
	)
}

var (
	_ destination.AtomicSnapshotPublisher = (*Destination)(nil)
	_ destination.AtomicSnapshotAborter   = (*Destination)(nil)
)

func validateAtomicSnapshotStage(tbl *icebergtable.Table, target icebergtable.Identifier, attemptID string) error {
	_, managed, err := managedTableExpiration(tbl.Properties())
	if err != nil {
		return fmt.Errorf("iceberg: invalid atomic snapshot staging table %s: %w", strings.Join(tbl.Identifier(), "."), err)
	}
	wantTarget := strings.Join(target, ".")
	if !managed || tbl.Properties()[atomicSnapshotAttemptProperty] != attemptID || tbl.Properties()[atomicSnapshotTargetProperty] != wantTarget {
		return fmt.Errorf("iceberg: atomic snapshot staging ownership mismatch for %s", strings.Join(tbl.Identifier(), "."))
	}
	return nil
}

func validateAtomicSnapshotStageGeneration(stage, target *icebergtable.Table, attemptID string) error {
	if err := validateAtomicSnapshotStage(stage, target.Identifier(), attemptID); err != nil {
		return err
	}
	expected := stage.Properties()[atomicSnapshotTargetUUID]
	if expected == "" {
		return fmt.Errorf("iceberg: atomic snapshot staging table %s has no target generation", strings.Join(stage.Identifier(), "."))
	}
	return validateAtomicSnapshotTargetGeneration(target, expected)
}

func validateAtomicSnapshotTargetGeneration(target *icebergtable.Table, expected string) error {
	actual := target.Metadata().TableUUID().String()
	if actual != expected {
		return fmt.Errorf("iceberg: atomic snapshot target generation changed for %s: expected UUID %s, got %s", strings.Join(target.Identifier(), "."), expected, actual)
	}
	return nil
}

func isRecoverablePreOwnershipAtomicSnapshotStage(tbl *icebergtable.Table) bool {
	_, managed, err := managedTableExpiration(tbl.Properties())
	return err == nil && managed && tbl.Properties()[atomicSnapshotAttemptProperty] == "" && tbl.Properties()[atomicSnapshotTargetProperty] == "" && tbl.Properties()[atomicSnapshotTargetUUID] == ""
}
