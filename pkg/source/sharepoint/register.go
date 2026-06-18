package sharepoint

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"sharepoint"},
		func() interface{} { return NewSharePointSource() },
	)
}
