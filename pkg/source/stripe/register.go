package stripe

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"stripe"},
		func() interface{} { return NewStripeSource() },
	)
}
