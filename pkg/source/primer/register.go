package primer

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"primer"},
		func() interface{} { return NewPrimerSource() },
	)
}
