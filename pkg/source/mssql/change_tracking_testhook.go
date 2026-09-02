//go:build integration

package mssql

import "context"

// SetChangeTrackingCursorValidationHook installs fn to run after the resume
// cursor is validated and before CHANGETABLE is queried, so integration tests
// can invalidate the cursor mid-read. Pass nil to clear it.
func SetChangeTrackingCursorValidationHook(fn func(ctx context.Context, sourceURI, table string) error) {
	if fn == nil {
		ctCursorValidationHook.Store(nil)
		return
	}
	ctCursorValidationHook.Store(&fn)
}
