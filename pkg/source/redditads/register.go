package redditads

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"redditads"},
		func() interface{} { return NewRedditAdsSource() },
	)
}
