package mongodb

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
)

type countingAllocator struct {
	memory.Allocator
	allocations int
}

func (a *countingAllocator) Allocate(size int) []byte {
	a.allocations++
	return a.Allocator.Allocate(size)
}

func TestRecyclingAllocatorReusesAndClearsBuffers(t *testing.T) {
	upstream := &countingAllocator{Allocator: memory.NewGoAllocator()}
	allocator := newRecyclingAllocator(upstream, 1024)

	buffer := allocator.Allocate(128)
	for i := range buffer {
		buffer[i] = 0xff
	}
	allocator.Free(buffer)

	reused := allocator.Allocate(128)
	if upstream.allocations != 1 {
		t.Fatalf("upstream allocations = %d, want 1", upstream.allocations)
	}
	for i, value := range reused {
		if value != 0 {
			t.Fatalf("reused buffer byte %d = %d, want 0", i, value)
		}
	}
}

func TestRecyclingAllocatorHonorsCacheLimit(t *testing.T) {
	upstream := &countingAllocator{Allocator: memory.NewGoAllocator()}
	allocator := newRecyclingAllocator(upstream, 128)

	allocator.Free(allocator.Allocate(128))
	allocator.Free(allocator.Allocate(64))
	allocator.Allocate(64)

	if upstream.allocations != 3 {
		t.Fatalf("upstream allocations = %d, want 3", upstream.allocations)
	}
}

func TestRecyclingAllocatorReallocatePreservesContents(t *testing.T) {
	upstream := &countingAllocator{Allocator: memory.NewGoAllocator()}
	allocator := newRecyclingAllocator(upstream, 1024)

	buffer := allocator.Allocate(64)
	copy(buffer, []byte("mongodb"))
	resized := allocator.Reallocate(128, buffer)

	if got := string(resized[:7]); got != "mongodb" {
		t.Fatalf("reallocated contents = %q, want %q", got, "mongodb")
	}
	if upstream.allocations != 2 {
		t.Fatalf("upstream allocations = %d, want 2", upstream.allocations)
	}

	allocator.Allocate(64)
	if upstream.allocations != 2 {
		t.Fatalf("upstream allocations after reuse = %d, want 2", upstream.allocations)
	}
}
