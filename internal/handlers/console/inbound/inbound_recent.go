// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
)

// RecentFeedback handles GET /fb/v1/console/inbound/sources/{id}/recent.
// Returns the last 5 feedback items linked to this inbound source.
// Uses a raw http.HandlerFunc because the response is a lightweight
// operational shape, not a proto message.
func (h *Handler) RecentFeedback(w http.ResponseWriter, r *http.Request) {
	const where = "console.inbound.RecentFeedback"
	auth := session.FromContext(r.Context())
	sourceID := chi.URLParam(r, "id")

	src, err := h.getOwnedSource(r.Context(), auth, sourceID, where)
	if err != nil {
		http.Error(w, "source not found", http.StatusNotFound)
		return
	}

	items, err := h.queryRecentFeedback(r.Context(), src.ID)
	if err != nil {
		logext.Errorf(r.Context(), "[%s] query failed,tenant_id:%s,source_id:%s,err:%+v", where, auth.TenantID, src.ID, err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"items": items}) //nolint:errcheck
}

type recentFeedbackItem struct {
	ID             int64          `json:"id"`
	ContentPreview string         `json:"content_preview"`
	Source         string         `json:"source"`
	SourceMeta     map[string]any `json:"source_meta,omitempty"`
	CreatedAt      string         `json:"created_at"`
}

func (h *Handler) queryRecentFeedback(ctx context.Context, sourceID string) ([]recentFeedbackItem, error) {
	if h.pool == nil {
		return nil, nil
	}
	const q = `SELECT id, LEFT(content, 120) AS content_preview, source, source_meta, created_at
	           FROM user_feedback
	           WHERE inbound_source_id = $1
	           ORDER BY created_at DESC
	           LIMIT 5`
	rows, err := h.pool.Query(ctx, q, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []recentFeedbackItem
	for rows.Next() {
		var item recentFeedbackItem
		var meta map[string]any
		if err := rows.Scan(&item.ID, &item.ContentPreview, &item.Source, &meta, &item.CreatedAt); err != nil { // ptrext:allow pgx-scan
			return nil, err
		}
		if meta != nil {
			item.SourceMeta = map[string]any{}
			for _, k := range []string{"zendesk_ticket_id", "zendesk_status", "zendesk_priority", "slack_channel_name"} {
				if v, ok := meta[k]; ok {
					item.SourceMeta[k] = v
				}
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
