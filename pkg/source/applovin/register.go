package applovin

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"applovin"},
		func() interface{} { return NewAppLovinSource() },
	)
}
