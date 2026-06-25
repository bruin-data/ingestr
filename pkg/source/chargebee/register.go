package chargebee

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"chargebee"},
		func() interface{} { return NewChargebeeSource() },
	)
}
