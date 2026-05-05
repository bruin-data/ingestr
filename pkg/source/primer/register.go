package primer

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"primer"},
		func() interface{} { return NewPrimerSource() },
	)
}
