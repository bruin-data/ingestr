package cratedb

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"cratedb"},
		func() interface{} { return NewCrateDBDestination() },
	)
}
