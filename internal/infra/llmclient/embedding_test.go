// SPDX-License-Identifier: Apache-2.0

package llmclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIEmbeddingClient_Embed_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/embeddings", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		var body openAIEmbeddingRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "text-embedding-3-small", body.Model)
		require.Equal(t, []string{"hello", "world"}, body.Input)
		require.Equal(t, "float", body.EncodingFormat)

		resp := openAIEmbeddingResponse{
			Object: "list",
			Data: []struct {
				Object    string    `json:"object"`
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				{Object: "embedding", Index: 0, Embedding: []float32{0.1, 0.2, 0.3}},
				{Object: "embedding", Index: 1, Embedding: []float32{0.4, 0.5, 0.6}},
			},
			Model: "text-embedding-3-small",
		}
		resp.Usage.PromptTokens = 5
		resp.Usage.TotalTokens = 5
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewOpenAIEmbeddingClient(srv.URL, "test-key")
	result, err := client.Embed(t.Context(), EmbeddingRequest{
		Model: "text-embedding-3-small",
		Input: []string{"hello", "world"},
	})
	require.NoError(t, err)
	require.Len(t, result.Embeddings, 2)
	require.Equal(t, []float32{0.1, 0.2, 0.3}, result.Embeddings[0])
	require.Equal(t, []float32{0.4, 0.5, 0.6}, result.Embeddings[1])
	require.Equal(t, int32(5), result.Usage.PromptTokens)
}

func TestOpenAIEmbeddingClient_Embed_EmptyInput(t *testing.T) {
	t.Parallel()
	client := NewOpenAIEmbeddingClient("http://localhost", "key")
	_, err := client.Embed(t.Context(), EmbeddingRequest{
		Model: "m",
		Input: []string{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty")
}

func TestOpenAIEmbeddingClient_Embed_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	client := NewOpenAIEmbeddingClient(srv.URL, "key")
	_, err := client.Embed(t.Context(), EmbeddingRequest{
		Model: "m",
		Input: []string{"test"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "status 500")
}

func TestOpenAIEmbeddingClient_Embed_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	client := NewOpenAIEmbeddingClient(srv.URL, "key")
	_, err := client.Embed(t.Context(), EmbeddingRequest{
		Model: "m",
		Input: []string{"test"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal")
}

func TestOpenAIEmbeddingClient_Embed_MissingEmbedding(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := openAIEmbeddingResponse{
			Object: "list",
			Data: []struct {
				Object    string    `json:"object"`
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				{Object: "embedding", Index: 0, Embedding: []float32{0.1}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewOpenAIEmbeddingClient(srv.URL, "key")
	_, err := client.Embed(t.Context(), EmbeddingRequest{
		Model: "m",
		Input: []string{"hello", "world"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing embedding for input 1")
}

func TestOpenAIEmbeddingClient_Embed_WithDimensions(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body openAIEmbeddingRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		require.Equal(t, 256, body.Dimensions)

		resp := openAIEmbeddingResponse{
			Object: "list",
			Data: []struct {
				Object    string    `json:"object"`
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				{Object: "embedding", Index: 0, Embedding: []float32{0.1}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewOpenAIEmbeddingClient(srv.URL, "key")
	result, err := client.Embed(t.Context(), EmbeddingRequest{
		Model:      "m",
		Input:      []string{"test"},
		Dimensions: 256,
	})
	require.NoError(t, err)
	require.Len(t, result.Embeddings, 1)
}

func TestOpenAIEmbeddingClient_Embed_OutOfBoundsIndex(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := openAIEmbeddingResponse{
			Object: "list",
			Data: []struct {
				Object    string    `json:"object"`
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				{Object: "embedding", Index: 0, Embedding: []float32{0.1}},
				{Object: "embedding", Index: 99, Embedding: []float32{0.2}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewOpenAIEmbeddingClient(srv.URL, "key")
	_, err := client.Embed(t.Context(), EmbeddingRequest{
		Model: "m",
		Input: []string{"test"},
	})
	require.NoError(t, err)
}

func TestOpenAIEmbeddingClient_Close(t *testing.T) {
	t.Parallel()
	client := NewOpenAIEmbeddingClient("http://localhost", "key")
	require.NoError(t, client.Close())
}
