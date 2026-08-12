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
	// Each row's payload alone exceeds the byte limit, so every row flushes on its own.
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

// The working set stays bounded no matter how large the total volume is: the
// source flushes and releases each batch as it pages, rather than accumulating
// everything and splitting at the end. A large total therefore yields many
// small batches, never one huge one.
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

// A single fetched page (100 rows in one HTTP request) is cut into several
// batches when it exceeds the byte cap. The cut is at row boundaries as the
// accumulator fills — the page is not "split in the middle" after the fact.
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

// The accumulator carries across page boundaries: a page never forces a new
// batch. Proven by producing a batch LARGER than a single 100-row page, which
// is only possible if rows from consecutive pages accumulated into one batch.
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

// Each row is sized by its own raw JSON length, so inter-row size differences
// are captured (not averaged): one oversized row flushes alone, while the small
// rows that follow group into a single batch.
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

// The source fetches page by page on demand (100 rows/request); it never
// downloads the whole dataset up front. Proven by the request count matching
// the page count.
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
