package github

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"github"},
		func() interface{} { return NewGitHubSource() },
	)
}
