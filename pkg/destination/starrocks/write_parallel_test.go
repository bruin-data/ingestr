package starrocks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type parallelLoadTransport func(*http.Request) (*http.Response, error)

func (f parallelLoadTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	defer func() { _ = r.Body.Close() }()
	return f(r)
}

func parallelLoadBatch(mem memory.Allocator, id int64) arrow.RecordBatch {
	builder := array.NewInt64Builder(mem)
	defer builder.Release()
	builder.Append(id)
	ids := builder.NewInt64Array()
	defer ids.Release()
	return array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{ids}, 1)
}

func runParallelLoadTest(t *testing.T, records <-chan source.RecordBatchResult, mem *memory.CheckedAllocator, parallelism int, transport parallelLoadTransport) (context.Context, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	d := &StarRocksDestination{loader: &streamLoader{endpoint: "http://starrocks", client: &http.Client{Transport: transport}}}
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- d.WriteParallel(ctx, records, destination.WriteOptions{Table: "db.events", Parallelism: parallelism})
	}()
	t.Cleanup(func() {
		cancel()
		<-finished
		for {
			select {
			case result, ok := <-records:
				if !ok {
					mem.AssertSize(t, 0)
					return
				}
				if result.Batch != nil {
					result.Batch.Release()
				}
			default:
				mem.AssertSize(t, 0)
				return
			}
		}
	})
	return ctx, cancel, done
}

func TestWriteParallelBoundsConcurrencyAndUsesUniqueLabels(t *testing.T) {
	for _, parallelism := range []int{1, 4, 0} {
		t.Run(strconv.Itoa(parallelism), func(t *testing.T) {
			workers := parallelism
			if workers == 0 {
				workers = 4
			}
			const batches = 12
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			records := make(chan source.RecordBatchResult, batches)
			for i := range batches {
				records <- source.RecordBatchResult{Batch: parallelLoadBatch(mem, int64(i))}
			}
			close(records)
			started := make(chan string, batches)
			release := make(chan struct{})
			var active, maximum atomic.Int64
			ctx, _, done := runParallelLoadTest(t, records, mem, parallelism, func(r *http.Request) (*http.Response, error) {
				n := active.Add(1)
				defer active.Add(-1)
				for previous := maximum.Load(); n > previous; previous = maximum.Load() {
					if maximum.CompareAndSwap(previous, n) {
						break
					}
				}
				started <- r.Header.Get("label")
				select {
				case <-release:
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"Status":"Success","NumberLoadedRows":1,"NumberTotalRows":1}`))}, nil
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			})
			labels := make(map[string]bool, batches)
			for range workers {
				select {
				case label := <-started:
					require.NotEmpty(t, label)
					require.False(t, labels[label])
					labels[label] = true
				case <-ctx.Done():
					t.Fatal("Stream Loads did not overlap at the requested parallelism")
				}
			}
			require.EqualValues(t, workers, active.Load())
			require.Len(t, records, batches-workers)
			close(release)
			require.NoError(t, <-done)
			close(started)
			for label := range started {
				require.NotEmpty(t, label)
				require.False(t, labels[label])
				labels[label] = true
			}
			require.Len(t, labels, batches)
			require.EqualValues(t, workers, maximum.Load())
			mem.AssertSize(t, 0)
		})
	}
}

func TestWriteParallelLoadFailureCancelsOtherLoads(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	records := make(chan source.RecordBatchResult, 5)
	for i := range cap(records) {
		records <- source.RecordBatchResult{Batch: parallelLoadBatch(mem, int64(i))}
	}
	defer close(records)
	started := make(chan struct{}, 2)
	fail := make(chan struct{})
	canceled := make(chan struct{})
	loadErr := errors.New("load rejected")
	var requests atomic.Int64
	ctx, _, done := runParallelLoadTest(t, records, mem, 2, func(r *http.Request) (*http.Response, error) {
		n := requests.Add(1)
		started <- struct{}{}
		if n == 2 {
			select {
			case <-fail:
				return nil, loadErr
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		<-r.Context().Done()
		close(canceled)
		return nil, r.Context().Err()
	})
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("expected two concurrent Stream Loads")
		}
	}
	close(fail)
	require.ErrorIs(t, <-done, loadErr)
	select {
	case <-canceled:
	default:
		t.Fatal("WriteParallel returned before canceling and joining the other load")
	}
	require.EqualValues(t, 2, requests.Load())
	require.Len(t, records, 3)
}

func TestWriteParallelSourceErrorCancelsLoadAndReleasesBatch(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Batch: parallelLoadBatch(mem, 1)}
	defer close(records)
	started := make(chan struct{})
	ctx, _, done := runParallelLoadTest(t, records, mem, 2, func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("Stream Load did not start")
	}
	sourceErr := errors.New("source read failed")
	records <- source.RecordBatchResult{Batch: parallelLoadBatch(mem, 2), Err: sourceErr}
	require.ErrorIs(t, <-done, sourceErr)
	mem.AssertSize(t, 0)
}

func TestWriteParallelCancellationUnblocksIdleWorkers(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	records := make(chan source.RecordBatchResult)
	defer close(records)
	_, cancel, done := runParallelLoadTest(t, records, mem, 4, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unexpected Stream Load")
	})
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
