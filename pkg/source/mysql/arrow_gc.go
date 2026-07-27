package mysql

import (
	"os"
	"runtime/debug"
	"sync"
)

var mysqlArrowGCState struct {
	sync.Mutex
	readers  int
	previous int
}

func beginMySQLArrowGCOptimization() func() {
	if !mysqlArrowBuffersOffHeap {
		return func() {}
	}
	if _, configured := os.LookupEnv("GOGC"); configured {
		return func() {}
	}

	mysqlArrowGCState.Lock()
	if mysqlArrowGCState.readers == 0 {
		mysqlArrowGCState.previous = debug.SetGCPercent(200)
	}
	mysqlArrowGCState.readers++
	mysqlArrowGCState.Unlock()

	return func() {
		mysqlArrowGCState.Lock()
		mysqlArrowGCState.readers--
		if mysqlArrowGCState.readers == 0 {
			debug.SetGCPercent(mysqlArrowGCState.previous)
		}
		mysqlArrowGCState.Unlock()
	}
}
