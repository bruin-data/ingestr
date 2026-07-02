package google_sheets

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"gsheets"},
		func() interface{} { return NewGoogleSheetsDestination() },
	)
}
