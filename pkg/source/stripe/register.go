package stripe

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"stripe"},
		func() interface{} { return NewStripeSource() },
	)
}
