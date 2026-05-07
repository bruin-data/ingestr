package notion

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"notion"},
		func() interface{} { return NewNotionSource() },
	)
}
