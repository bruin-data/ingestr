package mailchimp

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mailchimp"},
		func() interface{} { return NewMailchimpSource() },
	)
}
