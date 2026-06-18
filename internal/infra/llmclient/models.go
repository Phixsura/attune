package llmclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/pkg/logext"
)

const (
	protocolOpenAICompat    = "openai-compat"
	protocolOpenAIResponses = "openai-responses"
	protocolAnthropic       = "anthropic"
	protocolGemini          = "gemini"
)

// ModelInfo is provider-reported model metadata used by the Console picker.
// The ID is always the provider-native value callers should persist.
type ModelInfo struct {
	ID          string
	DisplayName string
	OwnedBy     string
}

// ListProviderModels discovers provider-native model IDs for a configured
// channel. Providers that do not require auth may pass an empty apiKey.
func ListProviderModels(
	ctx context.Context,
	protocol string,
	baseURL string,
	apiKey string,
) ([]ModelInfo, error) {
	switch protocol {
	case protocolOpenAICompat, protocolOpenAIResponses:
		return listOpenAIStyleModels(ctx, baseURL, apiKey)
	case protocolAnthropic:
		return listAnthropicModels(ctx, baseURL, apiKey)
	case protocolGemini:
		return listGeminiModels(ctx, baseURL, apiKey)
	default:
		return nil, fmt.Errorf("llm models: unsupported protocol %q", protocol)
	}
}

func listOpenAIStyleModels(ctx context.Context, baseURL, apiKey string) ([]ModelInfo, error) {
	endpoint, err := modelsURL(baseURL, "https://api.openai.com", true)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("openai models: build request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	raw, err := doModelRequest(req)
	if err != nil {
		return nil, fmt.Errorf("openai models: %w", err)
	}
	models, err := parseOpenAIModelList(raw)
	if err != nil {
		return nil, fmt.Errorf("openai models: %w", err)
	}
	return models, nil
}

func listAnthropicModels(ctx context.Context, baseURL, apiKey string) ([]ModelInfo, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic models: api_key is required")
	}
	endpoint, err := modelsURL(baseURL, "https://api.anthropic.com", true)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("anthropic models: build request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	raw, err := doModelRequest(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic models: %w", err)
	}
	models, err := parseOpenAIModelList(raw)
	if err != nil {
		return nil, fmt.Errorf("anthropic models: %w", err)
	}
	return models, nil
}

func listGeminiModels(ctx context.Context, baseURL, apiKey string) ([]ModelInfo, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("gemini models: api_key is required")
	}
	endpoint, err := geminiModelsURL(baseURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("gemini models: build request: %w", err)
	}
	req.Header.Set("x-goog-api-key", apiKey)
	raw, err := doModelRequest(req)
	if err != nil {
		return nil, fmt.Errorf("gemini models: %w", err)
	}
	models, err := parseGeminiModelList(raw)
	if err != nil {
		return nil, fmt.Errorf("gemini models: %w", err)
	}
	return models, nil
}

func modelsURL(baseURL, fallback string, appendV1 bool) (string, error) {
	trimmed := strings.TrimRight(baseURL, "/")
	if trimmed == "" {
		trimmed = fallback
	}
	if trimmed == "" {
		return "", fmt.Errorf("models: base_url is required")
	}
	if appendV1 && !strings.HasSuffix(trimmed, "/v1") {
		trimmed += "/v1"
	}
	return trimmed + "/models", nil
}

func geminiModelsURL(baseURL string) (string, error) {
	trimmed := strings.TrimRight(baseURL, "/")
	if trimmed == "" {
		trimmed = "https://generativelanguage.googleapis.com/v1beta"
	}
	if !strings.HasSuffix(trimmed, "/v1") && !strings.HasSuffix(trimmed, "/v1beta") {
		trimmed += "/v1beta"
	}
	return trimmed + "/models", nil
}

func doModelRequest(req *http.Request) ([]byte, error) {
	const where = "llmclient.ListProviderModels"
	client := http.Client{
		Transport: otelhttp.NewTransport(guardedEgressTransport()),
	}
	logext.Infof(req.Context(), "[%s] upstream REQUEST,url:%s", where, logext.SafeURLForLog(req.URL.String()))
	resp, err := client.Do(req)
	if err != nil {
		logext.Errorf(req.Context(), "[%s] transport err,url:%s,err_type:%T",
			where, logext.SafeURLForLog(req.URL.String()), err)
		return nil, safeModelTransportError(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	logext.Infof(req.Context(), "[%s] upstream RESPONSE,url:%s,status:%d,resp_len:%d",
		where, logext.SafeURLForLog(req.URL.String()), resp.StatusCode, len(raw))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return raw, nil
}

func safeModelTransportError(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, context.Canceled):
		return context.Canceled
	default:
		return errors.New("transport failed")
	}
}

func parseOpenAIModelList(raw []byte) ([]ModelInfo, error) {
	var parsed struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			OwnedBy     string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	models := make([]ModelInfo, 0, len(parsed.Data))
	for _, row := range parsed.Data {
		models = appendModel(models, ModelInfo{
			ID:          row.ID,
			DisplayName: row.DisplayName,
			OwnedBy:     row.OwnedBy,
		})
	}
	return models, nil
}

func parseGeminiModelList(raw []byte) ([]ModelInfo, error) {
	var parsed struct {
		Models []struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	models := make([]ModelInfo, 0, len(parsed.Models))
	for _, row := range parsed.Models {
		if !slices.Contains(row.SupportedGenerationMethods, "generateContent") {
			continue
		}
		models = appendModel(models, ModelInfo{
			ID:          strings.TrimPrefix(row.Name, "models/"),
			DisplayName: row.DisplayName,
			OwnedBy:     "google",
		})
	}
	return models, nil
}

func appendModel(models []ModelInfo, model ModelInfo) []ModelInfo {
	model.ID = strings.TrimSpace(model.ID)
	if model.ID == "" {
		return models
	}
	for _, existing := range models {
		if existing.ID == model.ID {
			return models
		}
	}
	return append(models, model)
}
