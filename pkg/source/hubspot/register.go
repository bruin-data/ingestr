package hubspot

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"hubspot"},
		func() interface{} { return NewHubSpotSource() },
	)
}
