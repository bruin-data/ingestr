package api_football

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"apifootball"},
		func() interface{} { return NewAPIFootballSource() },
	)
}
