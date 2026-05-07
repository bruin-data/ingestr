package http

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"http", "https"},
		func() interface{} { return NewHTTPSource() },
	)
}
