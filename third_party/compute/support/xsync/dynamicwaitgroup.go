// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package xsync

import (
	"sync"

	"github.com/pkg/errors"
)

// DynamicWaitGroup is a WaitGroup-like synchronization primitive that allows the count
// to be changed (new values added) while someone is waiting for it.
//
// It uses sync.Cond to coordinate changes.
type DynamicWaitGroup struct {
	mu    sync.Mutex
	cond  *sync.Cond
	count int64 // Internal counter for active goroutines
}

// NewDynamicWaitGroup creates a new DynamicWaitGroup.
func NewDynamicWaitGroup() *DynamicWaitGroup {
	cwg := &DynamicWaitGroup{}
	cwg.cond = sync.NewCond(&cwg.mu) // Initialize cond with its mutex
	return cwg
}

// Add changes the DynamicWaitGroup counter by the given delta.
// If the counter becomes zero, it broadcasts to all waiting goroutines.
// If the counter would go negative, it panics.
func (cwg *DynamicWaitGroup) Add(delta int) {
	cwg.mu.Lock()
	defer cwg.mu.Unlock()

	cwg.count += int64(delta)

	if cwg.count < 0 {
		// Standard WaitGroup panics if the counter goes negative.
		panic(errors.Errorf("DynamicWaitGroup: negative counter"))
	}

	// If the counter reaches zero, wake up all waiting goroutines.
	// This allows dynamic additions even if the count was momentarily zero.
	// Waiters will re-check the condition (count > 0) upon waking.
	if cwg.count == 0 {
		cwg.cond.Broadcast()
	}
}

// Done decrements the DynamicWaitGroup counter by one.
// This is a convenience wrapper around Add(-1).
func (cwg *DynamicWaitGroup) Done() {
	cwg.Add(-1)
}

// Wait blocks until the DynamicWaitGroup counter is zero.
func (cwg *DynamicWaitGroup) Wait() {
	cwg.mu.Lock()
	defer cwg.mu.Unlock()

	// Loop while the counter is greater than zero.
	// The loop is necessary because sync.Cond.Wait() can have spurious wakeups.
	for cwg.count > 0 {
		cwg.cond.Wait() // Atomically unlocks mu, waits, and re-locks mu on wakeup
	}
	// When the loop exits, cwg.count is guaranteed to be 0 (or less if panic was avoided).
}
