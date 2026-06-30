package starrocks

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"starrocks"},
		func() interface{} { return NewStarRocksSource() },
	)
}
