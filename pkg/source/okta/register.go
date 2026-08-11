package okta

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"okta"},
		func() interface{} { return NewOktaSource() },
	)
}
