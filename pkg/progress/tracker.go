package progress

import (
	"context"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
)

// DefaultTracker is the default implementation of the Tracker interface.
// It combines metrics collection, display, and channel wrapping.
type DefaultTracker struct {
	collector *MetricsCollector
	display   Display
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewTracker creates a new progress tracker with the specified display implementation.
func NewTracker(display Display) (Tracker, error) {
	collector, err := NewMetricsCollector()
	if err != nil {
		return nil, err
	}

	return &DefaultTracker{
		collector: collector,
		display:   display,
	}, nil
}

// Start begins progress tracking and starts the display updates.
func (t *DefaultTracker) Start(ctx context.Context) {
	t.ctx, t.cancel = context.WithCancel(ctx)
	t.display.Start(t.ctx, t.collector)
	config.Debug("[PROGRESS] Tracker started")
}

// Stop halts progress tracking and displays the final summary.
func (t *DefaultTracker) Stop(err error) {
	if t.cancel != nil {
		t.cancel()
	}

	metrics := t.collector.Snapshot()
	t.display.Stop(metrics, err)
	config.Debug("[PROGRESS] Tracker stopped")
}

// GetMetrics returns the current metrics snapshot.
func (t *DefaultTracker) GetMetrics() Metrics {
	return t.collector.Snapshot()
}

// Wrap creates a transparent wrapper around a RecordBatchResult channel.
// The wrapper intercepts batches, records metrics, and forwards batches unchanged.
// This is the core of the message-bus architecture - a single goroutine sees every batch exactly once.
func (t *DefaultTracker) Wrap(ch <-chan source.RecordBatchResult) <-chan source.RecordBatchResult {
	// Create wrapped channel with same capacity as source channel
	capacity := cap(ch)
	wrapped := make(chan source.RecordBatchResult, capacity)

	config.Debug("[PROGRESS] Wrapping channel with capacity %d", capacity)

	// Single goroutine intercepts all batches
	go func() {
		defer close(wrapped)

		for result := range ch {
			// Record metrics if this is a valid batch (no error, non-nil)
			if result.Err == nil && result.Batch != nil {
				numRows := result.Batch.NumRows()
				t.collector.RecordBatch(int64(numRows))
			}

			// Forward the result unchanged to the destination
			wrapped <- result
		}

		config.Debug("[PROGRESS] Channel wrapper completed")
	}()

	return wrapped
}
