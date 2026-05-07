package mmap

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mmap"},
		func() interface{} { return NewMMapSource() },
	)
}
