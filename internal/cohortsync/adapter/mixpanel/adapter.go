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

// Check verifies Mixpanel API credentials by calling the Engage API with limit=0.
func (a *Adapter) Check(ctx context.Context, conn core.Connection) (core.CheckResult, error) {
	baseURL := conn.BaseURL
	if baseURL == "" {
		baseURL = "https://mixpanel.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/2.0/engage?page_size=0", nil)
	if err != nil {
		return core.CheckResult{OK: false, Error: err.Error()}, err
	}
	req.SetBasicAuth(string(conn.Credential), "")
	resp, err := a.client.Do(req)
	if err != nil {
		return core.CheckResult{OK: false, Error: err.Error()}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return core.CheckResult{OK: true}, nil
	}
	msg := fmt.Sprintf("mixpanel API returned %d", resp.StatusCode)
	return core.CheckResult{OK: false, Error: msg}, fmt.Errorf("%s", msg)
}

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
// The credential is expected to be "username:secret" (pull credential).
// Operator-initiated only.
func (a *Adapter) PullCohort(ctx context.Context, conn core.Connection, externalCohortID string) (core.SyncPayload, error) {
	baseURL := conn.BaseURL
	if baseURL == "" {
		baseURL = "https://mixpanel.com"
	}
	parts := strings.SplitN(string(conn.Credential), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return core.SyncPayload{}, fmt.Errorf("mixpanel: pull credential must be 'username:secret'")
	}
	username, secret := parts[0], parts[1]

	var allDeltas []core.MemberDelta
	page := 0
	sessionID := ""

	for {
		engageURL := fmt.Sprintf("%s/api/2.0/engage?filter_by_cohort={\"id\":%s}&page_size=1000", baseURL, externalCohortID)
		if sessionID != "" {
			engageURL += "&session_id=" + sessionID + "&page=" + fmt.Sprintf("%d", page)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, engageURL, nil)
		if err != nil {
			return core.SyncPayload{}, fmt.Errorf("mixpanel: create engage request: %w", err)
		}
		req.SetBasicAuth(username, secret)
		resp, err := a.client.Do(req)
		if err != nil {
			return core.SyncPayload{}, fmt.Errorf("mixpanel: engage request: %w", err)
		}

		var engageResp struct {
			Results   []engagePerson `json:"results"`
			Total     int            `json:"total"`
			Page      int            `json:"page"`
			SessionID string         `json:"session_id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&engageResp); err != nil { // ptrext:allow decode-out-param
			_ = resp.Body.Close()
			return core.SyncPayload{}, fmt.Errorf("mixpanel: parse engage response: %w", err)
		}
		_ = resp.Body.Close()

		for _, person := range engageResp.Results {
			uid := strings.TrimSpace(person.DistinctID)
			if uid == "" {
				continue
			}
			props := person.Properties
			allDeltas = append(allDeltas, core.MemberDelta{
				ExternalUserID: uid,
				Email:          strings.TrimSpace(props.Email),
				DisplayName:    buildDisplayName(props.FirstName, props.LastName),
				Action:         "add",
			})
		}

		sessionID = engageResp.SessionID
		if len(engageResp.Results) == 0 || len(allDeltas) >= engageResp.Total {
			break
		}
		page++
		if page > 100 { // safety limit
			break
		}
	}

	return core.SyncPayload{
		Provider:         providerID,
		ExternalCohortID: externalCohortID,
		IsFullSnapshot:   true,
		Deltas:           allDeltas,
	}, nil
}

type engagePerson struct {
	DistinctID string                 `json:"$distinct_id"`
	Properties engagePersonProperties `json:"$properties"`
}

type engagePersonProperties struct {
	Email     string `json:"$email"`
	FirstName string `json:"$first_name"`
	LastName  string `json:"$last_name"`
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
