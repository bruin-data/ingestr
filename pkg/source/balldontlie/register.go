package balldontlie

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"balldontlie"},
		func() interface{} { return NewBallDontLieSource() },
	)
}
