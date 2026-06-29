// SPDX-License-Identifier: Apache-2.0

// Package sse provides a tenant-scoped Server-Sent Events broker.
//
// The broker fans out events to all connected clients subscribed to a
// given tenant. Each client is a long-lived HTTP connection managed by
// ServeHTTP. The broker is safe for concurrent use.
package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Event is a typed SSE event payload. The Type field maps to the SSE
// "event:" line; Data is JSON-serialized into the "data:" line.
type Event struct {
	Type     string `json:"type"`
	TenantID string `json:"-"`
	Data     any    `json:"data"`
}

type client struct {
	ch       chan []byte
	tenantID string
}

// Broker fans out events to per-tenant SSE subscribers.
type Broker struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
}

// NewBroker creates a ready-to-use SSE broker.
func NewBroker() *Broker {
	return ptrext.Of(Broker{
		clients: make(map[*client]struct{}),
	})
}

// Publish sends an event to all clients subscribed to the event's tenant.
// Non-blocking: slow clients that can't keep up have their events dropped.
func (b *Broker) Publish(evt Event) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	line := fmt.Appendf(nil, "event: %s\ndata: %s\n\n", evt.Type, data)

	b.mu.RLock()
	defer b.mu.RUnlock()

	for c := range b.clients {
		if c.tenantID != evt.TenantID {
			continue
		}
		select {
		case c.ch <- line:
		default:
		}
	}
}

// subscribe registers a client. Returns a cleanup function.
func (b *Broker) subscribe(tenantID string) (*client, func()) {
	c := ptrext.Of(client{
		ch:       make(chan []byte, 64),
		tenantID: tenantID,
	})
	b.mu.Lock()
	b.clients[c] = struct{}{}
	b.mu.Unlock()

	return c, func() {
		b.mu.Lock()
		delete(b.clients, c)
		b.mu.Unlock()
	}
}

// ClientCount returns the number of active SSE connections.
func (b *Broker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// ServeSSE handles an SSE connection for the given tenant. It blocks
// until the client disconnects or the context is cancelled. The caller
// must extract the tenant ID from the session before calling this.
func (b *Broker) ServeSSE(ctx context.Context, w http.ResponseWriter, tenantID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	c, cleanup := b.subscribe(tenantID)
	defer cleanup()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	logext.Infof(ctx, "[sse] client connected,tenant:%s,clients:%d", tenantID, b.ClientCount())

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[sse] client disconnected,tenant:%s", tenantID)
			return
		case msg := <-c.ch:
			if _, err := w.Write(msg); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
