package multitable

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestRouterDeliversSourceErrorToFullTableChannels(t *testing.T) {
	sourceErr := errors.New("source failed")
	input := make(chan source.RecordBatchResult, 3)
	input <- source.RecordBatchResult{TableName: "users"}
	input <- source.RecordBatchResult{TableName: "orders"}
	input <- source.RecordBatchResult{Err: sourceErr}
	close(input)

	router := NewRouter([]string{"users", "orders"}, 1)
	router.Route(context.Background(), input)

	deadline := time.Now().Add(time.Second)
	for router.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !errors.Is(router.Err(), sourceErr) {
		t.Fatalf("Router.Err() = %v, want source error", router.Err())
	}

	select {
	case <-router.done:
		t.Fatal("router completed before it could deliver the source error to full channels")
	case <-time.After(20 * time.Millisecond):
	}

	users := router.GetChannel("users")
	orders := router.GetChannel("orders")
	if result := <-users; result.Err != nil {
		t.Fatalf("first users result error = %v, want data result", result.Err)
	}
	if result := <-orders; result.Err != nil {
		t.Fatalf("first orders result error = %v, want data result", result.Err)
	}

	usersErr, usersOpen := <-users
	ordersErr, ordersOpen := <-orders
	if !usersOpen || !errors.Is(usersErr.Err, sourceErr) {
		t.Fatalf("users terminal result = (%+v, open=%v), want source error", usersErr, usersOpen)
	}
	if !ordersOpen || !errors.Is(ordersErr.Err, sourceErr) {
		t.Fatalf("orders terminal result = (%+v, open=%v), want source error", ordersErr, ordersOpen)
	}

	router.Wait()
	if _, open := <-users; open {
		t.Fatal("users channel remained open after terminal error")
	}
	if _, open := <-orders; open {
		t.Fatal("orders channel remained open after terminal error")
	}
}

func TestRouterReleasesBatchForUnknownTable(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	builder := array.NewInt64Builder(mem)
	builder.Append(1)
	values := builder.NewArray()
	record := array.NewRecordBatch(
		arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil),
		[]arrow.Array{values},
		1,
	)
	values.Release()
	builder.Release()

	input := make(chan source.RecordBatchResult, 1)
	input <- source.RecordBatchResult{TableName: "unknown", Batch: record}
	close(input)

	router := NewRouter([]string{"known"}, 1)
	router.Route(context.Background(), input)
	router.Wait()
}
