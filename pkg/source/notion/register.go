package notion

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"notion"},
		func() interface{} { return NewNotionSource() },
	)
}
