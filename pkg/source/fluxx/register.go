package fluxx

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"fluxx"},
		func() interface{} { return NewFluxxSource() },
	)
}
