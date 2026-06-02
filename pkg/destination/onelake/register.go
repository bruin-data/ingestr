package onelake

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"onelake"},
		func() interface{} { return NewOneLakeDestination() },
	)
}
