package applovinmax

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"applovinmax"},
		func() interface{} { return NewAppLovinMaxSource() },
	)
}
