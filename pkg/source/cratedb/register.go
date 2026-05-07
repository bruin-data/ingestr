package cratedb

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"cratedb"},
		func() interface{} { return NewCrateDBSource() },
	)
}
