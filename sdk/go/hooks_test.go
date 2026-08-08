package attune

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

func TestCreateWebhookSubscription(t *testing.T) {
	var gotReq *http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"aaaaaaaa-1111-2222-3333-444444444444","target_url":"https://hooks.zapier.com/x","event_types":["feedback.created"],"status":"active","consumer":"zapier","created_at":"2026-07-29T00:00:00Z"}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	out, err := c.CreateWebhookSubscription(context.Background(), &CreateWebhookSubscriptionRequest{
		TargetUrl:  "https://hooks.zapier.com/x",
		EventTypes: []string{"feedback.created"},
		Consumer:   "zapier",
	})
	if err != nil {
		t.Fatalf("CreateWebhookSubscription: %v", err)
	}
	if gotReq.Method != http.MethodPost || gotReq.URL.Path != "/v1/hooks" {
		t.Errorf("request = %s %s, want POST /v1/hooks", gotReq.Method, gotReq.URL.Path)
	}
	if !strings.Contains(string(gotBody), `"feedback.created"`) {
		t.Errorf("body = %s", gotBody)
	}
	if out.GetId() == "" || out.GetStatus() != "active" {
		t.Errorf("parsed = %+v", out)
	}
}

func TestCreateWebhookSubscription_NilRequest(t *testing.T) {
	c, _ := newTestClient(t, httptest.NewServer(http.NotFoundHandler()))
	if _, err := c.CreateWebhookSubscription(context.Background(), nil); err == nil {
		t.Fatal("want error for nil request")
	}
}

func TestListWebhookSubscriptions(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		_, _ = io.WriteString(w, `{"subscriptions":[{"id":"s1","status":"active"}]}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	out, err := c.ListWebhookSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("ListWebhookSubscriptions: %v", err)
	}
	if gotReq.Method != http.MethodGet || gotReq.URL.Path != "/v1/hooks" {
		t.Errorf("request = %s %s, want GET /v1/hooks", gotReq.Method, gotReq.URL.Path)
	}
	if len(out.GetSubscriptions()) != 1 {
		t.Errorf("parsed = %+v", out.GetSubscriptions())
	}
}

func TestDeleteWebhookSubscription(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	if err := c.DeleteWebhookSubscription(context.Background(), "sub-1"); err != nil {
		t.Fatalf("DeleteWebhookSubscription: %v", err)
	}
	if gotReq.Method != http.MethodDelete || gotReq.URL.Path != "/v1/hooks/sub-1" {
		t.Errorf("request = %s %s, want DELETE /v1/hooks/sub-1", gotReq.Method, gotReq.URL.Path)
	}

	if err := c.DeleteWebhookSubscription(context.Background(), ""); err == nil {
		t.Fatal("want error for empty id")
	}
}

func TestListWebhookSamples(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		_, _ = io.WriteString(w, `{"samples":[{"version":"2","event_type":"feedback.created"}]}`)
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)

	out, err := c.ListWebhookSamples(context.Background(), "feedback.created")
	if err != nil {
		t.Fatalf("ListWebhookSamples: %v", err)
	}
	if gotReq.URL.Path != "/v1/hooks/samples/feedback.created" {
		t.Errorf("path = %s", gotReq.URL.Path)
	}
	if len(out.GetSamples()) != 1 {
		t.Errorf("samples = %+v", out.GetSamples())
	}
}

func TestRequestAutomationSurface(t *testing.T) {
	var paths []string
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		methods = append(methods, r.Method)
		switch r.Method {
		case http.MethodGet:
			_, _ = io.WriteString(w, `{"requests":[]}`)
		default:
			_, _ = io.WriteString(w, `{"request":{"id":"r1","title":"T","status":"CUSTOMER_REQUEST_STATUS_OPEN"}}`)
		}
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	ctx := context.Background()

	if _, err := c.ListRequests(ctx, &ListRequestsAutomationRequest{
		Status: []attunev1.CustomerRequestStatus{attunev1.CustomerRequestStatus_CUSTOMER_REQUEST_STATUS_OPEN},
	}); err != nil {
		t.Fatalf("ListRequests: %v", err)
	}
	if _, err := c.CreateRequest(ctx, &CreateRequestAutomationRequest{
		Title: "T", IdempotencyKey: "zap-recipe-1",
	}); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if _, err := c.UpdateRequest(ctx, &UpdateRequestAutomationRequest{Id: "r1"}); err != nil {
		t.Fatalf("UpdateRequest: %v", err)
	}
	if _, err := c.AddRequestNote(ctx, &AddRequestNoteAutomationRequest{
		Id: "r1", Body: "note", Visibility: "internal",
	}); err != nil {
		t.Fatalf("AddRequestNote: %v", err)
	}

	want := []string{
		"/v1/requests?status=CUSTOMER_REQUEST_STATUS_OPEN",
		"/v1/requests?",
		"/v1/requests/r1?",
		"/v1/requests/r1/notes?",
	}
	for i, w := range want {
		if paths[i] != w {
			t.Errorf("call %d: path %q want %q", i, paths[i], w)
		}
	}
	if methods[2] != http.MethodPatch {
		t.Errorf("update method = %s want PATCH", methods[2])
	}

	if _, err := c.UpdateRequest(ctx, &UpdateRequestAutomationRequest{}); err == nil {
		t.Fatal("want error for empty request id")
	}
}
