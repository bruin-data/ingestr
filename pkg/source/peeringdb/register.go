package peeringdb

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"peeringdb"},
		func() interface{} { return NewPeeringDBSource() },
	)
}
