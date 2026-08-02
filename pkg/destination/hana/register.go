package hana

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"hana", "saphana"},
		func() interface{} { return NewHanaDestination() },
	)
}
