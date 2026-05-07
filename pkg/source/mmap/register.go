package mmap

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"mmap"},
		func() interface{} { return NewMMapSource() },
	)
}
