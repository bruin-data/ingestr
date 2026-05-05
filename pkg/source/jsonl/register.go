package jsonl

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"jsonl", "ndjson"},
		func() interface{} { return NewJSONLSource() },
	)
}
