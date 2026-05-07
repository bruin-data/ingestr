package jsonl

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"jsonl", "ndjson"},
		func() interface{} { return NewJSONLDestination() },
	)
}
