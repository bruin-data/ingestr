package frankfurter

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"frankfurter"},
		func() interface{} { return NewFrankfurterSource() },
	)
}
