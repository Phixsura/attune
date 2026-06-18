package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// EmbeddingClient is the single abstraction for embedding generation.
// Embedding is structurally different from chat completions: batch texts
// in, batch vectors out, with dimension control.
type EmbeddingClient interface {
	// Embed generates embeddings for the input texts.
	// Returns one vector per input text in the same order.
	Embed(ctx context.Context, req EmbeddingRequest) (EmbeddingResponse, error)
	// Close releases backend-owned resources.
	Close() error
}

// EmbeddingRequest is the provider-agnostic input for embeddings.
type EmbeddingRequest struct {
	Model      string   // provider-native model id (e.g. "text-embedding-3-small")
	Input      []string // texts to embed (batch)
	Dimensions int      // optional dimension reduction (Matryoshka)
	UserID     string   // audit field
}

// EmbeddingResponse is the provider-agnostic output.
type EmbeddingResponse struct {
	Embeddings [][]float32 // one vector per input text
	Usage      EmbeddingUsage
	Route      RouteMetadata // routing metadata when DB router selected upstream
}

// EmbeddingUsage is provider-reported token accounting.
type EmbeddingUsage struct {
	PromptTokens int32
	TotalTokens  int32
}

// OpenAIEmbeddingClient implements EmbeddingClient for OpenAI's
// /v1/embeddings endpoint. Also works with Azure OpenAI and compatible APIs.
type OpenAIEmbeddingClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// NewOpenAIEmbeddingClient creates an embedding client for OpenAI-compatible APIs.
func NewOpenAIEmbeddingClient(baseURL, apiKey string) *OpenAIEmbeddingClient {
	return ptrext.Of(OpenAIEmbeddingClient{
		httpClient: ptrext.Of(http.Client{
			Transport: otelhttp.NewTransport(guardedEgressTransport()),
			Timeout:   60 * time.Second,
		}),
		baseURL: baseURL,
		apiKey:  apiKey,
	})
}

type openAIEmbeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	Dimensions     int      `json:"dimensions,omitempty"`
	EncodingFormat string   `json:"encoding_format,omitempty"`
	User           string   `json:"user,omitempty"`
}

type openAIEmbeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int32 `json:"prompt_tokens"`
		TotalTokens  int32 `json:"total_tokens"`
	} `json:"usage"`
}

// Embed implements EmbeddingClient.
func (c *OpenAIEmbeddingClient) Embed(
	ctx context.Context,
	req EmbeddingRequest,
) (EmbeddingResponse, error) {
	const where = "OpenAIEmbeddingClient.Embed"
	if len(req.Input) == 0 {
		return EmbeddingResponse{}, fmt.Errorf("embedding input is empty")
	}

	body := openAIEmbeddingRequest{
		Model:          req.Model,
		Input:          req.Input,
		EncodingFormat: "float",
		User:           req.UserID,
	}
	if req.Dimensions > 0 {
		body.Dimensions = req.Dimensions
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return EmbeddingResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/embeddings",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		return EmbeddingResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return EmbeddingResponse{}, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return EmbeddingResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logext.Errorf(ctx, "[%s] status:%d,body:%s", where, resp.StatusCode, string(respBody))
		return EmbeddingResponse{}, fmt.Errorf("embedding API error: status %d", resp.StatusCode)
	}

	var apiResp openAIEmbeddingResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return EmbeddingResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}

	embeddings := make([][]float32, len(req.Input))
	for _, d := range apiResp.Data {
		if d.Index < 0 || d.Index >= len(embeddings) {
			continue
		}
		embeddings[d.Index] = d.Embedding
	}

	for i, e := range embeddings {
		if len(e) == 0 {
			return EmbeddingResponse{}, fmt.Errorf("missing embedding for input %d", i)
		}
	}

	return EmbeddingResponse{
		Embeddings: embeddings,
		Usage: EmbeddingUsage{
			PromptTokens: apiResp.Usage.PromptTokens,
			TotalTokens:  apiResp.Usage.TotalTokens,
		},
	}, nil
}

// Close implements EmbeddingClient.
func (c *OpenAIEmbeddingClient) Close() error {
	return nil
}
