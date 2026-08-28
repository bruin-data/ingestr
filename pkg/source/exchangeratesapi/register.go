package exchangeratesapi

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"exchangeratesapi"},
		func() interface{} { return New() },
	)
}
