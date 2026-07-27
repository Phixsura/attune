// SPDX-License-Identifier: Apache-2.0

// Package amplitude implements the cohort sync adapter for Amplitude's
// list-based cohort destination protocol.
package amplitude

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	core "github.com/Phixsura/attune/internal/cohortsync"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const providerID = "amplitude"

// Adapter implements cohortsync.Provider for Amplitude.
type Adapter struct {
	client *http.Client
}

func init() {
	core.Register(providerID, "Amplitude", func() core.Provider {
		return ptrext.Of(Adapter{client: core.NewHTTPClient(30 * time.Second)})
	})
}

// Provider returns the stable provider token.
func (a *Adapter) Provider() string { return providerID }

// webhookPayload is the JSON shape Amplitude sends to list-based destinations.
type webhookPayload struct {
	CohortID   string   `json:"cohort_id"`
	CohortName string   `json:"cohort_name"`
	Operation  string   `json:"operation"`
	UserIDs    []string `json:"user_ids"`
	UserIDType string   `json:"user_id_type"`
}

// ParseWebhook normalizes an Amplitude list-based cohort sync request.
// The operation (create/add/remove) may come from the JSON body or from the
// URL path suffix passed as headers["x-operation"] by the webhook handler.
func (a *Adapter) ParseWebhook(body []byte, headers map[string]string, _ []byte) (core.SyncPayload, error) {
	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil { // ptrext:allow unmarshal-out-param
		return core.SyncPayload{}, fmt.Errorf("amplitude: invalid JSON: %w", err)
	}

	if p.CohortID == "" {
		return core.SyncPayload{}, fmt.Errorf("amplitude: cohort_id is required")
	}

	// Determine the operation: prefer explicit header (set by handler from URL path),
	// fall back to JSON body field.
	operation := strings.TrimSpace(strings.ToLower(headers["x-operation"]))
	if operation == "" {
		operation = strings.TrimSpace(strings.ToLower(p.Operation))
	}

	var action string
	switch operation {
	case "create", "add":
		action = "add"
	case "remove":
		action = "remove"
	default:
		return core.SyncPayload{}, fmt.Errorf("amplitude: unknown operation %q", operation)
	}

	deltas := make([]core.MemberDelta, 0, len(p.UserIDs))
	for _, uid := range p.UserIDs {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		deltas = append(deltas, core.MemberDelta{
			ExternalUserID: uid,
			Action:         action,
		})
	}

	return core.SyncPayload{
		Provider:         providerID,
		ExternalCohortID: p.CohortID,
		CohortName:       p.CohortName,
		IsFullSnapshot:   false, // Amplitude is always incremental (add/remove)
		Deltas:           deltas,
	}, nil
}

// PullCohort fetches the current full membership for on-demand refresh via
// the Amplitude Behavioral Cohorts Download API (async three-step).
// This is rate-limited at 500 requests/month; operator-initiated only.
func (a *Adapter) PullCohort(_ context.Context, _ core.Connection, _ string) (core.SyncPayload, error) {
	// PullCohort is implemented as a stub for v1. The Behavioral Cohorts
	// Download API requires async polling (request → poll → download CSV)
	// which will be wired in a follow-up when operator demand justifies it.
	return core.SyncPayload{}, fmt.Errorf("amplitude: PullCohort not yet implemented")
}
