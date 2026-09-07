package hubspot

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"hubspot"},
		func() interface{} { return NewHubSpotDestination() },
	)
}
