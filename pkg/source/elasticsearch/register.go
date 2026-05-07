package elasticsearch

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"elasticsearch"},
		func() interface{} { return NewElasticsearchSource() },
	)
}
