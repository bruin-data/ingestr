package revenuecat

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"revenuecat"},
		func() interface{} { return NewRevenueCatSource() },
	)
}
