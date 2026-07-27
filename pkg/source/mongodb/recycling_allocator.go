package mongodb

import (
	"sync"

	"github.com/apache/arrow-go/v18/arrow/memory"
)

const mongoArrowBufferCacheSize = 32 << 20

type recyclingAllocator struct {
	upstream       memory.Allocator
	maxCachedBytes int

	mu          sync.Mutex
	free        map[int][][]byte
	cachedBytes int
}

func newRecyclingAllocator(upstream memory.Allocator, maxCachedBytes int) *recyclingAllocator {
	return &recyclingAllocator{
		upstream:       upstream,
		maxCachedBytes: maxCachedBytes,
		free:           make(map[int][][]byte),
	}
}

func (a *recyclingAllocator) Allocate(size int) []byte {
	a.mu.Lock()
	buffers := a.free[size]
	if len(buffers) > 0 {
		buffer := buffers[len(buffers)-1]
		if len(buffers) == 1 {
			delete(a.free, size)
		} else {
			a.free[size] = buffers[:len(buffers)-1]
		}
		a.cachedBytes -= cap(buffer)
		a.mu.Unlock()
		clear(buffer)
		return buffer
	}
	a.mu.Unlock()

	return a.upstream.Allocate(size)
}

func (a *recyclingAllocator) Reallocate(size int, buffer []byte) []byte {
	if cap(buffer) >= size {
		if size > len(buffer) {
			clear(buffer[len(buffer):size])
		}
		return buffer[:size]
	}

	resized := a.Allocate(size)
	copy(resized, buffer)
	a.Free(buffer)
	return resized
}

func (a *recyclingAllocator) Free(buffer []byte) {
	if len(buffer) == 0 || cap(buffer) > a.maxCachedBytes {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cachedBytes+cap(buffer) > a.maxCachedBytes {
		return
	}

	a.free[len(buffer)] = append(a.free[len(buffer)], buffer)
	a.cachedBytes += cap(buffer)
}

var _ memory.Allocator = (*recyclingAllocator)(nil)
