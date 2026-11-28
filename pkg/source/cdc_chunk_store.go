package source

import "context"

// CDCChunkEvolutionIntentKey stores durable proof of startup schema evolution
// destination table recreations until the resulting per-table resnapshot
// checkpoint is persisted.
const CDCChunkEvolutionIntentKey = "evolution-intent"

// CDCChunkStore persists resumable snapshot checkpoints and captured changes.
// Load returns nil when the key is absent or deleted. Identity binds a
// checkpoint to the registered source tables and physical destination tables.
type CDCChunkStore interface {
	Load(context.Context, string) ([]byte, error)
	Save(context.Context, string, []byte) error
	Delete(context.Context, string) error
	Identity(context.Context) (string, error)
}

// CDCChunkTableIdentity describes a table's current physical destination target
// and any previously certified CDC state, allowing chunked snapshots to
// distinguish source schema/OID changes from destination table replacements.
type CDCChunkTableIdentity struct {
	DestTable             string
	PhysicalIdentity      string
	PreEvolutionIdentity  string
	LastCertifiedDest     string
	LastCertifiedPhysical string
	LastCertifiedSource   string
	LastCertifiedSchema   string
}

// CDCChunkTableIdentityProvider optionally exposes per-table physical
// destination identities for chunked snapshot reconciliation.
type CDCChunkTableIdentityProvider interface {
	TableIdentities(context.Context) (map[string]CDCChunkTableIdentity, error)
}
