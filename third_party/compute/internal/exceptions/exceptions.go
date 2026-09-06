// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package exceptions provide helper functions to leverage Go's `panic`, `recover` and `defer`
// as an "exceptions" system.
//
// The `panic`, `recover` and the added runtime type checking are slow when compared to
// simply returning errors.
// So it should be used where a little latency in case of errors is not an issue.
//
// It defines `Try` and `TryCatch[E any]`.
//
// This is an internal copy of github.com/gomlx/exceptions, to avoid the extra dependency.
//
// Mostly it is used in the graph building functions.
package exceptions

import (
	"runtime"

	"github.com/pkg/errors"
)

// convertRuntimePanic converts a runtime panic error to something with a printable stack.
func convertRuntimePanic(e any) any {
	if e == nil {
		return e
	}
	runtimeErr, ok := e.(runtime.Error)
	if !ok {
		// Not a runtime error.
		return e
	}
	return errors.Wrap(runtimeErr, "runtime panic: ")
}

// Try calls `fn` and return any exception (`panic`) that may occur.
//
// Runtime panics are converted to an error with a stack-trace, to facilitate debugging.
//
// Example:
//
//	var x ResultType
//	e := Try(func() { x = DoSomething() })
//	if e != nil {
//		if eInt, ok := e.(int); ok {
//			errorInt = e
//		} else if err, ok := e.(err); ok {
//			klog.Errorf("%v", e)
//		} else {
//			panic(e)
//		}
//	}
func Try(fn func()) (exception any) {
	defer func() {
		exception = recover()
		exception = convertRuntimePanic(exception)
	}()
	fn()
	return exception
}

// TryCatch executes `fn` and in case of `panic`, it recovers if of the type `E`.
// For a `panic` of any other type, it simply re-throws the `panic`.
//
// Runtime panics are converted to errors.Error, with a stack trace.
//
// Example:
//
//	var x ResultType
//	err := TryCatch[error](func() { x = DoSomething() })
//	if err != nil {
//		// Handle error ...
//	}
func TryCatch[E any](fn func()) (exception E) {
	defer func() {
		e := recover()
		if e == nil {
			// No exceptions.
			return
		}
		e = convertRuntimePanic(e)

		var ok bool
		exception, ok = e.(E)
		if !ok {
			// Re-throw an exception with the unexpected type.
			panic(e)
		}
		// Return the recovered exception.
	}()
	fn()
	return exception
}

// Panicf is a shortcut to `panic(errors.Errorf(format, args...))`.
// It throws an error with a stack-trace and the formatted message.
func Panicf(format string, args ...any) {
	panic(errors.Errorf(format, args...))
}
