package trino

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"trino"},
		func() interface{} { return NewTrinoSource() },
	)
}
