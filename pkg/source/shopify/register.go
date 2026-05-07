package shopify

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"shopify"},
		func() interface{} { return NewShopifySource() },
	)
}
