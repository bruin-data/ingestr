package google_sheets

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"gsheets"},
		func() interface{} { return NewGoogleSheetsSource() },
	)
}
