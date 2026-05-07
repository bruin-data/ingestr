package jsonl

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"jsonl", "ndjson"},
		func() interface{} { return NewJSONLSource() },
	)
}
