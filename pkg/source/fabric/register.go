package fabric

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"fabric"},
		func() interface{} { return NewFabricSource() },
	)
}
