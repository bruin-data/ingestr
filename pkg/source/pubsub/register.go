package pubsub

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"pubsub"},
		func() interface{} { return NewPubSubSource() },
	)
}
