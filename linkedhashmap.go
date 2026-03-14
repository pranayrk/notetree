// Copyright (c) 2015, Emir Pasic. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package linkedhashmap is a map that preserves insertion-order.
//
// It is backed by a hash table to store values and doubly-linked list to store ordering.
//
// Structure is not thread safe.
//
// Reference: http://en.wikipedia.org/wiki/Associative_array
package main

import (
	"fmt"
	"strings"

	"github.com/emirpasic/gods/v2/lists/doublylinkedlist"
	"github.com/emirpasic/gods/v2/maps"
)

// Assert Map implementation
var _ maps.Map[string, int] = (*Map[string, int])(nil)

// Map holds the elements in a regular hash table, and uses doubly-linked list to store key ordering.
type Map[K comparable, V any] struct {
	table    map[K]V
	ordering *doublylinkedlist.List[K]
}

// New instantiates a new map.
func New[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{
		table:    make(map[K]V),
		ordering: doublylinkedlist.New[K](),
	}
}

// Put inserts element into the map.
func (m *Map[K, V]) Put(key K, value V) {
	if _, contains := m.table[key]; !contains {
		m.ordering.Append(key)
	}
	m.table[key] = value
}

// Get searches the element in the map by key and returns its value or nil with second boolean parameter set to false if not found.
func (m *Map[K, V]) Get(key K) (value V, found bool) {
	value, found = m.table[key]
	return value, found
}

// Remove removes the element from the map by key.
func (m *Map[K, V]) Remove(key K) {
	if _, contains := m.table[key]; contains {
		delete(m.table, key)
		index := m.ordering.IndexOf(key)
		m.ordering.Remove(index)
	}
}

// Empty returns true if map does not contain any elements.
func (m *Map[K, V]) Empty() bool {
	return m.Size() == 0
}

// Size returns number of elements in the map.
func (m *Map[K, V]) Size() int {
	return m.ordering.Size()
}

// Keys returns all keys (preserving insertion order).
func (m *Map[K, V]) Keys() []K {
	return m.ordering.Values()
}

// Values returns all values (preserving insertion order).
func (m *Map[K, V]) Values() []V {
	keys := m.Keys()
	values := make([]V, len(keys))
	for i, key := range keys {
		values[i] = m.table[key]
	}
	return values
}

// Clear removes all elements from the map.
func (m *Map[K, V]) Clear() {
	clear(m.table)
	m.ordering.Clear()
}

// String returns a string representation of the container.
func (m *Map[K, V]) String() string {
	str := "LinkedHashMap\nmap["
	items := make([]string, 0, m.Size())
	for _, key := range m.Keys() {
		items = append(items, fmt.Sprintf("%v: %v", key, m.table[key]))
	}
	str += strings.Join(items, ", ")
	str += "]"
	return str
}
