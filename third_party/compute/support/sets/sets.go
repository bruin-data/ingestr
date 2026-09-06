// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package sets implement a set type as a `map[T]struct{}` but with better ergonomics.
package sets

import "maps"

// Set implements a Set for the key type T.
type Set[T comparable] map[T]struct{}

// Make returns an empty Set of the given type. Size is optional, and if given
// will reserve the expected size.
func Make[T comparable](size ...int) Set[T] {
	if len(size) == 0 {
		return make(Set[T])
	}
	return make(Set[T], size[0])
}

// MakeWith creates a Set[T] with the given elements inserted.
func MakeWith[T comparable](elements ...T) Set[T] {
	s := Make[T](len(elements))
	for _, element := range elements {
		s.Insert(element)
	}
	return s
}

// Union returns the union of all the given sets.
func Union[T comparable](sets ...Set[T]) Set[T] {
	s := Make[T]()
	for _, s2 := range sets {
		for k := range maps.Keys(s2) {
			s.Insert(k)
		}
	}
	return s
}

// Has returns true if Set s has the given key.
func (s Set[T]) Has(key T) bool {
	_, found := s[key]
	return found
}

// Insert keys into set.
func (s Set[T]) Insert(keys ...T) {
	for _, key := range keys {
		s[key] = struct{}{}
	}
}

// Sub returns `s - s2`, that is, all elements in `s` that are not in `s2`.
func (s Set[T]) Sub(s2 Set[T]) Set[T] {
	sub := Make[T]()
	for k := range s {
		if !s2.Has(k) {
			sub.Insert(k)
		}
	}
	return sub
}

// Equal returns whether s and s2 have the exact same elements.
func (s Set[T]) Equal(s2 Set[T]) bool {
	if len(s) != len(s2) {
		return false
	}
	for k := range s {
		if !s2.Has(k) {
			return false
		}
	}
	return true
}
