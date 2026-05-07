package synapse

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"synapse"},
		func() interface{} { return NewSynapseDestination() },
	)
}
