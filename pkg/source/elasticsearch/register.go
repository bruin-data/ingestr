package elasticsearch

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"elasticsearch"},
		func() interface{} { return NewElasticsearchSource() },
	)
}
