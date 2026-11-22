package source

import "context"

// CDCChunkStore persists resumable snapshot checkpoints and captured changes.
// Load returns nil when the key is absent or deleted. Identity binds a
// checkpoint to the registered source tables and physical destination tables.
type CDCChunkStore interface {
	Load(context.Context, string) ([]byte, error)
	Save(context.Context, string, []byte) error
	Delete(context.Context, string) error
	Identity(context.Context) (string, error)
}
