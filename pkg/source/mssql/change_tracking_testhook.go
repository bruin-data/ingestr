//go:build integration

package mssql

import "context"

// SetChangeTrackingCursorValidationHook installs the integration-test cursor
// invalidation hook. Pass nil to clear it.
func SetChangeTrackingCursorValidationHook(fn func(ctx context.Context, sourceURI, table string) error) {
	if fn == nil {
		ctCursorValidationHook.Store(nil)
		return
	}
	ctCursorValidationHook.Store(&fn)
}
