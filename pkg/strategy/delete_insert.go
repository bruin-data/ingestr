package strategy

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type DeleteInsertStrategy struct{}

func (s *DeleteInsertStrategy) Name() config.IncrementalStrategy {
	return config.StrategyDeleteInsert
}

func (s *DeleteInsertStrategy) Validate(cfg *config.IngestConfig) error {
	if cfg.IncrementalKey == "" {
		return fmt.Errorf("delete+insert strategy requires an incremental_key")
	}
	return nil
}

func (s *DeleteInsertStrategy) RequiresPrimaryKey() bool {
	return false
}

func (s *DeleteInsertStrategy) RequiresIncrementalKey() bool {
	return true
}

func (s *DeleteInsertStrategy) Execute(ctx context.Context, job *IngestionJob) error {
	if !job.Destination.SupportsDeleteInsertStrategy() {
		return fmt.Errorf("destination %s does not support delete+insert strategy", job.Destination.GetScheme())
	}

	stagingTable := managedStagingTableName(job.Destination, job.Config.DestTable, "di", job.Config.StagingDataset)
	fmt.Printf("[DELETE+INSERT] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), stagingTable)

	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:       job.Config.DestTable,
		Schema:      destination.DestinationTableSchema(job.Schema),
		DropFirst:   false,
		PrimaryKeys: job.Config.PrimaryKeys,
		PartitionBy: job.Config.PartitionBy,
		ClusterBy:   job.Config.ClusterBy,
	}); err != nil {
		return fmt.Errorf("failed to prepare destination table: %w", err)
	}

	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:        stagingTable,
		Schema:       job.Schema,
		DropFirst:    true,
		PrimaryKeys:  nil,
		PartitionBy:  job.Config.PartitionBy,
		ClusterBy:    job.Config.ClusterBy,
		ExpiresAfter: destination.ManagedStagingTTL,
	}); err != nil {
		return fmt.Errorf("failed to prepare staging table: %w", err)
	}

	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	incrementalKey, incrementalKeyType := resolveSchemaColumn(job.Schema, job.Config.IncrementalKey)

	readOpts := source.ReadOptions{
		IncrementalKey: incrementalKey,
		IntervalStart:  job.Config.IntervalStart,
		IntervalEnd:    job.Config.IntervalEnd,
		PageSize:       job.Config.PageSize,
		Limit:          job.Config.SQLLimit,
		ExcludeColumns: job.Config.SQLExcludeColumns,
		Parallelism:    parallelism,
		Schema:         job.SourceSchema,
		FullRefresh:    job.Config.FullRefresh,
	}

	records, err := job.GetRecords(ctx, readOpts)
	if err != nil {
		return fmt.Errorf("failed to get records: %w", err)
	}

	intervalTracker := NewIntervalTracker(incrementalKey)
	records = intervalTracker.Wrap(records)

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	if err := job.Destination.WriteParallel(ctx, records, destination.WriteOptions{
		Table:            stagingTable,
		Schema:           job.Schema,
		Parallelism:      parallelism,
		StagingTable:     true,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
	}); err != nil {
		return fmt.Errorf("failed to write to staging: %w", err)
	}

	intervalStart := resolveIntervalBound(job.Config.IntervalStart, intervalTracker.Min)
	intervalEnd := resolveIntervalBound(job.Config.IntervalEnd, intervalTracker.Max)

	if intervalStart == nil || intervalEnd == nil {
		config.Debug("[DELETE+INSERT] No interval detected (empty data?), skipping delete+insert")
		if !job.Config.KeepStaging {
			if err := job.Destination.DropTable(ctx, stagingTable); err != nil {
				config.Debug("[DELETE+INSERT] Warning: failed to drop staging table: %v", err)
			}
		}
		return nil
	}

	config.Debug("[DELETE+INSERT] Interval: %v to %v", intervalStart, intervalEnd)
	config.Debug("[DELETE+INSERT] Executing delete+insert operation")

	// When the incremental key is DATE, convert timestamp interval bounds to date-only.
	// This prevents type mismatches (e.g., TIMESTAMP vs DATE) in the DELETE query.
	if incrementalKeyType == schema.TypeDate {
		intervalStart = toDateOnly(intervalStart)
		intervalEnd = toDateOnly(intervalEnd)
	}

	if err := job.ApplyEvolution(ctx); err != nil {
		return fmt.Errorf("failed to apply schema evolution: %w", err)
	}

	if err := job.Destination.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
		StagingTable:       stagingTable,
		TargetTable:        job.Config.DestTable,
		IncrementalKey:     incrementalKey,
		IncrementalKeyType: incrementalKeyType,
		IntervalStart:      intervalStart,
		IntervalEnd:        intervalEnd,
		Columns:            job.Schema.ColumnNames(),
		PrimaryKeys:        job.Config.PrimaryKeys,
	}); err != nil {
		return fmt.Errorf("failed to delete+insert data: %w", err)
	}

	if !job.Config.KeepStaging {
		if err := job.Destination.DropTable(ctx, stagingTable); err != nil {
			config.Debug("[DELETE+INSERT] Warning: failed to drop staging table: %v", err)
		}
	}

	return nil
}

func resolveSchemaColumn(tableSchema *schema.TableSchema, columnName string) (string, schema.DataType) {
	if tableSchema == nil {
		return columnName, schema.TypeUnknown
	}
	for _, col := range tableSchema.Columns {
		if col.Name == columnName {
			return col.Name, col.DataType
		}
	}
	for _, col := range tableSchema.Columns {
		if strings.EqualFold(col.Name, columnName) {
			return col.Name, col.DataType
		}
	}
	return columnName, schema.TypeUnknown
}

func resolveIntervalBound(userProvided interface{}, autoDetected interface{}) interface{} {
	if !isNilInterface(userProvided) {
		return userProvided
	}
	return autoDetected
}

func isNilInterface(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// toDateOnly converts a time.Time interval bound to a date-only string (YYYY-MM-DD)
// so that DATE columns can be compared correctly without timestamp precision issues.
func toDateOnly(v interface{}) interface{} {
	switch val := v.(type) {
	case time.Time:
		return val.Format("2006-01-02")
	case *time.Time:
		if val == nil {
			return nil
		}
		return val.Format("2006-01-02")
	default:
		return v
	}
}
