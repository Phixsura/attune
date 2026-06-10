package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// openaiHTTPTimeout — OpenAI-compatible endpoint HTTP timeout. 60s
// covers most 4-8K-token inferences; vLLM / ollama on a slow box with a
// large prompt may exceed — for those, use a reverse-proxy with a larger
// timeout.
const openaiHTTPTimeout = 60 * time.Second

// OpenAICompatBackend POSTs OpenAI-compatible /v1/chat/completions over
// hand-rolled net/http. One transport covers every wire-compatible
// endpoint:
//
// - OpenAI: https://api.openai.com
// - Azure OpenAI: https://<resource>.openai.azure.com/openai/deployments/<dep>
// (the deployment-style path / api-version query is left to a
// reverse-proxy — not the backend's responsibility)
// - vLLM: http://localhost:8000
// - ollama: http://localhost:11434
// - oneapi: http://your-oneapi-host:3000
//
// Structured output is requested via response_format json_schema (#10).
// Backends never feature-detect — the post-parse modules whitelist
// (Gate 2) guarantees clean stored output regardless.
type OpenAICompatBackend struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewOpenAICompat builds a backend pointed at baseURL with the given
// bearer apiKey. baseURL is the prefix before /v1/chat/completions
// (trailing slash stripped). apiKey may be empty for local ollama /
// vLLM that doesn't require auth.
func NewOpenAICompat(baseURL, apiKey string) (*OpenAICompatBackend, error) {
	const where = "llmclient.NewOpenAICompat"
	if baseURL == "" {
		return nil, fmt.Errorf("openai-compat backend: base_url is required")
	}
	trimmed := strings.TrimRight(baseURL, "/")
	logext.Infof(context.Background(), "[%s] OK,base_url:%s,api_key_set:%t",
		where, logext.SafeURLForLog(trimmed), apiKey != "")
	return ptrext.Of(OpenAICompatBackend{
		baseURL: trimmed,
		apiKey:  apiKey,
		client: ptrext.Of(http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   openaiHTTPTimeout,
		}),
	}), nil
}

// Close is a no-op — net/http has nothing to release at the client
// level. Implements LLMClient.
func (b *OpenAICompatBackend) Close() error { return nil }

// openaiMessage / openaiChatRequest / openaiChatResponse mirror the
// minimal subset of OpenAI Chat Completions attune needs. JSON tags
// reproduce OpenAI's schema exactly.
type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponseFormat struct {
	Type       string            `json:"type"`
	JSONSchema *openaiJSONSchema `json:"json_schema,omitempty"`
}

type openaiJSONSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type openaiChatRequest struct {
	Model          string                `json:"model"`
	Messages       []openaiMessage       `json:"messages"`
	Temperature    float64               `json:"temperature"`
	MaxTokens      int32                 `json:"max_tokens,omitempty"`
	User           string                `json:"user,omitempty"`
	ResponseFormat *openaiResponseFormat `json:"response_format,omitempty"`
}

type openaiChatResponse struct {
	Choices []struct {
		Message openaiMessage `json:"message"`
	} `json:"choices"`
	Usage *openaiUsage `json:"usage,omitempty"`
}

type openaiUsage struct {
	PromptTokens     int32 `json:"prompt_tokens"`
	CompletionTokens int32 `json:"completion_tokens"`
}

// Complete implements LLMClient.
func (b *OpenAICompatBackend) Complete(
	ctx context.Context, req CompletionRequest,
) (CompletionResponse, error) {
	const where = "llmclient.OpenAICompatBackend.Complete"
	msgs := make([]openaiMessage, 0, 2)
	if req.System != "" {
		msgs = append(msgs, openaiMessage{Role: "system", Content: req.System})
	}
	msgs = append(msgs, openaiMessage{Role: "user", Content: req.Prompt})

	body := openaiChatRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		User:        req.UserID,
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}
	if req.Schema != nil {
		body.ResponseFormat = ptrext.Of(openaiResponseFormat{
			Type: "json_schema",
			JSONSchema: ptrext.Of(openaiJSONSchema{
				Name:   req.Schema.Name,
				Strict: true,
				Schema: req.Schema.Schema,
			}),
		})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai-compat: marshal request: %w", err)
	}

	url := b.baseURL + "/v1/chat/completions"
	logext.Infof(ctx, "[%s] upstream REQUEST,user_id:%s,model:%s,url:%s,temp:%v,max_tokens:%d,prompt_len:%d,structured:%t,schema:%s,request_bytes:%d",
		where, req.UserID, req.Model, logext.SafeURLForLog(url), req.Temperature, req.MaxTokens,
		len(req.Prompt), req.Schema != nil, schemaName(req), len(raw))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai-compat: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if b.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)
	}

	resp, err := b.client.Do(httpReq)
	if err != nil {
		logext.Errorf(ctx, "[%s] transport err,user_id:%s,model:%s,err:%+v",
			where, req.UserID, req.Model, err.Error())
		return CompletionResponse{}, fmt.Errorf("openai-compat: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logext.Errorf(ctx, "[%s] read body err,user_id:%s,model:%s,status:%d,err:%+v",
			where, req.UserID, req.Model, resp.StatusCode, err.Error())
		return CompletionResponse{}, fmt.Errorf("openai-compat: read body: %w", err)
	}
	logext.Infof(ctx, "[%s] upstream RESPONSE,user_id:%s,model:%s,status:%d,resp_len:%d",
		where, req.UserID, req.Model, resp.StatusCode, len(respBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CompletionResponse{}, fmt.Errorf("openai-compat: status %d", resp.StatusCode)
	}

	var parsed openaiChatResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return CompletionResponse{}, fmt.Errorf("openai-compat: unmarshal response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return CompletionResponse{}, fmt.Errorf("openai-compat: empty choices")
	}
	out := CompletionResponse{Text: parsed.Choices[0].Message.Content}
	if parsed.Usage != nil {
		out.Usage = Usage{
			InputTokens:  parsed.Usage.PromptTokens,
			OutputTokens: parsed.Usage.CompletionTokens,
		}
	}
	return out, nil
}
