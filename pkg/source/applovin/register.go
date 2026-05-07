package applovin

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"applovin"},
		func() interface{} { return NewAppLovinSource() },
	)
}
