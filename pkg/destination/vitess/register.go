package vitess

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"vitess"},
		func() interface{} { return NewDestination() },
	)
}
