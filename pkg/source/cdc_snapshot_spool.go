package source

// CDCSnapshotSpoolOptions captures changes while a normal snapshot is copied.
// Store holds destination-managed recovery checkpoints; durable WAL storage
// can use that same store or Directory on persistent local disk.
type CDCSnapshotSpoolOptions struct {
	WALStaging     string
	WALBufferBytes int64
	Directory      string
	Continuous     bool
	RefreshID      string
	Store          CDCChunkStore
}
