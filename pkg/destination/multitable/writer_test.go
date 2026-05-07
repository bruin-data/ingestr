package multitable

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/gong/pkg/destination"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

type fakeDestination struct {
	writeFn func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error
}

func (d *fakeDestination) Schemes() []string                             { return nil }
func (d *fakeDestination) Connect(ctx context.Context, uri string) error { return nil }
func (d *fakeDestination) Close(ctx context.Context) error               { return nil }
func (d *fakeDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	return nil
}

func (d *fakeDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *fakeDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if d.writeFn != nil {
		return d.writeFn(ctx, records, opts)
	}
	for result := range records {
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}

func (d *fakeDestination) SwapTable(ctx context.Context, stagingTable, targetTable string) error {
	return nil
}

func (d *fakeDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	return nil
}

func (d *fakeDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return nil
}

func (d *fakeDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return nil
}
func (d *fakeDestination) DropTable(ctx context.Context, table string) error { return nil }
func (d *fakeDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}

func (d *fakeDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return nil, nil
}

func (d *fakeDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}
func (d *fakeDestination) GetScheme() string                  { return "fake" }
func (d *fakeDestination) SupportsReplaceStrategy() bool      { return true }
func (d *fakeDestination) SupportsAppendStrategy() bool       { return true }
func (d *fakeDestination) SupportsMergeStrategy() bool        { return true }
func (d *fakeDestination) SupportsDeleteInsertStrategy() bool { return true }
func (d *fakeDestination) SupportsSCD2Strategy() bool         { return true }
func (d *fakeDestination) SupportsAtomicSwap() bool           { return true }

func TestWriteCancelsOtherTablesOnFirstError(t *testing.T) {
	dest := &fakeDestination{
		writeFn: func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
			switch opts.Table {
			case "dataset.fail":
				result, ok := <-records
				if !ok {
					return nil
				}
				if result.Err != nil {
					return result.Err
				}
				return errors.New("boom")
			case "dataset.wait":
				<-ctx.Done()
				return ctx.Err()
			default:
				return nil
			}
		},
	}

	records := make(chan source.RecordBatchResult, 16)
	for i := 0; i < 16; i++ {
		records <- source.RecordBatchResult{TableName: "fail"}
	}
	close(records)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := Write(ctx, dest, records, destination.MultiTableWriteOptions{
		TableConfigs: map[string]destination.TableWriteConfig{
			"fail": {DestTable: "dataset.fail"},
			"wait": {DestTable: "dataset.wait"},
		},
		Parallelism: 4,
	})
	if err == nil {
		t.Fatal("Write returned nil error, want failure")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Write error = %v, want boom", err)
	}
}

// TestWriteRouterDeadlock reproduces the exact deadlock pattern:
// interleaved records for two tables, one writer fails after first record.
// Without cancellation propagation, the router blocks sending to the failed
// table's full channel, starving the healthy table forever.
func TestWriteRouterDeadlock(t *testing.T) {
	dest := &fakeDestination{
		writeFn: func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
			if opts.Table == "dataset.fail" {
				// Consume one record then fail
				result, ok := <-records
				if !ok {
					return nil
				}
				if result.Err != nil {
					return result.Err
				}
				return errors.New("write failed")
			}
			// Healthy table: consume records normally until channel closes
			for result := range records {
				if result.Err != nil {
					return result.Err
				}
			}
			return nil
		},
	}

	// Feed interleaved records for both tables via a goroutine so the test
	// doesn't block if the router stops consuming.
	records := make(chan source.RecordBatchResult)
	go func() {
		defer close(records)
		for i := 0; i < 100; i++ {
			name := "fail"
			if i%2 == 1 {
				name = "healthy"
			}
			select {
			case records <- source.RecordBatchResult{TableName: name}:
			case <-time.After(5 * time.Second):
				return
			}
		}
	}()

	done := make(chan error, 1)
	go func() {
		done <- Write(context.Background(), dest, records, destination.MultiTableWriteOptions{
			TableConfigs: map[string]destination.TableWriteConfig{
				"fail":    {DestTable: "dataset.fail"},
				"healthy": {DestTable: "dataset.healthy"},
			},
			Parallelism: 4,
		})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Write returned nil error, want failure")
		}
		if !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("Write error = %v, want 'write failed'", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Write deadlocked: router blocked on failed table's channel, starving healthy table")
	}
}

func TestWriteReturnsParentContextError(t *testing.T) {
	dest := &fakeDestination{
		writeFn: func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	records := make(chan source.RecordBatchResult)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Write(ctx, dest, records, destination.MultiTableWriteOptions{
		TableConfigs: map[string]destination.TableWriteConfig{
			"users": {DestTable: "dataset.users"},
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Write error = %v, want context.Canceled", err)
	}
}
