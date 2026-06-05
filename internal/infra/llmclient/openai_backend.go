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

	"github.com/Phixsura/attune/internal/logext"
)

// openaiHTTPTimeout —— OpenAI 兼容端点 HTTP 超时。 60s 覆盖大多数 4-8K token 推理;
// vllm / ollama 本地慢机器 + 大 prompt 偶尔会冲;真要更长靠 reverse-proxy。
const openaiHTTPTimeout = 60 * time.Second

// OpenAIBackend POSTs OpenAI-compatible /v1/chat/completions over HTTP.
//
// 适配端点(BaseURL 写法):
//   - OpenAI 官方:   https://api.openai.com
//   - Azure OpenAI:  https://<resource>.openai.azure.com/openai/deployments/<deployment>
//     (Azure 的 deployment-style 路径 / api-version query 需要靠 reverse-proxy 整形,
//     标准 /v1/chat/completions 之外的需求不在 backend 责任范围)
//   - vllm:          http://localhost:8000
//   - ollama:        http://localhost:11434
//   - oneapi:        http://your-oneapi-host:3000
type OpenAIBackend struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewOpenAI builds a backend pointed at baseURL with the given bearer apiKey.
// baseURL is the prefix before "/v1/chat/completions"; trailing slashes are
// stripped. apiKey may be empty for local ollama / vllm endpoints that don't
// require auth (the Authorization header is then omitted).
func NewOpenAI(baseURL, apiKey string) (*OpenAIBackend, error) {
	const where = "llmclient.NewOpenAI"
	if baseURL == "" {
		return nil, fmt.Errorf("openai backend: base_url is required")
	}
	trimmed := strings.TrimRight(baseURL, "/")
	logext.Infof(context.Background(), "[%s] OK,base_url:%s,api_key_set:%t",
		where, trimmed, apiKey != "")
	return &OpenAIBackend{
		baseURL: trimmed,
		apiKey:  apiKey,
		client: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   openaiHTTPTimeout,
		},
	}, nil
}

// Close is a no-op — net/http has no connection to release at the client
// level. Implements LLMClient.
func (b *OpenAIBackend) Close() error { return nil }

// openaiChatRequest mirrors the minimal subset of OpenAI /v1/chat/completions
// attune needs (single user message). Fields kept untyped pointers/values so
// the marshaled JSON exactly matches OpenAI's schema.
type openaiChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openaiChatMessage `json:"messages"`
	Temperature float64             `json:"temperature"`
	MaxTokens   int32               `json:"max_tokens"`
}

type openaiChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiChatResponse struct {
	Choices []struct {
		Message openaiChatMessage `json:"message"`
	} `json:"choices"`
}

// Chat issues a single non-streaming completion against an OpenAI-compatible
// /v1/chat/completions endpoint.
//
// Upstream HTTP body / response 必须 INFO 级永久落盘(memory: upstream_http_log_info)。
// req body marshal 后 log (跳过 Authorization header) + resp status/body log。
// truncate 4096 字节,跟 GatewayBackend 同 pattern。
func (b *OpenAIBackend) Chat(ctx context.Context, userID, model, prompt string, temperature float64, maxTokens int32) (string, error) {
	const where = "llmclient.OpenAIBackend.Chat"
	reqBody := openaiChatRequest{
		Model: model,
		Messages: []openaiChatMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openai chat: marshal request: %w", err)
	}

	url := b.baseURL + "/v1/chat/completions"
	logext.Infof(ctx, "[%s] upstream REQUEST,user_id:%s,model:%s,url:%s,temp:%v,max_tokens:%d,prompt_len:%d,body:%s",
		where, userID, model, url, temperature, maxTokens, len(prompt), truncate(string(raw), upstreamBodyLogCap))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("openai chat: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if b.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.apiKey)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		logext.Errorf(ctx, "[%s] upstream RESPONSE (transport err),user_id:%s,model:%s,err:%+v",
			where, userID, model, err.Error())
		return "", fmt.Errorf("openai chat: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logext.Errorf(ctx, "[%s] upstream RESPONSE (read err),user_id:%s,model:%s,status:%d,err:%+v",
			where, userID, model, resp.StatusCode, err.Error())
		return "", fmt.Errorf("openai chat: read body: %w", err)
	}
	logext.Infof(ctx, "[%s] upstream RESPONSE,user_id:%s,model:%s,status:%d,resp_len:%d,resp:%s",
		where, userID, model, resp.StatusCode, len(respBytes), truncate(string(respBytes), upstreamBodyLogCap))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai chat: status %d: %s", resp.StatusCode, truncate(string(respBytes), 512))
	}

	var parsed openaiChatResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("openai chat: unmarshal response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openai chat: empty choices")
	}
	return parsed.Choices[0].Message.Content, nil
}
