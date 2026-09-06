// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package sets

import (
	"testing"
)

func TestSet(t *testing.T) {
	// Sets are created empty.
	s := Make[int](10)
	if len(s) != 0 {
		t.Errorf("expected empty set, got %d elements", len(s))
	}

	// Check inserting and recovery.
	s.Insert(3, 7)
	if len(s) != 2 {
		t.Errorf("expected 2 elements, got %d", len(s))
	}
	if !s.Has(3) {
		t.Errorf("expected set to have 3")
	}
	if !s.Has(7) {
		t.Errorf("expected set to have 7")
	}
	if s.Has(5) {
		t.Errorf("expected set not to have 5")
	}

	s2 := MakeWith(5, 7)
	if len(s2) != 2 {
		t.Errorf("expected 2 elements in s2, got %d", len(s2))
	}
	if !s2.Has(5) {
		t.Errorf("expected s2 to have 5")
	}
	if !s2.Has(7) {
		t.Errorf("expected s2 to have 7")
	}
	if s2.Has(3) {
		t.Errorf("expected s2 not to have 3")
	}

	s3 := s.Sub(s2)
	if len(s3) != 1 {
		t.Errorf("expected 1 element in s3, got %d", len(s3))
	}
	if !s3.Has(3) {
		t.Errorf("expected s3 to have 3")
	}

	delete(s, 7)
	if len(s) != 1 {
		t.Errorf("expected 1 element in s after delete, got %d", len(s))
	}
	if !s.Has(3) {
		t.Errorf("expected s to still have 3")
	}
	if s.Has(7) {
		t.Errorf("expected s not to have 7 after delete")
	}
	if !s.Equal(s3) {
		t.Errorf("expected s to be equal to s3")
	}
	if s.Equal(s2) {
		t.Errorf("expected s not to be equal to s2")
	}
	s4 := MakeWith(-3)
	if s.Equal(s4) {
		t.Errorf("expected s not to be equal to s4")
	}
}
