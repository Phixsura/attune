package llmclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"github.com/Phixsura/attune/internal/logext"
)

// OpenAIResponsesBackend posts to OpenAI's /v1/responses endpoint via
// the official openai-go/v3 SDK.
//
// Use this when the operator wants the newer Responses surface
// (text.format json_schema, server-side conversation state, reasoning
// tokens) instead of the Chat Completions wire. Functionally equivalent
// for attune's single-shot enrich call; provided so operators on the
// Responses path don't have to translate.
type OpenAIResponsesBackend struct {
	client openai.Client
}

// NewOpenAIResponses builds the SDK-backed Responses client. baseURL is
// the same value operators use for llm_openai_base_url (e.g.
// "https://api.openai.com"); /v1 is appended automatically because the
// SDK concatenates it directly onto the base URL for /v1/responses.
// apiKey is required (the SDK refuses empty keys for OpenAI).
//
// Transport is an *http.Client wrapped with otelhttp.NewTransport so
// outbound spans flow into the same trace as the inbound request
// (CLAUDE.md §7).
func NewOpenAIResponses(baseURL, apiKey string) (*OpenAIResponsesBackend, error) {
	const where = "llmclient.NewOpenAIResponses"
	if apiKey == "" {
		return nil, fmt.Errorf("openai-responses backend: api_key is required")
	}
	httpClient := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   openaiHTTPTimeout,
	}
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(httpClient),
	}
	if baseURL != "" {
		// The openai-go/v3 SDK appends paths directly to the base URL
		// (e.g. "{baseURL}/responses"), so /v1 must be part of the
		// base URL for the request to land on /v1/responses.
		trimmed := strings.TrimRight(baseURL, "/")
		if !strings.HasSuffix(trimmed, "/v1") {
			trimmed += "/v1"
		}
		opts = append(opts, option.WithBaseURL(trimmed))
	}
	logext.Infof(context.Background(), "[%s] OK,base_url:%s,api_key_set:%t",
		where, baseURL, apiKey != "")
	return &OpenAIResponsesBackend{client: openai.NewClient(opts...)}, nil
}

// Close is a no-op — the SDK uses an *http.Client we don't need to
// release.
func (b *OpenAIResponsesBackend) Close() error { return nil }

// Complete implements LLMClient against /v1/responses.
//
// Structured output is mapped via text.format json_schema (the SDK's
// ResponseFormatTextConfigParamOfJSONSchema helper takes a map[string]any
// schema verbatim — the same shape we accept in OutputSchema).
func (b *OpenAIResponsesBackend) Complete(
	ctx context.Context, req CompletionRequest,
) (CompletionResponse, error) {
	const where = "llmclient.OpenAIResponsesBackend.Complete"

	params := responses.ResponseNewParams{
		Model: req.Model,
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(req.Prompt),
		},
	}
	if req.System != "" {
		params.Instructions = openai.String(req.System)
	}
	// Forward zero values too — keeping the body deterministic for
	// debugging beats SDK defaults silently changing behavior.
	params.Temperature = openai.Float(req.Temperature)
	if req.MaxTokens > 0 {
		params.MaxOutputTokens = openai.Int(int64(req.MaxTokens))
	}
	if req.UserID != "" {
		params.User = openai.String(req.UserID)
	}
	if req.Schema != nil {
		params.Text = responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigParamOfJSONSchema(
				req.Schema.Name, req.Schema.Schema,
			),
		}
	}

	logext.Infof(ctx, "[%s] upstream REQUEST,user_id:%s,model:%s,temp:%v,max_tokens:%d,prompt_len:%d,structured:%t",
		where, req.UserID, req.Model, req.Temperature, req.MaxTokens,
		len(req.Prompt), req.Schema != nil)

	resp, err := b.client.Responses.New(ctx, params)
	if err != nil {
		logext.Errorf(ctx, "[%s] Responses.New failed,user_id:%s,model:%s,err:%+v",
			where, req.UserID, req.Model, err.Error())
		return CompletionResponse{}, fmt.Errorf("openai-responses: %w", err)
	}
	text := resp.OutputText()
	logext.Infof(ctx, "[%s] upstream RESPONSE,user_id:%s,model:%s,output_len:%d,input_tokens:%d,output_tokens:%d,resp:%s",
		where, req.UserID, req.Model, len(text),
		resp.Usage.InputTokens, resp.Usage.OutputTokens,
		truncate(text, upstreamBodyLogCap))

	return CompletionResponse{
		Text: text,
		Usage: Usage{
			InputTokens:  int32(resp.Usage.InputTokens),
			OutputTokens: int32(resp.Usage.OutputTokens),
		},
	}, nil
}
