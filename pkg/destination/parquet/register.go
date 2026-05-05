package parquet

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"parquet"},
		func() interface{} { return NewParquetDestination() },
	)
}
