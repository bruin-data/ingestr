package cratedb

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"cratedb"},
		func() interface{} { return NewCrateDBSource() },
	)
}
