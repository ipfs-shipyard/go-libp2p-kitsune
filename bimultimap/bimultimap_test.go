package bimultimap

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewBiMultiMap(t *testing.T) {
	sut := New()
	expected := &BiMultiMap{
		forward:     make(map[interface{}][]interface{}),
		inverse:     make(map[interface{}][]interface{}),
		readLocked:  false,
		writeLocked: false,
	}
	assert.Equal(t, expected, sut, "a new BiMultiMap should be empty")
}

func TestBiMultiMapPut(t *testing.T) {
	sut := New()
	sut.Add("key", "value")

	assert.True(t, sut.KeyExists("key"), "the key should exist")
	assert.Equal(t, []interface{}{"value"}, sut.LookupKey("key"), "the value associated with the key should be the correct one")

	assert.True(t, sut.ValueExists("value"), "the value should exist")
	assert.Equal(t, []interface{}{"key"}, sut.LookupValue("value"), "the key associated with the value should be the correct one")
}

func TestBiMultiMapPutDup(t *testing.T) {
	sut := New()
	sut.Add("key", "value")
	sut.Add("key", "value")

	assert.True(t, sut.KeyExists("key"), "the key should exist")
	assert.Equal(t, []interface{}{"value"}, sut.LookupKey("key"), "the value associated with the key should not be duplicated")

	assert.True(t, sut.ValueExists("value"), "the value should exist")
	assert.Equal(t, []interface{}{"key"}, sut.LookupValue("value"), "the key associated with the value should not be duplicated")
}

func TestBiMultiMapMultiPut(t *testing.T) {
	sut := biMultiMapWithMultipleKeysValues()

	assert.True(t, sut.KeyExists("key1"), "key1 should exist")
	assert.True(t, sut.KeyExists("key2"), "key2 should exist")
	assert.Equal(t, []interface{}{"value1", "value2"}, sut.LookupKey("key1"), "the values associated with the key should be the correct one")

	assert.True(t, sut.ValueExists("value1"), "value1 should exist")
	assert.True(t, sut.ValueExists("value2"), "value2 should exist")
	assert.Equal(t, []interface{}{"key1", "key2"}, sut.LookupValue("value1"), "the keys associated with the value should be the correct one")
}

func TestBiMultiMapGetEmpty(t *testing.T) {
	sut := New()
	assert.Equal(t, []interface{}{}, sut.LookupValue("foo"), "a nonexistent key should return an empty slice")
	assert.Equal(t, []interface{}{}, sut.LookupKey("foo"), "a nonexistent value should return an empty slice")

	sut.Add("key", "value")
	assert.Equal(t, []interface{}{}, sut.LookupValue("foo"), "a nonexistent key should return an empty slice")
	assert.Equal(t, []interface{}{}, sut.LookupKey("foo"), "a nonexistent value should return an empty slice")
}

func TestBiMultiMapDeleteKey(t *testing.T) {
	sut := New()
	sut.Add("key", "value")

	value := sut.DeleteKey("key")

	assert.Equal(t, []interface{}{}, sut.LookupKey("key"), "deleting a key should delete it")
	assert.Equal(t, []interface{}{}, sut.LookupValue("value"), "a deleted key should delete its values")
	assert.Equal(t, []interface{}{"value"}, value, "deleting a key should return its associated values")
}

func TestBiMultiMapDeleteValue(t *testing.T) {
	sut := New()
	sut.Add("key", "value")

	value := sut.DeleteValue("value")

	assert.Equal(t, []interface{}{}, sut.LookupKey("key"), "deleting a value should delete it")
	assert.Equal(t, []interface{}{}, sut.LookupValue("value"), "a deleted value should delete its keys")
	assert.Equal(t, []interface{}{"key"}, value, "deleting a value should return its associated keys")
}

func TestBiMultiMapDeleteMultiKey(t *testing.T) {
	sut := biMultiMapWithMultipleKeysValues()

	sut.DeleteKey("key1")

	assert.Equal(t, []interface{}{}, sut.LookupKey("key1"), "deleting a key should delete all its values")
	assert.Equal(t, []interface{}{"value1", "value2"}, sut.LookupKey("key2"), "deleting a key should only delete its own values")
	assert.Equal(t, []interface{}{"key2"}, sut.LookupValue("value1"), "deleting a key should delete the inverse")
}

func TestBiMultiMapDeleteMultiValue(t *testing.T) {
	sut := biMultiMapWithMultipleKeysValues()

	sut.DeleteValue("value1")

	assert.Equal(t, []interface{}{}, sut.LookupValue("value1"), "deleting a value should delete all its keys")
	assert.Equal(t, []interface{}{"key1", "key2"}, sut.LookupValue("value2"), "deleting a value should only delete its own keys")
	assert.Equal(t, []interface{}{"value2"}, sut.LookupKey("key1"), "deleting a value should delete the inverse")
}

func TestMultiMapDeleteKeyValue(t *testing.T) {
	sut := biMultiMapWithMultipleKeysValues()

	sut.DeleteKeyValue("key1", "value1")

	assert.Equal(t, []interface{}{"value2"}, sut.LookupKey("key1"), "deleting a key/value pair should delete only that key/value pair from its key")
	assert.Equal(t, []interface{}{"value1", "value2"}, sut.LookupKey("key2"), "deleting a key/value pair should not affect other keys")
	assert.Equal(t, []interface{}{"key2"}, sut.LookupValue("value1"), "deleting a key/value pair delete only that key/value pair from its value")
	assert.Equal(t, []interface{}{"key1", "key2"}, sut.LookupValue("value2"), "deleting a key/value pair should not affect other values")
}

func TestBiMultiMapKeysValues(t *testing.T) {
	sut := biMultiMapWithMultipleKeysValues()
	sut.Add("key3", "value3")

	keys := sut.Keys()
	values := sut.Values()

	sort.Slice(keys, func(i, j int) bool {
		return keys[i].(string) < keys[j].(string)
	})

	sort.Slice(values, func(i, j int) bool {
		return values[i].(string) < values[j].(string)
	})

	assert.Equal(t, []interface{}{"key1", "key2", "key3"}, keys, "Keys() should return a slice containing the keys")
	assert.Equal(t, []interface{}{"value1", "value2", "value3"}, values, "Values() should return a slice containing the keys")
}

func TestBiMultiMapWriteLock(t *testing.T) {
	sut := New()

	go func() {
		sut.Lock()
		assert.Truef(t, sut.IsLocked(), "Lock() should lock the BiMultiMap for writing")

		time.Sleep(50 * time.Millisecond)
		sut.Add("key1", "value1")
		time.Sleep(50 * time.Millisecond)
		sut.Add("key1", "value2")
		time.Sleep(50 * time.Millisecond)
		sut.Add("key2", "value1")
		time.Sleep(50 * time.Millisecond)
		sut.Add("key2", "value2")
		time.Sleep(50 * time.Millisecond)

		sut.Unlock()
	}()

	// Wait a moment for the goroutine to start
	time.Sleep(50 * time.Millisecond)

	sut.Lock()

	sut.DeleteKeyValue("key1", "value1")
	time.Sleep(50 * time.Millisecond)
	sut.DeleteKeyValue("key1", "value2")
	time.Sleep(50 * time.Millisecond)
	sut.DeleteKeyValue("key2", "value1")
	time.Sleep(50 * time.Millisecond)
	sut.DeleteKeyValue("key2", "value2")
	time.Sleep(50 * time.Millisecond)

	sut.Unlock()

	expected := &BiMultiMap{
		forward:     make(map[interface{}][]interface{}),
		inverse:     make(map[interface{}][]interface{}),
		readLocked:  false,
		writeLocked: false,
	}

	assert.Equal(t, expected, sut, "Lock() should lock the BiMultiMap for writing and RLock() for reading")
}

func TestBiMultiMapClear(t *testing.T) {
	sut := biMultiMapWithMultipleKeysValues()
	sut.Clear()

	assert.Equal(t, []interface{}{}, sut.Keys())
	assert.Equal(t, []interface{}{}, sut.Values())
}

func TestBiMultiMapReadLock(t *testing.T) {
	sut := New()

	go func() {
		sut.Lock()
		assert.Truef(t, sut.IsLocked(), "Lock() should lock the BiMultiMap for writing")
		assert.Falsef(t, sut.IsRLocked(), "Lock() should not lock the BiMultiMap for reading")

		time.Sleep(50 * time.Millisecond)
		sut.Add("key1", "value1")
		time.Sleep(50 * time.Millisecond)
		sut.Add("key1", "value2")
		time.Sleep(50 * time.Millisecond)
		sut.Add("key2", "value1")
		time.Sleep(50 * time.Millisecond)
		sut.Add("key2", "value2")
		time.Sleep(50 * time.Millisecond)

		sut.Unlock()
	}()

	// Wait a moment for the goroutine to start
	time.Sleep(50 * time.Millisecond)

	sut.RLock()
	assert.Truef(t, sut.IsRLocked(), "RLock() should lock the BiMultiMap for writing")
	assert.Falsef(t, sut.IsLocked(), "RLock() should not lock the BiMultiMap for reading")
	assert.Equal(t, []interface{}{"value1", "value2"}, sut.LookupKey("key1"), "RLock() should wait for a write lock to be unlocked")
	assert.Equal(t, []interface{}{"value1", "value2"}, sut.LookupKey("key2"), "RLock() should wait for a write lock to be unlocked")
}

func biMultiMapWithMultipleKeysValues() *BiMultiMap {
	m := New()
	m.Add("key1", "value1")
	m.Add("key1", "value2")
	m.Add("key2", "value1")
	m.Add("key2", "value2")

	return m
}
