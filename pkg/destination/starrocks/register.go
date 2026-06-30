package starrocks

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"starrocks"},
		func() interface{} { return NewStarRocksDestination() },
	)
}
