package manifold

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"manifold"},
		func() interface{} { return NewSource() },
	)
}
