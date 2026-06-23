package gitlab

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"gitlab"},
		func() interface{} { return NewGitLabSource() },
	)
}
