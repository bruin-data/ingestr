package mqtt

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mqtt", "mqtts"},
		func() interface{} { return NewMQTTSource() },
	)
}
