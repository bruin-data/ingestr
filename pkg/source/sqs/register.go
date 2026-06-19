package sqs

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"sqs"},
		func() interface{} { return NewSQSSource() },
	)
}
