package llmclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListProviderModelsOpenAIStyle(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err)
	require.Equal(t, "Bearer sk-test", gotAuth)
	require.Len(t, models, 2)
	require.Equal(t, "gpt-4o-mini", models[0].ID)
	require.Equal(t, "openai", models[0].OwnedBy)
	require.Equal(t, "gpt-4.1-mini", models[1].ID)
	require.Equal(t, "GPT 4.1 mini", models[1].DisplayName)
}

func TestListProviderModelsGemini(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err)
	require.Equal(t, "gemini-key", gotKey)
	require.Len(t, models, 1)
	require.Equal(t, "gemini-2.0-flash", models[0].ID)
	require.Equal(t, "Gemini 2.0 Flash", models[0].DisplayName)
}

func TestListProviderModelsStatusError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := ListProviderModels(context.Background(), protocolOpenAICompat, srv.URL, "")
	require.Error(t, err)
}

func TestListProviderModels_UnsupportedProtocol(t *testing.T) {
	t.Parallel()
	_, err := ListProviderModels(context.Background(), "unknown-proto", "http://localhost", "key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported protocol")
}

func TestListProviderModels_OpenAIResponsesProtocol(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "gpt-4.1"},
			},
		})
	}))
	defer srv.Close()

	models, err := ListProviderModels(context.Background(), protocolOpenAIResponses, srv.URL, "sk-resp")
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "gpt-4.1", models[0].ID)
}

func TestListProviderModels_AnthropicHeaders(t *testing.T) {
	t.Parallel()
	var gotAPIKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "claude-sonnet-4-20250514", "display_name": "Claude Sonnet 4"},
			},
		})
	}))
	defer srv.Close()

	models, err := ListProviderModels(context.Background(), protocolAnthropic, srv.URL, "sk-ant-test")
	require.NoError(t, err)
	require.Equal(t, "sk-ant-test", gotAPIKey)
	require.Equal(t, "2023-06-01", gotVersion)
	require.Len(t, models, 1)
	require.Equal(t, "claude-sonnet-4-20250514", models[0].ID)
}

func TestListProviderModels_AnthropicRequiresAPIKey(t *testing.T) {
	t.Parallel()
	_, err := ListProviderModels(context.Background(), protocolAnthropic, "http://localhost", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key is required")
}

func TestListProviderModels_GeminiRequiresAPIKey(t *testing.T) {
	t.Parallel()
	_, err := ListProviderModels(context.Background(), protocolGemini, "http://localhost", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key is required")
}

func TestListProviderModels_OpenAINoAuth(t *testing.T) {
	t.Parallel()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"id": "local-model"},
			},
		})
	}))
	defer srv.Close()

	models, err := ListProviderModels(context.Background(), protocolOpenAICompat, srv.URL, "")
	require.NoError(t, err)
	require.Empty(t, gotAuth)
	require.Len(t, models, 1)
}

func TestModelsURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		baseURL   string
		fallback  string
		appendV1  bool
		want      string
		wantError bool
	}{
		{
			name:     "base URL with trailing slash and appendV1",
			baseURL:  "http://localhost:8080/",
			fallback: "https://api.openai.com",
			appendV1: true,
			want:     "http://localhost:8080/v1/models",
		},
		{
			name:     "base URL already has /v1",
			baseURL:  "http://localhost:8080/v1",
			fallback: "https://api.openai.com",
			appendV1: true,
			want:     "http://localhost:8080/v1/models",
		},
		{
			name:     "empty base URL uses fallback",
			baseURL:  "",
			fallback: "https://api.openai.com",
			appendV1: true,
			want:     "https://api.openai.com/v1/models",
		},
		{
			name:     "appendV1 false",
			baseURL:  "http://localhost:8080",
			fallback: "",
			appendV1: false,
			want:     "http://localhost:8080/models",
		},
		{
			name:      "empty base and empty fallback",
			baseURL:   "",
			fallback:  "",
			appendV1:  true,
			wantError: true,
		},
		{
			name:     "multiple trailing slashes",
			baseURL:  "http://localhost:8080///",
			fallback: "",
			appendV1: true,
			want:     "http://localhost:8080/v1/models",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := modelsURL(tt.baseURL, tt.fallback, tt.appendV1)
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGeminiModelsURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{
			name:    "empty uses default",
			baseURL: "",
			want:    "https://generativelanguage.googleapis.com/v1beta/models",
		},
		{
			name:    "custom base appends v1beta",
			baseURL: "http://localhost:9090",
			want:    "http://localhost:9090/v1beta/models",
		},
		{
			name:    "already has /v1beta",
			baseURL: "http://localhost:9090/v1beta",
			want:    "http://localhost:9090/v1beta/models",
		},
		{
			name:    "already has /v1",
			baseURL: "http://localhost:9090/v1",
			want:    "http://localhost:9090/v1/models",
		},
		{
			name:    "trailing slashes stripped",
			baseURL: "http://localhost:9090///",
			want:    "http://localhost:9090/v1beta/models",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := geminiModelsURL(tt.baseURL)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseOpenAIModelList(t *testing.T) {
	t.Parallel()

	t.Run("valid response", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{"data":[{"id":"m1","owned_by":"org"},{"id":"m2","display_name":"Model 2"}]}`)
		models, err := parseOpenAIModelList(raw)
		require.NoError(t, err)
		require.Len(t, models, 2)
		require.Equal(t, "m1", models[0].ID)
		require.Equal(t, "org", models[0].OwnedBy)
		require.Equal(t, "m2", models[1].ID)
		require.Equal(t, "Model 2", models[1].DisplayName)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		t.Parallel()
		_, err := parseOpenAIModelList([]byte(`not json`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "unmarshal")
	})

	t.Run("empty data array", func(t *testing.T) {
		t.Parallel()
		models, err := parseOpenAIModelList([]byte(`{"data":[]}`))
		require.NoError(t, err)
		require.Empty(t, models)
	})

	t.Run("skips empty IDs", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{"data":[{"id":"","owned_by":"x"},{"id":"  ","owned_by":"y"},{"id":"valid"}]}`)
		models, err := parseOpenAIModelList(raw)
		require.NoError(t, err)
		require.Len(t, models, 1)
		require.Equal(t, "valid", models[0].ID)
	})

	t.Run("deduplicates by ID", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{"data":[{"id":"a","owned_by":"first"},{"id":"a","owned_by":"second"}]}`)
		models, err := parseOpenAIModelList(raw)
		require.NoError(t, err)
		require.Len(t, models, 1)
		require.Equal(t, "first", models[0].OwnedBy)
	})
}

func TestParseGeminiModelList(t *testing.T) {
	t.Parallel()

	t.Run("valid response", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{"models":[
			{"name":"models/gemini-pro","displayName":"Gemini Pro","supportedGenerationMethods":["generateContent","countTokens"]},
			{"name":"models/gemini-flash","displayName":"Gemini Flash","supportedGenerationMethods":["generateContent"]}
		]}`)
		models, err := parseGeminiModelList(raw)
		require.NoError(t, err)
		require.Len(t, models, 2)
		require.Equal(t, "gemini-pro", models[0].ID)
		require.Equal(t, "Gemini Pro", models[0].DisplayName)
		require.Equal(t, "google", models[0].OwnedBy)
		require.Equal(t, "gemini-flash", models[1].ID)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		t.Parallel()
		_, err := parseGeminiModelList([]byte(`{broken`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "unmarshal")
	})

	t.Run("filters out non-generateContent models", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{"models":[
			{"name":"models/text-embedding","displayName":"Embedding","supportedGenerationMethods":["embedContent"]},
			{"name":"models/gemini-pro","displayName":"Gemini Pro","supportedGenerationMethods":["generateContent"]}
		]}`)
		models, err := parseGeminiModelList(raw)
		require.NoError(t, err)
		require.Len(t, models, 1)
		require.Equal(t, "gemini-pro", models[0].ID)
	})

	t.Run("strips models/ prefix from name", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{"models":[{"name":"models/my-model","displayName":"My","supportedGenerationMethods":["generateContent"]}]}`)
		models, err := parseGeminiModelList(raw)
		require.NoError(t, err)
		require.Len(t, models, 1)
		require.Equal(t, "my-model", models[0].ID)
	})

	t.Run("empty models array", func(t *testing.T) {
		t.Parallel()
		models, err := parseGeminiModelList([]byte(`{"models":[]}`))
		require.NoError(t, err)
		require.Empty(t, models)
	})

	t.Run("skips models with empty name after prefix strip", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{"models":[
			{"name":"models/","displayName":"Empty","supportedGenerationMethods":["generateContent"]},
			{"name":"models/valid","displayName":"Valid","supportedGenerationMethods":["generateContent"]}
		]}`)
		models, err := parseGeminiModelList(raw)
		require.NoError(t, err)
		require.Len(t, models, 1)
		require.Equal(t, "valid", models[0].ID)
	})

	t.Run("deduplicates by ID", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{"models":[
			{"name":"models/gemini-pro","displayName":"First","supportedGenerationMethods":["generateContent"]},
			{"name":"models/gemini-pro","displayName":"Second","supportedGenerationMethods":["generateContent"]}
		]}`)
		models, err := parseGeminiModelList(raw)
		require.NoError(t, err)
		require.Len(t, models, 1)
		require.Equal(t, "First", models[0].DisplayName)
	})

	t.Run("model without generateContent in methods", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{"models":[{"name":"models/no-gen","displayName":"NoGen","supportedGenerationMethods":["countTokens","embedContent"]}]}`)
		models, err := parseGeminiModelList(raw)
		require.NoError(t, err)
		require.Empty(t, models)
	})
}

func TestListProviderModels_OpenAIBadJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	_, err := ListProviderModels(context.Background(), protocolOpenAICompat, srv.URL, "key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal")
}

func TestListProviderModels_AnthropicBadJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{broken`))
	}))
	defer srv.Close()

	_, err := ListProviderModels(context.Background(), protocolAnthropic, srv.URL, "ant-key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal")
}

func TestListProviderModels_GeminiBadJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid}`))
	}))
	defer srv.Close()

	_, err := ListProviderModels(context.Background(), protocolGemini, srv.URL, "gem-key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal")
}

func TestListProviderModels_AnthropicStatusError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := ListProviderModels(context.Background(), protocolAnthropic, srv.URL, "bad-key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "status 401")
}

func TestListProviderModels_GeminiStatusError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := ListProviderModels(context.Background(), protocolGemini, srv.URL, "bad-key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "status 403")
}

func TestSafeModelTransportError(t *testing.T) {
	t.Parallel()

	t.Run("deadline exceeded", func(t *testing.T) {
		t.Parallel()
		err := safeModelTransportError(context.DeadlineExceeded)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("canceled", func(t *testing.T) {
		t.Parallel()
		err := safeModelTransportError(context.Canceled)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("generic error becomes opaque", func(t *testing.T) {
		t.Parallel()
		original := strings.NewReader("") // arbitrary non-context error source
		_ = original
		err := safeModelTransportError(http.ErrServerClosed)
		require.Error(t, err)
		require.Equal(t, "transport failed", err.Error())
		require.NotErrorIs(t, err, http.ErrServerClosed)
	})
}

func TestAppendModel(t *testing.T) {
	t.Parallel()

	t.Run("appends new model", func(t *testing.T) {
		t.Parallel()
		var models []ModelInfo
		models = appendModel(models, ModelInfo{ID: "a"})
		require.Len(t, models, 1)
		require.Equal(t, "a", models[0].ID)
	})

	t.Run("skips empty ID", func(t *testing.T) {
		t.Parallel()
		var models []ModelInfo
		models = appendModel(models, ModelInfo{ID: ""})
		require.Empty(t, models)
	})

	t.Run("skips whitespace-only ID", func(t *testing.T) {
		t.Parallel()
		var models []ModelInfo
		models = appendModel(models, ModelInfo{ID: "   "})
		require.Empty(t, models)
	})

	t.Run("trims whitespace from ID", func(t *testing.T) {
		t.Parallel()
		var models []ModelInfo
		models = appendModel(models, ModelInfo{ID: "  trimmed  "})
		require.Len(t, models, 1)
		require.Equal(t, "trimmed", models[0].ID)
	})

	t.Run("deduplicates", func(t *testing.T) {
		t.Parallel()
		models := []ModelInfo{{ID: "x", OwnedBy: "first"}}
		models = appendModel(models, ModelInfo{ID: "x", OwnedBy: "second"})
		require.Len(t, models, 1)
		require.Equal(t, "first", models[0].OwnedBy)
	})
}

func TestListProviderModels_CanceledContext(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ListProviderModels(ctx, protocolOpenAICompat, srv.URL, "key")
	require.Error(t, err)
}
