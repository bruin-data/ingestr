package socrata

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"socrata"},
		func() interface{} { return NewSocrataSource() },
	)
}
