package typeform

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"typeform"},
		func() interface{} { return NewTypeformSource() },
	)
}
