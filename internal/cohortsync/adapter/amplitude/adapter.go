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
	"net/url"
	"strings"
	"time"

	core "github.com/Phixsura/attune/internal/cohortsync"
	"github.com/Phixsura/attune/internal/pkg/logext"
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
		logext.Warnf(ctx, "[amplitude.Check] request failed,err:%s", err.Error())
		return core.CheckResult{OK: false, Error: err.Error()}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return core.CheckResult{OK: true}, nil
	}
	msg := fmt.Sprintf("amplitude API returned %d", resp.StatusCode)
	logext.Warnf(ctx, "[amplitude.Check] %s", msg)
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

	requestID, err := a.requestCohortExport(ctx, baseURL, apiKey, secretKey, externalCohortID)
	if err != nil {
		return core.SyncPayload{}, err
	}
	if err := a.pollExportStatus(ctx, baseURL, apiKey, secretKey, requestID); err != nil {
		return core.SyncPayload{}, err
	}
	userIDs, err := a.downloadAndParseCohort(ctx, baseURL, apiKey, secretKey, requestID)
	if err != nil {
		return core.SyncPayload{}, err
	}

	deltas := make([]core.MemberDelta, 0, len(userIDs))
	for _, uid := range userIDs {
		if uid == "" {
			continue
		}
		deltas = append(deltas, core.MemberDelta{ExternalUserID: uid, Action: "add"})
	}
	return core.SyncPayload{
		Provider:         providerID,
		ExternalCohortID: externalCohortID,
		IsFullSnapshot:   true,
		Deltas:           deltas,
	}, nil
}

func (a *Adapter) requestCohortExport(ctx context.Context, baseURL, apiKey, secretKey, cohortID string) (string, error) {
	reqURL := baseURL + "/api/5/cohorts/request/" + url.PathEscape(cohortID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("amplitude: create request: %w", err)
	}
	req.SetBasicAuth(apiKey, secretKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("amplitude: request cohort: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("amplitude: request cohort returned %d", resp.StatusCode)
	}
	var out struct {
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { // ptrext:allow decode-out-param
		return "", fmt.Errorf("amplitude: parse request response: %w", err)
	}
	return out.RequestID, nil
}

func (a *Adapter) pollExportStatus(ctx context.Context, baseURL, apiKey, secretKey, requestID string) error {
	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		statusURL := baseURL + "/api/5/cohorts/request/" + url.PathEscape(requestID) + "/status"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
		if err != nil {
			return fmt.Errorf("amplitude: create status request: %w", err)
		}
		req.SetBasicAuth(apiKey, secretKey)
		resp, err := a.client.Do(req)
		if err != nil {
			return fmt.Errorf("amplitude: poll status: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return fmt.Errorf("amplitude: status poll returned %d", resp.StatusCode)
		}
		var status struct {
			AsyncStatus string `json:"async_status"`
		}
		decErr := json.NewDecoder(resp.Body).Decode(&status) // ptrext:allow decode-out-param
		_ = resp.Body.Close()
		if decErr != nil {
			return fmt.Errorf("amplitude: parse status response: %w", decErr)
		}
		if strings.EqualFold(status.AsyncStatus, "COMPLETE") {
			return nil
		}
		if strings.EqualFold(status.AsyncStatus, "FAILED") {
			return fmt.Errorf("amplitude: cohort export failed")
		}
	}
	return fmt.Errorf("amplitude: cohort export timed out after 2 minutes")
}

func (a *Adapter) downloadAndParseCohort(ctx context.Context, baseURL, apiKey, secretKey, requestID string) ([]string, error) {
	dlURL := baseURL + "/api/5/cohorts/request/" + url.PathEscape(requestID) + "/file"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return nil, fmt.Errorf("amplitude: create download request: %w", err)
	}
	req.SetBasicAuth(apiKey, secretKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("amplitude: download cohort: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("amplitude: download returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20)) // 64MB limit
	if err != nil {
		return nil, fmt.Errorf("amplitude: read download: %w", err)
	}

	var userIDs []string
	if err := json.Unmarshal(body, &userIDs); err != nil { // ptrext:allow unmarshal-out-param
		// Fallback: try line-separated format.
		for _, line := range strings.Split(string(body), "\n") {
			uid := strings.TrimSpace(line)
			if uid != "" {
				userIDs = append(userIDs, uid)
			}
		}
	}
	return userIDs, nil
}

func splitPullCredential(cred []byte) (string, string, error) {
	parts := strings.SplitN(string(cred), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("amplitude: pull credential must be 'api_key:secret_key'")
	}
	return parts[0], parts[1], nil
}
