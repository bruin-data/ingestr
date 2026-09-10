package fakturoid

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"fakturoid"},
		func() interface{} { return NewFakturoidSource() },
	)
}
