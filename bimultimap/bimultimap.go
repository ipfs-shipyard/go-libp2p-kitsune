// Package bimultimap is a thread-safe bidirectional MultiMap. It associates a key with multiple
// values and each value with its corresponding keys.
package bimultimap

import (
	"sync"
)

// A thread-safe bidirectional multimap where neither the keys nor the values need to be unique
type BiMultiMap struct {
	forward map[interface{}][]interface{}
	inverse map[interface{}][]interface{}
	s       sync.RWMutex
}

// NewBiMultiMap creates a new, empty biMultiMap
func NewBiMultiMap() *BiMultiMap {
	return &BiMultiMap{
		forward: make(map[interface{}][]interface{}),
		inverse: make(map[interface{}][]interface{}),
	}
}

// GetValues gets the values associated with a key, or an empty slice if the key does not exist
func (m *BiMultiMap) GetValues(key interface{}) []interface{} {
	m.s.RLock()
	defer m.s.RUnlock()

	values, found := m.forward[key]
	if !found {
		return make([]interface{}, 0)
	}
	return values
}

// GetKeys gets the keys associated with a value, or an empty slice if the value does not exist
func (m *BiMultiMap) GetKeys(value interface{}) []interface{} {
	m.s.RLock()
	defer m.s.RUnlock()

	keys, found := m.inverse[value]

	if !found {
		return make([]interface{}, 0)
	}
	return keys
}

// Put adds a key/value pair
func (m *BiMultiMap) Put(key interface{}, value interface{}) {
	m.s.Lock()
	defer m.s.Unlock()

	values, found := m.forward[key]
	if !found {
		values = make([]interface{}, 0, 1)
	}

	// Value already exists for that key - early exit
	for _, v := range values {
		if v == value {
			return
		}
	}

	values = append(values, value)
	m.forward[key] = values

	keys, found := m.inverse[value]
	if !found {
		keys = make([]interface{}, 0, 1)
	}
	keys = append(keys, key)
	m.inverse[value] = keys
}

// KeyExists returns true if a key exists in the map
func (m *BiMultiMap) KeyExists(key interface{}) bool {
	m.s.RLock()
	defer m.s.RUnlock()

	_, found := m.forward[key]
	return found
}

// ValueExists returns true if a value exists in the map
func (m *BiMultiMap) ValueExists(value interface{}) bool {
	m.s.RLock()
	defer m.s.RUnlock()

	_, found := m.inverse[value]
	return found
}

// DeleteKey deletes a key from the map and returns its associated values
func (m *BiMultiMap) DeleteKey(key interface{}) []interface{} {
	m.s.Lock()
	defer m.s.Unlock()

	values, found := m.forward[key]
	if !found {
		return make([]interface{}, 0)
	}

	delete(m.forward, key)

	for _, v := range values {
		newKeys := deleteElement(m.inverse[v], key)
		m.inverse[v] = newKeys
	}

	return values
}

// DeleteValue deletes a value from the map and returns its associated keys
func (m *BiMultiMap) DeleteValue(value interface{}) []interface{} {
	m.s.Lock()
	defer m.s.Unlock()

	keys, found := m.inverse[value]
	if !found {
		return make([]interface{}, 0)
	}

	delete(m.inverse, value)

	for _, k := range keys {
		newVals := deleteElement(m.forward[k], value)
		m.forward[k] = newVals
	}

	return keys
}

// DeleteKeyValue deletes a single key/value pair
func (m *BiMultiMap) DeleteKeyValue(key interface{}, value interface{}) {
	m.s.Lock()
	defer m.s.Unlock()

	_, foundValue := m.forward[key]
	_, foundKey := m.inverse[value]

	if foundKey && foundValue {
		newVals := deleteElement(m.forward[key], value)
		m.forward[key] = newVals

		newKeys := deleteElement(m.inverse[value], key)
		m.inverse[value] = newKeys
	}
}

// Keys returns an unordered slice containing all of the map's keys
func (m *BiMultiMap) Keys() []interface{} {
	keys := make([]interface{}, 0, len(m.forward))
	for k := range m.forward {
		keys = append(keys, k)
	}
	return keys
}

// Values returns an unordered slice containing all of the map's values
func (m *BiMultiMap) Values() []interface{} {
	values := make([]interface{}, 0, len(m.inverse))
	for v := range m.inverse {
		values = append(values, v)
	}
	return values
}

// Helper function: delete an element from a slice if it exists
func deleteElement(slice []interface{}, element interface{}) []interface{} {
	newSlice := make([]interface{}, 0, len(slice)-1)

	for _, val := range slice {
		if val != element {
			newSlice = append(newSlice, val)
		}
	}

	return newSlice
}
