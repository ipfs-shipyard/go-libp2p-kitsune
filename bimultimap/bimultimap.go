// Package bimultimap is a thread-safe bidirectional MultiMap. It associates a key with multiple
// values and each value with its corresponding keys.
// TODO Move to its own repo
// TODO Move to go 1.18 and generics
package bimultimap

import (
	"sync"
)

// BiMultiMap is a thread-safe bidirectional multimap where neither the keys nor the values need to be unique
type BiMultiMap struct {
	forward     map[interface{}][]interface{}
	inverse     map[interface{}][]interface{}
	mutex       sync.RWMutex
	readLocked  bool
	writeLocked bool
}

// New creates a new, empty biMultiMap
func New() *BiMultiMap {
	return &BiMultiMap{
		forward:     make(map[interface{}][]interface{}),
		inverse:     make(map[interface{}][]interface{}),
		readLocked:  false,
		writeLocked: false,
	}
}

// LookupKey gets the values associated with a key, or an empty slice if the key does not exist
func (m *BiMultiMap) LookupKey(key interface{}) []interface{} {
	m.rLock()
	defer m.rUnlock()

	values, found := m.forward[key]
	if !found {
		return make([]interface{}, 0)
	}
	return values
}

// LookupValue gets the keys associated with a value, or an empty slice if the value does not exist
func (m *BiMultiMap) LookupValue(value interface{}) []interface{} {
	m.rLock()
	defer m.rUnlock()

	keys, found := m.inverse[value]

	if !found {
		return make([]interface{}, 0)
	}
	return keys
}

// Add adds a key/value pair
func (m *BiMultiMap) Add(key interface{}, value interface{}) {
	m.lock()
	defer m.unlock()

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
	m.rLock()
	defer m.rUnlock()

	_, found := m.forward[key]
	return found
}

// ValueExists returns true if a value exists in the map
func (m *BiMultiMap) ValueExists(value interface{}) bool {
	m.rLock()
	defer m.rUnlock()

	_, found := m.inverse[value]
	return found
}

// DeleteKey deletes a key from the map and returns its associated values
func (m *BiMultiMap) DeleteKey(key interface{}) []interface{} {
	m.lock()
	defer m.unlock()

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
	m.rLock()
	defer m.rUnlock()

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
	m.lock()
	defer m.unlock()

	_, foundValue := m.forward[key]
	_, foundKey := m.inverse[value]

	if foundKey && foundValue {
		newVals := deleteElement(m.forward[key], value)
		if len(newVals) > 0 {
			m.forward[key] = newVals
		} else {
			delete(m.forward, key)
		}

		newKeys := deleteElement(m.inverse[value], key)
		if len(newKeys) > 0 {
			m.inverse[value] = newKeys
		} else {
			delete(m.inverse, value)
		}
	}
}

// Merge merges two BiMultiMaps: returns a new BiMultiMap consisting of all the key/value pairs in
// this one and all key/value pairs in the other one
func (m *BiMultiMap) Merge(other *BiMultiMap) *BiMultiMap {
	m.rLock()
	other.rLock()
	defer func() {
		other.rUnlock()
		m.rUnlock()
	}()

	res := New()

	for _, k := range m.Keys() {
		for _, v := range m.LookupKey(k) {
			res.Add(k, v)
		}
	}

	for _, k := range other.Keys() {
		for _, v := range other.LookupKey(k) {
			res.Add(k, v)
		}
	}

	return res
}

// Clear clears all entries in the BiMultiMap
func (m *BiMultiMap) Clear() {
	m.lock()
	defer m.unlock()

	m.forward = make(map[interface{}][]interface{})
	m.inverse = make(map[interface{}][]interface{})
}

// Keys returns an unordered slice containing all of the map's keys
func (m *BiMultiMap) Keys() []interface{} {
	m.rLock()
	defer m.rUnlock()

	keys := make([]interface{}, 0, len(m.forward))
	for k := range m.forward {
		keys = append(keys, k)
	}
	return keys
}

// Values returns an unordered slice containing all of the map's values
func (m *BiMultiMap) Values() []interface{} {
	m.rLock()
	defer m.rUnlock()

	values := make([]interface{}, 0, len(m.inverse))
	for v := range m.inverse {
		values = append(values, v)
	}
	return values
}

// IsRLocked returns true if the BiMultiMap is read-locked
func (m *BiMultiMap) IsRLocked() bool {
	return m.readLocked
}

// IsLocked returns true if the BiMultiMap is write-locked
func (m *BiMultiMap) IsLocked() bool {
	return m.writeLocked
}

// RLock locks the BiMultiMap for reading. It will wait until the lock is available.
func (m *BiMultiMap) RLock() {
	m.mutex.RLock()
	m.readLocked = true
}

// Lock locks the BiMultiMap for writing. It will wait until the lock is available.
func (m *BiMultiMap) Lock() {
	m.mutex.Lock()
	m.writeLocked = true
}

// RUnlock releases a read lock on the BiMultiMap. If it's not locked it's a no-op.
func (m *BiMultiMap) RUnlock() {
	m.mutex.RUnlock()
	m.writeLocked = false
}

// Unlock releases a write lock on the BiMultiMap. If it's not locked it's a no-op.
func (m *BiMultiMap) Unlock() {
	m.mutex.Unlock()
	m.writeLocked = false
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

// Helper function: read-lock the BiMultiMap if it's not already locked
func (m *BiMultiMap) rLock() {
	if !m.readLocked && !m.writeLocked {
		m.mutex.RLock()
	}
}

// Helper function: write-lock the BiMultiMap if it's not already locked
func (m *BiMultiMap) lock() {
	if !m.writeLocked {
		m.mutex.Lock()
	}
}

// Helper function: unlock the BiMultiMap from a ReadLock if it wasn't previously read-locked
func (m *BiMultiMap) rUnlock() {
	if !m.readLocked && !m.writeLocked {
		m.mutex.RUnlock()
	}
}

// Helper function: unlock the BiMultiMap from a WriteLock if it wasn't previously write-locked
func (m *BiMultiMap) unlock() {
	if !m.writeLocked {
		m.mutex.Unlock()
	}
}
