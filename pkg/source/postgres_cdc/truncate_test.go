package postgres_cdc

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

func TestBatchAccumulatorKeepsOnlyRowsAfterLastTruncate(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns:     append([]schema.Column{{Name: "id", DataType: schema.TypeInt32}}, cdcMetaColumns()...),
		PrimaryKeys: []string{"id"},
	}
	accum := newBatchAccumulator(100, map[string]*schema.TableSchema{"": tableSchema})
	accum.add("", []Change{
		{Operation: "INSERT", LSN: 10, Values: []interface{}{int32(1)}},
		{Operation: "TRUNCATE", LSN: 10},
		{Operation: "INSERT", LSN: 10, Values: []interface{}{int32(2)}},
	}, pglogrepl.LSN(10))

	results := make(chan source.RecordBatchResult, 2)
	if err := accum.flushAll(results, func() any { return "durable-10" }); err != nil {
		t.Fatal(err)
	}
	truncate := <-results
	if !truncate.Truncate || !truncate.CDCWALTruncate || truncate.CommitToken != nil {
		t.Fatalf("first result = %+v, want truncate without commit token", truncate)
	}
	batch := <-results
	defer batch.Batch.Release()
	if batch.Truncate || batch.CommitToken != "durable-10" {
		t.Fatalf("second result = %+v, want post-truncate batch with token", batch)
	}
	ids := batch.Batch.Column(0).(*array.Int32)
	if ids.Len() != 1 || ids.Value(0) != 2 {
		t.Fatalf("post-truncate IDs = %v, want [2]", ids.Int32Values())
	}
}

func TestBatchAccumulatorTruncateOnlyCarriesCommitToken(t *testing.T) {
	tableSchema := &schema.TableSchema{Columns: cdcMetaColumns()}
	accum := newBatchAccumulator(100, map[string]*schema.TableSchema{"": tableSchema})
	accum.add("", []Change{{Operation: "TRUNCATE", LSN: 11}}, pglogrepl.LSN(11))

	results := make(chan source.RecordBatchResult, 1)
	if err := accum.flushAll(results, func() any { return "durable-11" }); err != nil {
		t.Fatal(err)
	}
	result := <-results
	if !result.Truncate || !result.CDCWALTruncate || result.CommitToken != "durable-11" {
		t.Fatalf("result = %+v, want truncate with durable token", result)
	}
}
