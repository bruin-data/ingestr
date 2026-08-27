package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	stripego "github.com/stripe/stripe-go/v81"
)

// parallelFetch mimics the readTableFromEvents worker pool logic
// so we can test cancellation and limit behavior without hitting Stripe.
func parallelFetch(ctx context.Context, ids map[string]bool, limit int) ([]string, error) {
	const fetchWorkers = 5
	fetchCtx, cancelFetch := context.WithCancel(ctx)
	defer cancelFetch()

	objChan := make(chan string, fetchWorkers)
	sem := make(chan struct{}, fetchWorkers)
	var wg sync.WaitGroup

	go func() {
		defer func() {
			wg.Wait()
			close(objChan)
		}()
		for id := range ids {
			select {
			case <-fetchCtx.Done():
				return
			case sem <- struct{}{}:
			}

			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				defer func() { <-sem }()

				select {
				case <-fetchCtx.Done():
					return
				default:
				}

				time.Sleep(10 * time.Millisecond) // simulate API call
				select {
				case objChan <- id:
				case <-fetchCtx.Done():
				}
			}(id)
		}
	}()

	var results []string
	for obj := range objChan {
		results = append(results, obj)
		if limit > 0 && len(results) >= limit {
			return results, nil
		}
	}
	return results, nil
}

func TestParallelFetch_ContextCancel_NoLeak(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		ids[fmt.Sprintf("id_%d", i)] = true
	}

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	parallelFetch(ctx, ids, 0) //nolint:errcheck

	// Give goroutines time to clean up
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()

	leaked := after - before
	if leaked > 2 {
		t.Errorf("goroutine leak: %d goroutines before, %d after (leaked %d)", before, after, leaked)
	}
}

func TestParallelFetch_Limit_NoLeak(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		ids[fmt.Sprintf("id_%d", i)] = true
	}

	before := runtime.NumGoroutine()

	results, _ := parallelFetch(context.Background(), ids, 10)

	if len(results) < 10 {
		t.Errorf("expected at least 10 results, got %d", len(results))
	}

	// Give goroutines time to clean up
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()

	leaked := after - before
	if leaked > 2 {
		t.Errorf("goroutine leak: %d goroutines before, %d after (leaked %d)", before, after, leaked)
	}
}

func TestParallelFetch_AllItems_NoCancel(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 50; i++ {
		ids[fmt.Sprintf("id_%d", i)] = true
	}

	results, err := parallelFetch(context.Background(), ids, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 50 {
		t.Errorf("expected 50 results, got %d", len(results))
	}
}

func TestChunkSizeForParallelism(t *testing.T) {
	cases := []struct {
		name     string
		interval time.Duration
		workers  int
		want     time.Duration
	}{
		{"default workers", 30 * 24 * time.Hour, 5, 3 * 24 * time.Hour},
		{"more workers", 30 * 24 * time.Hour, 20, 18 * time.Hour},
		{"chunk cap", 30 * 24 * time.Hour, 120, 5*time.Hour + 37*time.Minute + 30*time.Second},
		{"fallback workers", 20 * time.Hour, 0, time.Hour},
		{"second floor", time.Second, 10, time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chunkSizeForParallelism(c.interval, c.workers); got != c.want {
				t.Errorf("chunkSizeForParallelism(%v, %d) = %v, want %v", c.interval, c.workers, got, c.want)
			}
		})
	}
}

func TestChunkTimeRange_CoversFullRange(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(25 * time.Hour)

	chunks := chunkTimeRange(start, end, time.Hour)

	if len(chunks) != 25 {
		t.Fatalf("expected 25 chunks, got %d", len(chunks))
	}
	if !chunks[0].start.Equal(start) {
		t.Errorf("first chunk start = %s, want %s", chunks[0].start, start)
	}
	if !chunks[len(chunks)-1].end.Equal(end) {
		t.Errorf("last chunk end = %s, want %s", chunks[len(chunks)-1].end, end)
	}
	for i := 1; i < len(chunks); i++ {
		if !chunks[i].start.Equal(chunks[i-1].end) {
			t.Errorf("gap between chunk %d and %d: %s != %s", i-1, i, chunks[i-1].end, chunks[i].start)
		}
	}
}

func TestChunkTimeRange_LastChunkTruncated(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(2*time.Hour + 15*time.Minute)

	chunks := chunkTimeRange(start, end, time.Hour)

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	last := chunks[len(chunks)-1]
	if last.end.Sub(last.start) != 15*time.Minute {
		t.Errorf("last chunk duration = %v, want 15m", last.end.Sub(last.start))
	}
	if !last.end.Equal(end) {
		t.Errorf("last chunk end = %s, want %s", last.end, end)
	}
}

func TestChunkTimeRange_ChunkBiggerThanInterval(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)

	chunks := chunkTimeRange(start, end, time.Hour)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !chunks[0].start.Equal(start) || !chunks[0].end.Equal(end) {
		t.Errorf("expected [%s, %s], got [%s, %s]", start, end, chunks[0].start, chunks[0].end)
	}
}

func TestChunkTimeRange_ZeroChunkSize(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	chunks := chunkTimeRange(start, end, 0)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 fallback chunk, got %d", len(chunks))
	}
}

// TestStripeByteCap proves the MaxBatchBytes flush in readTableFromEvents (the
// events-based path used by tables with an eventTypeFilter, e.g. "charge"). A
// single page of change events fans out to padded object fetches; with the cap
// off they land in one batch, with a small cap they split, with no row loss.
func TestStripeByteCap(t *testing.T) {
	const mockRows = 50
	wide := strings.Repeat("x", 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/events":
			// One page of charge.* events, each pointing at a unique charge id.
			data := make([]map[string]interface{}, 0, mockRows)
			for i := 0; i < mockRows; i++ {
				data = append(data, map[string]interface{}{
					"id":      "evt_" + strconv.Itoa(i),
					"object":  "event",
					"type":    "charge.updated",
					"created": time.Now().Unix(),
					"data": map[string]interface{}{
						"object": map[string]interface{}{
							"id":     "ch_" + strconv.Itoa(i),
							"object": "charge",
						},
					},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"object":   "list",
				"url":      "/v1/events",
				"has_more": false,
				"data":     data,
			})
		case strings.HasPrefix(r.URL.Path, "/v1/charges/"):
			// Re-fetched charge object carries the padding that drives the byte cap.
			id := strings.TrimPrefix(r.URL.Path, "/v1/charges/")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":          id,
				"object":      "charge",
				"description": wide,
			})
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	originalKey := stripego.Key
	originalBackend := stripego.GetBackend(stripego.APIBackend)
	stripego.Key = "sk_test_bytecap"
	stripego.SetBackend(stripego.APIBackend, stripego.GetBackendWithConfig(stripego.APIBackend, &stripego.BackendConfig{
		URL:        stripego.String(srv.URL),
		HTTPClient: srv.Client(),
	}))
	t.Cleanup(func() {
		stripego.SetBackend(stripego.APIBackend, originalBackend)
		stripego.Key = originalKey
	})

	start := time.Now().Add(-24 * time.Hour)

	run := func(max int64) (int64, int64) {
		s := &StripeSource{}
		results := make(chan source.RecordBatchResult, 8)
		errCh := make(chan error, 1)
		go func() {
			err := s.readTableFromEvents(context.Background(), "charge", "charge.*", source.ReadOptions{MaxBatchBytes: max}, &start, nil, results)
			close(results)
			errCh <- err
		}()
		var batches, rows int64
		for res := range results {
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			batches++
			rows += res.Batch.NumRows()
			res.Batch.Release()
		}
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
		return batches, rows
	}

	offB, offR := run(0)
	onB, onR := run(4096)

	if offB != 1 {
		t.Fatalf("cap-off batches=%d want 1", offB)
	}
	if onB <= 1 {
		t.Fatalf("cap-on batches=%d want >1", onB)
	}
	if offR != onR || offR != mockRows {
		t.Fatalf("row mismatch off=%d on=%d want %d", offR, onR, mockRows)
	}
}
