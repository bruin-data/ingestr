package db2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectDb2PlatformFromPRDID(t *testing.T) {
	cases := []struct {
		prdid string
		want  db2Platform
	}{
		{"QSQ07030", db2PlatformIBMi},
		{"qsq07030", db2PlatformIBMi},
		{"DSN12015", db2PlatformZOS},
		{"SQL11050", db2PlatformLUW},
		{"JCC04028", db2PlatformLUW},
		{"", db2PlatformAuto},
		{"UNKNOWN1", db2PlatformAuto},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, detectDb2PlatformFromPRDID(tc.prdid), "prdid=%s", tc.prdid)
	}
}

func TestParseDb2PlatformOverride(t *testing.T) {
	cases := []struct {
		value   string
		want    db2Platform
		wantErr bool
	}{
		{"", db2PlatformAuto, false},
		{"ibmi", db2PlatformIBMi, false},
		{"iseries", db2PlatformIBMi, false},
		{"AS400", db2PlatformIBMi, false},
		{"zos", db2PlatformZOS, false},
		{"luw", db2PlatformLUW, false},
		{"bogus", db2PlatformAuto, true},
	}
	for _, tc := range cases {
		got, err := parseDb2PlatformOverride(tc.value)
		if tc.wantErr {
			require.Error(t, err, "value=%s", tc.value)
			continue
		}
		require.NoError(t, err, "value=%s", tc.value)
		require.Equal(t, tc.want, got, "value=%s", tc.value)
	}
}
