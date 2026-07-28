// SPDX-License-Identifier: Apache-2.0

// Package mixpanel implements the cohort sync adapter for Mixpanel's custom
// webhook cohort sync protocol.
package mixpanel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	core "github.com/Phixsura/attune/internal/cohortsync"
	"github.com/Phixsura/attune/internal/pkg/logext"
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
		logext.Warnf(ctx, "[mixpanel.Check] request failed,err:%s", err.Error())
		return core.CheckResult{OK: false, Error: err.Error()}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return core.CheckResult{OK: true}, nil
	}
	msg := fmt.Sprintf("mixpanel API returned %d", resp.StatusCode)
	logext.Warnf(ctx, "[mixpanel.Check] %s", msg)
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

	var allDeltas []core.MemberDelta
	page, sessionID := 0, ""
	for {
		resp, err := a.fetchEngagePage(ctx, baseURL, parts[0], parts[1], externalCohortID, page, sessionID)
		if err != nil {
			return core.SyncPayload{}, err
		}
		allDeltas = append(allDeltas, personsToDelta(resp.Results)...)
		sessionID = resp.SessionID
		if len(resp.Results) == 0 || len(allDeltas) >= resp.Total || page >= 100 {
			break
		}
		page++
	}

	return core.SyncPayload{
		Provider:         providerID,
		ExternalCohortID: externalCohortID,
		IsFullSnapshot:   true,
		Deltas:           allDeltas,
	}, nil
}

type engagePageResult struct {
	Results   []engagePerson
	Total     int
	SessionID string
}

func (a *Adapter) fetchEngagePage(ctx context.Context, baseURL, user, secret, cohortID string, page int, sessionID string) (engagePageResult, error) {
	q := url.Values{}
	q.Set("filter_by_cohort", fmt.Sprintf(`{"id":%s}`, cohortID))
	q.Set("page_size", "1000")
	if sessionID != "" {
		q.Set("session_id", sessionID)
		q.Set("page", fmt.Sprintf("%d", page))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/2.0/engage?"+q.Encode(), nil)
	if err != nil {
		return engagePageResult{}, fmt.Errorf("mixpanel: create engage request: %w", err)
	}
	req.SetBasicAuth(user, secret)
	resp, err := a.client.Do(req)
	if err != nil {
		return engagePageResult{}, fmt.Errorf("mixpanel: engage request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return engagePageResult{}, fmt.Errorf("mixpanel: engage API returned %d on page %d", resp.StatusCode, page)
	}
	var out struct {
		Results   []engagePerson `json:"results"`
		Total     int            `json:"total"`
		SessionID string         `json:"session_id"`
	}
	limited := io.LimitReader(resp.Body, 64<<20)                  // 64MB per-page limit
	if err := json.NewDecoder(limited).Decode(&out); err != nil { // ptrext:allow decode-out-param
		_ = resp.Body.Close()
		return engagePageResult{}, fmt.Errorf("mixpanel: parse engage response: %w", err)
	}
	_ = resp.Body.Close()
	return engagePageResult{Results: out.Results, Total: out.Total, SessionID: out.SessionID}, nil
}

func personsToDelta(persons []engagePerson) []core.MemberDelta {
	deltas := make([]core.MemberDelta, 0, len(persons))
	for _, person := range persons {
		uid := strings.TrimSpace(person.DistinctID)
		if uid == "" {
			continue
		}
		props := person.Properties
		deltas = append(deltas, core.MemberDelta{
			ExternalUserID: uid,
			Email:          strings.TrimSpace(props.Email),
			DisplayName:    buildDisplayName(props.FirstName, props.LastName),
			Action:         "add",
		})
	}
	return deltas
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
