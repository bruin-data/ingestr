package balldontlie_fifa

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"balldontlie-fifa"},
		func() interface{} { return NewBallDontLieFIFASource() },
	)
}
