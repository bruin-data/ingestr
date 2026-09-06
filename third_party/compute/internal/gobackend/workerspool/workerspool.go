// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package workerspool

import (
	"runtime"
	"sync"
	"sync/atomic"
)

type Pool struct {
	// maxParallelism is a soft target on the limit of parallel work to do.
	// The actual number of goroutines is higher than that -- because of waits and such.
	maxParallelism int
	mu             sync.Mutex
	cond           sync.Cond // Should be signaled whenever numRunning is decreased.
	numRunning     int

	// extraParallelism is temporarily increased when a worked goes to sleep.
	extraParallelism atomic.Int32
}

// New return a new Pool of workers with the default parallelism (2 * runtime.GOMAXPROCS()).
func New() *Pool {
	w := &Pool{}
	w.maxParallelism = 2 * runtime.GOMAXPROCS(0)
	w.cond = sync.Cond{L: &w.mu}
	return w
}

// IsEnabled returns whether parallelism is enabled (maxParallelism is != 0)
func (w *Pool) IsEnabled() bool {
	return w.maxParallelism != 0
}

// IsUnlimited returns whether parallelism is unlimited (maxParallelism < 0)
func (w *Pool) IsUnlimited() bool {
	return w.maxParallelism < 0
}

// MaxParallelism returns the soft-target for parallelism.
// If set to 0 parallelism is disabled.
// If set to -1 parallelism is unlimited.
func (w *Pool) MaxParallelism() int {
	return w.maxParallelism
}

// AdjustedMaxParallelism returns the adjusted soft-target for parallelism (>= 1).
//
// If the target is set to -1 (unlimited parallelism) it returns runtime.GOMAXPROCS.
// If the target is 0 (no parallelism) it returns 1.
//
// Also, it limits the number of workers to runtime.GOMAXPROCS.
func (w *Pool) AdjustedMaxParallelism() int {
	if w.maxParallelism < 0 {
		return runtime.GOMAXPROCS(0)
	}
	return min(max(w.maxParallelism, 1), runtime.GOMAXPROCS(0))
}

// SetMaxParallelism sets the maxParallelism.
//
// You should only change the parallelism before any workers start running. If changed during the execution
// the behavior is undefined.
func (w *Pool) SetMaxParallelism(maxParallelism int) {
	w.maxParallelism = maxParallelism
}

const goroutineToParallelismRatio = 2

// lockedIsFull returns whether all available workers are in use.
//
// It must be called with workerPool.mu acquired.
func (w *Pool) lockedIsFull() bool {
	if w.maxParallelism == 0 {
		return true
	} else if w.maxParallelism < 0 {
		return false
	}
	return w.numRunning >= w.maxParallelism+int(w.extraParallelism.Load())
}

// WaitToStart waits until there is a worker available to run the task.
//
// If parallelism is disabled (maxParallelism is 0), it runs the task inline and returns when it is finished.
// This is risky if one is relying on concurrency, and it can lead to deadlocks.
// Avoid using this function if the parallelism is disabled.
func (w *Pool) WaitToStart(task func()) {
	if w.IsUnlimited() {
		go task()
		return

	} else if w.maxParallelism == 0 {
		// No parallelism, run inline -- better avoided.
		task()
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	for w.lockedIsFull() {
		w.cond.Wait()
	}
	w.lockedRunTaskInGoroutine(task)
}

// lockedRunTaskInGoroutine and keep tabs on w.numRunning.
//
// It must be called with workerPool.mu acquired.
func (w *Pool) lockedRunTaskInGoroutine(task func()) {
	w.numRunning++
	go func() {
		task()
		w.mu.Lock()
		w.numRunning--
		w.cond.Signal()
		w.mu.Unlock()
	}()
}

// StartIfAvailable runs the task in a separate goroutine, if there are enough workers left.
// It returns true if it found workers to run the function, false otherwise.
//
// It's up to the client to synchronize the end of the function execution.
func (w *Pool) StartIfAvailable(task func()) bool {
	if w.IsUnlimited() {
		go task()
		return true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.lockedIsFull() {
		return false
	}
	w.lockedRunTaskInGoroutine(task)
	return true
}

// Saturate fans out as many workers as available, each running the given task.
// It keeps spawning workers if more workers become available.
//
// Each task is expected to keep doing work until there nothing else to be done.
// As soon as any task finishes, it indicates there is no more need for more task, and it
// immediately stops spawning new tasks.
//
// It returns when all started tasks have finished.
func (w *Pool) Saturate(task func()) {
	if w.maxParallelism == 0 {
		task()
		return
	}

	limit := w.maxParallelism
	if limit < 0 {
		limit = runtime.GOMAXPROCS(0)
	}

	var wg sync.WaitGroup
	var doneFanningOut atomic.Bool

	w.mu.Lock()
	started := 0

	for !doneFanningOut.Load() {
		// Check global limits and local limits (for unlimited pool)
		if (w.IsUnlimited() && started >= limit) || (!w.IsUnlimited() && w.lockedIsFull()) {
			w.cond.Wait()
			// If we woke up because a task finished (signaled), we need to check if we are done.
			if doneFanningOut.Load() {
				w.cond.Signal() // Propagate signal to other potential waiters
				break
			}
			continue
		}

		started++
		wg.Add(1)
		w.lockedRunTaskInGoroutine(func() {
			defer wg.Done()
			task()
			doneFanningOut.Store(true)
		})
	}
	w.mu.Unlock()
	wg.Wait()
}

// WorkerIsAsleep indicates the worker (the one that called the method) is going to sleep waiting
// for other workers, and temporarily increases the available number of workers.
//
// Call WorkerRestarted when the worker is ready to run again.
func (w *Pool) WorkerIsAsleep() {
	w.extraParallelism.Add(1)
}

// WorkerRestarted indicates the worker (the one that called the method) is ready to run again.
// It should only be called after WorkerIsAsleep.
//
// It returns the temporary number of extra available workers.
func (w *Pool) WorkerRestarted() {
	w.extraParallelism.Add(-1)
}
