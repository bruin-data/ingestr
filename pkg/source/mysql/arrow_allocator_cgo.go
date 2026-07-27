//go:build cgo

package mysql

import (
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/memory/mallocator"
)

const mysqlArrowBuffersOffHeap = true

func newMySQLArrowAllocator() memory.Allocator {
	return mallocator.NewMallocator()
}
