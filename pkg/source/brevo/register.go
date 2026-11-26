package brevo

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"brevo"},
		func() interface{} { return NewBrevoSource() },
	)
}
