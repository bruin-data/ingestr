package hubspot

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"hubspot"},
		func() interface{} { return NewHubSpotSource() },
	)
}
