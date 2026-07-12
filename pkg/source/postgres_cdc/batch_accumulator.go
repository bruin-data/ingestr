package postgres_cdc

import (
	"context"
	"fmt"
	"reflect"
	"time"
	"unsafe"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

const accumulatorTableOverhead = int64(1024)

// These limits are part of the pgcdc-keyless-v1 durable ID contract. The
// namespace predates keyed append support and is retained for replay
// compatibility. They must not track decoder tuning constants; changing either
// requires a new identity version.
const (
	keylessDataBatchWindowChanges uint64 = 1024
	keylessDataBatchWindowBytes   int64  = 8 << 20
)

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
	// blockedKeyless contains tables whose newest transaction sequence window
	// has not closed yet. Its historical name is retained because the identity
	// contract originated with keyless CDC.
	blockedKeyless map[string]bool
	stableSpills   map[string]*stableWindowSpill
	stableAll      bool
}

type stableWindowSpill struct {
	lsn    pglogrepl.LSN
	window uint64
	spool  *changeSpool[Change]
}

func newBatchAccumulator(threshold int, schemas map[string]*schema.TableSchema) *batchAccumulator {
	return &batchAccumulator{
		schemas:        schemas,
		changes:        make(map[string][]Change),
		minLSN:         make(map[string]pglogrepl.LSN),
		bytes:          make(map[string]int64),
		threshold:      threshold,
		byteLimit:      defaultDecoderMemoryBytes,
		blockedKeyless: make(map[string]bool),
		stableSpills:   make(map[string]*stableWindowSpill),
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
	for _, spill := range a.stableSpills {
		_ = spill.spool.Close()
	}
	a.changes = make(map[string][]Change)
	a.minLSN = make(map[string]pglogrepl.LSN)
	a.bytes = make(map[string]int64)
	a.totalBytes = 0
	a.blockedKeyless = make(map[string]bool)
	a.stableSpills = make(map[string]*stableWindowSpill)
}

// tokenFunc computes the CommitToken to attach to a flushed batch. It is
// evaluated after the flushed table's changes have been removed from the
// accumulator, so it reflects only data that remains un-emitted.
type tokenFunc func() any

type keylessBatchWindowState struct {
	window  uint64
	changes uint64
	bytes   int64
}

func (s *keylessBatchWindowState) assign(change *Change) {
	size := stableChangeBytes(*change)
	if s.changes > 0 && (s.changes >= keylessDataBatchWindowChanges || s.bytes+size > keylessDataBatchWindowBytes) {
		s.window++
		s.changes = 0
		s.bytes = 0
	}
	change.DataBatchWindow = s.window
	s.changes++
	s.bytes += size
}

func stableChangeBytes(change Change) int64 {
	size := int64(32 + len(change.Operation))
	for _, values := range [][]interface{}{change.Values, change.OldValues} {
		size += 8
		for _, value := range values {
			size += stableValueBytes(reflect.ValueOf(value))
		}
	}
	return size
}

func stableValueBytes(value reflect.Value) int64 {
	if !value.IsValid() {
		return 1
	}
	if value.Type() == reflect.TypeOf(time.Time{}) {
		return 16
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 1
		}
		return 1 + stableValueBytes(value.Elem())
	}
	switch value.Kind() {
	case reflect.Bool:
		return 1
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return int64(value.Type().Bits() / 8)
	case reflect.String:
		return 8 + int64(value.Len())
	case reflect.Slice, reflect.Array:
		size := int64(8)
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return size + int64(value.Len())
		}
		for i := 0; i < value.Len(); i++ {
			size += stableValueBytes(value.Index(i))
		}
		return size
	case reflect.Map:
		size := int64(8)
		iter := value.MapRange()
		for iter.Next() {
			size += stableValueBytes(iter.Key()) + stableValueBytes(iter.Value())
		}
		return size
	case reflect.Struct:
		size := int64(8)
		for i := 0; i < value.NumField(); i++ {
			size += stableValueBytes(value.Field(i))
		}
		return size
	default:
		return 8
	}
}

func transactionDataBatchID(tableName string, changes []Change) source.DurableID {
	first := changes[0]
	return source.DurableID(fmt.Sprintf(
		"pgcdc-keyless-v1:%d:%s:%s:%d",
		len(tableName),
		tableName,
		FormatLSN(first.LSN),
		first.DataBatchWindow,
	))
}

// flushReady sends merged batches for tables that have accumulated enough rows.
func (a *batchAccumulator) flushReady(results chan<- source.RecordBatchResult, token tokenFunc) error {
	return a.flushReadyContext(context.Background(), results, token)
}

func (a *batchAccumulator) flushReadyContext(ctx context.Context, results chan<- source.RecordBatchResult, token tokenFunc) error {
	for tableName, changes := range a.changes {
		if a.blockedKeyless[tableName] {
			continue
		}
		if len(changes) >= a.threshold || lastTruncateIndex(changes) >= 0 {
			if err := a.flushTableContext(ctx, tableName, results, token); err != nil {
				return err
			}
		}
	}
	for a.byteLimit > 0 && a.totalBytes >= a.byteLimit {
		tableName, ok := a.largestTable()
		if !ok {
			tableName, ok = a.largestBlockedTable()
			if !ok {
				return fmt.Errorf("CDC accumulator exceeded its memory limit with no flushable or spillable table")
			}
			if err := a.spillBlockedStableWindow(tableName); err != nil {
				return err
			}
			continue
		}
		if err := a.flushTableContext(ctx, tableName, results, token); err != nil {
			return err
		}
	}
	return nil
}

func (a *batchAccumulator) largestBlockedTable() (string, bool) {
	var largest string
	var largestBytes int64
	found := false
	for tableName, size := range a.bytes {
		if !a.blockedKeyless[tableName] || len(a.changes[tableName]) == 0 {
			continue
		}
		if !found || size > largestBytes {
			largest = tableName
			largestBytes = size
			found = true
		}
	}
	return largest, found
}

func (a *batchAccumulator) largestTable() (string, bool) {
	var largest string
	var largestBytes int64
	found := false
	for tableName, size := range a.bytes {
		if a.blockedKeyless[tableName] {
			continue
		}
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
		if a.blockedKeyless[tableName] {
			continue
		}
		if err := a.flushTableContext(ctx, tableName, results, token); err != nil {
			return err
		}
	}
	return nil
}

// flushStableKeylessContext emits CDC data in fixed transaction-local sequence
// windows. pendingLow is the decoder's oldest transaction that has not been
// fully handed to this accumulator. A window is safe to emit when a later
// window for the same table has arrived, or when its transaction is no longer
// pending in the decoder. The historical name is retained because this replay
// contract originated with keyless CDC.
func (a *batchAccumulator) flushStableKeylessContext(
	ctx context.Context,
	results chan<- source.RecordBatchResult,
	token tokenFunc,
	pendingLow pglogrepl.LSN,
	pending bool,
) error {
	tables := make(map[string]struct{}, len(a.changes)+len(a.stableSpills))
	for tableName := range a.changes {
		tables[tableName] = struct{}{}
	}
	for tableName := range a.stableSpills {
		tables[tableName] = struct{}{}
	}
	for tableName := range tables {
		tableSchema := a.schemas[tableName]
		if tableSchema == nil || !a.stableAll {
			continue
		}
		if spill := a.stableSpills[tableName]; spill != nil {
			changes := a.changes[tableName]
			prefix := 0
			for prefix < len(changes) && changes[prefix].LSN == spill.lsn && changes[prefix].DataBatchWindow == spill.window {
				prefix++
			}
			if prefix > 0 {
				if err := a.appendResidentChangesToStableSpill(tableName, prefix); err != nil {
					return err
				}
			}
			transactionComplete := !pending || spill.lsn < pendingLow
			windowClosed := len(a.changes[tableName]) > 0
			if !transactionComplete && !windowClosed {
				a.blockedKeyless[tableName] = true
				continue
			}
			delete(a.blockedKeyless, tableName)
			if err := a.restoreStableWindowSpill(tableName); err != nil {
				return err
			}
		}
		for {
			changes := a.changes[tableName]
			if len(changes) == 0 {
				delete(a.blockedKeyless, tableName)
				break
			}
			lsn := changes[0].LSN
			window := changes[0].DataBatchWindow
			end := 1
			for end < len(changes) && changes[end].LSN == lsn && changes[end].DataBatchWindow == window {
				end++
			}
			transactionComplete := !pending || lsn < pendingLow
			windowClosed := end < len(changes)
			if !transactionComplete && !windowClosed {
				a.blockedKeyless[tableName] = true
				break
			}
			delete(a.blockedKeyless, tableName)
			if err := a.flushTablePrefixContext(ctx, tableName, end, results, token); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *batchAccumulator) spillBlockedStableWindow(tableName string) error {
	changes := a.changes[tableName]
	if len(changes) == 0 {
		return fmt.Errorf("blocked stable CDC window for %s has no resident changes", tableName)
	}
	lsn := changes[0].LSN
	window := changes[0].DataBatchWindow
	for i := 1; i < len(changes); i++ {
		if changes[i].LSN != lsn || changes[i].DataBatchWindow != window {
			return fmt.Errorf("blocked stable CDC window for %s contains multiple durable identities", tableName)
		}
	}
	spill := a.stableSpills[tableName]
	if spill == nil {
		spill = &stableWindowSpill{lsn: lsn, window: window, spool: newChangeSpool[Change](1)}
		a.stableSpills[tableName] = spill
	} else if spill.lsn != lsn || spill.window != window {
		return fmt.Errorf("blocked stable CDC window for %s changed identity before its spill was emitted", tableName)
	}
	if err := a.appendResidentChangesToStableSpill(tableName, len(changes)); err != nil {
		if spill.spool.Len() == 0 {
			_ = spill.spool.Close()
			delete(a.stableSpills, tableName)
		}
		return err
	}
	return nil
}

func (a *batchAccumulator) appendResidentChangesToStableSpill(tableName string, count int) error {
	spill := a.stableSpills[tableName]
	changes := a.changes[tableName]
	if spill == nil || count < 1 || count > len(changes) {
		return fmt.Errorf("invalid stable CDC spill prefix for %s", tableName)
	}
	for i := 0; i < count; i++ {
		if changes[i].LSN != spill.lsn || changes[i].DataBatchWindow != spill.window {
			return fmt.Errorf("stable CDC spill prefix for %s changed durable identity", tableName)
		}
		if err := spill.spool.Append(changes[i]); err != nil {
			return fmt.Errorf("failed to spill stable CDC window for %s: %w", tableName, err)
		}
	}
	remainder := changes[count:]
	a.removeResidentTable(tableName)
	if len(remainder) > 0 {
		a.add(tableName, remainder, remainder[0].LSN)
	}
	if current, exists := a.minLSN[tableName]; !exists || spill.lsn < current {
		a.minLSN[tableName] = spill.lsn
	}
	return nil
}

func (a *batchAccumulator) restoreStableWindowSpill(tableName string) error {
	spill := a.stableSpills[tableName]
	if spill == nil {
		return nil
	}
	remainder := a.changes[tableName]
	a.removeResidentTable(tableName)
	if err := spill.spool.Seal(); err != nil {
		return fmt.Errorf("failed to seal stable CDC spill for %s: %w", tableName, err)
	}
	restored := make([]Change, 0, spill.spool.Len()+len(remainder))
	for spill.spool.Len() > 0 {
		changes, err := spill.spool.Drain(defaultCommittedDrainChanges)
		if err != nil {
			return fmt.Errorf("failed to restore stable CDC spill for %s: %w", tableName, err)
		}
		restored = append(restored, changes...)
	}
	if err := spill.spool.Close(); err != nil {
		return fmt.Errorf("failed to remove stable CDC spill for %s: %w", tableName, err)
	}
	delete(a.stableSpills, tableName)
	restored = append(restored, remainder...)
	a.add(tableName, restored, spill.lsn)
	return nil
}

func (a *batchAccumulator) removeResidentTable(tableName string) {
	delete(a.changes, tableName)
	delete(a.minLSN, tableName)
	a.totalBytes -= a.bytes[tableName]
	if a.totalBytes < 0 {
		a.totalBytes = 0
	}
	delete(a.bytes, tableName)
}

func (a *batchAccumulator) flushTableContext(ctx context.Context, tableName string, results chan<- source.RecordBatchResult, token tokenFunc) error {
	return a.flushTablePrefixContext(ctx, tableName, len(a.changes[tableName]), results, token)
}

func (a *batchAccumulator) flushTablePrefixContext(ctx context.Context, tableName string, count int, results chan<- source.RecordBatchResult, token tokenFunc) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		a.discard()
		return err
	}
	bufferedChanges := a.changes[tableName]
	if len(bufferedChanges) == 0 || count <= 0 {
		return nil
	}
	if count > len(bufferedChanges) {
		count = len(bufferedChanges)
	}
	changes := bufferedChanges[:count:count]
	remainder := bufferedChanges[count:]

	a.removeResidentTable(tableName)
	if len(remainder) > 0 {
		a.add(tableName, remainder, remainder[0].LSN)
	}

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
	if a.stableAll && len(tableSchema.PrimaryKeys) == 0 {
		for start := 0; start < len(changes); {
			end := start + 1
			for end < len(changes) && changes[end].LSN == changes[start].LSN {
				end++
			}
			transactionToken := commitToken
			if stateToken, ok := transactionToken.(source.CDCStateCommitToken); ok {
				stateToken.DataBatchID = transactionDataBatchID(tableName, changes[start:end])
				transactionToken = stateToken
			}
			batch, err := changesToBatch(changes[start:end], tableSchema)
			if err != nil {
				return fmt.Errorf("failed to materialize transaction batch for table %s: %w", tableName, err)
			}
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				batch.Release()
				a.discard()
				return err
			}
			if err := sendResult(ctx, results, source.RecordBatchResult{
				Batch: batch, TableName: tableName, CommitToken: transactionToken,
			}); err != nil {
				a.discard()
				return err
			}
			start = end
		}
		config.Debug("[CDC] Flushed %d buffered changes as transaction batches for keyless table %s", buffered, tableName)
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
	batchToken := commitToken
	if stateToken, ok := batchToken.(source.CDCStateCommitToken); ok && a.stableAll {
		stateToken.DataBatchID = transactionDataBatchID(tableName, changes)
		batchToken = stateToken
	}

	if err := sendResult(ctx, results, source.RecordBatchResult{
		Batch:       batch,
		TableName:   tableName,
		CommitToken: batchToken,
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
