package iceberg

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergtable "github.com/apache/iceberg-go/table"
	icebergcompaction "github.com/apache/iceberg-go/table/compaction"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/google/uuid"
)

const (
	maintenanceEnabledProperty            = "ingestr.maintenance.enabled"
	maintenanceIntervalMSProperty         = "ingestr.maintenance.interval-ms"
	maintenanceLastRunAtProperty          = "ingestr.maintenance.last-run-at"
	maintenanceExpireSnapshotsProperty    = "ingestr.maintenance.expire-snapshots"
	maintenanceSnapshotMaxAgeMSProperty   = "ingestr.maintenance.snapshot-max-age-ms"
	maintenanceMinSnapshotsProperty       = "ingestr.maintenance.min-snapshots-to-keep"
	maintenanceDeleteOrphansProperty      = "ingestr.maintenance.delete-orphans"
	maintenanceOrphanAgeMSProperty        = "ingestr.maintenance.orphan-file-age-ms"
	maintenanceCompactDataProperty        = "ingestr.maintenance.compact-data-files"
	maintenanceTargetFileSizeProperty     = "ingestr.maintenance.target-file-size-bytes"
	maintenanceMinInputFilesProperty      = "ingestr.maintenance.min-input-files"
	maintenanceDeleteFileThreshold        = "ingestr.maintenance.delete-file-threshold"
	maintenanceManifestMergeProperty      = "ingestr.maintenance.manifest-merge"
	maintenanceManifestMinCountProperty   = "ingestr.maintenance.manifest-min-count"
	maintenanceManifestTargetSizeProperty = "ingestr.maintenance.manifest-target-size-bytes"
	maintenancePreviousMetadataProperty   = "ingestr.maintenance.previous-metadata-versions"

	defaultMaintenanceInterval      = time.Hour
	managedExpirationScanInterval   = time.Hour
	managedExpirationCleanupTimeout = 30 * time.Minute
	maintenanceRunTimeout           = 30 * time.Minute
	defaultSnapshotMaxAge           = 7 * 24 * time.Hour
	defaultMinSnapshotsToKeep       = 3
	defaultOrphanFileAge            = 72 * time.Hour
	minimumSafeOrphanFileAge        = 24 * time.Hour
	defaultPreviousMetadataVersion  = 100
)

// MaintenanceOptions controls an explicit maintenance run. Every operation is
// opt-in. Configured post-commit maintenance uses these same options after the
// table property ingestr.maintenance.enabled has explicitly enabled it.
type MaintenanceOptions struct {
	ExpireSnapshots        bool
	SnapshotMaxAge         time.Duration
	MinSnapshotsToKeep     int
	DeleteOrphanFiles      bool
	OrphanFileAge          time.Duration
	CompactDataFiles       bool
	TargetFileSizeBytes    int64
	MinInputFiles          uint
	DeleteFileThreshold    int
	EnableManifestMerge    bool
	ManifestMinCount       int
	ManifestTargetSize     int64
	PreviousMetadataFiles  int
	configureManifestMerge bool
}

// MaintenanceResult contains the observable effects of a maintenance run.
// Data/delete files removed from snapshots remain on storage until they also
// pass the orphan-file grace period.
type MaintenanceResult struct {
	SnapshotsBefore              int
	SnapshotsAfter               int
	CompactionGroups             int
	AddedDataFiles               int
	RemovedDataFiles             int
	RemovedPositionDeleteFiles   int
	RemovedEqualityDeleteFiles   int
	DeletedOrphanFiles           int
	DeletedOrphanFileLocations   []string
	ManifestMergeEnabled         bool
	PreviousMetadataVersionsKept int
}

// MaintainTable safely performs the selected maintenance operations. Snapshot
// file deletion is deferred to orphan cleanup so a lost commit response never
// causes the process to delete files that may still be referenced.
func (d *Destination) MaintainTable(ctx context.Context, table string, opts MaintenanceOptions) (MaintenanceResult, error) {
	if d.catalog == nil {
		return MaintenanceResult{}, errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return MaintenanceResult{}, err
	}
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		return MaintenanceResult{}, fmt.Errorf("iceberg: failed to load table %s for maintenance: %w", table, err)
	}
	return d.maintainLoadedTable(ctx, tbl, opts)
}

func (d *Destination) maintainLoadedTable(
	ctx context.Context,
	tbl *icebergtable.Table,
	opts MaintenanceOptions,
) (result MaintenanceResult, retErr error) {
	opts, err := normalizeMaintenanceOptions(opts)
	if err != nil {
		return MaintenanceResult{}, err
	}
	if opts.DeleteOrphanFiles {
		if err := d.validateOrphanCleanupIsolation(ctx, tbl); err != nil {
			return MaintenanceResult{}, err
		}
	}
	result = MaintenanceResult{
		SnapshotsBefore: len(tbl.Metadata().Snapshots()),
		SnapshotsAfter:  len(tbl.Metadata().Snapshots()),
	}
	generatedCompactionPaths := make([]string, 0)
	compactionCommitted, cleanupSafe := false, true
	defer func() {
		if compactionCommitted || !cleanupSafe || len(generatedCompactionPaths) == 0 {
			return
		}
		fs, err := tbl.FS(context.WithoutCancel(ctx))
		if err == nil {
			err = removeGeneratedMergeFiles(fs, tbl.Location(), "", generatedCompactionPaths)
		}
		if err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()

	txn := tbl.NewTransaction()
	properties := maintenanceTablePropertyUpdates(tbl, opts)
	if len(properties) > 0 {
		if err := txn.SetProperties(properties); err != nil {
			return result, fmt.Errorf("iceberg: failed to stage maintenance properties: %w", err)
		}
	}

	if opts.CompactDataFiles && tbl.CurrentSnapshot() != nil {
		rewriteResult, err := compactTable(ctx, txn, tbl, opts, &generatedCompactionPaths)
		if err != nil {
			return result, fmt.Errorf("iceberg: failed to compact table %s: %w", strings.Join(tbl.Identifier(), "."), err)
		}
		result.CompactionGroups = rewriteResult.RewrittenGroups
		result.AddedDataFiles = rewriteResult.AddedDataFiles
		result.RemovedDataFiles = rewriteResult.RemovedDataFiles
		result.RemovedPositionDeleteFiles = rewriteResult.RemovedPositionDeleteFiles
		result.RemovedEqualityDeleteFiles = rewriteResult.RemovedEqualityDeleteFiles
	}

	if opts.ExpireSnapshots {
		if result.CompactionGroups == 0 {
			if err := stageMaintenanceLineageSnapshot(ctx, txn, tbl); err != nil {
				return result, fmt.Errorf("iceberg: failed to preserve commit lineage before snapshot expiration: %w", err)
			}
		}
		// Never delete files from ExpireSnapshots.PostCommit. If Commit returns
		// an unknown state, no physical deletion has happened. A later orphan
		// pass reloads confirmed metadata and observes a mandatory grace period.
		if err := txn.ExpireSnapshots(
			icebergtable.WithOlderThan(opts.SnapshotMaxAge),
			icebergtable.WithRetainLast(opts.MinSnapshotsToKeep),
			icebergtable.WithPostCommit(false),
		); err != nil {
			return result, fmt.Errorf("iceberg: failed to stage snapshot expiration: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	committed, err := txn.Commit(ctx)
	if err != nil {
		// This may be an unknown commit outcome. Do not reload-and-delete here:
		// the generated compaction files and all pre-commit files are retained
		// for a future, grace-delayed orphan cleanup to reconcile safely.
		cleanupSafe = errors.Is(err, icebergtable.ErrCommitFailed)
		return result, fmt.Errorf("iceberg: failed to commit maintenance for table %s: %w", strings.Join(tbl.Identifier(), "."), err)
	}
	compactionCommitted = true
	result.SnapshotsAfter = len(committed.Metadata().Snapshots())
	result.ManifestMergeEnabled = committed.Properties().GetBool(icebergtable.ManifestMergeEnabledKey, false)
	result.PreviousMetadataVersionsKept = committed.Properties().GetInt(
		icebergtable.MetadataPreviousVersionsMaxKey,
		icebergtable.MetadataPreviousVersionsMaxDefault,
	)

	if opts.DeleteOrphanFiles {
		confirmed, err := d.catalog.LoadTable(ctx, committed.Identifier())
		if err != nil {
			return result, fmt.Errorf("iceberg: failed to reload confirmed metadata before orphan cleanup for table %s: %w", strings.Join(tbl.Identifier(), "."), err)
		}
		orphans, err := confirmed.DeleteOrphanFiles(
			ctx,
			icebergtable.WithFilesOlderThan(opts.OrphanFileAge),
			icebergtable.WithPrefixMismatchMode(icebergtable.PrefixMismatchError),
		)
		if err != nil {
			return result, fmt.Errorf("iceberg: failed to delete orphan files for table %s: %w", strings.Join(tbl.Identifier(), "."), err)
		}
		result.DeletedOrphanFiles = len(orphans.DeletedFiles)
		result.DeletedOrphanFileLocations = append([]string(nil), orphans.DeletedFiles...)
	}
	return result, nil
}

func stageMaintenanceLineageSnapshot(
	ctx context.Context,
	txn *icebergtable.Transaction,
	tbl *icebergtable.Table,
) error {
	props := maintenanceSnapshotProperties(tbl)
	if (props[snapshotCommitTokenKey] == "" && props[snapshotCDCResumeLSNKey] == "" && props[snapshotCDCResetKey] == "") ||
		currentSnapshotHasProperties(tbl, props) {
		// With no durable lineage values there is nothing that expiration needs
		// to carry.
		return nil
	}
	writeSchema, err := tableWriteSchema(tbl)
	if err != nil {
		return err
	}
	reader, err := array.NewRecordReader(writeSchema, nil)
	if err != nil {
		return err
	}
	defer reader.Release()
	return txn.Append(ctx, reader, props)
}

func currentSnapshotHasProperties(tbl *icebergtable.Table, props iceberggo.Properties) bool {
	snapshot := tbl.CurrentSnapshot()
	if snapshot == nil || snapshot.Summary == nil {
		return false
	}
	for _, key := range []string{snapshotCommitTokenKey, snapshotCDCResumeLSNKey, snapshotCDCResetKey} {
		if value := props[key]; value != "" && snapshot.Summary.Properties[key] != value {
			return false
		}
	}
	return true
}

func normalizeMaintenanceOptions(opts MaintenanceOptions) (MaintenanceOptions, error) {
	if opts.ExpireSnapshots {
		if opts.SnapshotMaxAge == 0 {
			opts.SnapshotMaxAge = defaultSnapshotMaxAge
		}
		if opts.SnapshotMaxAge < 0 {
			return opts, fmt.Errorf("iceberg: snapshot max age must not be negative: %s", opts.SnapshotMaxAge)
		}
		if opts.MinSnapshotsToKeep == 0 {
			opts.MinSnapshotsToKeep = defaultMinSnapshotsToKeep
		}
		if opts.MinSnapshotsToKeep < 1 {
			return opts, fmt.Errorf("iceberg: min snapshots to keep must be at least 1, got %d", opts.MinSnapshotsToKeep)
		}
	}
	if opts.DeleteOrphanFiles {
		if opts.OrphanFileAge == 0 {
			opts.OrphanFileAge = defaultOrphanFileAge
		}
		if opts.OrphanFileAge < minimumSafeOrphanFileAge {
			return opts, fmt.Errorf("iceberg: orphan file age %s is unsafe; minimum is %s", opts.OrphanFileAge, minimumSafeOrphanFileAge)
		}
	}
	if opts.TargetFileSizeBytes < 0 {
		return opts, fmt.Errorf("iceberg: compaction target file size must not be negative, got %d", opts.TargetFileSizeBytes)
	}
	const maxCompactionTargetFileSize = int64(1<<63-1) / 9 * 5
	if opts.TargetFileSizeBytes > maxCompactionTargetFileSize {
		return opts, fmt.Errorf("iceberg: compaction target file size is too large: %d", opts.TargetFileSizeBytes)
	}
	if opts.DeleteFileThreshold < 0 {
		return opts, fmt.Errorf("iceberg: delete file threshold must not be negative, got %d", opts.DeleteFileThreshold)
	}
	if opts.PreviousMetadataFiles < 0 {
		return opts, fmt.Errorf("iceberg: previous metadata versions must not be negative, got %d", opts.PreviousMetadataFiles)
	}
	if opts.ManifestMinCount < 0 {
		return opts, fmt.Errorf("iceberg: manifest minimum count must not be negative, got %d", opts.ManifestMinCount)
	}
	if opts.ManifestTargetSize < 0 {
		return opts, fmt.Errorf("iceberg: manifest target size must not be negative, got %d", opts.ManifestTargetSize)
	}
	return opts, nil
}

func maintenanceTablePropertyUpdates(tbl *icebergtable.Table, opts MaintenanceOptions) iceberggo.Properties {
	desired := iceberggo.Properties{}
	if opts.EnableManifestMerge || opts.configureManifestMerge {
		desired[icebergtable.ManifestMergeEnabledKey] = strconv.FormatBool(opts.EnableManifestMerge)
	}
	if opts.EnableManifestMerge {
		if opts.ManifestMinCount > 0 {
			desired[icebergtable.ManifestMinMergeCountKey] = strconv.Itoa(opts.ManifestMinCount)
		}
		if opts.ManifestTargetSize > 0 {
			desired[icebergtable.ManifestTargetSizeBytesKey] = strconv.FormatInt(opts.ManifestTargetSize, 10)
		}
	}
	if opts.PreviousMetadataFiles > 0 {
		desired[icebergtable.MetadataPreviousVersionsMaxKey] = strconv.Itoa(opts.PreviousMetadataFiles)
	}

	updates := iceberggo.Properties{}
	for key, value := range desired {
		if tbl.Properties()[key] != value {
			updates[key] = value
		}
	}
	return updates
}

func compactTable(
	ctx context.Context,
	txn *icebergtable.Transaction,
	tbl *icebergtable.Table,
	opts MaintenanceOptions,
	generatedPaths *[]string,
) (*icebergtable.RewriteResult, error) {
	tasks, err := tbl.Scan().PlanFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("plan table files: %w", err)
	}

	cfg := icebergcompaction.DefaultConfig()
	targetSize := opts.TargetFileSizeBytes
	if targetSize == 0 {
		targetSize = int64(tbl.Properties().GetInt(
			icebergtable.WriteTargetFileSizeBytesKey,
			icebergtable.WriteTargetFileSizeBytesDefault,
		))
	}
	if targetSize > 0 {
		cfg.TargetFileSizeBytes = targetSize
		cfg.MinFileSizeBytes = targetSize * 3 / 4
		cfg.MaxFileSizeBytes = targetSize * 9 / 5
	}
	if opts.MinInputFiles > 0 {
		cfg.MinInputFiles = opts.MinInputFiles
	}
	if opts.DeleteFileThreshold > 0 {
		cfg.DeleteFileThreshold = opts.DeleteFileThreshold
	}
	plan, err := cfg.PlanCompaction(tasks)
	if err != nil {
		return nil, err
	}
	if len(plan.Groups) == 0 {
		return &icebergtable.RewriteResult{}, nil
	}

	groups := make([]icebergtable.CompactionTaskGroup, 0, len(plan.Groups))
	rewrittenPaths := make(map[string]struct{})
	for _, group := range plan.Groups {
		groups = append(groups, icebergtable.CompactionTaskGroup{
			PartitionKey:   group.PartitionKey,
			Tasks:          group.Tasks,
			TotalSizeBytes: group.TotalSizeBytes,
		})
		for _, task := range group.Tasks {
			rewrittenPaths[task.File.FilePath()] = struct{}{}
		}
	}

	fs, err := tbl.FS(ctx)
	if err != nil {
		return nil, fmt.Errorf("load table file IO: %w", err)
	}
	deadEqualityDeletes, err := icebergcompaction.CollectDeadEqualityDeletes(
		ctx,
		fs,
		tbl.CurrentSnapshot(),
		rewrittenPaths,
	)
	if err != nil {
		return nil, fmt.Errorf("identify dead equality deletes: %w", err)
	}

	if tbl.SortOrder().Len() > 0 {
		return rewriteSortedDataFiles(ctx, txn, tbl, tasks, groups, deadEqualityDeletes, targetSize, generatedPaths)
	}
	return txn.RewriteDataFiles(ctx, groups, icebergtable.RewriteDataFilesOptions{
		SnapshotProps:            maintenanceSnapshotProperties(tbl),
		ExtraDeleteFilesToRemove: deadEqualityDeletes,
		GroupOptions: []icebergtable.CompactionGroupOption{
			icebergtable.WithCompactionTargetFileSize(targetSize),
		},
	})
}

func rewriteSortedDataFiles(
	ctx context.Context,
	txn *icebergtable.Transaction,
	tbl *icebergtable.Table,
	allTasks []icebergtable.FileScanTask,
	groups []icebergtable.CompactionTaskGroup,
	deadEqualityDeletes []iceberggo.DataFile,
	targetSize int64,
	transferredPaths *[]string,
) (retResult *icebergtable.RewriteResult, retErr error) {
	clusterBy, ok := identitySortColumns(tbl)
	if !ok {
		return nil, fmt.Errorf("table sort order uses transforms, direction, or null ordering that the compactor cannot preserve")
	}
	result := &icebergtable.RewriteResult{}
	retResult = result
	tableFS, err := tbl.FS(ctx)
	if err != nil {
		return result, err
	}
	generatedPaths := make([]string, 0)
	committed, cleanupSafe := false, true
	defer func() {
		if committed || !cleanupSafe {
			return
		}
		if cleanupErr := removeGeneratedMergeFiles(tableFS, tbl.Location(), "", generatedPaths); cleanupErr != nil {
			retErr = errors.Join(retErr, cleanupErr)
		}
	}()
	rewrite := txn.NewRewrite(maintenanceSnapshotProperties(tbl))
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		groupResult, err := executeSortedCompactionGroup(ctx, tbl, group, clusterBy, targetSize)
		if err != nil {
			return result, err
		}
		if len(groupResult.OldDataFiles) == 0 && len(groupResult.NewDataFiles) == 0 {
			continue
		}
		rewrite.ApplyResult(groupResult)
		for _, file := range groupResult.NewDataFiles {
			generatedPaths = append(generatedPaths, file.FilePath())
		}
		result.RewrittenGroups++
		result.AddedDataFiles += len(groupResult.NewDataFiles)
		result.RemovedDataFiles += len(groupResult.OldDataFiles)
		result.RemovedPositionDeleteFiles += len(groupResult.SafePosDeletes)
		result.BytesBefore += groupResult.BytesBefore
		result.BytesAfter += groupResult.BytesAfter
	}
	if result.RewrittenGroups == 0 {
		return result, nil
	}
	for _, file := range deadEqualityDeletes {
		rewrite.DeleteFile(file)
		result.RemovedEqualityDeleteFiles++
	}
	for _, file := range safePositionDeletesForRewrite(allTasks, groups) {
		rewrite.DeleteFile(file)
		result.RemovedPositionDeleteFiles++
	}
	if err := rewrite.Commit(ctx); err != nil {
		cleanupSafe = errors.Is(err, icebergtable.ErrCommitFailed)
		return result, fmt.Errorf("commit sorted compaction: %w", err)
	}
	*transferredPaths = append(*transferredPaths, generatedPaths...)
	committed = true
	return result, nil
}

func safePositionDeletesForRewrite(
	allTasks []icebergtable.FileScanTask,
	groups []icebergtable.CompactionTaskGroup,
) []iceberggo.DataFile {
	rewritten := make(map[string]struct{})
	candidates := make(map[string]iceberggo.DataFile)
	for _, group := range groups {
		for _, task := range group.Tasks {
			rewritten[task.File.FilePath()] = struct{}{}
			for _, file := range task.DeleteFiles {
				if file.ContentType() == iceberggo.EntryContentPosDeletes {
					candidates[file.FilePath()] = file
				}
			}
		}
	}
	unsafe := make(map[string]struct{})
	for _, task := range allTasks {
		if _, ok := rewritten[task.File.FilePath()]; ok {
			continue
		}
		for _, file := range task.DeleteFiles {
			if _, ok := candidates[file.FilePath()]; ok {
				unsafe[file.FilePath()] = struct{}{}
			}
		}
	}
	paths := make([]string, 0, len(candidates))
	for path := range candidates {
		if _, ok := unsafe[path]; !ok {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	result := make([]iceberggo.DataFile, 0, len(paths))
	for _, path := range paths {
		result = append(result, candidates[path])
	}
	return result
}

func executeSortedCompactionGroup(
	ctx context.Context,
	tbl *icebergtable.Table,
	group icebergtable.CompactionTaskGroup,
	clusterBy []string,
	targetSize int64,
) (retResult icebergtable.CompactionGroupResult, retErr error) {
	if len(group.Tasks) == 0 {
		return icebergtable.CompactionGroupResult{PartitionKey: group.PartitionKey}, nil
	}
	tableFS, err := tbl.FS(ctx)
	if err != nil {
		return retResult, err
	}
	writeID := uuid.New()
	generatedPaths := make([]string, 0)
	transferred := false
	defer func() {
		if transferred {
			return
		}
		if err := removeGeneratedMergeFiles(tableFS, tbl.Location(), writeID.String(), generatedPaths); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()
	scan := tbl.Scan()
	preserveLineage := tbl.Metadata().Version() >= 3 && allTasksHaveRowLineage(group.Tasks)
	if preserveLineage {
		scan = tbl.Scan(icebergtable.WithRowLineage())
	}
	arrowSchema, records, err := scan.ReadTasks(ctx, group.Tasks)
	if err != nil {
		return icebergtable.CompactionGroupResult{}, fmt.Errorf("read sorted compaction group %q: %w", group.PartitionKey, err)
	}
	if preserveLineage {
		arrowSchema, err = icebergtable.SchemaToArrowSchema(iceberggo.SchemaWithRowLineage(tbl.Schema()), nil, true, false)
		if err != nil {
			return icebergtable.CompactionGroupResult{}, fmt.Errorf("build row-lineage schema for sorted compaction group %q: %w", group.PartitionKey, err)
		}
	}
	reader := streamingReader(arrowSchema, func(sink func(arrow.RecordBatch) error) error {
		for batch, readErr := range records {
			if readErr != nil {
				return readErr
			}
			if err := sink(batch); err != nil {
				return err
			}
		}
		return nil
	})
	defer reader.Release()
	clustered, cleanup, err := clusterRecordReader(ctx, reader, clusterBy)
	if err != nil {
		return icebergtable.CompactionGroupResult{}, fmt.Errorf("sort compaction group %q: %w", group.PartitionKey, err)
	}
	defer cleanup()

	writeOpts := []icebergtable.WriteRecordOption{icebergtable.WithClusteredWrite(), icebergtable.WithWriteUUID(writeID)}
	if targetSize > 0 {
		writeOpts = append(writeOpts, icebergtable.WithTargetFileSize(targetSize))
	}
	if preserveLineage {
		writeOpts = append(writeOpts, icebergtable.WithPreserveRowLineage(iceberggo.SchemaWithRowLineage(tbl.Schema())))
	}
	newFiles := make([]iceberggo.DataFile, 0)
	var bytesAfter int64
	for dataFile, writeErr := range icebergtable.WriteRecords(ctx, tbl, arrowSchema, retainedRecordIterator(clustered), writeOpts...) {
		if writeErr != nil {
			return icebergtable.CompactionGroupResult{}, fmt.Errorf("write sorted compaction group %q: %w", group.PartitionKey, writeErr)
		}
		dataFile, err = withDataFileSortOrderID(dataFile, tbl, tbl.SortOrder().OrderID())
		if err != nil {
			return icebergtable.CompactionGroupResult{}, err
		}
		newFiles = append(newFiles, dataFile)
		generatedPaths = append(generatedPaths, dataFile.FilePath())
		bytesAfter += dataFile.FileSizeBytes()
	}
	if err := clustered.Err(); err != nil {
		return icebergtable.CompactionGroupResult{}, err
	}
	oldFiles := make([]iceberggo.DataFile, 0, len(group.Tasks))
	for _, task := range group.Tasks {
		oldFiles = append(oldFiles, task.File)
	}
	retResult = icebergtable.CompactionGroupResult{
		PartitionKey:   group.PartitionKey,
		OldDataFiles:   oldFiles,
		NewDataFiles:   newFiles,
		SafePosDeletes: nil,
		BytesBefore:    group.TotalSizeBytes,
		BytesAfter:     bytesAfter,
	}
	transferred = true
	return retResult, nil
}

func allTasksHaveRowLineage(tasks []icebergtable.FileScanTask) bool {
	if len(tasks) == 0 {
		return false
	}
	for _, task := range tasks {
		if task.FirstRowID == nil {
			return false
		}
	}
	return true
}

func maintenanceSnapshotProperties(tbl *icebergtable.Table) iceberggo.Properties {
	props := snapshotProps("maintenance")
	var newestToken string
	snapshotInCurrentLineage(tbl, func(snapshot *icebergtable.Snapshot) bool {
		if snapshot.Summary == nil {
			return false
		}
		token := snapshot.Summary.Properties[snapshotCommitTokenKey]
		if newestToken == "" {
			newestToken = token
		}
		if cursor := snapshot.Summary.Properties[snapshotCDCResumeLSNKey]; cursor != "" {
			props[snapshotCDCResumeLSNKey] = cursor
			if token != "" {
				props[snapshotCommitTokenKey] = token
			}
			return true
		}
		operation := snapshot.Summary.Properties["ingestr.operation"]
		cdcBarrier := snapshot.Summary.Properties[snapshotCDCResetKey] == "true" ||
			operation == "replace" || operation == "truncate" || operation == "truncate+insert"
		if cdcBarrier {
			props[snapshotCDCResetKey] = "true"
		}
		return cdcBarrier
	})
	if props[snapshotCommitTokenKey] == "" && props[snapshotCDCResumeLSNKey] == "" {
		props[snapshotCommitTokenKey] = newestToken
	}
	if props[snapshotCommitTokenKey] == "" {
		delete(props, snapshotCommitTokenKey)
	}
	if props[snapshotCDCResumeLSNKey] == "" {
		delete(props, snapshotCDCResumeLSNKey)
	}
	return props
}

// runConfiguredMaintenance is the post-commit hook used by WriteParallel. It
// deliberately has no error result: a confirmed data commit remains successful
// even when optional maintenance fails. Configure it through table URI
// properties such as table.ingestr.maintenance.enabled=true.
func (d *Destination) runConfiguredMaintenance(ctx context.Context, table string) {
	d.runConfiguredMaintenanceExpected(ctx, table, "")
}

func (d *Destination) runConfiguredMaintenanceExpected(ctx context.Context, table, expectedIncarnation string) {
	ident, err := parseIdentifier(table)
	if err != nil {
		config.Debug("[ICEBERG] Skipping configured maintenance for %s: %v", table, err)
		return
	}
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		config.Debug("[ICEBERG] Failed to load %s for configured maintenance: %v", table, err)
		return
	}
	if err := d.validateExpectedIncarnation(ctx, tbl, expectedIncarnation); err != nil {
		config.Debug("[ICEBERG] Skipping configured maintenance for %s: %v", table, err)
		return
	}
	managed, err := boolProperty(tbl.Properties(), managedTableProperty, false)
	if err != nil {
		config.Debug("[ICEBERG] Invalid managed-table marker for %s: %v", table, err)
		return
	}
	if managed {
		// Staging tables are short-lived and physically purged. Compacting them
		// after extraction only delays the merge and creates redundant files.
		if err := d.refreshManagedTableLease(ctx, tbl, time.Now()); err != nil {
			config.Debug("[ICEBERG] Failed to refresh managed-table lease for %s: %v", table, err)
		}
	}
	cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), managedExpirationCleanupTimeout)
	d.runExpiredManagedTableCleanup(cleanupCtx, table, time.Now())
	cancelCleanup()
	if managed {
		return
	}
	effectiveProperties := maps.Clone(tbl.Properties())
	if effectiveProperties == nil {
		effectiveProperties = iceberggo.Properties{}
	}
	for key, value := range d.cfg.TableProperties {
		if strings.HasPrefix(key, "ingestr.maintenance.") {
			effectiveProperties[key] = value
		}
	}
	opts, due, err := configuredMaintenanceOptions(effectiveProperties, time.Now())
	if err != nil {
		config.Debug("[ICEBERG] Invalid maintenance configuration for %s: %v", table, err)
		return
	}
	if !due {
		return
	}
	// The caller's context only budgets the cheap post-commit inspection;
	// whether maintenance runs is decided by the table's effective properties,
	// so the actual run gets its own budget here.
	maintainCtx, cancelMaintain := context.WithTimeout(context.WithoutCancel(ctx), maintenanceRunTimeout)
	defer cancelMaintain()
	if _, err := d.maintainLoadedTable(maintainCtx, tbl, opts); err != nil {
		config.Debug("[ICEBERG] Maintenance failed for %s after successful write: %v", table, err)
		return
	}
	if err := d.recordMaintenanceRunExpected(maintainCtx, ident, time.Now(), expectedIncarnation); err != nil {
		config.Debug("[ICEBERG] Failed to record maintenance run for %s: %v", table, err)
	}
}

func (d *Destination) runExpiredManagedTableCleanup(ctx context.Context, activeTable string, now time.Time) {
	if ctx.Err() != nil {
		return
	}
	active, err := parseIdentifier(activeTable)
	if err != nil {
		config.Debug("[ICEBERG] Skipping managed-table expiration cleanup for %s: %v", activeTable, err)
		return
	}
	namespace := icebergcatalog.NamespaceFromIdent(active)
	key := strings.Join(namespace, ".")
	claim := now.UnixNano()

	d.mu.Lock()
	if d.expirationScans == nil {
		d.expirationScans = make(map[string]int64)
	}
	lastClaim, claimed := d.expirationScans[key]
	if claimed && now.Sub(time.Unix(0, lastClaim)) < managedExpirationScanInterval {
		d.mu.Unlock()
		return
	}
	d.expirationScans[key] = claim
	d.mu.Unlock()

	result, err := d.cleanupExpiredManagedTablesExcept(ctx, namespace, active, now)
	if err != nil {
		// Keep the claim after a failure as well. Catalog outages or malformed
		// metadata must not turn every successful write into another full scan.
		config.Debug("[ICEBERG] Managed-table expiration cleanup failed in namespace %s: %v", key, err)
		return
	}
	if len(result.Purged) > 0 {
		config.Debug("[ICEBERG] Purged %d expired managed table(s) in namespace %s", len(result.Purged), key)
	}
}

func configuredMaintenanceOptions(props iceberggo.Properties, now time.Time) (MaintenanceOptions, bool, error) {
	enabled, err := boolProperty(props, maintenanceEnabledProperty, false)
	if err != nil || !enabled {
		return MaintenanceOptions{}, false, err
	}
	interval, err := durationMSProperty(props, maintenanceIntervalMSProperty, defaultMaintenanceInterval)
	if err != nil {
		return MaintenanceOptions{}, false, err
	}
	if interval < 0 {
		return MaintenanceOptions{}, false, fmt.Errorf("%s must not be negative", maintenanceIntervalMSProperty)
	}
	if raw := strings.TrimSpace(props[maintenanceLastRunAtProperty]); raw != "" {
		lastRun, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return MaintenanceOptions{}, false, fmt.Errorf("invalid %s value %q: %w", maintenanceLastRunAtProperty, raw, err)
		}
		if now.Before(lastRun.Add(interval)) {
			return MaintenanceOptions{}, false, nil
		}
	}

	opts := MaintenanceOptions{}
	if opts.ExpireSnapshots, err = boolProperty(props, maintenanceExpireSnapshotsProperty, true); err != nil {
		return opts, false, err
	}
	if opts.SnapshotMaxAge, err = durationMSProperty(props, maintenanceSnapshotMaxAgeMSProperty, defaultSnapshotMaxAge); err != nil {
		return opts, false, err
	}
	if opts.MinSnapshotsToKeep, err = intProperty(props, maintenanceMinSnapshotsProperty, defaultMinSnapshotsToKeep); err != nil {
		return opts, false, err
	}
	if opts.DeleteOrphanFiles, err = boolProperty(props, maintenanceDeleteOrphansProperty, true); err != nil {
		return opts, false, err
	}
	if opts.OrphanFileAge, err = durationMSProperty(props, maintenanceOrphanAgeMSProperty, defaultOrphanFileAge); err != nil {
		return opts, false, err
	}
	if opts.CompactDataFiles, err = boolProperty(props, maintenanceCompactDataProperty, true); err != nil {
		return opts, false, err
	}
	if opts.TargetFileSizeBytes, err = int64Property(props, maintenanceTargetFileSizeProperty, 0); err != nil {
		return opts, false, err
	}
	minInput, err := intProperty(props, maintenanceMinInputFilesProperty, int(icebergcompaction.DefaultMinInputFiles))
	if err != nil || minInput < 1 {
		return opts, false, propertyRangeError(maintenanceMinInputFilesProperty, minInput, err)
	}
	opts.MinInputFiles = uint(minInput)
	if opts.DeleteFileThreshold, err = intProperty(props, maintenanceDeleteFileThreshold, 5); err != nil {
		return opts, false, err
	}
	if opts.DeleteFileThreshold < 1 {
		return opts, false, fmt.Errorf("invalid %s value %d: must be at least 1", maintenanceDeleteFileThreshold, opts.DeleteFileThreshold)
	}
	if opts.EnableManifestMerge, err = boolProperty(props, maintenanceManifestMergeProperty, true); err != nil {
		return opts, false, err
	}
	opts.configureManifestMerge = true
	if opts.ManifestMinCount, err = intProperty(props, maintenanceManifestMinCountProperty, icebergtable.ManifestMinMergeCountDefault); err != nil {
		return opts, false, err
	}
	if opts.ManifestTargetSize, err = int64Property(props, maintenanceManifestTargetSizeProperty, icebergtable.ManifestTargetSizeBytesDefault); err != nil {
		return opts, false, err
	}
	if opts.EnableManifestMerge && (opts.ManifestMinCount < 1 || opts.ManifestTargetSize < 1) {
		return opts, false, errors.New("manifest minimum count and target size must be positive when manifest merging is enabled")
	}
	if opts.PreviousMetadataFiles, err = intProperty(props, maintenancePreviousMetadataProperty, defaultPreviousMetadataVersion); err != nil {
		return opts, false, err
	}
	if opts.PreviousMetadataFiles < 1 {
		return opts, false, fmt.Errorf("invalid %s value %d: must be at least 1", maintenancePreviousMetadataProperty, opts.PreviousMetadataFiles)
	}

	opts, err = normalizeMaintenanceOptions(opts)
	return opts, err == nil, err
}

func validateMaintenanceConfiguration(props iceberggo.Properties) error {
	_, _, err := configuredMaintenanceOptions(props, time.Now())
	if err != nil {
		return fmt.Errorf("iceberg uri: invalid maintenance configuration: %w", err)
	}
	return nil
}

func validateMaintenanceTableLocation(cfg icebergConfig) error {
	return validateOrphanCleanupTemplate(cfg.TableLocation)
}

func validateOrphanCleanupTemplate(template string) error {
	if template == "" {
		return nil
	}
	if strings.Contains(template, "{identifier}") || strings.Contains(template, "{identifier_dot}") {
		return nil
	}
	hasNamespace := strings.Contains(template, "{namespace}") || strings.Contains(template, "{namespace_dot}")
	if hasNamespace && strings.Contains(template, "{table}") {
		return nil
	}
	return errors.New("iceberg uri: table_location/table_path must include {identifier}, {identifier_dot}, or both a namespace and {table} so every table has an isolated physical root")
}

func (d *Destination) validateOrphanCleanupIsolation(ctx context.Context, target *icebergtable.Table) error {
	if err := validateOrphanCleanupTemplate(d.cfg.TableLocation); err != nil {
		return err
	}
	if err := validateIsolatedTableFilePaths(target.Properties()); err != nil {
		return fmt.Errorf("iceberg: table %s: %w", strings.Join(target.Identifier(), "."), err)
	}
	return d.validateOrphanCleanupLocation(ctx, target.Identifier(), target.Location())
}

func (d *Destination) validateOrphanCleanupLocation(ctx context.Context, targetIdent icebergtable.Identifier, location string) error {
	targetLocation, err := canonicalTableRoot(location)
	if err != nil {
		return fmt.Errorf("iceberg: failed to resolve table %s location: %w", strings.Join(targetIdent, "."), err)
	}
	if targetLocation == "" {
		return fmt.Errorf("iceberg: cannot delete orphan files for table %s with an empty table location", strings.Join(targetIdent, "."))
	}
	isolated, err := d.configuredTableLocationProvesIsolation(targetIdent, location, targetLocation)
	if err != nil {
		return err
	}
	if isolated {
		return nil
	}

	queue := []icebergtable.Identifier{nil}
	seenNamespaces := make(map[string]struct{})
	seenTables := make(map[string]struct{})
	flatNamespaces := d.cfg.Properties.Get("type", "") == "hive"
	for len(queue) > 0 {
		namespace := queue[0]
		queue = queue[1:]
		if !flatNamespaces || len(namespace) == 0 {
			children, err := d.catalog.ListNamespaces(ctx, namespace)
			if err != nil {
				return fmt.Errorf("iceberg: failed to verify orphan-cleanup namespace isolation: %w", err)
			}
			for _, child := range children {
				if len(child) > 0 && child[0] == ".ingestr" {
					continue
				}
				key := strings.Join(child, "\x00")
				if _, ok := seenNamespaces[key]; ok {
					continue
				}
				seenNamespaces[key] = struct{}{}
				queue = append(queue, child)
			}
		}

		if len(namespace) == 0 {
			continue
		}
		for ident, err := range d.catalog.ListTables(ctx, namespace) {
			if err != nil {
				return fmt.Errorf("iceberg: failed to verify orphan-cleanup table isolation: %w", err)
			}
			key := strings.Join(ident, "\x00")
			if _, ok := seenTables[key]; ok {
				continue
			}
			seenTables[key] = struct{}{}
			if slices.Equal(ident, targetIdent) {
				continue
			}
			other, err := d.catalog.LoadTable(ctx, ident)
			if err != nil {
				return fmt.Errorf("iceberg: failed to load table %s while verifying orphan-cleanup isolation: %w", strings.Join(ident, "."), err)
			}
			if err := validateIsolatedTableFilePaths(other.Properties()); err != nil {
				return fmt.Errorf("iceberg: cannot verify orphan-cleanup isolation against table %s: %w", strings.Join(ident, "."), err)
			}
			otherLocation, err := canonicalTableRoot(other.Location())
			if err != nil {
				return fmt.Errorf("iceberg: failed to resolve table %s location: %w", strings.Join(ident, "."), err)
			}
			if otherLocation == targetLocation ||
				strings.HasPrefix(otherLocation, targetLocation+"/") ||
				strings.HasPrefix(targetLocation, otherLocation+"/") {
				return fmt.Errorf(
					"iceberg: cannot delete orphan files for table %s because table %s has overlapping location %q",
					strings.Join(targetIdent, "."), strings.Join(ident, "."), other.Location(),
				)
			}
		}
	}
	return nil
}

func (d *Destination) configuredTableLocationProvesIsolation(
	targetIdent icebergtable.Identifier,
	location string,
	targetLocation string,
) (bool, error) {
	if d.cfg.TableLocation == "" {
		return false, nil
	}
	if err := validateOrphanCleanupTemplate(d.cfg.TableLocation); err != nil {
		return false, err
	}
	expectedLocation, err := canonicalTableRoot(renderTableLocation(d.cfg.TableLocation, targetIdent))
	if err != nil {
		return false, fmt.Errorf("iceberg: failed to resolve configured location for table %s: %w", strings.Join(targetIdent, "."), err)
	}
	if targetLocation != expectedLocation && !strings.HasPrefix(targetLocation, expectedLocation+"/") {
		return false, fmt.Errorf(
			"iceberg: table %s location %q is not within configured isolated location %q",
			strings.Join(targetIdent, "."), location, renderTableLocation(d.cfg.TableLocation, targetIdent),
		)
	}

	warehouse := strings.TrimSuffix(strings.TrimSpace(d.cfg.Properties.Get("warehouse", "")), "/")
	if warehouse == "" {
		return false, nil
	}
	if !locationContains(warehouse, location) {
		return false, fmt.Errorf("iceberg: table location %q must be a strict descendant of configured warehouse %q", location, warehouse)
	}
	if err := validateCanonicalLocalContainment(warehouse, location); err != nil {
		return false, err
	}
	return true, nil
}

func canonicalTableRoot(location string) (string, error) {
	if local, ok := localFilesystemPath(location); ok {
		resolved, err := resolveExistingPath(local)
		if err != nil {
			return "", err
		}
		return normalizeTableRoot(filepath.ToSlash(resolved)), nil
	}
	return normalizeTableRoot(location), nil
}

func normalizeTableRoot(location string) string {
	return strings.TrimRight(strings.TrimSpace(location), "/")
}

func (d *Destination) recordMaintenanceRunExpected(
	ctx context.Context,
	ident icebergtable.Identifier,
	now time.Time,
	expectedIncarnation string,
) error {
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		return err
	}
	if err := d.validateExpectedIncarnation(ctx, tbl, expectedIncarnation); err != nil {
		return err
	}
	txn := tbl.NewTransaction()
	if err := txn.SetProperties(iceberggo.Properties{
		maintenanceLastRunAtProperty: now.UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return err
	}
	_, err = txn.Commit(ctx)
	return err
}

func boolProperty(props iceberggo.Properties, key string, fallback bool) (bool, error) {
	raw, ok := props[key]
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s value %q: %w", key, raw, err)
	}
	return value, nil
}

func durationMSProperty(props iceberggo.Properties, key string, fallback time.Duration) (time.Duration, error) {
	value, err := int64Property(props, key, fallback.Milliseconds())
	if err != nil {
		return 0, err
	}
	const (
		maxDurationMilliseconds = int64(1<<63-1) / int64(time.Millisecond)
		minDurationMilliseconds = int64(-1<<63) / int64(time.Millisecond)
	)
	if value > maxDurationMilliseconds || value < minDurationMilliseconds {
		return 0, fmt.Errorf("invalid %s value %d: duration overflows", key, value)
	}
	return time.Duration(value) * time.Millisecond, nil
}

func intProperty(props iceberggo.Properties, key string, fallback int) (int, error) {
	value, err := int64Property(props, key, int64(fallback))
	if err != nil {
		return 0, err
	}
	if int64(int(value)) != value {
		return 0, fmt.Errorf("invalid %s value %d: outside integer range", key, value)
	}
	return int(value), nil
}

func int64Property(props iceberggo.Properties, key string, fallback int64) (int64, error) {
	raw, ok := props[key]
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value %q: %w", key, raw, err)
	}
	return value, nil
}

func propertyRangeError(key string, value int, err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("invalid %s value %d: must be at least 1", key, value)
}
