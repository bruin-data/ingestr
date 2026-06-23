package nats

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"nats", "tls", "ws", "wss"},
		func() interface{} { return NewNATSSource() },
	)
}
