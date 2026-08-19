package deel

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"deel"},
		func() interface{} { return NewDeelSource() },
	)
}
