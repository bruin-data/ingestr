package mailchimp

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mailchimp"},
		func() interface{} { return NewMailchimpSource() },
	)
}
