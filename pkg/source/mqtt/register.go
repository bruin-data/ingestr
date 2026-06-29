package mqtt

import "github.com/bruin-data/ingestr/internal/registry"

// internal/registry/imports/mqtt.go provides the versioned blank import that
// activates this registration outside generated Makefile builds.
func init() {
	registry.RegisterSource(
		[]string{"mqtt", "mqtts"},
		func() interface{} { return NewMQTTSource() },
	)
}
