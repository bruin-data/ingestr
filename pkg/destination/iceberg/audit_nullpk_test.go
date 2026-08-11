package iceberg

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

// Audit probe: source delivers a NULL in a primary-key column (source schema
// marks the PK nullable, e.g. schema inference). PrepareTable forces the PK
// to required in Iceberg. What lands in the table?
func TestAuditNullPrimaryKeyWrite(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)

	sourceSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: true},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
	}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       "lake.audit.nullpk",
		Schema:      sourceSchema,
		PrimaryKeys: []string{"id"},
	}))

	// Build a batch with the SOURCE arrow schema (nullable id) containing a null id.
	arrowSchema := sourceSchema.ToArrowSchema()
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrowSchema)
	defer b.Release()
	idB := b.Field(0).(*array.Int64Builder)
	nameB := b.Field(1).(*array.StringBuilder)
	idB.Append(7)
	nameB.Append("seven")
	idB.AppendNull()
	nameB.Append("null-id-row")
	batch := b.NewRecordBatch()

	err := dest.WriteParallel(ctx, recordBatches(batch), destination.WriteOptions{
		Table:  "lake.audit.nullpk",
		Schema: sourceSchema,
	})
	t.Logf("write err: %v", err)

	if err == nil {
		rows := readTableRows(t, dest, "lake.audit.nullpk")
		for _, row := range rows.Rows {
			t.Logf("row: %#v", row)
		}
	}
}

// Audit probe: what does the reader do when the batch schema is equal to the
// target (PK non-nullable in write schema) but data has nulls anyway?
func TestAuditNullPKRequiredSchema(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)

	srcSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
	}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       "lake.audit.nullpk2",
		Schema:      srcSchema,
		PrimaryKeys: []string{"id"},
	}))

	fields := []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
	defer b.Release()
	b.Field(0).(*array.Int64Builder).Append(1)
	b.Field(1).(*array.StringBuilder).Append("one")
	b.Field(0).(*array.Int64Builder).AppendNull()
	b.Field(1).(*array.StringBuilder).Append("null-id")
	batch := b.NewRecordBatch()

	err := dest.WriteParallel(ctx, recordBatches(batch), destination.WriteOptions{
		Table:  "lake.audit.nullpk2",
		Schema: srcSchema,
	})
	t.Logf("write err: %v", err)
	if err == nil {
		rows := readTableRows(t, dest, "lake.audit.nullpk2")
		for _, row := range rows.Rows {
			t.Logf("row: %#v", row)
		}
	}
}
