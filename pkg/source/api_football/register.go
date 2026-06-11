package api_football

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"api-football"},
		func() interface{} { return NewAPIFootballSource() },
	)
}
