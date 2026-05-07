package json

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"json"},
		func() interface{} { return NewJSONSource() },
	)
}
