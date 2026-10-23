//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	postgresdest "github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/stretchr/testify/require"
)

func TestPostgresDestinationMergeNullIncrementalKey(t *testing.T) {
	uri := sharedPostgresURI(t, "dest")
	ctx := t.Context()
	dest := postgresdest.NewPostgresDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, predicate := range []string{"", "target.id > 0"} {
		t.Run("predicate="+predicate, func(t *testing.T) {
			target := "public.dedup_target_" + uniqueSuffix()
			staging := "public.dedup_staging_" + uniqueSuffix()
			t.Cleanup(func() {
				_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s, %s", target, staging))
			})
			require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`
				CREATE TABLE %s (id bigint PRIMARY KEY, score bigint);
				CREATE TABLE %s (id bigint, score bigint);
				INSERT INTO %s VALUES (1, 99);
				INSERT INTO %s VALUES
					(1, NULL), (1, -10), (1, -3),
					(2, -7), (2, NULL), (3, NULL), (3, NULL);
			`, target, staging, target, staging)))
			require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
				TargetTable: target, StagingTable: staging, PrimaryKeys: []string{"id"},
				Columns: []string{"id", "score"}, IncrementalKey: "score", IncrementalPredicate: predicate,
			}))

			var winners string
			require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`
				SELECT string_agg(id || ':' || coalesce(score::text, 'NULL'), ',' ORDER BY id) FROM %s
			`, target)).Scan(&winners))
			require.Equal(t, "1:-3,2:-7,3:NULL", winners)
		})
	}
}
