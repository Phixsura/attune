// SPDX-License-Identifier: Apache-2.0

package outbound

import (
	"fmt"
	"sort"
	"sync"
	"testing"
)

var (
	mu       sync.RWMutex
	channels = map[string]any{}
)

// Register — called from each adapter package's init(). Panics on
// duplicate channel ID (compile-time-equivalent guarantee). The channel
// must implement at least one of EventChannel, DigestChannel, or
// NotificationChannel.
func Register(ch any) {
	var id string
	switch c := ch.(type) {
	case EventChannel:
		id = c.ID()
	case DigestChannel:
		id = c.ID()
	case NotificationChannel:
		id = c.ID()
	default:
		panic("outbound: Register requires EventChannel, DigestChannel, or NotificationChannel")
	}

	mu.Lock()
	defer mu.Unlock()
	if _, exists := channels[id]; exists {
		panic(fmt.Sprintf("outbound: channel %q already registered", id))
	}
	channels[id] = ch
}

// LookupEvent returns the EventChannel for the given destination type,
// or nil if the channel is not registered or does not implement EventChannel.
func LookupEvent(destType string) EventChannel {
	mu.RLock()
	defer mu.RUnlock()
	if ch, ok := channels[destType]; ok {
		if ec, ok := ch.(EventChannel); ok {
			return ec
		}
	}
	return nil
}

// LookupDigest returns the DigestChannel for the given destination type,
// or nil if the channel is not registered or does not implement DigestChannel.
func LookupDigest(destType string) DigestChannel {
	mu.RLock()
	defer mu.RUnlock()
	if ch, ok := channels[destType]; ok {
		if dc, ok := ch.(DigestChannel); ok {
			return dc
		}
	}
	return nil
}

// LookupNotification returns the NotificationChannel for the given destination
// type, or nil if the channel is not registered or does not implement
// NotificationChannel.
func LookupNotification(destType string) NotificationChannel {
	mu.RLock()
	defer mu.RUnlock()
	if ch, ok := channels[destType]; ok {
		if nc, ok := ch.(NotificationChannel); ok {
			return nc
		}
	}
	return nil
}

// Entry — what Channels() returns. Named struct for consumable public API.
type Entry struct {
	ID                   string
	SupportsEvent        bool
	SupportsDigest       bool
	SupportsNotification bool
}

// Channels — snapshot of registered channel IDs. Returns a sorted slice
// for deterministic iteration.
func Channels() []Entry {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Entry, 0, len(channels))
	for id, ch := range channels {
		entry := Entry{ID: id}
		if _, ok := ch.(EventChannel); ok {
			entry.SupportsEvent = true
		}
		if _, ok := ch.(DigestChannel); ok {
			entry.SupportsDigest = true
		}
		if _, ok := ch.(NotificationChannel); ok {
			entry.SupportsNotification = true
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ResetForTest — clears the registry. Test fixtures use it to deduplicate
// across tests that import multiple adapter packages transitively.
// Runtime-gated via testing.Testing() so production binaries panic if
// this is accidentally called.
//
// WARNING: Do not use in parallel tests — use UnregisterForTest instead.
func ResetForTest() {
	if !testing.Testing() {
		panic("outbound.ResetForTest must only be called from tests")
	}
	mu.Lock()
	defer mu.Unlock()
	channels = map[string]any{}
}

// UnregisterForTest — removes a single channel by ID. Safe for parallel tests
// since it only affects the specific adapter registered by that test.
// Runtime-gated via testing.Testing() so production binaries panic if
// this is accidentally called.
func UnregisterForTest(id string) {
	if !testing.Testing() {
		panic("outbound.UnregisterForTest must only be called from tests")
	}
	mu.Lock()
	defer mu.Unlock()
	delete(channels, id)
}
