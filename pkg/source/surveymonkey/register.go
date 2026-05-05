package surveymonkey

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"surveymonkey"},
		func() interface{} { return NewSurveyMonkeySource() },
	)
}
