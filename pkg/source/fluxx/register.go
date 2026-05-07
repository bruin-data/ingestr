package fluxx

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"fluxx"},
		func() interface{} { return NewFluxxSource() },
	)
}
