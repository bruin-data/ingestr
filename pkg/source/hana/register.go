package hana

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"hana", "saphana"},
		func() interface{} { return NewHanaSource() },
	)
}
