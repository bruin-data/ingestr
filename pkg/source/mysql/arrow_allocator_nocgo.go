//go:build !cgo

package mysql

import "github.com/apache/arrow-go/v18/arrow/memory"

const mysqlArrowBuffersOffHeap = false

func newMySQLArrowAllocator() memory.Allocator {
	return memory.NewGoAllocator()
}
