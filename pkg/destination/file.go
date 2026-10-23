package destination

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

// CreateReplacementFile creates a unique sibling of target for later publication
// by rename. Existing permissions are retained; new files use 0666 under umask.
func CreateReplacementFile(target string) (*os.File, error) {
	info, err := os.Stat(target)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to stat replacement target: %w", err)
	}
	mode := os.FileMode(0o666)
	if info != nil {
		mode = 0o600
	}
	path := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+"-"+rand.Text())
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to create replacement file: %w", err)
	}
	if info != nil {
		if err := f.Chmod(info.Mode().Perm()); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return nil, fmt.Errorf("failed to preserve replacement permissions: %w", err)
		}
	}
	return f, nil
}
