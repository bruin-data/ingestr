package hana

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"hana", "saphana"},
		func() interface{} { return NewHanaSource() },
	)
}
