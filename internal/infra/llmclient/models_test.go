package llmclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListProviderModelsOpenAIStyle(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q; want /v1/models", r.URL.Path)
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "gpt-4o-mini", "owned_by": "openai"},
				{"id": "gpt-4o-mini", "owned_by": "duplicate"},
				{"id": "gpt-4.1-mini", "display_name": "GPT 4.1 mini"},
			},
		})
	}))
	defer srv.Close()

	models, err := ListProviderModels(context.Background(), protocolOpenAICompat, srv.URL, "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if len(models) != 2 {
		t.Fatalf("models = %#v; want 2 deduped models", models)
	}
	if models[0].ID != "gpt-4o-mini" || models[0].OwnedBy != "openai" {
		t.Fatalf("models[0] = %#v", models[0])
	}
	if models[1].ID != "gpt-4.1-mini" || models[1].DisplayName != "GPT 4.1 mini" {
		t.Fatalf("models[1] = %#v", models[1])
	}
}

func TestListProviderModelsGemini(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models" {
			t.Errorf("path = %q; want /v1beta/models", r.URL.Path)
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q; want empty query", r.URL.RawQuery)
		}
		gotKey = r.Header.Get("x-goog-api-key")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{
					"name":                       "models/gemini-2.0-flash",
					"displayName":                "Gemini 2.0 Flash",
					"supportedGenerationMethods": []string{"generateContent"},
				},
				{
					"name":                       "models/embedding-001",
					"displayName":                "Embedding",
					"supportedGenerationMethods": []string{"embedContent"},
				},
			},
		})
	}))
	defer srv.Close()

	models, err := ListProviderModels(context.Background(), protocolGemini, srv.URL, "gemini-key")
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "gemini-key" {
		t.Fatalf("x-goog-api-key = %q", gotKey)
	}
	if len(models) != 1 {
		t.Fatalf("models = %#v; want only generateContent models", models)
	}
	if models[0].ID != "gemini-2.0-flash" || models[0].DisplayName != "Gemini 2.0 Flash" {
		t.Fatalf("models[0] = %#v", models[0])
	}
}

func TestListProviderModelsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := ListProviderModels(context.Background(), protocolOpenAICompat, srv.URL, "")
	if err == nil {
		t.Fatal("expected status error")
	}
}
