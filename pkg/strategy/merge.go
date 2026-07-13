package strategy

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/multitable"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type MergeStrategy struct{}

// mergeTableParams describes one dest/staging table pair for a merge.
type mergeTableParams struct {
	DestTable    string
	StagingTable string
	Schema       *schema.TableSchema
	PrimaryKeys  []string
	PartitionBy  string
	ClusterBy    []string
	IsCDC        bool
}

// prepareMergeTables ensures the destination table exists (without dropping it)
// and creates a fresh staging table for it.
func prepareMergeTables(ctx context.Context, dest destination.Destination, p mergeTableParams) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       p.DestTable,
		Schema:      destination.DestinationTableSchema(p.Schema),
		DropFirst:   false,
		PrimaryKeys: p.PrimaryKeys,
		PartitionBy: p.PartitionBy,
		ClusterBy:   p.ClusterBy,
	}); err != nil {
		return fmt.Errorf("failed to prepare destination table %s: %w", p.DestTable, err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:        p.StagingTable,
		Schema:       p.Schema,
		DropFirst:    true,
		PrimaryKeys:  nil,
		CDCMode:      p.IsCDC, // Allow NULLs for CDC deletes in staging
		PartitionBy:  p.PartitionBy,
		ClusterBy:    p.ClusterBy,
		ExpiresAfter: destination.ManagedStagingTTL,
	}); err != nil {
		return fmt.Errorf("failed to prepare staging table %s: %w", p.StagingTable, err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	return nil
}

// mergeStagingInto merges the staging table into the target table by primary
// key. A non-empty incrementalKey makes the per-PK dedup keep the row with the
// highest value of that key (latest wins) instead of an arbitrary one.
func mergeStagingInto(ctx context.Context, dest destination.Destination, stagingTable, targetTable string, primaryKeys []string, tableSchema *schema.TableSchema, incrementalKey string) error {
	return dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable:   stagingTable,
		TargetTable:    targetTable,
		PrimaryKeys:    primaryKeys,
		Columns:        destination.MergeColumnsFor(dest, tableSchema.ColumnNames()),
		IncrementalKey: mergeIncrementalKeyForSchema(tableSchema, incrementalKey),
		Schema:         tableSchema,
	})
}

func mergeIncrementalKeyForSchema(tableSchema *schema.TableSchema, incrementalKey string) string {
	if incrementalKey == "" || tableSchema == nil {
		return ""
	}
	for _, col := range tableSchema.Columns {
		if col.Name == incrementalKey {
			return col.Name
		}
	}
	for _, col := range tableSchema.Columns {
		if strings.EqualFold(col.Name, incrementalKey) {
			return col.Name
		}
	}
	return ""
}

// isAppendOnlyCDCTable reports whether a CDC table must be ingested as an
// append-only change log: it has no usable row identity (no primary key and no
// replica identity index on the source), so a merge has nothing to match on.
// The source emits its updates as delete+insert pairs (see postgres_cdc
// expandUpdates), making the landed log a self-contained retract stream the
// user applies downstream.
func isAppendOnlyCDCTable(ti source.SourceTableInfo) bool {
	return len(ti.PrimaryKeys) == 0 && hasCDCColumns(ti.Schema)
}

// prepareAppendOnlyCDCTable creates the destination table a keyless CDC table's
// change log lands in directly (no staging, no merge). The full schema is kept
// — including the otherwise staging-only _cdc_unchanged_cols — because raw
// change batches carry it, and CDCMode relaxes NOT NULL since rows are change
// events rather than complete entities.
func prepareAppendOnlyCDCTable(ctx context.Context, dest destination.Destination, table string, tableSchema *schema.TableSchema) error {
	if err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     table,
		Schema:    tableSchema,
		DropFirst: false,
		CDCMode:   true,
	}); err != nil {
		return fmt.Errorf("failed to prepare append-only change-log table %s: %w", table, err)
	}
	return nil
}

// warnIfCDCMergeUnsupported prints a warning when CDC data is headed at a
// destination that can't process deletes during merge.
func warnIfCDCMergeUnsupported(dest destination.Destination) {
	if cdcAware, ok := dest.(destination.CDCMergeAware); !ok || !cdcAware.SupportsCDCMerge() {
		output.Warnf("Warning: CDC data detected but the destination does not support CDC-aware merge; deleted rows will be inserted as regular data with _cdc_deleted=true instead of being processed as deletes\n")
	}
}

func supportsCDCSnapshotReplace(dest destination.Destination) bool {
	_, ok := dest.(destination.TruncateCapable)
	return ok
}

func (s *MergeStrategy) Name() config.IncrementalStrategy {
	return config.StrategyMerge
}

func (s *MergeStrategy) Validate(cfg *config.IngestConfig) error {
	if len(cfg.PrimaryKeys) == 0 {
		return fmt.Errorf("merge strategy requires at least one primary_key")
	}
	return nil
}

func (s *MergeStrategy) RequiresPrimaryKey() bool {
	return true
}

func (s *MergeStrategy) RequiresIncrementalKey() bool {
	return false
}

func (s *MergeStrategy) Execute(ctx context.Context, job *IngestionJob) error {
	// Generate staging table name
	stagingTable := managedStagingTableName(job.Destination, job.Config.DestTable, "merge", job.Config.StagingDataset)
	output.Statusf("[MERGE] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), stagingTable)
	isCDC := hasCDCColumns(job.Schema)
	if isCDC {
		warnIfCDCMergeUnsupported(job.Destination)
	}

	if err := prepareMergeTables(ctx, job.Destination, mergeTableParams{
		DestTable:    job.Config.DestTable,
		StagingTable: stagingTable,
		Schema:       job.Schema,
		PrimaryKeys:  job.Config.PrimaryKeys,
		PartitionBy:  job.Config.PartitionBy,
		ClusterBy:    job.Config.ClusterBy,
		IsCDC:        isCDC,
	}); err != nil {
		return err
	}
	if job.CDCStateManager != nil {
		if err := job.CDCStateManager.BindDestinationIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable); err != nil {
			return fmt.Errorf("failed to bind CDC destination before merge: %w", err)
		}
	}

	// Read from source
	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	readOpts := source.ReadOptions{
		IncrementalKey:                  job.Config.IncrementalKey,
		IntervalStart:                   job.Config.IntervalStart,
		IntervalEnd:                     job.Config.IntervalEnd,
		ExtractPartitionBy:              job.Config.ExtractPartitionBy,
		ExtractPartitionInterval:        job.Config.ExtractPartitionInterval,
		ExtractPartitionNumericInterval: job.Config.ExtractPartitionNumericInterval,
		ExtractPartitionAuto:            job.Config.ExtractPartitionAuto,
		PageSize:                        job.Config.PageSize,
		Limit:                           job.Config.SQLLimit,
		ExcludeColumns:                  job.Config.SQLExcludeColumns,
		Parallelism:                     parallelism,
		Schema:                          job.SourceSchema,
		CDCResumeLSN:                    job.Config.CDCResumeLSN, // For CDC incremental resume
		CDCResumeIncarnation:            job.Config.CDCResumeIncarnation,
		CDCResumeSchemaFingerprint:      job.Config.CDCResumeSchemaFingerprint,
		CDCSlotSuffix:                   job.Config.CDCSlotSuffix, // Destination-aware slot suffix
		CDCLegacySlotSuffix:             job.Config.CDCLegacySlotSuffix,
		CDCSnapshotReplace:              isCDC && supportsCDCSnapshotReplace(job.Destination),
		FullRefresh:                     job.Config.FullRefresh,
	}

	records, err := job.GetRecords(ctx, readOpts)
	if err != nil {
		return fmt.Errorf("failed to get records: %w", err)
	}

	// Wrap channel with progress tracker if provided
	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	// Write to staging table using parallel writes. Source TRUNCATE controls
	// split the input into ordered segments and clear earlier staged changes.
	sourceTruncated, err := destination.WriteWithTruncateBoundaries(ctx, job.Destination, records, destination.WriteOptions{
		Table:            stagingTable,
		Schema:           job.Schema,
		Parallelism:      parallelism,
		StagingTable:     true,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
		PreStaged:        job.PreStaged,
	})
	if err != nil {
		return fmt.Errorf("failed to write to staging: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := job.ApplyEvolution(ctx); err != nil {
		return fmt.Errorf("failed to apply schema evolution: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	// Perform merge: UPDATE existing + INSERT new
	// Note: We only use source columns here. Destination-only columns (removed columns)
	// will naturally receive NULL for new rows and remain unchanged for existing rows.
	config.Debug("[MERGE] Executing merge operation")
	if isCDC && sourceTruncated {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := destination.ApplyCDCTruncate(ctx, job.Destination, job.Config.DestTable); err != nil {
			return err
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := mergeStagingInto(ctx, job.Destination, stagingTable, job.Config.DestTable, job.Config.PrimaryKeys, job.Schema, job.Config.IncrementalKey); err != nil {
		return fmt.Errorf("failed to merge data: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	// Drop staging table (skip when KeepStaging is set for test inspection).
	if !job.Config.KeepStaging {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := job.Destination.DropTable(ctx, stagingTable); err != nil {
			config.Debug("[MERGE] Warning: failed to drop staging table: %v", err)
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
	}

	return nil
}

// ExecuteMultiTable implements multi-table merge strategy for CDC sources.
func (s *MergeStrategy) ExecuteMultiTable(ctx context.Context, job *MultiTableIngestionJob) error {
	if len(job.Tables) == 0 {
		return nil
	}

	config.Debug("[STRATEGY] Multi-table merge with %d tables", len(job.Tables))

	anyTableHasCDC := false
	for _, tableInfo := range job.Tables {
		if hasCDCColumns(tableInfo.Schema) {
			anyTableHasCDC = true
			break
		}
	}
	if anyTableHasCDC {
		warnIfCDCMergeUnsupported(job.Destination)
	}

	stagingTables := make(map[string]string)
	tableConfigs := make(map[string]destination.TableWriteConfig)
	var mu sync.Mutex

	var wg sync.WaitGroup
	errChan := make(chan error, len(job.Tables)*2)

	for _, tableInfo := range job.Tables {
		wg.Add(1)
		go func(ti source.SourceTableInfo) {
			defer wg.Done()

			destTable := job.GetDestTableName(ti.Name)

			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				errChan <- err
				return
			}
			if err := job.ApplyEvolutionFor(ctx, ti.Name); err != nil {
				errChan <- fmt.Errorf("failed to evolve destination table %s: %w", ti.Name, err)
				return
			}
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				errChan <- err
				return
			}

			if isAppendOnlyCDCTable(ti) {
				if err := source.ConnectorLeaseLoss(ctx); err != nil {
					errChan <- err
					return
				}
				if err := prepareAppendOnlyCDCTable(ctx, job.Destination, destTable, ti.Schema); err != nil {
					errChan <- err
					return
				}
				if job.CDCStateManager != nil {
					if err := job.CDCStateManager.BindDestinationIncarnation(ctx, ti.Name, destTable); err != nil {
						errChan <- fmt.Errorf("failed to bind CDC destination table %s: %w", ti.Name, err)
						return
					}
				}
				if err := source.ConnectorLeaseLoss(ctx); err != nil {
					errChan <- err
					return
				}
				mu.Lock()
				tableConfigs[ti.Name] = destination.TableWriteConfig{
					DestTable: destTable,
					Schema:    ti.Schema,
				}
				mu.Unlock()
				return
			}

			stagingTable := managedStagingTableName(job.Destination, destTable, "merge", job.Config.StagingDataset)

			if err := prepareMergeTables(ctx, job.Destination, mergeTableParams{
				DestTable:    destTable,
				StagingTable: stagingTable,
				Schema:       ti.Schema,
				PrimaryKeys:  ti.PrimaryKeys,
				IsCDC:        hasCDCColumns(ti.Schema), // Make non-PK columns nullable for CDC staging tables
			}); err != nil {
				errChan <- err
				return
			}
			if job.CDCStateManager != nil {
				if err := job.CDCStateManager.BindDestinationIncarnation(ctx, ti.Name, destTable); err != nil {
					errChan <- fmt.Errorf("failed to bind CDC destination table %s: %w", ti.Name, err)
					return
				}
			}

			mu.Lock()
			stagingTables[ti.Name] = stagingTable
			tableConfigs[ti.Name] = destination.TableWriteConfig{
				DestTable:   stagingTable,
				Schema:      ti.Schema,
				PrimaryKeys: nil,
			}
			mu.Unlock()
		}(tableInfo)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		return err
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	resumeIncarnations, resumeSchemas := cdcResumeMetadata(job.Tables)
	records, err := job.ReadAll(readCtx, source.MultiTableReadOptions{
		ReadOptions: source.ReadOptions{
			Parallelism:             parallelism,
			PageSize:                job.Config.PageSize,
			Limit:                   job.Config.SQLLimit,
			CDCSlotSuffix:           job.Config.CDCSlotSuffix,
			CDCLegacySlotSuffix:     job.Config.CDCLegacySlotSuffix,
			CDCSnapshotReplace:      anyTableHasCDC && supportsCDCSnapshotReplace(job.Destination),
			FullRefresh:             job.Config.FullRefresh,
		},
		CDCResumeLSNs:               job.CDCResumeLSNs,
		CDCResumeIncarnations:       resumeIncarnations,
		CDCResumeSchemaFingerprints: resumeSchemas,
	})
	if err != nil {
		if source.ConnectorLeaseLoss(ctx) == nil {
			for _, stagingTable := range stagingTables {
				if source.ConnectorLeaseLoss(ctx) != nil {
					break
				}
				_ = job.Destination.DropTable(ctx, stagingTable)
			}
		}
		return fmt.Errorf("failed to read from multi-table source: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	writeResult, err := multitable.WriteWithResult(ctx, job.Destination, records, destination.MultiTableWriteOptions{
		TableConfigs:     tableConfigs,
		Parallelism:      parallelism,
		StagingTable:     true,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
		CancelSource:     cancelRead,
	})
	if err != nil {
		if source.ConnectorLeaseLoss(ctx) == nil {
			for _, stagingTable := range stagingTables {
				if source.ConnectorLeaseLoss(ctx) != nil {
					break
				}
				_ = job.Destination.DropTable(ctx, stagingTable)
			}
		}
		return fmt.Errorf("failed to write multi-table data: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	mergeErrChan := make(chan error, len(job.Tables))
	var mergeWg sync.WaitGroup

	for _, tableInfo := range job.Tables {
		mergeWg.Add(1)
		go func(ti source.SourceTableInfo) {
			defer mergeWg.Done()

			destTable := job.GetDestTableName(ti.Name)
			stagingTable, ok := stagingTables[ti.Name]
			if !ok {
				// Append-only change-log table: rows were written directly.
				return
			}
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				mergeErrChan <- err
				return
			}
			if hasCDCColumns(ti.Schema) && writeResult.TruncatedTables[ti.Name] {
				if err := source.ConnectorLeaseLoss(ctx); err != nil {
					mergeErrChan <- err
					return
				}
				if err := destination.ApplyCDCTruncate(ctx, job.Destination, destTable); err != nil {
					mergeErrChan <- fmt.Errorf("failed to reset CDC target %s: %w", ti.Name, err)
					return
				}
				if err := source.ConnectorLeaseLoss(ctx); err != nil {
					mergeErrChan <- err
					return
				}
			}

			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				mergeErrChan <- err
				return
			}
			if err := mergeStagingInto(ctx, job.Destination, stagingTable, destTable, ti.PrimaryKeys, ti.Schema, ""); err != nil {
				mergeErrChan <- fmt.Errorf("failed to merge table %s: %w", ti.Name, err)
				return
			}
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				mergeErrChan <- err
				return
			}

			if !job.Config.KeepStaging {
				if err := source.ConnectorLeaseLoss(ctx); err != nil {
					mergeErrChan <- err
					return
				}
				if err := job.Destination.DropTable(ctx, stagingTable); err != nil {
					config.Debug("[MERGE] Warning: failed to drop staging table %s: %v", stagingTable, err)
				}
				if err := source.ConnectorLeaseLoss(ctx); err != nil {
					mergeErrChan <- err
				}
			}
		}(tableInfo)
	}

	mergeWg.Wait()
	close(mergeErrChan)

	var mergeErr error
	for err := range mergeErrChan {
		if mergeErr == nil {
			mergeErr = err
		}
	}

	if mergeErr != nil {
		if source.ConnectorLeaseLoss(ctx) == nil {
			for _, stagingTable := range stagingTables {
				if source.ConnectorLeaseLoss(ctx) != nil {
					break
				}
				if err := job.Destination.DropTable(ctx, stagingTable); err != nil {
					config.Debug("[MERGE] Warning: failed to drop staging table %s: %v", stagingTable, err)
				}
			}
		}
		return mergeErr
	}

	return nil
}

// hasCDCColumns checks if a schema has CDC columns (specifically _cdc_deleted).
// This is used to detect CDC sources for special merge handling.
func hasCDCColumns(s *schema.TableSchema) bool {
	if s == nil {
		return false
	}
	return destination.HasCDCDeletedColumn(s.ColumnNames())
}
