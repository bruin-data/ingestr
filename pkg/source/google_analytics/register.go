package google_analytics

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"googleanalytics"},
		func() interface{} { return NewGoogleAnalyticsSource() },
	)
}
