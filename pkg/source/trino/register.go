package trino

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"trino"},
		func() interface{} { return NewTrinoSource() },
	)
}
