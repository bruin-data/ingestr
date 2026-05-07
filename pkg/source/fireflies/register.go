package fireflies

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"fireflies"},
		func() interface{} { return NewFirefliesSource() },
	)
}
