package filesreader

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/gomlx/onnx-gomlx/onnx"
	"github.com/stretchr/testify/require"
)

func TestReaderCaching(t *testing.T) {
	tmpDir := t.TempDir()

	// Create external data file
	data := []float32{1.0, 2.0}
	rawBytes := make([]byte, len(data)*4)
	for i, val := range data {
		bits := *(*uint32)(unsafe.Pointer(&val))
		rawBytes[i*4] = byte(bits)
		rawBytes[i*4+1] = byte(bits >> 8)
		rawBytes[i*4+2] = byte(bits >> 16)
		rawBytes[i*4+3] = byte(bits >> 24)
	}
	externalFile := filepath.Join(tmpDir, "cached.bin")
	err := os.WriteFile(externalFile, rawBytes, 0644)
	require.NoError(t, err)

	reader := New(tmpDir)
	defer reader.Close()

	info := onnx.ExternalDataInfo{
		Location: "cached.bin",
		Offset:   0,
		Length:   8,
	}

	// Read multiple times - should reuse cached mmap
	for range 3 {
		dst := make([]byte, 8)
		err := reader.ReadInto(info, dst)
		require.NoError(t, err)
	}

	// Verify only one file handle was created
	require.Len(t, reader.files, 1)
}

func TestCloseReleasesResources(t *testing.T) {
	tmpDir := t.TempDir()

	// Create external data file
	data := []float32{1.0, 2.0}
	rawBytes := make([]byte, len(data)*4)
	for i, val := range data {
		bits := *(*uint32)(unsafe.Pointer(&val))
		rawBytes[i*4] = byte(bits)
		rawBytes[i*4+1] = byte(bits >> 8)
		rawBytes[i*4+2] = byte(bits >> 16)
		rawBytes[i*4+3] = byte(bits >> 24)
	}
	externalFile := filepath.Join(tmpDir, "close_test.bin")
	err := os.WriteFile(externalFile, rawBytes, 0644)
	require.NoError(t, err)

	reader := New(tmpDir)

	info := onnx.ExternalDataInfo{
		Location: "close_test.bin",
		Offset:   0,
		Length:   8,
	}

	// Read to open a file handle
	dst := make([]byte, 8)
	err = reader.ReadInto(info, dst)
	require.NoError(t, err)
	require.Len(t, reader.files, 1)

	// Close should release resources
	err = reader.Close()
	require.NoError(t, err)
	require.Empty(t, reader.files)
}
