package llmclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// AnthropicBackend talks to /v1/messages via the official
// anthropic-sdk-go.
//
// Anthropic Messages has no response_format json_schema field; the
// canonical way to coerce structured output is **forced tool use**:
// declare exactly one tool whose input_schema is the desired JSON
// Schema, then pin tool_choice to that tool. The model fills the tool
// input with the schema-shaped JSON, which we read out of the
// tool_use content block and return as resp.Text.
//
// When OutputSchema is nil, the backend falls back to a plain text
// completion — the first text block is returned as resp.Text.
type AnthropicBackend struct {
	client anthropic.Client
}

// NewAnthropic builds the SDK-backed Messages client. baseURL is
// optional (the SDK defaults to api.anthropic.com / ANTHROPIC_BASE_URL).
// apiKey is required.
func NewAnthropic(baseURL, apiKey string) (*AnthropicBackend, error) {
	const where = "llmclient.NewAnthropic"
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic backend: api_key is required")
	}
	httpClient := ptrext.Of(http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   chatHTTPTimeout,
	})
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(httpClient),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(strings.TrimRight(baseURL, "/")))
	}
	logext.Infof(context.Background(), "[%s] OK,base_url:%s,api_key_set:%t",
		where, logext.SafeURLForLog(baseURL), apiKey != "")
	return ptrext.Of(AnthropicBackend{client: anthropic.NewClient(opts...)}), nil
}

func (b *AnthropicBackend) Close() error { return nil }

// Complete implements LLMClient.
//
// MaxTokens is Anthropic's only required generation knob; if the
// caller didn't set it, default to 1024 — enough for attune's
// single-JSON-object enricher output.
func (b *AnthropicBackend) Complete(
	ctx context.Context, req CompletionRequest,
) (CompletionResponse, error) {
	const where = "llmclient.AnthropicBackend.Complete"

	maxTokens := int64(req.MaxTokens)
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	params := anthropic.MessageNewParams{
		Model:     req.Model,
		MaxTokens: maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.Prompt)),
		},
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	params.Temperature = anthropic.Float(req.Temperature)
	if req.Schema != nil {
		params.Tools = []anthropic.ToolUnionParam{{
			OfTool: ptrext.Of(anthropic.ToolParam{
				Name:        req.Schema.Name,
				InputSchema: schemaToAnthropicInput(req.Schema.Schema),
			}),
		}}
		params.ToolChoice = anthropic.ToolChoiceParamOfTool(req.Schema.Name)
	}

	logext.Infof(ctx, "[%s] upstream REQUEST,user_id:%s,model:%s,temp:%v,max_tokens:%d,prompt_len:%d,structured:%t,schema:%s",
		where, req.UserID, req.Model, req.Temperature, maxTokens,
		len(req.Prompt), req.Schema != nil, schemaName(req))

	msg, err := b.client.Messages.New(ctx, params)
	if err != nil {
		logext.Errorf(ctx, "[%s] Messages.New failed,user_id:%s,model:%s,err:%+v",
			where, req.UserID, req.Model, err.Error())
		return CompletionResponse{}, fmt.Errorf("anthropic: %w", err)
	}

	text := extractAnthropicText(msg.Content, req.Schema != nil)
	logext.Infof(ctx, "[%s] upstream RESPONSE,user_id:%s,model:%s,output_len:%d,input_tokens:%d,output_tokens:%d",
		where, req.UserID, req.Model, len(text),
		msg.Usage.InputTokens, msg.Usage.OutputTokens)

	return CompletionResponse{
		Text: text,
		Usage: Usage{
			InputTokens:  int32(msg.Usage.InputTokens),
			OutputTokens: int32(msg.Usage.OutputTokens),
		},
	}, nil
}

// schemaToAnthropicInput translates our map[string]any JSON Schema into
// Anthropic's ToolInputSchemaParam shape. Properties / Required are the
// two fields the SDK exposes natively; anything else (additionalProperties,
// description) rides via the ExtraFields map so the SDK still sends it.
func schemaToAnthropicInput(schema map[string]any) anthropic.ToolInputSchemaParam {
	out := anthropic.ToolInputSchemaParam{}
	if props, ok := schema["properties"]; ok {
		out.Properties = props
	}
	if req, ok := schema["required"]; ok {
		switch r := req.(type) {
		case []string:
			out.Required = r
		case []any:
			for _, v := range r {
				if s, ok := v.(string); ok {
					out.Required = append(out.Required, s)
				}
			}
		}
	}
	extra := map[string]any{}
	for k, v := range schema {
		if k == "type" || k == "properties" || k == "required" {
			continue
		}
		extra[k] = v
	}
	if len(extra) > 0 {
		out.ExtraFields = extra
	}
	return out
}

// extractAnthropicText prefers the tool_use input JSON when structured
// output was requested; falls back to the first text block otherwise.
// Returning the tool input as a JSON string keeps the parsing surface
// identical across all four backends (callers run parseEnrichJSON over
// resp.Text regardless of provider).
func extractAnthropicText(blocks []anthropic.ContentBlockUnion, structured bool) string {
	if structured {
		for _, b := range blocks {
			if b.Type == "tool_use" && len(b.Input) > 0 {
				return string(b.Input)
			}
		}
	}
	for _, b := range blocks {
		if b.Type == "text" {
			return b.Text
		}
	}
	// Last-resort fallback so callers always see *something* — they
	// can fail parsing rather than silently treating empty as success.
	if raw, err := json.Marshal(blocks); err == nil {
		return string(raw)
	}
	return ""
}
