// SPDX-License-Identifier: Apache-2.0

package sse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestBroker_PubSub(t *testing.T) {
	b := NewBroker()
	c, cleanup := b.subscribe("tenant-1")
	defer cleanup()

	b.Publish(Event{Type: "feedback.created", TenantID: "tenant-1", Data: map[string]string{"id": "42"}})

	select {
	case msg := <-c.ch:
		s := string(msg)
		if !strings.Contains(s, "event: feedback.created") {
			t.Errorf("expected event line, got %q", s)
		}
		if !strings.Contains(s, `"id":"42"`) {
			t.Errorf("expected data with id, got %q", s)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBroker_TenantIsolation(t *testing.T) {
	b := NewBroker()
	c1, cleanup1 := b.subscribe("tenant-1")
	defer cleanup1()
	c2, cleanup2 := b.subscribe("tenant-2")
	defer cleanup2()

	b.Publish(Event{Type: "test", TenantID: "tenant-1", Data: nil})

	select {
	case <-c1.ch:
	case <-time.After(time.Second):
		t.Fatal("tenant-1 should receive event")
	}

	select {
	case <-c2.ch:
		t.Fatal("tenant-2 should NOT receive event for tenant-1")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBroker_Cleanup(t *testing.T) {
	b := NewBroker()
	_, cleanup := b.subscribe("t1")

	if b.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", b.ClientCount())
	}
	cleanup()
	if b.ClientCount() != 0 {
		t.Fatalf("expected 0 clients after cleanup, got %d", b.ClientCount())
	}
}

func TestBroker_ServeSSE(t *testing.T) {
	b := NewBroker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		b.ServeSSE(ctx, rec, "tenant-1")
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	b.Publish(Event{Type: "update", TenantID: "tenant-1", Data: "hello"})
	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "event: update") {
		t.Errorf("expected event in body, got %q", body)
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("wrong content type: %s", rec.Header().Get("Content-Type"))
	}
}

func TestBroker_NonFlusher(t *testing.T) {
	b := NewBroker()
	w := ptrext.Of(nonFlusher{header: http.Header{}})
	b.ServeSSE(context.Background(), w, "t1")
}

type nonFlusher struct {
	header http.Header
	code   int
}

func (n *nonFlusher) Header() http.Header         { return n.header }
func (n *nonFlusher) Write(b []byte) (int, error) { return len(b), nil }
func (n *nonFlusher) WriteHeader(code int)        { n.code = code }
