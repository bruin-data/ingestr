package elasticsearch

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"elasticsearch"},
		func() interface{} { return NewElasticsearchDestination() },
	)
}
