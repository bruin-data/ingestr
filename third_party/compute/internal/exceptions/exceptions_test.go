// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package exceptions_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gomlx/compute/internal/exceptions"
	"github.com/gomlx/compute/support/testutil"
	"github.com/pkg/errors"
)

// testCatchExceptions is a helper function to catch different types of exceptions and return them accordingly.
func testCatchExceptions(fn func()) (int, float64, error) {
	var (
		eInt   int
		eErr   error
		eFloat float64
	)
	exception := exceptions.Try(fn)
	if exception != nil {
		switch e := exception.(type) {
		case int:
			eInt = e
		case error:
			eErr = e
		case float64:
			eFloat = e
		default:
			panic(e)
		}
	}
	return eInt, eFloat, eErr
}

func TestTry(t *testing.T) {
	// No throws.
	eInt, eFloat, eErr := testCatchExceptions(func() {})
	if eInt != 0 {
		t.Errorf("Expected eInt 0, got %d", eInt)
	}
	if eErr != nil {
		t.Errorf("Unexpected error: %+v", eErr)
	}
	if eFloat != 0.0 {
		t.Errorf("Expected eFloat 0.0, got %f", eFloat)
	}

	// Panic an int.
	eInt, eFloat, eErr = testCatchExceptions(func() {
		panic(7)
	})
	if eInt != 7 {
		t.Errorf("Expected eInt 7, got %d", eInt)
	}
	if eErr != nil {
		t.Errorf("Unexpected error: %+v", eErr)
	}
	if eFloat != 0.0 {
		t.Errorf("Expected eFloat 0.0, got %f", eFloat)
	}

	// Panic an error.
	e := errors.New("blah")
	eInt, eFloat, eErr = testCatchExceptions(func() { panic(e) })
	if eInt != 0 {
		t.Errorf("Expected eInt 0, got %d", eInt)
	}
	if !errors.Is(eErr, e) {
		t.Errorf("Expected error %v, got %v", e, eErr)
	}
	if eFloat != 0.0 {
		t.Errorf("Expected eFloat 0.0, got %f", eFloat)
	}

	// Panic something different.
	if panicked, _ := testutil.Try(func() {
		// A string exception is not caught.
		_, _, _ = testCatchExceptions(func() { panic("some string") })
	}); !panicked {
		t.Errorf("Expected panic for string exception")
	}
}

func TestTryCatch(t *testing.T) {
	want := errors.New("test error")
	var err error
	if panicked, panicErr := testutil.Try(func() { err = exceptions.TryCatch[error](func() { panic(want) }) }); panicked {
		t.Errorf("Unexpected panic: %v", panicErr)
	}
	if err == nil || err.Error() != want.Error() {
		t.Errorf("Expected error %q, got %v", want.Error(), err)
	}
}

func TestThrow(t *testing.T) {
	err := exceptions.TryCatch[error](func() { exceptions.Panicf("2+3=%d", 2+3) })
	if err == nil || err.Error() != "2+3=5" {
		t.Errorf("Expected error %q, got %v", "2+3=5", err)
	}
}

func TestRuntimeErrors(t *testing.T) {
	var x any = 0.0
	//nolint:forbidigo // fmt.Println is used to cause a panic here.
	err := exceptions.TryCatch[error](func() { fmt.Println(x.(string)) })
	if err == nil || !strings.Contains(err.Error(), "interface conversion") {
		t.Errorf("Expected error containing %q, got %v", "interface conversion", err)
	}
}
