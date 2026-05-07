package applovinmax

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"applovinmax"},
		func() interface{} { return NewAppLovinMaxSource() },
	)
}
