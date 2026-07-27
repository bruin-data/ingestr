package postgres_cdc

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
)

func TestSingleTableKeylessResumeFiltersDurableTransactionAtEquality(t *testing.T) {
	const resumeLSN = pglogrepl.LSN(100)
	keyless := &Replicator{startLSN: resumeLSN, tableSchema: &schema.TableSchema{}}
	assert.True(t, keyless.ShouldFilterChange(resumeLSN-1))
	assert.True(t, keyless.ShouldFilterChange(resumeLSN))
	assert.False(t, keyless.ShouldFilterChange(resumeLSN+1))

	keyless.SetSnapshotBoundary(true)
	assert.False(t, keyless.ShouldFilterChange(resumeLSN), "a transaction at an exported snapshot boundary is not in the snapshot")

	keyed := &Replicator{startLSN: resumeLSN, tableSchema: &schema.TableSchema{PrimaryKeys: []string{"id"}}}
	assert.False(t, keyed.ShouldFilterChange(resumeLSN), "keyed merge remains at-least-once at equality")
}
