// SPDX-License-Identifier: Apache-2.0

// Package appstore is the App Store review ingest adapter. It receives
// push events containing app reviews from Apple App Store / Google Play
// via webhook callbacks (from services like AppFollow, or custom bridges).
// Self-registers via init().
package appstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
)

const channelName = "appstore"

func init() {
	inbound.Register(channelName, "App Store", NewAdapter)
}

type adapter struct {
	deps inbound.Deps
}

// NewAdapter returns a fresh adapter instance.
func NewAdapter() inbound.Adapter { return &adapter{} } // ptrext:allow inbound-handle-identity

// Channel reports the registered channel name.
func (a *adapter) Channel() string { return channelName }

// ShutdownTimeout — push mode, no goroutines.
func (a *adapter) ShutdownTimeout() time.Duration { return 0 }

// Start mounts POST /v1/inbound/appstore/{tenant-slug}/{source-slug}.
func (a *adapter) Start(_ context.Context, deps inbound.Deps) error {
	a.deps = deps
	deps.Mux.Method(
		http.MethodPost,
		"/appstore/{tenant-slug}/{source-slug}",
		http.HandlerFunc(a.handleHTTP),
	)
	return nil
}

// Shutdown is a no-op for push adapters.
func (a *adapter) Shutdown(_ context.Context) error { return nil }

type reviewPayload struct {
	Platform string `json:"platform"`
	AppName  string `json:"app_name"`
	Author   string `json:"author"`
	Rating   int    `json:"rating"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Version  string `json:"version"`
	Country  string `json:"country"`
}

func (a *adapter) handleHTTP(w http.ResponseWriter, r *http.Request) {
	tenantSlug := r.PathValue("tenant-slug")
	sourceSlug := r.PathValue("source-slug")
	where := fmt.Sprintf("appstore/%s/%s", tenantSlug, sourceSlug)

	src, err := a.deps.Sources.GetBySlugs(r.Context(), tenantSlug, channelName, sourceSlug)
	if err != nil || !src.Enabled {
		logext.Warnf(r.Context(), "[%s] source resolve: %v", where, err)
		http.Error(w, "source not found", http.StatusNotFound)
		return
	}

	var payload reviewPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil { // ptrext:allow unmarshal-out-param
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	content := payload.Body
	if payload.Title != "" {
		content = payload.Title + "\n\n" + content
	}

	input := domain.IngestInput{
		Content:    content,
		Source:     channelName,
		SourceUser: payload.Author,
		SourceMeta: map[string]any{
			"platform": payload.Platform,
			"app_name": payload.AppName,
			"rating":   payload.Rating,
			"version":  payload.Version,
			"country":  payload.Country,
		},
	}

	if _, err := a.deps.Ingest.Ingest(r.Context(), src.TenantID, uuid.Nil, input); err != nil {
		logext.Errorf(r.Context(), "[%s] ingest: %v", where, err)
		http.Error(w, "ingest failed", http.StatusInternalServerError)
		return
	}

	a.deps.Metrics.Total(channelName, src.TenantID, sourceSlug, "ok")
	w.WriteHeader(http.StatusAccepted)
}

func ratingSeverity(rating int) string {
	switch {
	case rating <= 2:
		return "high"
	case rating == 3:
		return "medium"
	default:
		return "low"
	}
}
