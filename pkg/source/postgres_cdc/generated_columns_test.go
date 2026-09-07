package postgres_cdc

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestGeneratedColumnCoverageInRelation(t *testing.T) {
	sc := addCDCColumns(&schema.TableSchema{Name: "items", Schema: "public", PrimaryKeys: []string{"id"}, Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt32}, {Name: "computed", DataType: schema.TypeInt32}}})
	missing := pgoRelationMsg(1, "public", "items")
	complete := pgoRelationMsgWithCols(1, "public", "items", pgoCol{"id", 23}, pgoCol{"computed", 23})
	single := NewDecoder(sc, "public", "items")
	single.generatedColumns = []string{"computed"}
	_, err := single.Decode(missing, 1)
	require.ErrorContains(t, err, "generated column \"computed\" is missing")
	require.Empty(t, single.relations)
	_, err = single.Decode(complete, 2)
	require.NoError(t, err)
	// Publication changes must be checked on every relation announcement.
	_, err = single.Decode(missing, 3)
	require.ErrorContains(t, err, "publish_generated_columns")
	multi := NewMultiTableDecoder([]source.SourceTableInfo{{Name: "public.items", Schema: sc}})
	multi.generatedColumns = map[string][]string{"public.items": {"computed"}}
	_, err = multi.Decode(missing, 1)
	require.ErrorContains(t, err, "generated column \"computed\" is missing")
	_, err = multi.Decode(complete, 2)
	require.NoError(t, err)
	_, err = multi.Decode(missing, 3)
	require.ErrorContains(t, err, "publish_generated_columns")
}
