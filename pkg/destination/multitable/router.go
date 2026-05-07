package multitable

import (
	"context"
	"sync"

	"github.com/bruin-data/gong/pkg/source"
)

// Router distributes batches from a single input channel to per-table channels.
// Each table gets its own buffered channel, allowing concurrent consumers.
type Router struct {
	tableChannels map[string]chan source.RecordBatchResult
	bufferSize    int
	mu            sync.RWMutex
	started       bool
	done          chan struct{}
	err           error
	errMu         sync.RWMutex
}

// NewRouter creates a router for the specified tables.
func NewRouter(tables []string, bufferSize int) *Router {
	if bufferSize <= 0 {
		bufferSize = 8
	}

	r := &Router{
		tableChannels: make(map[string]chan source.RecordBatchResult),
		bufferSize:    bufferSize,
		done:          make(chan struct{}),
	}

	for _, table := range tables {
		r.tableChannels[table] = make(chan source.RecordBatchResult, bufferSize)
	}

	return r
}

// Route starts routing batches from the input channel to per-table channels.
// This method should be called once and runs asynchronously.
// It closes all table channels when the input channel is exhausted or an error occurs.
func (r *Router) Route(ctx context.Context, input <-chan source.RecordBatchResult) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.mu.Unlock()

	go func() {
		defer r.closeAllChannels()
		defer close(r.done)

		for {
			select {
			case <-ctx.Done():
				r.setError(ctx.Err())
				return
			case result, ok := <-input:
				if !ok {
					return
				}

				if result.Err != nil {
					r.setError(result.Err)
					r.broadcastError(result.Err)
					return
				}

				ch, ok := r.tableChannels[result.TableName]
				if !ok {
					continue
				}

				select {
				case ch <- result:
				case <-ctx.Done():
					r.setError(ctx.Err())
					return
				}
			}
		}
	}()
}

// GetChannel returns the channel for a specific table.
// Returns nil if the table is not registered.
func (r *Router) GetChannel(table string) <-chan source.RecordBatchResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tableChannels[table]
}

// Wait blocks until routing is complete.
func (r *Router) Wait() {
	<-r.done
}

// Err returns any error that occurred during routing.
func (r *Router) Err() error {
	r.errMu.RLock()
	defer r.errMu.RUnlock()
	return r.err
}

func (r *Router) setError(err error) {
	r.errMu.Lock()
	if r.err == nil {
		r.err = err
	}
	r.errMu.Unlock()
}

func (r *Router) broadcastError(err error) {
	for _, ch := range r.tableChannels {
		select {
		case ch <- source.RecordBatchResult{Err: err}:
		default:
		}
	}
}

func (r *Router) closeAllChannels() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ch := range r.tableChannels {
		close(ch)
	}
}
