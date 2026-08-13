package fluxx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

// fluxxTestServer serves paginated grant records with a wide payload column.
func fluxxTestServer(t *testing.T, totalRows, payloadBytes int) *httptest.Server {
	t.Helper()
	pad := strings.Repeat("x", payloadBytes)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		if page < 1 {
			page = 1
		}
		if perPage < 1 {
			perPage = 100
		}
		items := []interface{}{}
		for i := (page - 1) * perPage; i < page*perPage && i < totalRows; i++ {
			items = append(items, map[string]interface{}{
				"id":      strconv.Itoa(i),
				"payload": fmt.Sprintf("%s-%d", pad, i),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"records":  map[string]interface{}{"grant": items},
			"per_page": perPage,
		})
	}))
}

// readAllFluxx drives readResource and returns, in emission order, the id of
// every row across all batches, plus the per-batch row counts.
func readAllFluxx(t *testing.T, srv *httptest.Server, opts source.ReadOptions) (order []string, batchRows []int) {
	t.Helper()
	s := &FluxxSource{client: httpclient.New(httpclient.WithBaseURL(srv.URL)), accessToken: "test"}
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeString},
		{Name: "payload", DataType: schema.TypeString},
	}
	results := make(chan source.RecordBatchResult, 64)
	go func() {
		defer close(results)
		if err := s.readResource(context.Background(), "grants", "grants", "test", cols, nil, results, opts); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	for r := range results {
		require.NoError(t, r.Err)
		if r.Batch == nil {
			continue
		}
		idCol := r.Batch.Column(0).(*array.String)
		for i := 0; i < idCol.Len(); i++ {
			order = append(order, idCol.Value(i))
		}
		batchRows = append(batchRows, int(r.Batch.NumRows()))
		r.Batch.Release()
	}
	return order, batchRows
}

// wantOrder is the id sequence "0","1",...,"n-1".
func wantOrder(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strconv.Itoa(i)
	}
	return out
}

func TestFluxxReadResource_ByteLimitSplitsWithoutLosingRows(t *testing.T) {
	const total, payload = 1000, 2000
	srv := fluxxTestServer(t, total, payload)
	defer srv.Close()

	// Unlimited bytes + a high row cap: everything lands in one batch, in order.
	order, batchRows := readAllFluxx(t, srv, source.ReadOptions{PageSize: 5000, MaxBatchBytes: 0})
	require.Equal(t, wantOrder(total), order, "no byte limit: all rows, in order")
	require.Equal(t, []int{total}, batchRows, "no byte limit -> single batch")

	// Small byte limit forces many flushes; the exact ordered sequence must be identical.
	order, batchRows = readAllFluxx(t, srv, source.ReadOptions{PageSize: 5000, MaxBatchBytes: 100_000})
	require.Equal(t, wantOrder(total), order, "byte flushing must preserve every row, in order, no gaps/dupes")
	require.Greater(t, len(batchRows), 1, "byte limit must split into multiple batches")
	require.Equal(t, total, sumInts(batchRows), "batch sizes must sum to the total")
}

func TestFluxxReadResource_RowCapStillApplies(t *testing.T) {
	const total = 250
	srv := fluxxTestServer(t, total, 10)
	defer srv.Close()

	// Narrow rows, byte limit never reached: page-size row cap governs batching.
	order, batchRows := readAllFluxx(t, srv, source.ReadOptions{PageSize: 100, MaxBatchBytes: 256 << 20})
	require.Equal(t, wantOrder(total), order)
	require.Equal(t, []int{100, 100, 50}, batchRows, "250 rows at a 100-row cap -> 100+100+50")
}

func TestFluxxReadResource_RowBiggerThanLimitFlushesPerRow(t *testing.T) {
	const total = 5
	srv := fluxxTestServer(t, total, 4096)
	defer srv.Close()

	order, batchRows := readAllFluxx(t, srv, source.ReadOptions{PageSize: 5000, MaxBatchBytes: 1024})
	require.Equal(t, wantOrder(total), order)
	require.Equal(t, []int{1, 1, 1, 1, 1}, batchRows, "each oversized row is its own batch")
}

func sumInts(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

func maxInt(xs []int) int {
	m := 0
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m
}

// Batch size stays bounded by the byte cap regardless of total volume.
func TestFluxxReadResource_LargeVolumeStaysBounded(t *testing.T) {
	const total, payload = 10000, 200 // ~225 raw JSON bytes/row
	srv := fluxxTestServer(t, total, payload)
	defer srv.Close()

	// 10 KB byte cap => ~45 rows per batch, independent of the 10k total.
	order, batchRows := readAllFluxx(t, srv, source.ReadOptions{PageSize: 1_000_000, MaxBatchBytes: 10_000})

	require.Equal(t, wantOrder(total), order, "all 10k rows, in order, across many batches")
	require.Equal(t, total, sumInts(batchRows))
	require.Greater(t, len(batchRows), 150, "large volume -> many small batches, not one big one")
	require.LessOrEqual(t, maxInt(batchRows), 50, "no batch grows with total volume; each stays bounded by the byte cap")
}

// A single page is cut into several byte-bounded batches at row boundaries.
func TestFluxxReadResource_SinglePageSubdividedByBytes(t *testing.T) {
	const total, payload = 100, 200 // one page (apiPageSize=100), ~225 raw JSON bytes/row
	srv := fluxxTestServer(t, total, payload)
	defer srv.Close()

	// 2 KB cap => ~9 rows per batch, so one 100-row page yields ~11 batches.
	order, batchRows := readAllFluxx(t, srv, source.ReadOptions{PageSize: 1_000_000, MaxBatchBytes: 2_000})

	require.Equal(t, wantOrder(total), order, "the single page's rows survive intact and in order")
	require.Equal(t, total, sumInts(batchRows))
	require.Greater(t, len(batchRows), 8, "one page split into several byte-bounded batches")
	require.LessOrEqual(t, maxInt(batchRows), 12, "each batch cut at a row boundary near the cap")
}

// A batch can exceed one page: the accumulator is not reset at page boundaries.
func TestFluxxReadResource_BatchSpansPageBoundary(t *testing.T) {
	const total, payload = 400, 200 // pages are 100 rows (apiPageSize=100)
	srv := fluxxTestServer(t, total, payload)
	defer srv.Close()

	// ~225 raw JSON bytes/row, 40 KB cap => ~180 rows/batch — wider than one page.
	order, batchRows := readAllFluxx(t, srv, source.ReadOptions{PageSize: 1_000_000, MaxBatchBytes: 40_000})

	require.Equal(t, wantOrder(total), order)
	require.Equal(t, total, sumInts(batchRows))
	require.Greater(t, maxInt(batchRows), 100, "a batch exceeds one 100-row page => it accumulated across pages")
}

// Rows are sized individually, so a fat row flushes alone and tiny rows group.
func TestFluxxReadResource_PerRowSizeDrivesFlush(t *testing.T) {
	sizes := []int{5000, 5, 5, 5, 5} // row 0 huge, rest tiny
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		items := []interface{}{}
		if page == 1 {
			for i, sz := range sizes {
				items = append(items, map[string]interface{}{"id": strconv.Itoa(i), "payload": strings.Repeat("x", sz)})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"records":  map[string]interface{}{"grant": items},
			"per_page": 100,
		})
	}))
	defer srv.Close()

	order, batchRows := readAllFluxx(t, srv, source.ReadOptions{PageSize: 1_000_000, MaxBatchBytes: 2000})
	require.Equal(t, wantOrder(len(sizes)), order)
	require.Equal(t, len(sizes), sumInts(batchRows))
	require.Equal(t, []int{1, 4}, batchRows, "oversized row flushes alone; tiny rows group — sized per-row, not averaged")
}

// The bytes the flush counts match the rows that land in each emitted batch,
// and each batch is cut exactly at the cap crossing (full batches reach the
// cap; none overshoot by more than one row).
func TestFluxxReadResource_ByteAccountingMatchesEmittedBatches(t *testing.T) {
	const total, payload = 500, 500
	capBytes := int64(20000)
	srv := fluxxTestServer(t, total, payload)
	defer srv.Close()

	s := &FluxxSource{client: httpclient.New(httpclient.WithBaseURL(srv.URL)), accessToken: "test"}
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeString},
		{Name: "payload", DataType: schema.TypeString},
	}
	results := make(chan source.RecordBatchResult, 64)
	go func() {
		defer close(results)
		require.NoError(t, s.readResource(context.Background(), "grants", "grants", "test", cols, nil, results,
			source.ReadOptions{PageSize: 1_000_000, MaxBatchBytes: capBytes}))
	}()

	var order []string
	var perBatchBytes []int64
	var maxRow int64
	for r := range results {
		require.NoError(t, r.Err)
		if r.Batch == nil {
			continue
		}
		idCol := r.Batch.Column(0).(*array.String)
		plCol := r.Batch.Column(1).(*array.String)
		var batchBytes int64
		for i := 0; i < idCol.Len(); i++ {
			order = append(order, idCol.Value(i))
			// Reconstruct the exact bytes the flush counted for this row.
			raw, err := json.Marshal(map[string]interface{}{"id": idCol.Value(i), "payload": plCol.Value(i)})
			require.NoError(t, err)
			batchBytes += int64(len(raw))
			if int64(len(raw)) > maxRow {
				maxRow = int64(len(raw))
			}
		}
		perBatchBytes = append(perBatchBytes, batchBytes)
		r.Batch.Release()
	}

	require.Equal(t, wantOrder(total), order, "no row lost, duplicated, or reordered")
	require.Greater(t, len(perBatchBytes), 1, "should split into multiple batches")
	for i, b := range perBatchBytes {
		// We flush before adding a row that would exceed the cap, so no batch
		// ever exceeds it, and each full batch is within one row of the cap.
		require.LessOrEqual(t, b, capBytes, "batch %d exceeds the cap", i)
		if i < len(perBatchBytes)-1 {
			require.Greater(t, b, capBytes-maxRow, "non-final batch %d flushed too early", i)
		}
	}
}

// Data is paged on demand, not fetched all at once.
func TestFluxxReadResource_PagesOnDemand(t *testing.T) {
	const total = 550
	var requests int32
	pad := strings.Repeat("x", 20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		if page < 1 {
			page = 1
		}
		if perPage < 1 {
			perPage = 100
		}
		items := []interface{}{}
		for i := (page - 1) * perPage; i < page*perPage && i < total; i++ {
			items = append(items, map[string]interface{}{"id": strconv.Itoa(i), "payload": fmt.Sprintf("%s-%d", pad, i)})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"records":  map[string]interface{}{"grant": items},
			"per_page": perPage,
		})
	}))
	defer srv.Close()

	order, _ := readAllFluxx(t, srv, source.ReadOptions{PageSize: 1_000_000, MaxBatchBytes: 0})
	require.Equal(t, wantOrder(total), order)
	// 550 rows at 100/page = 6 fetches (6th returns the last 50 and stops).
	require.Equal(t, int32(6), atomic.LoadInt32(&requests), "data is paged on demand, not downloaded at once")
}
