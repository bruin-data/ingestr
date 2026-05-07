package revenuecat

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"revenuecat"},
		func() interface{} { return NewRevenueCatSource() },
	)
}
