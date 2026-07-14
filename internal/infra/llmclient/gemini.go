package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// GeminiBackend talks to Google's generative-language endpoint via direct REST.
//
// Structured output uses responseMimeType + responseSchema so the backend can
// stay dependency-light while preserving the same JSON-schema contract exposed
// by the SDK-backed implementation.
type GeminiBackend struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

// NewGemini builds the REST client for Gemini. baseURL is optional (the client
// defaults to generativelanguage.googleapis.com); apiKey is required.
func NewGemini(baseURL, apiKey string) (*GeminiBackend, error) {
	const where = "llmclient.NewGemini"
	if apiKey == "" {
		return nil, fmt.Errorf("gemini backend: api_key is required")
	}
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	logext.Infof(context.Background(), "[%s] OK,base_url:%s,api_key_set:%t",
		where, logext.SafeURLForLog(baseURL), apiKey != "")
	return ptrext.Of(GeminiBackend{
		client: ptrext.Of(http.Client{
			Transport: otelhttp.NewTransport(guardedEgressTransport()),
			Timeout:   chatHTTPTimeout,
		}),
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
	}), nil
}

func (b *GeminiBackend) Close() error { return nil }

// Complete implements LLMClient against generateContent.
func (b *GeminiBackend) Complete(
	ctx context.Context, req CompletionRequest,
) (CompletionResponse, error) {
	const where = "llmclient.GeminiBackend.Complete"

	payload := geminiGenerateContentRequest{
		Contents: []geminiContent{{
			Role:  "user",
			Parts: []geminiPart{{Text: req.Prompt}},
		}},
		GenerationConfig: ptrext.Of(geminiGenerationConfig{
			Temperature: float64Ptr(req.Temperature),
		}),
	}
	if req.System != "" {
		payload.SystemInstruction = ptrext.Of(geminiContent{
			Parts: []geminiPart{{Text: req.System}},
		})
	}
	if req.MaxTokens > 0 {
		payload.GenerationConfig.MaxOutputTokens = req.MaxTokens
	}
	if req.Schema != nil {
		payload.GenerationConfig.ResponseMIMEType = "application/json"
		payload.GenerationConfig.ResponseSchema = req.Schema.Schema
	}

	logext.Infof(ctx, "[%s] upstream REQUEST,user_id:%s,model:%s,temp:%v,max_tokens:%d,prompt_len:%d,structured:%t,schema:%s",
		where, req.UserID, req.Model, req.Temperature, req.MaxTokens,
		len(req.Prompt), req.Schema != nil, schemaName(req))

	reqURL := b.requestURL(req.Model)
	body, err := json.Marshal(payload)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("gemini: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("gemini: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", b.apiKey)

	resp, err := b.client.Do(httpReq)
	if err != nil {
		logext.Errorf(ctx, "[%s] generateContent failed,user_id:%s,model:%s,err:%+v",
			where, req.UserID, req.Model, err.Error())
		return CompletionResponse{}, fmt.Errorf("gemini: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxLLMResponseBody+1))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("gemini: read response: %w", err)
	}
	if len(raw) > maxLLMResponseBody {
		return CompletionResponse{}, fmt.Errorf("gemini: response exceeded %d bytes", maxLLMResponseBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "" {
			trimmed = resp.Status
		}
		logext.Errorf(ctx, "[%s] upstream error,user_id:%s,model:%s,status:%s,body:%s",
			where, req.UserID, req.Model, resp.Status, trimmed)
		return CompletionResponse{}, fmt.Errorf("gemini: upstream status %s: %s", resp.Status, trimmed)
	}

	var parsed geminiGenerateContentResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return CompletionResponse{}, fmt.Errorf("gemini: decode response: %w", err)
	}

	text := extractGeminiText(parsed.Candidates)
	usage := Usage{}
	if parsed.UsageMetadata != nil {
		usage = Usage{
			InputTokens:  parsed.UsageMetadata.PromptTokenCount,
			OutputTokens: parsed.UsageMetadata.CandidatesTokenCount,
		}
	}
	logext.Infof(ctx, "[%s] upstream RESPONSE,user_id:%s,model:%s,output_len:%d,input_tokens:%d,output_tokens:%d",
		where, req.UserID, req.Model, len(text), usage.InputTokens, usage.OutputTokens)

	return CompletionResponse{Text: text, Usage: usage}, nil
}

func (b *GeminiBackend) requestURL(model string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(model), "models/")
	if trimmed == "" {
		trimmed = model
	}
	return fmt.Sprintf("%s/v1beta/models/%s:generateContent", b.baseURL, trimmed)
}

func float64Ptr(v float64) *float64 {
	return ptrext.Of(v)
}

func extractGeminiText(candidates []geminiCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	var out strings.Builder
	for _, part := range candidates[0].Content.Parts {
		if part.Text != "" {
			out.WriteString(part.Text)
		}
	}
	if out.Len() > 0 {
		return out.String()
	}
	if raw, err := json.Marshal(candidates[0]); err == nil {
		return string(raw)
	}
	return ""
}

type geminiGenerateContentRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiGenerationConfig struct {
	Temperature      *float64       `json:"temperature,omitempty"`
	MaxOutputTokens  int32          `json:"maxOutputTokens,omitempty"`
	ResponseMIMEType string         `json:"responseMimeType,omitempty"`
	ResponseSchema   map[string]any `json:"responseSchema,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiGenerateContentResponse struct {
	Candidates    []geminiCandidate    `json:"candidates"`
	UsageMetadata *geminiUsageMetadata `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int32 `json:"promptTokenCount"`
	CandidatesTokenCount int32 `json:"candidatesTokenCount"`
}
