package attune

import (
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

var outboxStatuses = map[string]struct{}{
	"pending":   {},
	"delivered": {},
	"failed":    {},
	"dead":      {},
}

var mcpScopes = map[string]struct{}{
	"mcp:read":   {},
	"mcp:write":  {},
	"mcp:ingest": {},
}

var mcpToolPolicyModes = map[string]struct{}{
	"legacy_allow_all": {},
	"allow_list":       {},
}

var mcpToolPolicyEffects = map[string]struct{}{
	"allow": {},
	"deny":  {},
}

func itoa32(v int32) string {
	return strconv.FormatInt(int64(v), 10)
}

func validateNonNegativeProtoInt32(v int32, message string) error {
	if v < 0 {
		return &AttuneError{Code: CodeBadRequest, Message: message}
	}
	return nil
}

func validateNonNegativeInt64(v int64, message string) error {
	if v < 0 {
		return &AttuneError{Code: CodeBadRequest, Message: message}
	}
	return nil
}

func normalizeOutboxStatuses(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, status := range in {
		normalized := strings.TrimSpace(status)
		if _, ok := outboxStatuses[normalized]; !ok {
			return nil, &AttuneError{
				Code:    CodeBadRequest,
				Message: `outbox status must be one of "pending", "delivered", "failed", or "dead"`,
			}
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}

func normalizeMCPScopes(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(in))
	for _, scope := range in {
		if _, ok := mcpScopes[scope]; !ok {
			return nil, &AttuneError{
				Code:    CodeBadRequest,
				Message: `mcp scope must be one of "mcp:read", "mcp:write", or "mcp:ingest"`,
			}
		}
		out = append(out, scope)
	}
	return out, nil
}

func normalizeMCPClientName(v string) (string, error) {
	normalized := strings.TrimSpace(v)
	if normalized == "" {
		return "", &AttuneError{Code: CodeBadRequest, Message: "mcp client name is required"}
	}
	if utf8.RuneCountInString(normalized) > 128 {
		return "", &AttuneError{Code: CodeBadRequest, Message: "mcp client name must be at most 128 characters"}
	}
	return normalized, nil
}

func validateMCPRedirectURIs(in []string) error {
	if len(in) == 0 {
		return &AttuneError{Code: CodeBadRequest, Message: "redirectUris must contain at least one URI"}
	}
	for _, raw := range in {
		parsed, err := url.Parse(raw)
		if err != nil {
			return &AttuneError{Code: CodeBadRequest, Message: "invalid redirect URI: " + raw}
		}
		if parsed.Fragment != "" {
			return &AttuneError{Code: CodeBadRequest, Message: "fragment not allowed in redirect URI"}
		}
		scheme := strings.ToLower(parsed.Scheme)
		if scheme == "javascript" || scheme == "data" || scheme == "file" || scheme == "vbscript" {
			return &AttuneError{Code: CodeBadRequest, Message: "dangerous URI scheme not allowed"}
		}
		if parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" {
			return &AttuneError{
				Code:    CodeBadRequest,
				Message: "redirect URI must be an absolute URI with a host",
			}
		}
		if scheme != "https" && !isLoopbackHostname(parsed.Hostname()) {
			return &AttuneError{
				Code:    CodeBadRequest,
				Message: "redirect URI must use HTTPS (except for localhost)",
			}
		}
	}
	return nil
}

func isLoopbackHostname(hostname string) bool {
	lower := strings.ToLower(hostname)
	return lower == "localhost" || lower == "127.0.0.1" || lower == "::1" || lower == "[::1]"
}

func normalizeRequiredMCPToolPolicyMode(v *string) (string, error) {
	if v == nil {
		return "", &AttuneError{
			Code:    CodeBadRequest,
			Message: `mcp toolPolicyMode must be "legacy_allow_all" or "allow_list"`,
		}
	}
	normalized := strings.TrimSpace(*v)
	if _, ok := mcpToolPolicyModes[normalized]; !ok {
		return "", &AttuneError{
			Code:    CodeBadRequest,
			Message: `mcp toolPolicyMode must be "legacy_allow_all" or "allow_list"`,
		}
	}
	return normalized, nil
}

func normalizeOptionalMCPToolPolicyMode(v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	normalized, err := normalizeRequiredMCPToolPolicyMode(v)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func validateOptionalPositiveMCPInt32(v *int32, message string) error {
	if v != nil && *v <= 0 {
		return &AttuneError{Code: CodeBadRequest, Message: message}
	}
	return nil
}

func normalizeMCPToolPolicies(in []*MCPToolPolicyInput) ([]*MCPToolPolicyInput, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]*MCPToolPolicyInput, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, policy := range in {
		if policy == nil {
			return nil, &AttuneError{Code: CodeBadRequest, Message: "mcp policies must be an array of objects"}
		}
		toolName := strings.TrimSpace(policy.GetToolName())
		if toolName == "" {
			return nil, &AttuneError{Code: CodeBadRequest, Message: "mcp tool policy toolName is required"}
		}
		if _, dup := seen[toolName]; dup {
			return nil, &AttuneError{
				Code:    CodeBadRequest,
				Message: "duplicate mcp tool policy toolName: " + toolName,
			}
		}
		seen[toolName] = struct{}{}
		effect := strings.TrimSpace(policy.GetEffect())
		if _, ok := mcpToolPolicyEffects[effect]; !ok {
			return nil, &AttuneError{
				Code:    CodeBadRequest,
				Message: `mcp tool policy effect must be "allow" or "deny"`,
			}
		}
		if err := validateOptionalPositiveMCPInt32(
			policy.RateLimitRpm,
			"mcp tool policy rateLimitRpm must be a positive integer",
		); err != nil {
			return nil, err
		}
		if err := validateOptionalPositiveMCPInt32(
			policy.RateLimitBurst,
			"mcp tool policy rateLimitBurst must be a positive integer",
		); err != nil {
			return nil, err
		}
		out = append(out, &MCPToolPolicyInput{
			ToolName:       toolName,
			Effect:         effect,
			RateLimitRpm:   policy.RateLimitRpm,
			RateLimitBurst: policy.RateLimitBurst,
		})
	}
	return out, nil
}

func validUUID(v string) bool {
	if len(v) != 36 {
		return false
	}
	for i := range v {
		switch i {
		case 8, 13, 18, 23:
			if v[i] != '-' {
				return false
			}
		default:
			if !isHexDigit(v[i]) {
				return false
			}
		}
	}
	return true
}

func isHexDigit(b byte) bool {
	return ('0' <= b && b <= '9') || ('a' <= b && b <= 'f') || ('A' <= b && b <= 'F')
}
