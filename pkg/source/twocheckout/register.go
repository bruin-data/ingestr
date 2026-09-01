package twocheckout

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		// `twocheckout`, not `2checkout`: RFC 3986 URI schemes must start with
		// a letter, and url.Parse rejects a leading digit.
		[]string{"twocheckout"},
		func() interface{} { return NewTwoCheckoutSource() },
	)
}
