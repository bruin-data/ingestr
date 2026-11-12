package starrocks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type snapshotTestTransaction struct {
	id         int64
	bodies     [][]byte
	rows       []int64
	committed  bool
	rolledBack bool
}

type snapshotTestServer struct {
	mu           sync.Mutex
	transactions map[string]*snapshotTestTransaction
	loads        int
	commits      int
	visible      int
	failLoad     int
	failPrepare  int
	alwaysFail   bool
	hook         func(*http.Request) error
}

func newSnapshotTestDestination(t *testing.T) (*StarRocksDestination, *snapshotTestServer) {
	t.Helper()
	server := &snapshotTestServer{transactions: make(map[string]*snapshotTestTransaction)}
	limits := defaultSnapshotWriteLimits(1)
	limits.requestBytes, limits.requestTarget = 1024, 512
	limits.transactionBytes = 4096
	limits.retryInitial, limits.retryMaximum, limits.retryTimeout = time.Millisecond, 2*time.Millisecond, 8*time.Millisecond
	d := &StarRocksDestination{snapshotLimits: &limits, loader: &streamLoader{
		endpoint: "http://starrocks", client: &http.Client{Transport: parallelLoadTransport(server.request)},
		waitForPublish: func(context.Context, string, int64) error {
			server.mu.Lock()
			defer server.mu.Unlock()
			server.visible++
			return nil
		},
	}}
	return d, server
}

func (s *snapshotTestServer) request(r *http.Request) (*http.Response, error) {
	if s.hook != nil {
		if err := s.hook(r); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	operation := strings.TrimPrefix(r.URL.Path, "/api/transaction/")
	label := r.Header.Get("label")
	if operation == "begin" {
		if _, exists := s.transactions[label]; exists {
			return nil, errors.New("transaction label reused")
		}
		s.transactions[label] = &snapshotTestTransaction{id: int64(len(s.transactions) + 1)}
	}
	tx := s.transactions[label]
	if tx == nil {
		return nil, fmt.Errorf("unknown transaction %s", label)
	}
	status, message := "OK", ""
	switch operation {
	case "load":
		s.loads++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		var rows []struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		tx.bodies = append(tx.bodies, body)
		for _, row := range rows {
			tx.rows = append(tx.rows, row.ID)
		}
		if s.loads == s.failLoad || s.alwaysFail {
			status, message = "Fail", "too many versions, current/limit: 1001/1000"
		}
	case "prepare":
		if s.failPrepare > 0 {
			s.failPrepare--
			status, message = "Fail", "too many versions, current/limit: 1001/1000"
		}
	case "commit":
		tx.committed = true
		s.commits++
	case "rollback":
		tx.rolledBack = true
	}
	body := fmt.Sprintf(`{"Status":%q,"Message":%q,"TxnId":%d,"NumberLoadedRows":%d,"NumberTotalRows":%d,"NumberFilteredRows":0,"NumberUnselectedRows":0}`, status, message, tx.id, len(tx.rows), len(tx.rows))
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func snapshotTestRecords(t *testing.T, rows int) (<-chan source.RecordBatchResult, *memory.CheckedAllocator) {
	t.Helper()
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	records := make(chan source.RecordBatchResult, rows)
	for id := range rows {
		records <- source.RecordBatchResult{Batch: snapshotLoadBatch(mem, int64(id), false, "[]"), CDCSnapshot: true}
	}
	close(records)
	return records, mem
}

func TestSnapshotWriterCoalescesPagesAndGroupsRequests(t *testing.T) {
	d, server := newSnapshotTestDestination(t)
	records, mem := snapshotTestRecords(t, 50)
	require.NoError(t, d.writeSnapshot(t.Context(), records, snapshotWriteOptions(1)))
	var ids []int64
	for _, tx := range server.transactions {
		require.True(t, tx.committed)
		bytes := 0
		for _, body := range tx.bodies {
			require.LessOrEqual(t, len(body), d.snapshotLimits.requestBytes)
			bytes += len(body)
		}
		// A window seals once it reaches transactionBytes; the request that crossed it and the
		// worker's partial request can still land in it.
		require.LessOrEqual(t, bytes, d.snapshotLimits.transactionBytes+2*d.snapshotLimits.requestBytes)
		ids = append(ids, tx.rows...)
	}
	require.Less(t, server.loads, 25, "small source pages must share requests")
	require.Less(t, server.commits, server.loads, "several requests must share a commit")
	require.Equal(t, server.commits, server.visible)
	want := make([]int64, 50)
	for i := range want {
		want[i] = int64(i)
	}
	require.ElementsMatch(t, want, ids)
	mem.AssertSize(t, 0)
}

func TestSnapshotWriterRangeFlushDoesNotCommit(t *testing.T) {
	d, server := newSnapshotTestDestination(t)
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	records := make(chan source.RecordBatchResult, 7)
	for id := range 3 {
		records <- source.RecordBatchResult{Batch: snapshotLoadBatch(mem, int64(id), false, "[]"), CDCSnapshot: true}
		records <- source.RecordBatchResult{CDCSnapshotRangeEnd: true}
	}
	records <- source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{SnapshotProgress: map[string]source.CDCSnapshotProgress{"events": {Boundary: "1"}}}}
	close(records)
	opts := snapshotWriteOptions(1)
	callbacks := 0
	opts.CDCSnapshotProgress = func(_ context.Context, token source.CDCStateCommitToken) error {
		server.mu.Lock()
		defer server.mu.Unlock()
		callbacks++
		require.Equal(t, 1, server.commits)
		require.Equal(t, server.commits, server.visible)
		require.Equal(t, "1", token.SnapshotProgress["events"].Boundary)
		require.Nil(t, token.SourceCommitToken)
		return nil
	}
	require.NoError(t, d.writeSnapshot(t.Context(), records, opts))
	require.Equal(t, 3, server.loads)
	require.Equal(t, 1, server.commits)
	require.Equal(t, 1, callbacks)
	mem.AssertSize(t, 0)
}

func TestSnapshotWriterRetriesWholeTransactionAfterVersionPressure(t *testing.T) {
	for _, operation := range []string{"load", "prepare"} {
		t.Run(operation, func(t *testing.T) {
			d, server := newSnapshotTestDestination(t)
			d.snapshotLimits.requestTarget = 1
			if operation == "load" {
				server.failLoad = 2
			} else {
				server.failPrepare = 1
			}
			records, mem := snapshotTestRecords(t, 3)
			require.NoError(t, d.writeSnapshot(t.Context(), records, snapshotWriteOptions(1)))
			require.Len(t, server.transactions, 2)
			for _, tx := range server.transactions {
				if tx.id == 1 {
					require.True(t, tx.rolledBack)
					require.False(t, tx.committed)
				} else {
					require.True(t, tx.committed)
					require.Equal(t, []int64{0, 1, 2}, tx.rows, "replay must include requests preceding the refused one")
				}
			}
			require.Equal(t, 1, server.commits)
			mem.AssertSize(t, 0)
		})
	}
}

func TestSnapshotWriterVersionPressureHasBoundedWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, server := newSnapshotTestDestination(t)
		server.alwaysFail = true
		records, mem := snapshotTestRecords(t, 1)
		start := time.Now()
		err := d.writeSnapshot(t.Context(), records, snapshotWriteOptions(1))
		require.ErrorContains(t, err, "version pressure exceeded")
		require.LessOrEqual(t, time.Since(start), d.snapshotLimits.retryTimeout)
		require.Zero(t, server.commits)
		for _, tx := range server.transactions {
			require.True(t, tx.rolledBack)
		}
		mem.AssertSize(t, 0)
	})
}

func TestSnapshotWriterRetryDeadlineIncludesRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, server := newSnapshotTestDestination(t)
		server.failLoad = 1
		loads := 0
		server.hook = func(r *http.Request) error {
			if r.URL.Path == "/api/transaction/load" {
				loads++
				if loads == 2 {
					<-r.Context().Done()
					return r.Context().Err()
				}
			}
			return nil
		}
		records, mem := snapshotTestRecords(t, 1)
		start := time.Now()
		err := d.writeSnapshot(t.Context(), records, snapshotWriteOptions(1))
		require.ErrorContains(t, err, "version pressure exceeded")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, d.snapshotLimits.retryTimeout, time.Since(start))
		require.Zero(t, server.commits)
		mem.AssertSize(t, 0)
	})
}

func TestSnapshotWriterIdleFlushAndPeriodicProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, server := newSnapshotTestDestination(t)
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		records := make(chan source.RecordBatchResult, 2)
		records <- source.RecordBatchResult{Batch: snapshotLoadBatch(mem, 1, false, "[]"), CDCSnapshot: true}
		records <- source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{SnapshotProgress: map[string]source.CDCSnapshotProgress{"events": {Boundary: "1"}}}}
		opts := snapshotWriteOptions(1)
		var callbacks atomic.Int64
		opts.CDCSnapshotProgress = func(context.Context, source.CDCStateCommitToken) error {
			callbacks.Add(1)
			return nil
		}
		done := make(chan error, 1)
		go func() { done <- d.writeSnapshot(t.Context(), records, opts) }()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		server.mu.Lock()
		require.Equal(t, 1, server.loads)
		require.Zero(t, server.commits)
		server.mu.Unlock()
		require.Zero(t, callbacks.Load())
		time.Sleep(9 * time.Second)
		synctest.Wait()
		server.mu.Lock()
		require.Equal(t, 1, server.commits)
		server.mu.Unlock()
		require.EqualValues(t, 1, callbacks.Load())
		close(records)
		require.NoError(t, <-done)
		mem.AssertSize(t, 0)
	})
}

func TestSnapshotWriterProgressWaitsForEveryVisibleCommit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, server := newSnapshotTestDestination(t)
		d.snapshotLimits.requestTarget = 1
		blocked := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		server.hook = func(r *http.Request) error {
			if r.URL.Path == "/api/transaction/load" {
				var wait bool
				once.Do(func() { wait = true; close(blocked) })
				if wait {
					<-release
				}
			}
			return nil
		}
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		records := make(chan source.RecordBatchResult, 3)
		for id := range 2 {
			records <- source.RecordBatchResult{Batch: snapshotLoadBatch(mem, int64(id), false, "[]"), CDCSnapshot: true}
		}
		records <- source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{SnapshotProgress: map[string]source.CDCSnapshotProgress{"events": {Boundary: "1"}}}}
		close(records)
		opts := snapshotWriteOptions(2)
		callbacks := 0
		opts.CDCSnapshotProgress = func(context.Context, source.CDCStateCommitToken) error {
			callbacks++
			server.mu.Lock()
			defer server.mu.Unlock()
			require.Equal(t, server.commits, server.visible)
			var committed []int64
			for _, tx := range server.transactions {
				if tx.committed {
					committed = append(committed, tx.rows...)
				}
			}
			require.ElementsMatch(t, []int64{0, 1}, committed, "progress must follow the commit of every preceding row")
			return nil
		}
		done := make(chan error, 1)
		go func() { done <- d.writeSnapshot(t.Context(), records, opts) }()
		<-blocked
		synctest.Wait()
		require.Zero(t, callbacks)
		close(release)
		require.NoError(t, <-done)
		require.Equal(t, 1, callbacks)
		mem.AssertSize(t, 0)
	})
}

func TestSnapshotWriterCheckpointFailureAborts(t *testing.T) {
	d, server := newSnapshotTestDestination(t)
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{SnapshotProgress: map[string]source.CDCSnapshotProgress{"events": {Boundary: "1"}}}}
	close(records)
	opts := snapshotWriteOptions(1)
	wantErr := errors.New("checkpoint unavailable")
	opts.CDCSnapshotProgress = func(context.Context, source.CDCStateCommitToken) error { return wantErr }
	require.ErrorIs(t, d.writeSnapshot(t.Context(), records, opts), wantErr)
	require.Zero(t, server.commits)
}

func TestSnapshotWriterMemoryBudget(t *testing.T) {
	for workers := 1; workers <= 8; workers++ {
		limits := defaultSnapshotWriteLimits(workers)
		window := limits.transactionBytes + workers*limits.requestBytes
		require.LessOrEqual(t, 2*window+workers*limits.requestBytes, 4<<30, "an open and a committing window plus one request per worker")
		require.GreaterOrEqual(t, limits.transactionBytes, limits.requestBytes)
	}
}

func TestSnapshotWriterCommitsNextWindowWhileEarlierAwaitsVisibility(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, server := newSnapshotTestDestination(t)
		d.snapshotLimits.requestTarget, d.snapshotLimits.transactionBytes = 1, 1
		release := make(chan struct{})
		var publishing atomic.Int64
		d.loader.waitForPublish = func(ctx context.Context, _ string, _ int64) error {
			publishing.Add(1)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			server.mu.Lock()
			defer server.mu.Unlock()
			server.visible++
			return nil
		}
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		records := make(chan source.RecordBatchResult, 4)
		for id := range 3 {
			records <- source.RecordBatchResult{Batch: snapshotLoadBatch(mem, int64(id), false, "[]"), CDCSnapshot: true}
		}
		records <- source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{SnapshotProgress: map[string]source.CDCSnapshotProgress{"events": {Boundary: "1"}}}}
		close(records)
		opts := snapshotWriteOptions(1)
		var callbacks atomic.Int64
		opts.CDCSnapshotProgress = func(context.Context, source.CDCStateCommitToken) error {
			server.mu.Lock()
			defer server.mu.Unlock()
			require.Equal(t, server.commits, server.visible, "progress waits until every committed window is visible")
			callbacks.Add(1)
			return nil
		}
		done := make(chan error, 1)
		go func() { done <- d.writeSnapshot(t.Context(), records, opts) }()
		synctest.Wait()
		server.mu.Lock()
		require.GreaterOrEqual(t, server.commits, 2, "windows commit while earlier ones await visibility")
		require.Zero(t, server.visible)
		server.mu.Unlock()
		require.EqualValues(t, 1, publishing.Load(), "visibility is awaited in commit order")
		require.Zero(t, callbacks.Load())
		close(release)
		require.NoError(t, <-done)
		require.EqualValues(t, 1, callbacks.Load())
		mem.AssertSize(t, 0)
	})
}
