package braze

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"braze"},
		func() interface{} { return NewBrazeSource() },
	)
}
