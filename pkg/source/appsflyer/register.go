package appsflyer

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"appsflyer"},
		func() interface{} { return NewAppsflyerSource() },
	)
}
