package fireflies

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"fireflies"},
		func() interface{} { return NewFirefliesSource() },
	)
}
