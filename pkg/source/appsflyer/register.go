package appsflyer

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"appsflyer"},
		func() interface{} { return NewAppsflyerSource() },
	)
}
