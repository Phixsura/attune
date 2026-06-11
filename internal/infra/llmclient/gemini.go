package llmclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/genai"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// GeminiBackend talks to Google's generative-language endpoint via the
// official google.golang.org/genai SDK.
//
// Structured output uses ResponseJsonSchema + ResponseMIMEType (Gemini
// 2.0+; for 1.5-era models the SDK falls back to ResponseSchema, but
// our default models target the JSON-Schema path). The SDK accepts our
// map[string]any schema verbatim — no dialect conversion needed.
type GeminiBackend struct {
	client *genai.Client
}

// NewGemini builds the SDK-backed Gemini client. baseURL is optional
// (the SDK defaults to generativelanguage.googleapis.com); apiKey is
// required.
func NewGemini(baseURL, apiKey string) (*GeminiBackend, error) {
	const where = "llmclient.NewGemini"
	if apiKey == "" {
		return nil, fmt.Errorf("gemini backend: api_key is required")
	}
	httpClient := ptrext.Of(http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	})
	cfg := ptrext.Of(genai.ClientConfig{
		APIKey:     apiKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: httpClient,
	})
	if baseURL != "" {
		cfg.HTTPOptions.BaseURL = strings.TrimRight(baseURL, "/")
	}
	client, err := genai.NewClient(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("gemini backend: new client: %w", err)
	}
	logext.Infof(context.Background(), "[%s] OK,base_url:%s,api_key_set:%t",
		where, logext.SafeURLForLog(baseURL), apiKey != "")
	return ptrext.Of(GeminiBackend{client: client}), nil
}

func (b *GeminiBackend) Close() error { return nil }

// Complete implements LLMClient against generateContent.
//
// The SDK exposes typed ResponseSchema *Schema *and* generic
// ResponseJsonSchema any — we use the latter so our map[string]any
// OutputSchema crosses unchanged. The MIME type is pinned to JSON when
// a schema is provided (Gemini requires both fields together).
func (b *GeminiBackend) Complete(
	ctx context.Context, req CompletionRequest,
) (CompletionResponse, error) {
	const where = "llmclient.GeminiBackend.Complete"

	cfg := ptrext.Of(genai.GenerateContentConfig{})
	if req.System != "" {
		cfg.SystemInstruction = genai.NewContentFromText(req.System, genai.RoleUser)
	}
	cfg.Temperature = ptrext.Of(float32(req.Temperature))
	if req.MaxTokens > 0 {
		cfg.MaxOutputTokens = req.MaxTokens
	}
	if req.Schema != nil {
		cfg.ResponseMIMEType = "application/json"
		cfg.ResponseJsonSchema = req.Schema.Schema
	}

	logext.Infof(ctx, "[%s] upstream REQUEST,user_id:%s,model:%s,temp:%v,max_tokens:%d,prompt_len:%d,structured:%t,schema:%s",
		where, req.UserID, req.Model, req.Temperature, req.MaxTokens,
		len(req.Prompt), req.Schema != nil, schemaName(req))

	resp, err := b.client.Models.GenerateContent(ctx, req.Model, genai.Text(req.Prompt), cfg)
	if err != nil {
		logext.Errorf(ctx, "[%s] GenerateContent failed,user_id:%s,model:%s,err:%+v",
			where, req.UserID, req.Model, err.Error())
		return CompletionResponse{}, fmt.Errorf("gemini: %w", err)
	}

	text := resp.Text()
	var usage Usage
	if resp.UsageMetadata != nil {
		usage = Usage{
			InputTokens:  resp.UsageMetadata.PromptTokenCount,
			OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
		}
	}
	logext.Infof(ctx, "[%s] upstream RESPONSE,user_id:%s,model:%s,output_len:%d,input_tokens:%d,output_tokens:%d",
		where, req.UserID, req.Model, len(text), usage.InputTokens, usage.OutputTokens,
	)

	return CompletionResponse{Text: text, Usage: usage}, nil
}
