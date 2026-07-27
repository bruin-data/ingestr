package postgres_cdc

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// changesToBatch materializes decoded changes into a single Arrow record
// batch. Rows keep their slice order; _cdc_synced_at increases by one
// microsecond per row so rows remain distinguishable within a batch.
func changesToBatch(changes []Change, tableSchema *schema.TableSchema) (arrow.RecordBatch, error) {
	if len(changes) == 0 {
		return nil, nil
	}

	mem := memory.NewGoAllocator()
	arrowSchema := buildArrowSchema(tableSchema.Columns)

	builders := make([]array.Builder, len(tableSchema.Columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	syncedAt := time.Now().UTC()
	nSource := sourceColumnCount(tableSchema)

	for i, change := range changes {
		for colIdx := 0; colIdx < nSource; colIdx++ {
			arrowconv.AppendValue(builders[colIdx], resolveColumnValue(change, colIdx))
		}

		builders[nSource].(*array.StringBuilder).Append(FormatChangeLSN(change.LSN, change.Sequence))
		builders[nSource+1].(*array.BooleanBuilder).Append(change.Operation == "DELETE")
		perRowSyncedAt := syncedAt.Add(time.Duration(i) * time.Microsecond)
		builders[nSource+2].(*array.TimestampBuilder).Append(arrow.Timestamp(perRowSyncedAt.UnixMicro()))
		builders[nSource+3].(*array.StringBuilder).Append(unchangedColumnsJSON(change, tableSchema.Columns, nSource))
	}

	arrays := make([]arrow.Array, len(builders))
	for i, b := range builders {
		arrays[i] = b.NewArray()
	}

	record := array.NewRecordBatch(arrowSchema, arrays, int64(len(changes)))

	for _, arr := range arrays {
		arr.Release()
	}

	return record, nil
}
