package postgres_cdc

import (
	"context"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

const accumulatorTableOverhead = int64(1024)

var (
	changeStructBytes = int64(unsafe.Sizeof(Change{}))
	interfaceBytes    = int64(unsafe.Sizeof(any(nil)))
)

// batchAccumulator buffers decoded changes per table as plain Go values and
// materializes a single Arrow batch per table at flush time. Building Arrow
// once per flush window instead of once per WAL transaction is what keeps
// autocommit workloads (single-row transactions) cheap: no per-transaction
// builders, schema construction, or batch concatenation.
type batchAccumulator struct {
	schemas map[string]*schema.TableSchema
	changes map[string][]Change
	// minLSN tracks the lowest (oldest) transaction LSN still buffered per
	// table. Changes are added in non-decreasing LSN order, so the first group
	// buffered for a table after a flush carries that table's minimum. Used to
	// compute a safe commit position for streaming mode.
	minLSN     map[string]pglogrepl.LSN
	bytes      map[string]int64
	totalBytes int64
	threshold  int
	byteLimit  int64
}

func newBatchAccumulator(threshold int, schemas map[string]*schema.TableSchema) *batchAccumulator {
	return &batchAccumulator{
		schemas:   schemas,
		changes:   make(map[string][]Change),
		minLSN:    make(map[string]pglogrepl.LSN),
		bytes:     make(map[string]int64),
		threshold: threshold,
		byteLimit: defaultDecoderMemoryBytes,
	}
}

func estimateChangeBytes(change Change) int64 {
	// The Change struct itself is charged from the accumulator slice's capacity
	// in add. This function accounts for memory retained through its references.
	size := int64(len(change.Operation))
	for _, values := range [][]interface{}{change.Values, change.OldValues} {
		size += int64(cap(values)) * interfaceBytes
		for _, value := range values {
			size += estimateValueBytes(reflect.ValueOf(value))
		}
	}
	return size
}

func estimateValueBytes(value reflect.Value) int64 {
	if !value.IsValid() {
		return 1
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 1
		}
		return int64(value.Type().Size()) + estimateValueBytes(value.Elem())
	}
	switch value.Kind() {
	case reflect.String:
		return int64(value.Type().Size()) + int64(value.Len())
	case reflect.Slice:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return int64(value.Type().Size()) + int64(value.Cap())
		}
		size := int64(value.Type().Size()) + int64(value.Cap())*int64(value.Type().Elem().Size())
		for i := 0; i < value.Len(); i++ {
			size += estimateValueBytes(value.Index(i))
		}
		return size
	case reflect.Array:
		size := int64(value.Type().Size())
		for i := 0; i < value.Len(); i++ {
			if value.Index(i).Kind() == reflect.String || value.Index(i).Kind() == reflect.Slice || value.Index(i).Kind() == reflect.Map || value.Index(i).Kind() == reflect.Pointer || value.Index(i).Kind() == reflect.Interface {
				size += estimateValueBytes(value.Index(i))
			}
		}
		return size
	case reflect.Map:
		// Map bucket layout is runtime-specific. This deliberately overcharges a
		// header plus two words of bucket/overflow metadata per entry, in addition
		// to the key/value storage and anything they reference.
		size := int64(value.Type().Size()) + 64 + int64(value.Len())*32
		iter := value.MapRange()
		for iter.Next() {
			size += estimateValueBytes(iter.Key()) + estimateValueBytes(iter.Value())
		}
		return size
	default:
		return int64(value.Type().Size())
	}
}

func (a *batchAccumulator) add(tableName string, changes []Change, lsn pglogrepl.LSN) {
	if len(changes) == 0 {
		return
	}
	existing, tableExists := a.changes[tableName]
	previousBytes := a.bytes[tableName]
	if _, ok := a.minLSN[tableName]; !ok {
		a.minLSN[tableName] = lsn
	}
	previousCapacity := cap(existing)
	buffered := append(existing, changes...)
	a.changes[tableName] = buffered
	if !tableExists {
		// changes, minLSN, and bytes each retain a map entry and table-name
		// header. A fixed conservative charge covers their buckets and keys.
		a.bytes[tableName] += accumulatorTableOverhead + int64(len(tableName))*3
	}
	if capacityGrowth := cap(buffered) - previousCapacity; capacityGrowth > 0 {
		a.bytes[tableName] += int64(capacityGrowth) * changeStructBytes
	}
	for i := range changes {
		size := estimateChangeBytes(changes[i])
		a.bytes[tableName] += size
	}
	a.totalBytes += a.bytes[tableName] - previousBytes
}

// minPendingLSN returns the lowest transaction LSN still buffered across all
// tables, or (0, false) when the accumulator is empty.
func (a *batchAccumulator) minPendingLSN() (pglogrepl.LSN, bool) {
	var min pglogrepl.LSN
	found := false
	for _, lsn := range a.minLSN {
		if !found || lsn < min {
			min = lsn
			found = true
		}
	}
	return min, found
}

func (a *batchAccumulator) discard() {
	a.changes = make(map[string][]Change)
	a.minLSN = make(map[string]pglogrepl.LSN)
	a.bytes = make(map[string]int64)
	a.totalBytes = 0
}

// tokenFunc computes the CommitToken to attach to a flushed batch. It is
// evaluated after the flushed table's changes have been removed from the
// accumulator, so it reflects only data that remains un-emitted.
type tokenFunc func() any

// flushReady sends merged batches for tables that have accumulated enough rows.
func (a *batchAccumulator) flushReady(results chan<- source.RecordBatchResult, token tokenFunc) error {
	return a.flushReadyContext(context.Background(), results, token)
}

func (a *batchAccumulator) flushReadyContext(ctx context.Context, results chan<- source.RecordBatchResult, token tokenFunc) error {
	for tableName, changes := range a.changes {
		if len(changes) >= a.threshold || lastTruncateIndex(changes) >= 0 {
			if err := a.flushTableContext(ctx, tableName, results, token); err != nil {
				return err
			}
		}
	}
	for a.byteLimit > 0 && a.totalBytes >= a.byteLimit {
		tableName, ok := a.largestTable()
		if !ok {
			break
		}
		if err := a.flushTableContext(ctx, tableName, results, token); err != nil {
			return err
		}
	}
	return nil
}

func (a *batchAccumulator) largestTable() (string, bool) {
	var largest string
	var largestBytes int64
	found := false
	for tableName, size := range a.bytes {
		if !found || size > largestBytes {
			largest = tableName
			largestBytes = size
			found = true
		}
	}
	return largest, found
}

// flushAll sends merged batches for all tables, regardless of row count.
func (a *batchAccumulator) flushAll(results chan<- source.RecordBatchResult, token tokenFunc) error {
	return a.flushAllContext(context.Background(), results, token)
}

func (a *batchAccumulator) flushAllContext(ctx context.Context, results chan<- source.RecordBatchResult, token tokenFunc) error {
	for tableName := range a.changes {
		if err := a.flushTableContext(ctx, tableName, results, token); err != nil {
			return err
		}
	}
	return nil
}

func (a *batchAccumulator) flushTableContext(ctx context.Context, tableName string, results chan<- source.RecordBatchResult, token tokenFunc) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		a.discard()
		return err
	}
	changes := a.changes[tableName]
	if len(changes) == 0 {
		return nil
	}

	delete(a.changes, tableName)
	delete(a.minLSN, tableName)
	a.totalBytes -= a.bytes[tableName]
	if a.totalBytes < 0 {
		a.totalBytes = 0
	}
	delete(a.bytes, tableName)

	truncateIndex := lastTruncateIndex(changes)
	if truncateIndex >= 0 {
		changes = changes[truncateIndex+1:]
	}

	tableSchema := a.schemas[tableName]
	if tableSchema == nil {
		return fmt.Errorf("no schema registered for table %q", tableName)
	}

	// Resolve unchanged-TOAST markers across the whole flush window: a row
	// carrying a column's full value fills a later partial UPDATE that omitted
	// it, including across transaction boundaries. The destination merge only
	// looks at the latest change per primary key, so without this fill an
	// INSERT + partial UPDATE landing in the same window would lose the
	// omitted column (the merge falls back to a target row that doesn't exist
	// yet).
	if len(changes) > 0 {
		applyIntraBatchFill(changes, tableSchema)
		changes = expandUpdates(changes, tableSchema)
	}

	// Last-write-wins compaction: only the latest change per primary key can
	// affect the merge outcome, so superseded row versions are dropped before
	// materialization.
	buffered := len(changes)
	if len(changes) > 0 {
		changes = compactChanges(changes, tableSchema)
	}

	var commitToken any
	if token != nil {
		commitToken = token()
	}

	if truncateIndex >= 0 {
		truncateToken := any(nil)
		if len(changes) == 0 {
			truncateToken = commitToken
		}
		if err := sendResult(ctx, results, source.RecordBatchResult{TableName: tableName, Truncate: true, CDCWALTruncate: true, CommitToken: truncateToken}); err != nil {
			a.discard()
			return err
		}
	}
	if len(changes) == 0 {
		return nil
	}

	batch, err := changesToBatch(changes, tableSchema)
	if err != nil {
		return fmt.Errorf("failed to materialize batch for table %s: %w", tableName, err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		batch.Release()
		a.discard()
		return err
	}

	config.Debug("[CDC] Flushed %d buffered changes (%d rows after compaction) for table %s", buffered, len(changes), tableName)

	if err := sendResult(ctx, results, source.RecordBatchResult{
		Batch:       batch,
		TableName:   tableName,
		CommitToken: commitToken,
	}); err != nil {
		a.discard()
		return err
	}
	return nil
}

func sendResult(ctx context.Context, results chan<- source.RecordBatchResult, result source.RecordBatchResult) error {
	if err := ctx.Err(); err != nil {
		if result.Batch != nil {
			result.Batch.Release()
		}
		return err
	}
	guard := source.ConnectorLeaseGuardFromContext(ctx)
	var leaseDone <-chan struct{}
	if guard != nil {
		leaseDone = guard.Done()
	}
	select {
	case results <- result:
		return nil
	case <-ctx.Done():
		if result.Batch != nil {
			result.Batch.Release()
		}
		return ctx.Err()
	case <-leaseDone:
		if result.Batch != nil {
			result.Batch.Release()
		}
		return guard.Err()
	}
}

func lastTruncateIndex(changes []Change) int {
	for i := len(changes) - 1; i >= 0; i-- {
		if changes[i].Operation == "TRUNCATE" {
			return i
		}
	}
	return -1
}
