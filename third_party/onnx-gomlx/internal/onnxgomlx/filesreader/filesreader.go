// Package filesreader implements the onnx.ExternalDataReader interface by reading files from a
package filesreader

import (
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/gomlx/onnx-gomlx/onnx"
	"github.com/pkg/errors"
)

// Compile-time check that ExternalDataReader implements onnx.ExternalDataReader.
var _ onnx.ExternalDataReader = &FilesReader{}

// FilesReader manages external data files for tensor loading.
// It caches file handles by path since multiple tensors often share the same external file,
// avoiding repeated open/close overhead during model loading.
type FilesReader struct {
	baseDir string
	files   map[string]*os.File
	mu      sync.Mutex
}

// New creates a reader for the given model directory.
//
// baseDir is the directory where to read the external data file from.
// Usually, it is the directory containing the ONNX model file.
func New(baseDir string) *FilesReader {
	return &FilesReader{
		baseDir: baseDir,
		files:   make(map[string]*os.File),
	}
}

// getOrOpenFile returns the file handle for the given location, opening it if necessary.
func (r *FilesReader) getOrOpenFile(location string) (*os.File, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if already open
	if file, ok := r.files[location]; ok {
		return file, nil
	}

	// Resolve the external file path relative to the model directory
	externalPath := filepath.Join(r.baseDir, location)

	// Open the file
	file, err := os.Open(externalPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open external data file %q", externalPath)
	}

	r.files[location] = file
	return file, nil
}

// ReadInto reads external data directly into the provided byte slice.
func (r *FilesReader) ReadInto(info onnx.ExternalDataInfo, output []byte) error {
	if r.baseDir == "" {
		return errors.New("base directory is required for reading external data")
	}

	file, err := r.getOrOpenFile(info.Location)
	if err != nil {
		return err
	}

	// Determine the length to read
	length := int64(len(output))
	if info.Length > 0 {
		// Explicit length specified
		if info.Length != int64(len(output)) {
			return errors.Errorf("external data length %d doesn't match destination size %d", info.Length, len(output))
		}
		length = info.Length
	}

	// Read at the specified offset (ReadAt is safe for concurrent use)
	n, err := file.ReadAt(output, info.Offset)
	if err != nil && err != io.EOF {
		return errors.Wrapf(err, "failed to read %d bytes at offset %d from external data file %q",
			length, info.Offset, info.Location)
	}
	if int64(n) != length {
		return errors.Errorf("read %d bytes but expected %d from external data file %q",
			n, length, info.Location)
	}

	return nil
}

// Close closes all cached file handles and releases resources.
//
// After Close is called, the reader can still be used at a later time.
func (r *FilesReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for path, file := range r.files {
		if err := file.Close(); err != nil && firstErr == nil {
			firstErr = errors.Wrapf(err, "failed to close file %q", path)
		}
	}
	clear(r.files) // Clear map of files open, but allows it to be reused later.
	return firstErr
}
