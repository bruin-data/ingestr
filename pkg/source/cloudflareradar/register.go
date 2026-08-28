package cloudflareradar

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"cloudflare-radar", "cloudflareradar"},
		func() interface{} { return NewCloudflareRadarSource() },
	)
}
