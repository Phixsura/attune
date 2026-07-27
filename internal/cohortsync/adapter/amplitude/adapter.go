// SPDX-License-Identifier: Apache-2.0

// Package amplitude implements the cohort sync adapter for Amplitude's
// list-based cohort destination protocol.
package amplitude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// Check verifies Amplitude API credentials by listing cohorts.
func (a *Adapter) Check(ctx context.Context, conn core.Connection) (core.CheckResult, error) {
	baseURL := conn.BaseURL
	if baseURL == "" {
		baseURL = "https://amplitude.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/5/cohorts/list", nil)
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
	msg := fmt.Sprintf("amplitude API returned %d", resp.StatusCode)
	return core.CheckResult{OK: false, Error: msg}, fmt.Errorf("%s", msg)
}

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

// PullCohort fetches the current full membership via the Amplitude Behavioral
// Cohorts Download API (async three-step: request → poll → download).
// Rate-limited at 500 requests/month; operator-initiated only.
// The credential is expected to be "api_key:secret_key" (pull credential).
func (a *Adapter) PullCohort(ctx context.Context, conn core.Connection, externalCohortID string) (core.SyncPayload, error) {
	baseURL := conn.BaseURL
	if baseURL == "" {
		baseURL = "https://amplitude.com"
	}
	apiKey, secretKey, err := splitPullCredential(conn.Credential)
	if err != nil {
		return core.SyncPayload{}, err
	}

	// Step 1: Request cohort export.
	reqURL := fmt.Sprintf("%s/api/5/cohorts/request/%s", baseURL, externalCohortID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return core.SyncPayload{}, fmt.Errorf("amplitude: create request: %w", err)
	}
	req.SetBasicAuth(apiKey, secretKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return core.SyncPayload{}, fmt.Errorf("amplitude: request cohort: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return core.SyncPayload{}, fmt.Errorf("amplitude: request cohort returned %d", resp.StatusCode)
	}
	var reqResp struct {
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reqResp); err != nil { // ptrext:allow decode-out-param
		return core.SyncPayload{}, fmt.Errorf("amplitude: parse request response: %w", err)
	}

	// Step 2: Poll for completion.
	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return core.SyncPayload{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}

		statusURL := fmt.Sprintf("%s/api/5/cohorts/request/%s/status", baseURL, reqResp.RequestID)
		statusReq, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
		if err != nil {
			return core.SyncPayload{}, fmt.Errorf("amplitude: create status request: %w", err)
		}
		statusReq.SetBasicAuth(apiKey, secretKey)
		statusResp, err := a.client.Do(statusReq)
		if err != nil {
			return core.SyncPayload{}, fmt.Errorf("amplitude: poll status: %w", err)
		}
		var status struct {
			AsyncStatus string `json:"async_status"`
		}
		_ = json.NewDecoder(statusResp.Body).Decode(&status) // ptrext:allow decode-out-param
		_ = statusResp.Body.Close()
		if strings.EqualFold(status.AsyncStatus, "COMPLETE") {
			break
		}
		if strings.EqualFold(status.AsyncStatus, "FAILED") {
			return core.SyncPayload{}, fmt.Errorf("amplitude: cohort export failed")
		}
		if i == 59 {
			return core.SyncPayload{}, fmt.Errorf("amplitude: cohort export timed out after 2 minutes")
		}
	}

	// Step 3: Download the result.
	dlURL := fmt.Sprintf("%s/api/5/cohorts/request/%s/file", baseURL, reqResp.RequestID)
	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return core.SyncPayload{}, fmt.Errorf("amplitude: create download request: %w", err)
	}
	dlReq.SetBasicAuth(apiKey, secretKey)
	dlResp, err := a.client.Do(dlReq)
	if err != nil {
		return core.SyncPayload{}, fmt.Errorf("amplitude: download cohort: %w", err)
	}
	defer func() { _ = dlResp.Body.Close() }()
	if dlResp.StatusCode != http.StatusOK {
		return core.SyncPayload{}, fmt.Errorf("amplitude: download returned %d", dlResp.StatusCode)
	}

	// Parse the response (JSON array of user IDs or CSV).
	body, err := io.ReadAll(io.LimitReader(dlResp.Body, 64<<20)) // 64MB limit
	if err != nil {
		return core.SyncPayload{}, fmt.Errorf("amplitude: read download: %w", err)
	}

	var userIDs []string
	if err := json.Unmarshal(body, &userIDs); err != nil { // ptrext:allow unmarshal-out-param
		// Fallback: try line-separated format
		for _, line := range strings.Split(string(body), "\n") {
			uid := strings.TrimSpace(line)
			if uid != "" {
				userIDs = append(userIDs, uid)
			}
		}
	}

	deltas := make([]core.MemberDelta, 0, len(userIDs))
	for _, uid := range userIDs {
		if uid == "" {
			continue
		}
		deltas = append(deltas, core.MemberDelta{
			ExternalUserID: uid,
			Action:         "add",
		})
	}

	return core.SyncPayload{
		Provider:         providerID,
		ExternalCohortID: externalCohortID,
		IsFullSnapshot:   true,
		Deltas:           deltas,
	}, nil
}

func splitPullCredential(cred []byte) (string, string, error) {
	parts := strings.SplitN(string(cred), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("amplitude: pull credential must be 'api_key:secret_key'")
	}
	return parts[0], parts[1], nil
}
