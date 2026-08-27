package ripestat

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"ripestat"},
		func() interface{} { return NewRIPEstatSource() },
	)
}
