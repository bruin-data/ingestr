//go:build stress

// Shared load generator for the CDC stress tests. A due-count scheduler emits
// operation tokens into a bounded queue at the configured rate — catching up
// after stalls instead of silently under-delivering like per-worker tickers —
// and a fixed pool of workers drains the queue, so the achieved throughput
// tracks the target as closely as the source engine allows. Every scheduled
// operation is accounted for as enqueued or explicitly dropped, and statement
// latency is bucketed so a slow source is visible in the logs.
package integration

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// stressPKMoveOffset is the shared offset used by every engine's primary-key
// move operation: rows with id below the offset are moved above it, so moved
// rows and their tombstones can be told apart during verification.
const stressPKMoveOffset = int64(1_000_000_000)

type cdcStressOpErrBox struct{ err error }

// cdcStressOpFunc performs one mutation against the source and reports the
// operation kind ("insert", "update", "delete", "pk-update"), the rows
// affected, and any error.
type cdcStressOpFunc func(rng *rand.Rand) (operation string, affected int64, err error)

type cdcStressLoadGen struct {
	t        *testing.T
	workers  int
	target   int
	duration time.Duration

	queueCapacity int
	queue         chan struct{}
	started       time.Time

	scheduled, enqueued, dropped, attempted atomic.Int64
	inserts, updates, deletes, pkUpdates    atomic.Int64
	opErrors, statementErrors, zeroAffected atomic.Int64
	inFlight, maxQueueDepth                 atomic.Int64
	totalLatencyNanos, maxLatencyNanos      atomic.Int64
	latencyBuckets                          [7]atomic.Int64
	// Boxed so atomic.Value always stores one concrete type; storing raw error
	// values panics as soon as two distinct error types are recorded.
	firstOpErr atomic.Value
}

func newCDCStressLoadGen(t *testing.T, workers, targetOpsPerSec int, duration time.Duration) *cdcStressLoadGen {
	queueCapacity := max(workers*8, targetOpsPerSec/4)
	return &cdcStressLoadGen{
		t:             t,
		workers:       workers,
		target:        targetOpsPerSec,
		duration:      duration,
		queueCapacity: queueCapacity,
		queue:         make(chan struct{}, queueCapacity),
	}
}

// recordOpErr is shared with the late-table and DDL goroutines so every load
// failure funnels into the same counters the final verification checks.
func (g *cdcStressLoadGen) recordOpErr(err error) {
	g.opErrors.Add(1)
	g.firstOpErr.CompareAndSwap(nil, cdcStressOpErrBox{err: err})
	g.t.Logf("load op error: %v", err)
}

func (g *cdcStressLoadGen) recordLatency(elapsed time.Duration) {
	nanos := elapsed.Nanoseconds()
	g.totalLatencyNanos.Add(nanos)
	for {
		current := g.maxLatencyNanos.Load()
		if nanos <= current || g.maxLatencyNanos.CompareAndSwap(current, nanos) {
			break
		}
	}
	bucket := 6
	switch {
	case elapsed < time.Millisecond:
		bucket = 0
	case elapsed < 5*time.Millisecond:
		bucket = 1
	case elapsed < 10*time.Millisecond:
		bucket = 2
	case elapsed < 50*time.Millisecond:
		bucket = 3
	case elapsed < 100*time.Millisecond:
		bucket = 4
	case elapsed < 500*time.Millisecond:
		bucket = 5
	}
	g.latencyBuckets[bucket].Add(1)
}

// start launches the scheduler and worker pool on wg. The scheduler stops and
// closes the queue when loadCtx is done; workers exit once the queue drains,
// so wg.Wait() observes a fully settled source.
func (g *cdcStressLoadGen) start(loadCtx context.Context, wg *sync.WaitGroup, doOp cdcStressOpFunc) {
	g.started = time.Now()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(g.queue)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		var emitted int64
		emitDue := func(elapsed time.Duration) {
			if elapsed > g.duration {
				elapsed = g.duration
			}
			due := int64(elapsed) * int64(g.target) / int64(time.Second)
			for emitted < due {
				emitted++
				g.scheduled.Add(1)
				select {
				case g.queue <- struct{}{}:
					g.enqueued.Add(1)
					depth := int64(len(g.queue))
					for {
						current := g.maxQueueDepth.Load()
						if depth <= current || g.maxQueueDepth.CompareAndSwap(current, depth) {
							break
						}
					}
				default:
					g.dropped.Add(1)
				}
			}
		}
		for {
			select {
			case <-loadCtx.Done():
				emitDue(g.duration)
				return
			case <-ticker.C:
				emitDue(time.Since(g.started))
			}
		}
	}()

	for w := 0; w < g.workers; w++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for range g.queue {
				g.attempted.Add(1)
				g.inFlight.Add(1)
				operationStarted := time.Now()
				operation, affected, err := doOp(rng)
				if err != nil {
					g.statementErrors.Add(1)
					g.recordOpErr(err)
				} else {
					switch operation {
					case "insert":
						g.inserts.Add(affected)
					case "update":
						g.updates.Add(affected)
					case "delete":
						g.deletes.Add(affected)
					case "pk-update":
						g.pkUpdates.Add(affected)
					}
					if affected == 0 {
						g.zeroAffected.Add(1)
					}
				}
				g.recordLatency(time.Since(operationStarted))
				g.inFlight.Add(-1)
			}
		}(int64(w + 1))
	}
}

func (g *cdcStressLoadGen) effectiveOps() int64 {
	return g.inserts.Load() + g.updates.Load() + g.deletes.Load() + g.pkUpdates.Load()
}

// status renders one progress line for the periodic status ticker.
func (g *cdcStressLoadGen) status() string {
	elapsed := time.Since(g.started)
	elapsedSeconds := elapsed.Seconds()
	effective := g.effectiveOps()
	completed := g.attempted.Load() - g.inFlight.Load()
	averageLatency := time.Duration(0)
	if completed > 0 {
		averageLatency = time.Duration(g.totalLatencyNanos.Load() / completed)
	}
	return fmt.Sprintf("scheduled=%d attempted=%d (%.0f/sec), effective=%d (%.0f/sec), dropped=%d, queue=%d/%d (max=%d), latency avg=%v max=%v; mutations=%d/%d/%d/%d, errors=%d",
		g.scheduled.Load(), g.attempted.Load(), float64(g.attempted.Load())/elapsedSeconds,
		effective, float64(effective)/elapsedSeconds, g.dropped.Load(), len(g.queue), g.queueCapacity, g.maxQueueDepth.Load(),
		averageLatency.Round(time.Microsecond), time.Duration(g.maxLatencyNanos.Load()).Round(time.Microsecond),
		g.inserts.Load(), g.updates.Load(), g.deletes.Load(), g.pkUpdates.Load(), g.opErrors.Load())
}

// verify asserts the scheduler accounting is airtight, that no operation
// failed, and that the achieved rate approximates the target closely enough
// for the run to be meaningful. Returns the achieved effective ops/sec.
func (g *cdcStressLoadGen) verify() float64 {
	t := g.t
	t.Helper()
	if n := g.opErrors.Load(); n > 0 {
		first, _ := g.firstOpErr.Load().(cdcStressOpErrBox)
		t.Fatalf("%d load operations failed, first error: %v", n, first.err)
	}
	require.Equal(t, g.scheduled.Load(), g.enqueued.Load()+g.dropped.Load(),
		"every scheduled operation must be enqueued or explicitly dropped")
	require.Equal(t, g.enqueued.Load(), g.attempted.Load(), "workers must drain every enqueued operation")

	totalOps := g.effectiveOps()
	achieved := float64(totalOps) / g.duration.Seconds()
	attemptedRate := float64(g.attempted.Load()) / g.duration.Seconds()
	dropPercent := float64(0)
	if g.scheduled.Load() > 0 {
		dropPercent = 100 * float64(g.dropped.Load()) / float64(g.scheduled.Load())
	}
	averageLatency := time.Duration(0)
	if g.attempted.Load() > 0 {
		averageLatency = time.Duration(g.totalLatencyNanos.Load() / g.attempted.Load())
	}
	t.Logf("load complete: scheduled=%d, enqueued/attempted=%d (%.0f/sec), dropped=%d (%.2f%%), no-op=%d, statement errors=%d, max queue=%d/%d",
		g.scheduled.Load(), g.attempted.Load(), attemptedRate, g.dropped.Load(), dropPercent,
		g.zeroAffected.Load(), g.statementErrors.Load(), g.maxQueueDepth.Load(), g.queueCapacity)
	t.Logf("effective mutations: %d (%d inserts, %d updates, %d deletes, %d pk-updates), %.0f/sec; latency avg=%v max=%v",
		totalOps, g.inserts.Load(), g.updates.Load(), g.deletes.Load(), g.pkUpdates.Load(), achieved,
		averageLatency.Round(time.Microsecond), time.Duration(g.maxLatencyNanos.Load()).Round(time.Microsecond))
	t.Logf("statement latency buckets: <1ms=%d, 1-5ms=%d, 5-10ms=%d, 10-50ms=%d, 50-100ms=%d, 100-500ms=%d, >=500ms=%d",
		g.latencyBuckets[0].Load(), g.latencyBuckets[1].Load(), g.latencyBuckets[2].Load(), g.latencyBuckets[3].Load(),
		g.latencyBuckets[4].Load(), g.latencyBuckets[5].Load(), g.latencyBuckets[6].Load())
	require.GreaterOrEqual(t, achieved, float64(g.target)/2,
		"load generator could not sustain enough pressure for the test to be meaningful")
	require.Positive(t, g.pkUpdates.Load(), "workload should execute real primary-key updates")
	require.Positive(t, g.deletes.Load(), "workload should execute real deletes")
	return achieved
}
