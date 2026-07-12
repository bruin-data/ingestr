package planetscale

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"ps_mysql"},
		func() interface{} { return NewDestination() },
	)
}
