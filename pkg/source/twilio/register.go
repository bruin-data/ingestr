package twilio

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"twilio"},
		func() interface{} { return NewTwilioSource() },
	)
}
