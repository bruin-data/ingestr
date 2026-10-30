package privatus

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"privatus"},
		func() interface{} { return NewPrivatusSource() },
	)
}
