package json

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"json"},
		func() interface{} { return NewJSONSource() },
	)
}
