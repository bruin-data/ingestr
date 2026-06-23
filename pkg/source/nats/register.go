package nats

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"nats"},
		func() interface{} { return NewNATSSource() },
	)
}
