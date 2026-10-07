//go:build integration

package d1

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
)

func TestD1LiveBatchContract(t *testing.T) {
	uri := os.Getenv("INGESTR_D1_TEST_URI")
	if uri == "" {
		t.Skip("set INGESTR_D1_TEST_URI to a disposable D1 database to run the live API contract test")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	d := NewD1Destination()
	require.NoError(t, d.Connect(ctx, uri))
	t.Cleanup(func() { _ = d.Close(context.Background()) })

	table := "_ingestr_d1_test_" + strings.ToLower(rand.Text())
	quoted := destination.QuoteIdentifier(table)
	require.NoError(t, d.Exec(ctx, "CREATE TABLE "+quoted+" (id INTEGER PRIMARY KEY, big_value INTEGER, payload BLOB)"))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cleanupCancel()
		if err := d.DropTable(cleanupCtx, table); err != nil {
			t.Errorf("clean up owned live D1 table %s: %v", table, err)
		}
	})

	insert := "INSERT INTO " + quoted + " (id, big_value, payload) VALUES (?, CAST(? AS INTEGER), ?)"
	require.NoError(t, d.executeBatch(ctx, []statement{
		{SQL: insert, Params: []interface{}{1, "9223372036854775807", []int{0, 127, 255}}},
		{SQL: insert, Params: []interface{}{2, "-9223372036854775808", []int{}}},
	}))

	assertRows := func() {
		t.Helper()
		results, err := d.query(ctx, "SELECT id, CAST(big_value AS TEXT) AS big_value, hex(payload) AS payload, typeof(payload) AS payload_type FROM "+quoted+" ORDER BY id")
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, []map[string]interface{}{
			{"id": json.Number("1"), "big_value": "9223372036854775807", "payload": "007FFF", "payload_type": "blob"},
			{"id": json.Number("2"), "big_value": "-9223372036854775808", "payload": "", "payload_type": "blob"},
		}, results[0].Results)
	}
	assertRows()

	err := d.executeBatch(ctx, []statement{
		{SQL: insert, Params: []interface{}{3, "3", []int{3}}},
		{SQL: insert, Params: []interface{}{1, "4", []int{4}}},
	})
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "unique constraint")
	assertRows()
}
