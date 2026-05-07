package stripe

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
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

func TestChunkSizeForInterval(t *testing.T) {
	cases := []struct {
		name     string
		interval time.Duration
		want     time.Duration
	}{
		{"sub-hour", 30 * time.Minute, 3 * time.Minute},
		{"hour", time.Hour, 5 * time.Minute},
		{"day", 24 * time.Hour, time.Hour},
		{"week", 7 * 24 * time.Hour, 6 * time.Hour},
		{"quarter", 90 * 24 * time.Hour, 24 * time.Hour},
		{"year", 365 * 24 * time.Hour, 24 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chunkSizeForInterval(c.interval); got != c.want {
				t.Errorf("chunkSizeForInterval(%v) = %v, want %v", c.interval, got, c.want)
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
