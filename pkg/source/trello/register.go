package trello

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"trello"},
		func() interface{} { return NewTrelloSource() },
	)
}
