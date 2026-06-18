package sendgrid

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"sendgrid"},
		func() interface{} { return NewSendGridSource() },
	)
}
