package discard

import (
	"context"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/destination"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

// DiscardDestination is a no-op destination that discards all data.
// Useful for testing source performance without write overhead.
type DiscardDestination struct{}

func NewDiscardDestination() *DiscardDestination {
	return &DiscardDestination{}
}

func (d *DiscardDestination) Schemes() []string {
	return []string{"discard"}
}

func (d *DiscardDestination) Connect(ctx context.Context, uri string) error {
	config.Debug("[DISCARD] Connected (no-op)")
	return nil
}

func (d *DiscardDestination) Close(ctx context.Context) error {
	config.Debug("[DISCARD] Closed (no-op)")
	return nil
}

func (d *DiscardDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	config.Debug("[DISCARD] PrepareTable called (no-op)")
	return nil
}

func (d *DiscardDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTotal := time.Now()
	batchNum := 0
	totalRows := int64(0)

	config.Debug("[DISCARD] Starting to consume records...")

	for result := range records {
		batchNum++

		if result.Err != nil {
			return result.Err
		}

		record := result.Batch
		if record == nil {
			continue
		}

		numRows := record.NumRows()
		if numRows == 0 {
			record.Release()
			continue
		}

		totalRows += numRows
		config.Debug("[DISCARD] Batch %d: discarded %d rows", batchNum, numRows)

		// Release the record immediately
		record.Release()
	}

	config.Debug("[DISCARD] Total: %d rows discarded in %d batches, time: %v (%.0f rows/sec)",
		totalRows, batchNum, time.Since(startTotal), float64(totalRows)/time.Since(startTotal).Seconds())

	return nil
}

func (d *DiscardDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	// For discard, parallel doesn't make sense - just use regular Write
	return d.Write(ctx, records, opts)
}

func (d *DiscardDestination) Exec(ctx context.Context, query string, args ...interface{}) error {
	config.Debug("[DISCARD] Exec called: %s (no-op)", query)
	return nil
}

func (d *DiscardDestination) GetSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	config.Debug("[DISCARD] GetSchema called (no-op)")
	return &schema.TableSchema{
		Name:    table,
		Columns: []schema.Column{},
	}, nil
}

func (d *DiscardDestination) SwapTable(ctx context.Context, stagingTable, targetTable string) error {
	config.Debug("[DISCARD] SwapTable called: %s -> %s (no-op)", stagingTable, targetTable)
	return nil
}

func (d *DiscardDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	config.Debug("[DISCARD] BeginTransaction called (no-op)")
	return &discardTransaction{}, nil
}

// discardTransaction is a no-op transaction
type discardTransaction struct{}

func (t *discardTransaction) Commit(ctx context.Context) error {
	config.Debug("[DISCARD] Transaction commit (no-op)")
	return nil
}

func (t *discardTransaction) Rollback(ctx context.Context) error {
	config.Debug("[DISCARD] Transaction rollback (no-op)")
	return nil
}

func (t *discardTransaction) Exec(ctx context.Context, query string, args ...interface{}) error {
	config.Debug("[DISCARD] Transaction exec: %s (no-op)", query)
	return nil
}

// MergeTable is a no-op for discard destination.
func (d *DiscardDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	config.Debug("[DISCARD] MergeTable called (no-op)")
	return nil
}

// DeleteInsertTable is a no-op for discard destination.
func (d *DiscardDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	config.Debug("[DISCARD] DeleteInsertTable called (no-op)")
	return nil
}

// SCD2Table is a no-op for discard destination.
func (d *DiscardDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	config.Debug("[DISCARD] SCD2Table called (no-op)")
	return nil
}

// DropTable is a no-op for discard destination.
func (d *DiscardDestination) DropTable(ctx context.Context, table string) error {
	config.Debug("[DISCARD] DropTable called (no-op)")
	return nil
}

// SupportsReplaceStrategy returns true as discard accepts all strategies.
func (d *DiscardDestination) SupportsReplaceStrategy() bool { return true }

// SupportsAppendStrategy returns true as discard accepts all strategies.
func (d *DiscardDestination) SupportsAppendStrategy() bool { return true }

// SupportsMergeStrategy returns true as discard accepts all strategies.
func (d *DiscardDestination) SupportsMergeStrategy() bool { return true }

// SupportsDeleteInsertStrategy returns true as discard accepts all strategies.
func (d *DiscardDestination) SupportsDeleteInsertStrategy() bool { return true }

// SupportsSCD2Strategy returns true as discard accepts all strategies.
func (d *DiscardDestination) SupportsSCD2Strategy() bool { return true }

// SupportsAtomicSwap returns true as discard accepts all strategies.
func (d *DiscardDestination) SupportsAtomicSwap() bool { return true }

func (d *DiscardDestination) GetScheme() string { return "discard" }

func (d *DiscardDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}
