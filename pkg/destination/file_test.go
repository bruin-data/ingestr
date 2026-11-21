package destination

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateReplacementFileNewTargetMode(t *testing.T) {
	dir := t.TempDir()
	control, err := os.Create(filepath.Join(dir, "control"))
	require.NoError(t, err)
	controlInfo, err := control.Stat()
	require.NoError(t, err)
	require.NoError(t, control.Close())
	target := filepath.Join(dir, "output")
	f, err := CreateReplacementFile(target)
	require.NoError(t, err)
	info, err := f.Stat()
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Equal(t, controlInfo.Mode().Perm(), info.Mode().Perm())
	_, err = os.Stat(target)
	require.ErrorIs(t, err, os.ErrNotExist)
}
