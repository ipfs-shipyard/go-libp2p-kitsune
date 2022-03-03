package bimultimap

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewBiMultiMap(t *testing.T) {
	sut := NewBiMultiMap()
	expected := &BiMultiMap{
		forward: make(map[interface{}][]interface{}),
		inverse: make(map[interface{}][]interface{}),
	}
	assert.Equal(t, expected, sut, "a new BiMultiMap should be empty")
}

func TestBiMultiMapPut(t *testing.T) {
	sut := NewBiMultiMap()
	sut.Put("key", "value")

	assert.True(t, sut.KeyExists("key"), "the key should exist")
	assert.Equal(t, []interface{}{"value"}, sut.GetValues("key"), "the value associated with the key should be the correct one")

	assert.True(t, sut.ValueExists("value"), "the value should exist")
	assert.Equal(t, []interface{}{"key"}, sut.GetKeys("value"), "the key associated with the value should be the correct one")
}

func TestBiMultiMapPutDup(t *testing.T) {
	sut := NewBiMultiMap()
	sut.Put("key", "value")
	sut.Put("key", "value")

	assert.True(t, sut.KeyExists("key"), "the key should exist")
	assert.Equal(t, []interface{}{"value"}, sut.GetValues("key"), "the value associated with the key should not be duplicated")

	assert.True(t, sut.ValueExists("value"), "the value should exist")
	assert.Equal(t, []interface{}{"key"}, sut.GetKeys("value"), "the key associated with the value should not be duplicated")
}

func TestBiMultiMapMultiPut(t *testing.T) {
	sut := biMultiMapWithMultipleKeysValues()

	assert.True(t, sut.KeyExists("key1"), "key1 should exist")
	assert.True(t, sut.KeyExists("key2"), "key2 should exist")
	assert.Equal(t, []interface{}{"value1", "value2"}, sut.GetValues("key1"), "the values associated with the key should be the correct one")

	assert.True(t, sut.ValueExists("value1"), "value1 should exist")
	assert.True(t, sut.ValueExists("value2"), "value2 should exist")
	assert.Equal(t, []interface{}{"key1", "key2"}, sut.GetKeys("value1"), "the keys associated with the value should be the correct one")
}

func TestBiMultiMapGetEmpty(t *testing.T) {
	sut := NewBiMultiMap()
	assert.Equal(t, []interface{}{}, sut.GetKeys("foo"), "a nonexistent key should return an empty slice")
	assert.Equal(t, []interface{}{}, sut.GetValues("foo"), "a nonexistent value should return an empty slice")

	sut.Put("key", "value")
	assert.Equal(t, []interface{}{}, sut.GetKeys("foo"), "a nonexistent key should return an empty slice")
	assert.Equal(t, []interface{}{}, sut.GetValues("foo"), "a nonexistent value should return an empty slice")
}

func TestBiMultiMapDeleteKey(t *testing.T) {
	sut := NewBiMultiMap()
	sut.Put("key", "value")

	value := sut.DeleteKey("key")

	assert.Equal(t, []interface{}{}, sut.GetValues("key"), "deleting a key should delete it")
	assert.Equal(t, []interface{}{}, sut.GetKeys("value"), "a deleted key should delete its values")
	assert.Equal(t, []interface{}{"value"}, value, "deleting a key should return its associated values")
}

func TestBiMultiMapDeleteValue(t *testing.T) {
	sut := NewBiMultiMap()
	sut.Put("key", "value")

	value := sut.DeleteValue("value")

	assert.Equal(t, []interface{}{}, sut.GetValues("key"), "deleting a value should delete it")
	assert.Equal(t, []interface{}{}, sut.GetKeys("value"), "a deleted value should delete its keys")
	assert.Equal(t, []interface{}{"key"}, value, "deleting a value should return its associated keys")
}

func TestBiMultiMapDeleteMultiKey(t *testing.T) {
	sut := biMultiMapWithMultipleKeysValues()

	sut.DeleteKey("key1")

	assert.Equal(t, []interface{}{}, sut.GetValues("key1"), "deleting a key should delete all its values")
	assert.Equal(t, []interface{}{"value1", "value2"}, sut.GetValues("key2"), "deleting a key should only delete its own values")
	assert.Equal(t, []interface{}{"key2"}, sut.GetKeys("value1"), "deleting a key should delete the inverse")
}

func TestBiMultiMapDeleteMultiValue(t *testing.T) {
	sut := biMultiMapWithMultipleKeysValues()

	sut.DeleteValue("value1")

	assert.Equal(t, []interface{}{}, sut.GetKeys("value1"), "deleting a value should delete all its keys")
	assert.Equal(t, []interface{}{"key1", "key2"}, sut.GetKeys("value2"), "deleting a value should only delete its own keys")
	assert.Equal(t, []interface{}{"value2"}, sut.GetValues("key1"), "deleting a value should delete the inverse")
}

func TestMultiMapDeleteKeyValue(t *testing.T) {
	sut := biMultiMapWithMultipleKeysValues()

	sut.DeleteKeyValue("key1", "value1")

	assert.Equal(t, []interface{}{"value2"}, sut.GetValues("key1"), "deleting a key/value pair should delete only that key/value pair from its key")
	assert.Equal(t, []interface{}{"value1", "value2"}, sut.GetValues("key2"), "deleting a key/value pair should not affect other keys")
	assert.Equal(t, []interface{}{"key2"}, sut.GetKeys("value1"), "deleting a key/value pair delete only that key/value pair from its value")
	assert.Equal(t, []interface{}{"key1", "key2"}, sut.GetKeys("value2"), "deleting a key/value pair should not affect other values")
}

func TestBiMultiMapKeysValues(t *testing.T) {
	sut := biMultiMapWithMultipleKeysValues()
	sut.Put("key3", "value3")

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

func biMultiMapWithMultipleKeysValues() *BiMultiMap {
	m := NewBiMultiMap()
	m.Put("key1", "value1")
	m.Put("key1", "value2")
	m.Put("key2", "value1")
	m.Put("key2", "value2")

	return m
}
