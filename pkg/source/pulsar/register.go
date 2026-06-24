package pulsar

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"pulsar", "pulsar+ssl"},
		func() interface{} { return NewPulsarSource() },
	)
}
