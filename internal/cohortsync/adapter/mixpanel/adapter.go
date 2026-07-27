// SPDX-License-Identifier: Apache-2.0

// Package mixpanel implements the cohort sync adapter for Mixpanel's custom
// webhook cohort sync protocol.
package mixpanel

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

const providerID = "mixpanel"

// Adapter implements cohortsync.Provider for Mixpanel.
type Adapter struct {
	client *http.Client
}

func init() {
	core.Register(providerID, "Mixpanel", func() core.Provider {
		return ptrext.Of(Adapter{client: core.NewHTTPClient(30 * time.Second)})
	})
}

// Provider returns the stable provider token.
func (a *Adapter) Provider() string { return providerID }

// webhookPayload is the JSON shape Mixpanel sends to custom webhook destinations.
type webhookPayload struct {
	Action            string          `json:"action"`
	CohortName        string          `json:"cohort_name"`
	CohortID          string          `json:"cohort_id"`
	MixpanelSessionID string          `json:"mixpanel_session_id"`
	Members           []webhookMember `json:"members"`
}

type webhookMember struct {
	Email              string `json:"email"`
	MixpanelDistinctID string `json:"mixpanel_distinct_id"`
	FirstName          string `json:"first_name"`
	LastName           string `json:"last_name"`
}

// ParseWebhook normalizes a Mixpanel custom webhook cohort sync request.
func (a *Adapter) ParseWebhook(body []byte, _ map[string]string, _ []byte) (core.SyncPayload, error) {
	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil { // ptrext:allow unmarshal-out-param
		return core.SyncPayload{}, fmt.Errorf("mixpanel: invalid JSON: %w", err)
	}

	if p.CohortID == "" {
		return core.SyncPayload{}, fmt.Errorf("mixpanel: cohort_id is required")
	}

	action := strings.TrimSpace(strings.ToLower(p.Action))

	var memberAction string
	var isFullSnapshot bool

	switch action {
	case "members":
		memberAction = "add"
		isFullSnapshot = true
	case "add_members":
		memberAction = "add"
	case "remove_members":
		memberAction = "remove"
	default:
		return core.SyncPayload{}, fmt.Errorf("mixpanel: unknown action %q", action)
	}

	deltas := make([]core.MemberDelta, 0, len(p.Members))
	for _, m := range p.Members {
		uid := strings.TrimSpace(m.MixpanelDistinctID)
		if uid == "" {
			continue
		}
		displayName := buildDisplayName(m.FirstName, m.LastName)
		deltas = append(deltas, core.MemberDelta{
			ExternalUserID: uid,
			Email:          strings.TrimSpace(m.Email),
			DisplayName:    displayName,
			Action:         memberAction,
		})
	}

	return core.SyncPayload{
		Provider:         providerID,
		ExternalCohortID: p.CohortID,
		CohortName:       p.CohortName,
		IsFullSnapshot:   isFullSnapshot,
		Deltas:           deltas,
	}, nil
}

// PullCohort fetches the current full membership via the Mixpanel Engage API.
// Operator-initiated only.
func (a *Adapter) PullCohort(_ context.Context, _ core.Connection, _ string) (core.SyncPayload, error) {
	// PullCohort is stubbed for v1. The Engage API query with cohort filter
	// will be wired when operator demand justifies it.
	return core.SyncPayload{}, fmt.Errorf("mixpanel: PullCohort not yet implemented")
}

func buildDisplayName(first, last string) string {
	first = strings.TrimSpace(first)
	last = strings.TrimSpace(last)
	switch {
	case first != "" && last != "":
		return first + " " + last
	case first != "":
		return first
	case last != "":
		return last
	default:
		return ""
	}
}
