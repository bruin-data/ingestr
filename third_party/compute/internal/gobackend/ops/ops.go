// Package ops has the base implementation for most operations for the Go backend.
//
// It works by registration, so it simply needs to be imported to be made available.
package ops

// Generate the DTypeMag and DTypePairMap registrations:
//go:generate go run ../../cmd/gobackend_dtypemap

// Generates trivial binary and unary operations.
//go:generate go run ../../cmd/gobackend_ops_generator
