// SPDX-License-Identifier: Apache-2.0

// Package cache provides caching utilities.
package cache

import (
	"container/list"
	"sync"
	"time"
)

// LRU is a thread-safe LRU cache with TTL support.
type LRU[K comparable, V any] struct {
	mu      sync.RWMutex
	maxSize int
	ttl     time.Duration
	items   map[K]*list.Element
	order   *list.List
	nowFunc func() time.Time
}

type entry[K comparable, V any] struct {
	key       K
	value     V
	expiresAt time.Time
}

// NewLRU creates a new LRU cache with the given max size and TTL.
func NewLRU[K comparable, V any](maxSize int, ttl time.Duration) *LRU[K, V] {
	return &LRU[K, V]{ // ptrext:allow generic type
		maxSize: maxSize,
		ttl:     ttl,
		items:   make(map[K]*list.Element),
		order:   list.New(),
		nowFunc: time.Now,
	}
}

// Get retrieves a value from the cache.
// Returns the value and true if found and not expired, zero value and false otherwise.
func (c *LRU[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}

	e := elem.Value.(*entry[K, V])
	if c.nowFunc().After(e.expiresAt) {
		c.removeElement(elem)
		var zero V
		return zero, false
	}

	c.order.MoveToFront(elem)
	return e.value, true
}

// Set adds or updates a value in the cache.
func (c *LRU[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.nowFunc()
	expiresAt := now.Add(c.ttl)

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		e := elem.Value.(*entry[K, V])
		e.value = value
		e.expiresAt = expiresAt
		return
	}

	e := &entry[K, V]{ // ptrext:allow generic type
		key:       key,
		value:     value,
		expiresAt: expiresAt,
	}
	elem := c.order.PushFront(e)
	c.items[key] = elem

	for c.order.Len() > c.maxSize {
		c.removeOldest()
	}
}

// Delete removes a value from the cache.
func (c *LRU[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.removeElement(elem)
	}
}

// Len returns the number of items in the cache.
func (c *LRU[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.order.Len()
}

// Clear removes all items from the cache.
func (c *LRU[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[K]*list.Element)
	c.order.Init()
}

func (c *LRU[K, V]) removeElement(elem *list.Element) {
	c.order.Remove(elem)
	e := elem.Value.(*entry[K, V])
	delete(c.items, e.key)
}

func (c *LRU[K, V]) removeOldest() {
	elem := c.order.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

// Cleanup removes expired entries. Call periodically from a goroutine.
func (c *LRU[K, V]) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.nowFunc()
	var toRemove []*list.Element

	for elem := c.order.Front(); elem != nil; elem = elem.Next() {
		e := elem.Value.(*entry[K, V])
		if now.After(e.expiresAt) {
			toRemove = append(toRemove, elem)
		}
	}

	for _, elem := range toRemove {
		c.removeElement(elem)
	}
}
