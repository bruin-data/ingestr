package dune

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"dune"},
		func() interface{} { return NewDuneSource() },
	)
}
