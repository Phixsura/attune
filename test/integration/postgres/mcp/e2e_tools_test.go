//go:build integration

package mcp_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/mcp"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	"github.com/Phixsura/attune/internal/testdb"
)

// TestE2E_AllMCPTools tests all 10 MCP tools with real database.
func TestE2E_AllMCPTools(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	// Create tenant
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "e2e-tools-test", "E2E Tools Test")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	// Initialize repos
	clientsRepo := mcprepo.NewClients(pool)
	codesRepo := mcprepo.NewCodes(pool)
	tokensRepo := mcprepo.NewTokens(pool)
	sessionsRepo := mcprepo.NewSessions(pool)
	feedbackRepo := feedback.NewFeedback(pool)
	workflowRepo := workflowstate.New(pool)
	tagRepo := feedbacktag.New(pool)
	tagAssignRepo := feedbacktagassignment.New(pool)

	// Create test workflow states
	stateNew := uuid.New()
	stateInProgress := uuid.New()
	stateClosed := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO tenant_workflow_states (id, tenant_id, name, display_name, color, category, position, is_default)
		VALUES ($2, $1, 'new', '{"en":"New","zh":"新建"}', '#3b82f6', 'open', 0, true),
		       ($3, $1, 'in_progress', '{"en":"In Progress","zh":"处理中"}', '#f59e0b', 'active', 1, false),
		       ($4, $1, 'closed', '{"en":"Closed","zh":"已关闭"}', '#10b981', 'closed', 2, false)
	`, tenantID, stateNew, stateInProgress, stateClosed)
	if err != nil {
		t.Fatalf("create workflow states: %v", err)
	}

	// Create test tag
	tagID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO tenant_feedback_tags (id, tenant_id, name, color, description)
		VALUES ($1, $2, 'bug', '#ef4444', 'Bug reports')
	`, tagID, tenantID)
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}

	// Create test feedback
	var feedbackID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, content, source, type, user_id, enrichment_status, created_at, workflow_state_id)
		VALUES ($1, 'Test feedback content', 'test', 'other', 'user-123', 'pending', NOW(), $2)
		RETURNING id
	`, tenantID, stateNew).Scan(&feedbackID)
	if err != nil {
		t.Fatalf("create feedback: %v", err)
	}

	cfg := mcp.Config{
		BaseURL:            "https://test.attune.io",
		JWTSecret:          []byte("test-secret-key-for-jwt-signing-32bytes!"),
		JWTIssuer:          "https://test.attune.io/mcp/oauth",
		RateLimitPerMinute: 100,
		AccessTokenTTL:     time.Hour,
		RefreshTokenTTL:    7 * 24 * time.Hour,
	}

	stores := mcp.Stores{
		Clients:          &clientStoreAdapter{repo: clientsRepo},
		Codes:            &codeStoreAdapter{repo: codesRepo},
		Tokens:           &tokenStoreAdapter{repo: tokensRepo},
		Sessions:         &sessionStoreAdapter{repo: sessionsRepo},
		ClientValidator:  &clientValidatorAdapter{repo: clientsRepo},
		SessionValidator: &sessionValidatorAdapter{repo: sessionsRepo},
	}

	// Full deps for all tools
	deps := ptrext.Of(tools.Deps{
		Feedback:        &fullFeedbackReaderAdapter{repo: feedbackRepo},
		FeedbackWriter:  &feedbackWriterAdapter{repo: feedbackRepo},
		WorkflowState:   &workflowStateAdapter{repo: workflowRepo},
		WorkflowTransit: &workflowTransitionAdapter{pool: pool},
		Tag:             &tagReaderAdapter{repo: tagRepo},
		TagAssign:       &tagAssignAdapter{repo: tagAssignRepo},
		Ingestor:        &ingestorAdapter{pool: pool},
		Audit:           &noopAuditAdapter{},
	})

	handler := mcp.NewHandler(cfg, stores, deps)
	router := handler.Routes()

	testRouter := chi.NewRouter()
	testRouter.Mount("/mcp", router)

	server := httptest.NewServer(testRouter)
	defer server.Close()

	// Create client with all scopes
	client, err := clientsRepo.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tenantID,
		Name:         "tools-test-agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read", "mcp:write", "mcp:ingest"},
		CreatedBy:    "test-admin",
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	// Get access token
	accessToken := getAccessToken(t, server.URL, client.ID)

	// Test all tools
	t.Run("list_feedback", func(t *testing.T) {
		result := callToolJSON(t, server.URL, accessToken, "list_feedback", map[string]any{})
		items, ok := result["items"].([]any)
		if !ok || len(items) == 0 {
			t.Error("expected at least 1 feedback item")
		}
		t.Logf("list_feedback: got %d items", len(items))
	})

	t.Run("get_feedback", func(t *testing.T) {
		result := callToolJSON(t, server.URL, accessToken, "get_feedback", map[string]any{"id": feedbackID})
		if id, ok := result["id"].(float64); !ok || int64(id) != feedbackID {
			t.Errorf("get_feedback: id = %v, want %d", result["id"], feedbackID)
		}
		t.Logf("get_feedback: got id=%v", result["id"])
	})

	t.Run("list_workflow_states", func(t *testing.T) {
		result := callToolJSON(t, server.URL, accessToken, "list_workflow_states", map[string]any{})
		items, ok := result["items"].([]any)
		if !ok || len(items) != 3 {
			t.Errorf("list_workflow_states: got %d items, want 3", len(items))
		}
		t.Logf("list_workflow_states: got %d states", len(items))
	})

	t.Run("get_workflow_state", func(t *testing.T) {
		result := callToolJSON(t, server.URL, accessToken, "get_workflow_state", map[string]any{"id": stateNew.String()})
		if result["id"] != stateNew.String() {
			t.Errorf("get_workflow_state: id = %v, want %s", result["id"], stateNew.String())
		}
		t.Log("get_workflow_state: success")
	})

	t.Run("list_tags", func(t *testing.T) {
		result := callToolJSON(t, server.URL, accessToken, "list_tags", map[string]any{})
		items, ok := result["items"].([]any)
		if !ok || len(items) != 1 {
			t.Errorf("list_tags: got %d items, want 1", len(items))
		}
		t.Logf("list_tags: got %d tags", len(items))
	})

	t.Run("update_workflow_state", func(t *testing.T) {
		result := callToolJSON(t, server.URL, accessToken, "update_workflow_state", map[string]any{
			"feedback_id": feedbackID,
			"state_id":    stateInProgress.String(),
			"comment":     "Moving to in progress",
		})
		if result["success"] != true {
			t.Error("update_workflow_state: expected success=true")
		}
		t.Log("update_workflow_state: success")
	})

	t.Run("add_tag", func(t *testing.T) {
		result := callToolJSON(t, server.URL, accessToken, "add_tag", map[string]any{
			"feedback_id": feedbackID,
			"tag_id":      tagID.String(),
		})
		if result["success"] != true {
			t.Error("add_tag: expected success=true")
		}
		t.Log("add_tag: success")
	})

	t.Run("remove_tag", func(t *testing.T) {
		result := callToolJSON(t, server.URL, accessToken, "remove_tag", map[string]any{
			"feedback_id": feedbackID,
			"tag_id":      tagID.String(),
		})
		if result["success"] != true {
			t.Error("remove_tag: expected success=true")
		}
		t.Log("remove_tag: success")
	})

	t.Run("set_urgent", func(t *testing.T) {
		result := callToolJSON(t, server.URL, accessToken, "set_urgent", map[string]any{
			"feedback_id": feedbackID,
			"urgent":      true,
		})
		if result["success"] != true {
			t.Error("set_urgent: expected success=true")
		}

		// Verify in database
		var isUrgent bool
		pool.QueryRow(ctx, "SELECT is_urgent FROM user_feedback WHERE id = $1", feedbackID).Scan(&isUrgent)
		if !isUrgent {
			t.Error("set_urgent: database not updated")
		}
		t.Log("set_urgent: success")
	})

	t.Run("submit_feedback", func(t *testing.T) {
		result := callToolJSON(t, server.URL, accessToken, "submit_feedback", map[string]any{
			"content": "New feedback from MCP",
			"source":  "mcp-test",
			"user_id": "mcp-user",
		})
		if id, ok := result["feedback_id"].(float64); !ok || id == 0 {
			t.Error("submit_feedback: no feedback_id returned")
		}
		t.Logf("submit_feedback: created feedback_id=%v", result["feedback_id"])
	})

	t.Log("=== All 10 MCP tools tested successfully ===")
}

// TestE2E_ScopeEnforcement verifies scope restrictions work.
func TestE2E_ScopeEnforcement(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, _ := tenant.NewTenant(pool).Create(ctx, "e2e-scope-test", "E2E Scope Test")

	clientsRepo := mcprepo.NewClients(pool)
	codesRepo := mcprepo.NewCodes(pool)
	tokensRepo := mcprepo.NewTokens(pool)
	sessionsRepo := mcprepo.NewSessions(pool)
	feedbackRepo := feedback.NewFeedback(pool)

	cfg := mcp.Config{
		BaseURL:            "https://test.attune.io",
		JWTSecret:          []byte("test-secret-key-for-jwt-signing-32bytes!"),
		JWTIssuer:          "https://test.attune.io/mcp/oauth",
		RateLimitPerMinute: 100,
		AccessTokenTTL:     time.Hour,
		RefreshTokenTTL:    7 * 24 * time.Hour,
	}

	stores := mcp.Stores{
		Clients:          &clientStoreAdapter{repo: clientsRepo},
		Codes:            &codeStoreAdapter{repo: codesRepo},
		Tokens:           &tokenStoreAdapter{repo: tokensRepo},
		Sessions:         &sessionStoreAdapter{repo: sessionsRepo},
		ClientValidator:  &clientValidatorAdapter{repo: clientsRepo},
		SessionValidator: &sessionValidatorAdapter{repo: sessionsRepo},
	}

	deps := ptrext.Of(tools.Deps{
		Feedback: &feedbackReaderAdapter{repo: feedbackRepo, tenantID: tenantID},
	})

	handler := mcp.NewHandler(cfg, stores, deps)
	router := handler.Routes()

	testRouter := chi.NewRouter()
	testRouter.Mount("/mcp", router)

	server := httptest.NewServer(testRouter)
	defer server.Close()

	// Create client with ONLY read scope
	readOnlyClient, _ := clientsRepo.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tenantID,
		Name:         "read-only-agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read"}, // No write or ingest
		CreatedBy:    "test-admin",
	})

	accessToken := getAccessTokenWithScope(t, server.URL, readOnlyClient.ID, "mcp:read")

	// Read should work
	resp := callTool(t, server.URL, accessToken, "list_feedback", map[string]any{})
	if resp.Error != nil {
		t.Errorf("list_feedback should work with read scope, got error: %+v", resp.Error)
	}

	// Write should fail
	resp = callTool(t, server.URL, accessToken, "set_urgent", map[string]any{
		"feedback_id": 1,
		"urgent":      true,
	})
	if resp.Error == nil {
		t.Error("set_urgent should fail without write scope")
	} else if resp.Error.Code != -32600 { // Invalid request
		t.Errorf("set_urgent error code = %d, want -32600", resp.Error.Code)
	}

	// Ingest should fail
	resp = callTool(t, server.URL, accessToken, "submit_feedback", map[string]any{
		"content": "test",
		"source":  "test",
		"type":    "bug",
		"user_id": "test",
	})
	if resp.Error == nil {
		t.Error("submit_feedback should fail without ingest scope")
	}

	t.Log("Scope enforcement test passed")
}

// Helper to get access token
func getAccessToken(t *testing.T, serverURL string, clientID uuid.UUID) string {
	return getAccessTokenWithScope(t, serverURL, clientID, "mcp:read mcp:write mcp:ingest")
}

func getAccessTokenWithScope(t *testing.T, serverURL string, clientID uuid.UUID, scope string) string {
	t.Helper()

	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	authResp, _ := noRedirectClient.Get(serverURL + "/mcp/oauth/authorize?" + url.Values{
		"client_id":             {clientID.String()},
		"redirect_uri":          {"http://localhost:8080/callback"},
		"response_type":         {"code"},
		"scope":                 {scope},
		"state":                 {"test"},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}.Encode())
	defer authResp.Body.Close()

	location := authResp.Header.Get("Location")
	redirectURL, _ := url.Parse(location)
	authCode := redirectURL.Query().Get("code")

	tokenResp, _ := http.PostForm(serverURL+"/mcp/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authCode},
		"client_id":     {clientID.String()},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"code_verifier": {codeVerifier},
	})
	defer tokenResp.Body.Close()

	var tokenData struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(tokenResp.Body).Decode(&tokenData)
	return tokenData.AccessToken
}

// Helper to call a tool and return raw response
func callTool(t *testing.T, serverURL, accessToken, method string, params any) *jsonrpc.Response {
	t.Helper()

	paramsJSON, _ := json.Marshal(params)
	req := jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
		ID:      "1",
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest(http.MethodPost, serverURL+"/mcp/v1", bytes.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp jsonrpc.Response
	json.NewDecoder(resp.Body).Decode(&rpcResp)
	return &rpcResp
}

// Helper to call a tool and return result as map
func callToolJSON(t *testing.T, serverURL, accessToken, method string, params any) map[string]any {
	t.Helper()

	resp := callTool(t, serverURL, accessToken, method, params)
	if resp.Error != nil {
		t.Fatalf("%s error: code=%d msg=%s", method, resp.Error.Code, resp.Error.Message)
	}

	// Result is already unmarshaled as map[string]any
	if result, ok := resp.Result.(map[string]any); ok {
		return result
	}

	t.Fatalf("%s: unexpected result type %T", method, resp.Result)
	return nil
}

// Additional adapters for full tools testing

type fullFeedbackReaderAdapter struct {
	repo *feedback.FeedbackRepo
}

func (a *fullFeedbackReaderAdapter) ListForConsole(ctx context.Context, tenantID string, opts feedback.ConsoleListOpts) ([]feedback.ConsoleListRow, error) {
	return a.repo.ListForConsole(ctx, tenantID, opts)
}

func (a *fullFeedbackReaderAdapter) GetForConsole(ctx context.Context, tenantID string, id int64) (*feedback.ConsoleDetailRow, error) {
	return a.repo.GetForConsole(ctx, tenantID, id)
}

type feedbackWriterAdapter struct {
	repo *feedback.FeedbackRepo
}

func (a *feedbackWriterAdapter) SetUrgent(ctx context.Context, tenantID string, id int64, urgent bool) error {
	return a.repo.SetUrgent(ctx, tenantID, id, urgent)
}

type workflowStateAdapter struct {
	repo *workflowstate.Repo
}

func (a *workflowStateAdapter) List(ctx context.Context, tenantID string, includeArchived bool) ([]workflowstate.WorkflowState, error) {
	return a.repo.List(ctx, tenantID, includeArchived)
}

func (a *workflowStateAdapter) GetByTenantAndID(ctx context.Context, tenantID, id string) (*workflowstate.WorkflowState, error) {
	return a.repo.GetByTenantAndID(ctx, tenantID, id)
}

type workflowTransitionAdapter struct {
	pool *pgxpool.Pool
}

func (a *workflowTransitionAdapter) Transition(ctx context.Context, tenantID string, feedbackID int64, toStateID, byUser, comment string) error {
	_, err := a.pool.Exec(ctx,
		"UPDATE user_feedback SET workflow_state_id = $1 WHERE id = $2 AND tenant_id = $3",
		toStateID, feedbackID, tenantID,
	)
	return err
}

type tagReaderAdapter struct {
	repo *feedbacktag.Repo
}

func (a *tagReaderAdapter) List(ctx context.Context, tenantID string, includeArchived bool) ([]feedbacktag.Tag, error) {
	return a.repo.List(ctx, tenantID, includeArchived)
}

type tagAssignAdapter struct {
	repo *feedbacktagassignment.Repo
}

func (a *tagAssignAdapter) Add(ctx context.Context, tenantID string, feedbackID int64, tagID uuid.UUID, createdBy string) (bool, error) {
	return a.repo.Add(ctx, tenantID, feedbackID, tagID, createdBy)
}

func (a *tagAssignAdapter) Remove(ctx context.Context, tenantID string, feedbackID int64, tagID uuid.UUID) (bool, error) {
	return a.repo.Remove(ctx, tenantID, feedbackID, tagID)
}

type ingestorAdapter struct {
	pool *pgxpool.Pool
}

func (a *ingestorAdapter) Ingest(ctx context.Context, tenantID, userID string, in domain.IngestInput) (int64, error) {
	var id int64
	err := a.pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, content, source, type, user_id, enrichment_status, created_at)
		VALUES ($1, $2, $3, 'other', $4, 'pending', NOW())
		RETURNING id
	`, tenantID, in.Content, in.Source, userID).Scan(&id)
	return id, err
}

type noopAuditAdapter struct{}

func (a *noopAuditAdapter) Record(ctx context.Context, event tools.AuditEvent) error {
	return nil
}
