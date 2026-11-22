package destination

import (
	"context"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// CDCSnapshotWriter supports writing full CDC snapshot rows directly into a
// keyed target while preserving rows that already carry a newer CDC position.
// WriteParallel must wait for every load to become visible before returning.
type CDCSnapshotWriter interface {
	SupportsDirectCDCSnapshot() bool
	// CanWriteCDCSnapshot may prepare destination-owned ordering metadata. Any
	// mutation must remain fenced to expectedIncarnation when it is supplied.
	CanWriteCDCSnapshot(ctx context.Context, table string, sourceSchema *schema.TableSchema, expectedIncarnation string) (bool, error)
}

func CanWriteDirectCDCSnapshot(ctx context.Context, dest Destination, table string, sourceSchema *schema.TableSchema, expectedIncarnation string) (bool, error) {
	writer, ok := dest.(CDCSnapshotWriter)
	if !ok || !writer.SupportsDirectCDCSnapshot() {
		return false, nil
	}
	return writer.CanWriteCDCSnapshot(ctx, table, sourceSchema, expectedIncarnation)
}

func SupportsDirectCDCSnapshot(dest Destination) bool {
	writer, ok := dest.(CDCSnapshotWriter)
	return ok && writer.SupportsDirectCDCSnapshot()
}
