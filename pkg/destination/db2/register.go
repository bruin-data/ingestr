package db2

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"db2", "ibmdb2"},
		func() interface{} { return NewDb2Destination() },
	)
}
