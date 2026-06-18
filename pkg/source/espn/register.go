package espn

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"espn"},
		func() interface{} { return NewESPNSource() },
	)
}
