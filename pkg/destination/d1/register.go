package d1

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination([]string{"d1", "cloudflare-d1"}, func() interface{} { return NewD1Destination() })
}
