// Package gobackend implements a native Go [compute.Backend]: very portable (including WASM) but not very fast.
//
// It only implements the most popular dtypes and operations.
// But generally, it's easy to add new ops: just open an issue in GoMLX.
//
// This package is simply a thin wrapper around internal/gobackend.
package gobackend

import (
	"sync"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/internal/gobackend"

	// Registers all the ops.
	_ "github.com/gomlx/compute/internal/gobackend/defaultpkgs"
)

// BackendName to be used in GOMLX_BACKEND to specify this backend.
const BackendName = "go"

// GetBackend returns a singleton Go backend, created with the default configuration.
// The backend is only created at the first call of the function.
//
// The singleton is never destroyed.
var GetBackend = sync.OnceValue(func() compute.Backend {
	backend, err := New("")
	if err != nil {
		panic(err)
	}
	return backend
})

// New constructs a new Go Backend.
// There are no configurations, the string is simply ignored.
func New(config string) (compute.Backend, error) {
	return gobackend.New(config)
}
