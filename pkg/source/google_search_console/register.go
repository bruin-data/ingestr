package google_search_console

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"gsc", "googlesearchconsole"},
		func() interface{} { return NewGoogleSearchConsoleSource() },
	)
}
