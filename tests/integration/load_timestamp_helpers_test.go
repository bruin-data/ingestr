//go:build integration || local

package integration

import (
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/stretchr/testify/require"
)

func withoutLoadTimestampTypes(types map[string]string) map[string]string {
	out := make(map[string]string, len(types))
	for name, typ := range types {
		if strings.EqualFold(name, naming.IngestrLoadedAtColumn) {
			continue
		}
		out[name] = typ
	}
	return out
}

func withoutLoadTimestampColumns(cols []string) []string {
	out := make([]string, 0, len(cols))
	for _, col := range cols {
		if strings.EqualFold(col, naming.IngestrLoadedAtColumn) {
			continue
		}
		out = append(out, col)
	}
	return out
}

func requireLoadTimestampColumn(t *testing.T, types map[string]string) {
	t.Helper()
	_, ok := types[naming.IngestrLoadedAtColumn]
	if !ok {
		for name := range types {
			if strings.EqualFold(name, naming.IngestrLoadedAtColumn) {
				ok = true
				break
			}
		}
	}
	require.True(t, ok, "expected %s column in %v", naming.IngestrLoadedAtColumn, types)
}
