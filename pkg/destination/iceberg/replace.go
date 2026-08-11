package iceberg

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	iceberggo "github.com/apache/iceberg-go"
)

func identifierFieldNames(tableSchema *iceberggo.Schema) ([]string, error) {
	names := make([]string, 0, len(tableSchema.IdentifierFieldIDs))
	for _, id := range tableSchema.IdentifierFieldIDs {
		name, ok := tableSchema.FindColumnName(id)
		if !ok {
			return nil, fmt.Errorf("identifier field ID %d is missing from the table schema", id)
		}
		names = append(names, name)
	}
	return names, nil
}

func newDeduplicatedReplaceReader(
	reader array.RecordReader,
	primaryKeys []string,
	incrementalKey string,
	table string,
) (*chanRecordReader, *spillSorter, error) {
	sorter, err := newSpillSorter(reader.Schema(), primaryKeys)
	if err != nil {
		return nil, nil, err
	}

	if err := forEachRecordReaderRow(reader, sorter.Add); err != nil {
		sorter.Close()
		return nil, nil, err
	}

	incrementalIdx, err := incrementalKeyIndex(arrowSchemaColumnNames(reader.Schema()), incrementalKey, table)
	if err != nil {
		sorter.Close()
		return nil, nil, err
	}

	deduped := streamingReader(reader.Schema(), func(sink func(arrow.RecordBatch) error) error {
		it, err := sorter.Iter()
		if err != nil {
			return err
		}
		defer it.Close()

		projection := newRowProjection(reader.Schema(), arrowSchemaColumnNames(reader.Schema()))
		emitter := newBatchEmitter(projection, sink)
		defer emitter.release()

		for it.NextGroup() {
			if err := emitter.add(selectGroupWinner(it, incrementalIdx)); err != nil {
				return err
			}
		}
		if err := it.Err(); err != nil {
			return err
		}
		return emitter.flushBatch()
	})

	return deduped, sorter, nil
}

func forEachRecordReaderRow(reader array.RecordReader, fn func([]any) error) error {
	for reader.Next() {
		batch := reader.RecordBatch()
		for rowIdx := range int(batch.NumRows()) {
			row := make([]any, batch.NumCols())
			for colIdx := range int(batch.NumCols()) {
				value, err := rowValue(batch.Column(colIdx), rowIdx)
				if err != nil {
					return fmt.Errorf("column %q: %w", batch.ColumnName(colIdx), err)
				}
				row[colIdx] = value
			}
			if err := fn(row); err != nil {
				return err
			}
		}
	}
	return reader.Err()
}
