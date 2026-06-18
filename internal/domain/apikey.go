package domain

import (
	"errors"
	"net"
	"time"
)

// APIKeyPrefix is the textual prefix every raw external API key carries
// ("fbk_live_<32 hex>"). The prefix lets the middleware reject malformed
// headers cheaply before hitting the database.
const APIKeyPrefix = "fbk_live_"

// ErrInvalidAPIKey signals a request whose API key did not match any
// active row. service.APIKeys returns it; the middleware maps it to 401
// while everything else maps to 500.
var ErrInvalidAPIKey = errors.New("invalid api key")

// ErrAPIKeyExpired signals a request with an expired API key.
var ErrAPIKeyExpired = errors.New("api key expired")

// ErrIPNotAllowed signals a request from an IP not in the allowlist.
var ErrIPNotAllowed = errors.New("ip not in allowlist")

// ErrRateLimitExceeded signals the per-key rate limit was exceeded.
var ErrRateLimitExceeded = errors.New("rate limit exceeded")

// APIKeyExpiry represents predefined expiration periods.
type APIKeyExpiry string

const (
	APIKeyExpiry7Days  APIKeyExpiry = "7d"
	APIKeyExpiry30Days APIKeyExpiry = "30d"
	APIKeyExpiry90Days APIKeyExpiry = "90d"
	APIKeyExpiry1Year  APIKeyExpiry = "1y"
	APIKeyExpiryNever  APIKeyExpiry = "never"
	APIKeyExpiryCustom APIKeyExpiry = "custom"
)

// ExpiryToDuration converts an APIKeyExpiry to a duration.
// Returns 0 for "never" (caller should treat as no expiration).
func ExpiryToDuration(e APIKeyExpiry) time.Duration {
	switch e {
	case APIKeyExpiry7Days:
		return 7 * 24 * time.Hour
	case APIKeyExpiry30Days:
		return 30 * 24 * time.Hour
	case APIKeyExpiry90Days:
		return 90 * 24 * time.Hour
	case APIKeyExpiry1Year:
		return 365 * 24 * time.Hour
	default:
		return 0
	}
}

// ValidExpiries returns all valid expiry options for UI.
func ValidExpiries() []APIKeyExpiry {
	return []APIKeyExpiry{
		APIKeyExpiry7Days,
		APIKeyExpiry30Days,
		APIKeyExpiry90Days,
		APIKeyExpiry1Year,
		APIKeyExpiryNever,
	}
}

// ParseCIDRs validates and parses a slice of CIDR strings.
// Returns error if any CIDR is invalid.
func ParseCIDRs(cidrs []string) ([]*net.IPNet, error) {
	if len(cidrs) == 0 {
		return nil, nil
	}
	result := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			ip := net.ParseIP(cidr)
			if ip == nil {
				return nil, errors.New("invalid CIDR: " + cidr)
			}
			if ip.To4() != nil {
				_, ipNet, _ = net.ParseCIDR(cidr + "/32")
			} else {
				_, ipNet, _ = net.ParseCIDR(cidr + "/128")
			}
		}
		result = append(result, ipNet)
	}
	return result, nil
}

// IPAllowed checks if an IP is allowed by the CIDR allowlist.
// Returns true if allowlist is empty (no restriction).
func IPAllowed(allowedCIDRs []*net.IPNet, clientIP net.IP) bool {
	if len(allowedCIDRs) == 0 {
		return true
	}
	if clientIP == nil {
		return false
	}
	for _, cidr := range allowedCIDRs {
		if cidr.Contains(clientIP) {
			return true
		}
	}
	return false
}

// IPAllowedStrings is a convenience wrapper that parses CIDRs first.
func IPAllowedStrings(allowedCIDRs []string, clientIPStr string) bool {
	if len(allowedCIDRs) == 0 {
		return true
	}
	clientIP := net.ParseIP(clientIPStr)
	if clientIP == nil {
		return false
	}
	for _, cidr := range allowedCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			ip := net.ParseIP(cidr)
			if ip != nil && ip.Equal(clientIP) {
				return true
			}
			continue
		}
		if ipNet.Contains(clientIP) {
			return true
		}
	}
	return false
}

// APIKeyEnvironment represents the deployment environment for an API key.
type APIKeyEnvironment string

const (
	APIKeyEnvProduction  APIKeyEnvironment = "production"
	APIKeyEnvStaging     APIKeyEnvironment = "staging"
	APIKeyEnvDevelopment APIKeyEnvironment = "development"
	APIKeyEnvTest        APIKeyEnvironment = "test"
)

// ValidEnvironments returns all valid environment options.
func ValidEnvironments() []APIKeyEnvironment {
	return []APIKeyEnvironment{
		APIKeyEnvProduction,
		APIKeyEnvStaging,
		APIKeyEnvDevelopment,
		APIKeyEnvTest,
	}
}

// IsValidEnvironment checks if an environment string is valid.
func IsValidEnvironment(env string) bool {
	switch APIKeyEnvironment(env) {
	case APIKeyEnvProduction, APIKeyEnvStaging, APIKeyEnvDevelopment, APIKeyEnvTest:
		return true
	}
	return false
}

// APIKeyEventType represents types of API key lifecycle events.
type APIKeyEventType string

const (
	APIKeyEventCreated      APIKeyEventType = "created"
	APIKeyEventRotated      APIKeyEventType = "rotated"
	APIKeyEventRevoked      APIKeyEventType = "revoked"
	APIKeyEventExpired      APIKeyEventType = "expired"
	APIKeyEventExpiringSoon APIKeyEventType = "expiring_soon"
	APIKeyEventUsed         APIKeyEventType = "used"
	APIKeyEventScopeDenied  APIKeyEventType = "scope_denied"
	APIKeyEventIPDenied     APIKeyEventType = "ip_denied"
)

// ValidEventTypes returns all valid event types.
func ValidEventTypes() []APIKeyEventType {
	return []APIKeyEventType{
		APIKeyEventCreated,
		APIKeyEventRotated,
		APIKeyEventRevoked,
		APIKeyEventExpired,
		APIKeyEventExpiringSoon,
		APIKeyEventUsed,
		APIKeyEventScopeDenied,
		APIKeyEventIPDenied,
	}
}

// ErrKeyInGracePeriod signals the key is rotated but still in grace period.
var ErrKeyInGracePeriod = errors.New("key is in grace period after rotation")

// ErrServiceAccountNotFound signals the service account does not exist.
var ErrServiceAccountNotFound = errors.New("service account not found")

// ErrResourceBindingViolation signals access to a resource not bound to the key.
var ErrResourceBindingViolation = errors.New("resource not bound to api key")

// ErrBudgetExceeded signals the API key has exceeded its budget limit.
var ErrBudgetExceeded = errors.New("api key budget exceeded")

// ErrApprovalRequired signals the API key creation requires approval.
var ErrApprovalRequired = errors.New("approval required for this operation")

// ErrMFARequired signals MFA verification is required.
var ErrMFARequired = errors.New("mfa verification required")

// ErrPolicyViolation signals the operation violates org-level policy.
var ErrPolicyViolation = errors.New("operation violates organization policy")

// ErrProjectNotFound signals the project does not exist.
var ErrProjectNotFound = errors.New("project not found")

// ErrTempTokenExpired signals the temporary token has expired.
var ErrTempTokenExpired = errors.New("temporary token expired")

// ErrTempTokenMaxUses signals the temporary token has reached max uses.
var ErrTempTokenMaxUses = errors.New("temporary token max uses reached")

// ApprovalStatus represents the status of an approval request.
type ApprovalStatus string

const (
	ApprovalStatusPending  ApprovalStatus = "pending"
	ApprovalStatusApproved ApprovalStatus = "approved"
	ApprovalStatusRejected ApprovalStatus = "rejected"
	ApprovalStatusExpired  ApprovalStatus = "expired"
)

// SecretManagerType represents supported secret manager integrations.
type SecretManagerType string

const (
	SecretManagerVault             SecretManagerType = "hashicorp_vault"
	SecretManagerAWSSecretsManager SecretManagerType = "aws_secrets_manager"
	SecretManagerGCPSecretManager  SecretManagerType = "gcp_secret_manager"
	SecretManagerAzureKeyVault     SecretManagerType = "azure_key_vault"
)

// ValidSecretManagerTypes returns all valid secret manager types.
func ValidSecretManagerTypes() []SecretManagerType {
	return []SecretManagerType{
		SecretManagerVault,
		SecretManagerAWSSecretsManager,
		SecretManagerGCPSecretManager,
		SecretManagerAzureKeyVault,
	}
}

// TempTokenPurpose represents the purpose of a temporary token.
type TempTokenPurpose string

const (
	TempTokenPurposeCIJob   TempTokenPurpose = "ci_job"
	TempTokenPurposeOneTime TempTokenPurpose = "one_time"
	TempTokenPurposeSession TempTokenPurpose = "session"
)

// BudgetOverageAction represents what happens when budget is exceeded.
type BudgetOverageAction string

const (
	BudgetOverageBlock BudgetOverageAction = "block"
	BudgetOverageAlert BudgetOverageAction = "alert"
	BudgetOverageNone  BudgetOverageAction = "none"
)

// IdentityProvider represents managed identity providers.
type IdentityProvider string

const (
	IdentityProviderAWSIAM        IdentityProvider = "aws_iam"
	IdentityProviderAzureAD       IdentityProvider = "azure_ad"
	IdentityProviderGCPWorkload   IdentityProvider = "gcp_workload"
	IdentityProviderKubernetes    IdentityProvider = "kubernetes"
	IdentityProviderGitHubActions IdentityProvider = "github_actions"
	IdentityProviderCustomOIDC    IdentityProvider = "custom_oidc"
)

// ValidIdentityProviders returns all valid identity providers.
func ValidIdentityProviders() []IdentityProvider {
	return []IdentityProvider{
		IdentityProviderAWSIAM,
		IdentityProviderAzureAD,
		IdentityProviderGCPWorkload,
		IdentityProviderKubernetes,
		IdentityProviderGitHubActions,
		IdentityProviderCustomOIDC,
	}
}

// SIEMProvider represents SIEM integration providers.
type SIEMProvider string

const (
	SIEMProviderSplunk        SIEMProvider = "splunk"
	SIEMProviderDatadog       SIEMProvider = "datadog"
	SIEMProviderElastic       SIEMProvider = "elastic"
	SIEMProviderSumoLogic     SIEMProvider = "sumo_logic"
	SIEMProviderChronicle     SIEMProvider = "chronicle"
	SIEMProviderSentinel      SIEMProvider = "sentinel"
	SIEMProviderCustomWebhook SIEMProvider = "custom_webhook"
)

// ValidSIEMProviders returns all valid SIEM providers.
func ValidSIEMProviders() []SIEMProvider {
	return []SIEMProvider{
		SIEMProviderSplunk,
		SIEMProviderDatadog,
		SIEMProviderElastic,
		SIEMProviderSumoLogic,
		SIEMProviderChronicle,
		SIEMProviderSentinel,
		SIEMProviderCustomWebhook,
	}
}

// SigningAlgorithm represents supported signing algorithms for PKCV.
type SigningAlgorithm string

const (
	SigningAlgorithmRS256 SigningAlgorithm = "RS256"
	SigningAlgorithmRS384 SigningAlgorithm = "RS384"
	SigningAlgorithmRS512 SigningAlgorithm = "RS512"
	SigningAlgorithmES256 SigningAlgorithm = "ES256"
	SigningAlgorithmES384 SigningAlgorithm = "ES384"
	SigningAlgorithmES512 SigningAlgorithm = "ES512"
)

// AIAgentType represents types of AI agent integrations.
type AIAgentType string

const (
	AIAgentTypeMCPServer      AIAgentType = "mcp_server"
	AIAgentTypeLangChain      AIAgentType = "langchain"
	AIAgentTypeOpenAIFunction AIAgentType = "openai_function"
	AIAgentTypeAnthropicTool  AIAgentType = "anthropic_tool"
)

// ErrBrowserAccessDenied signals the key was used from a browser.
var ErrBrowserAccessDenied = errors.New("api key cannot be used from browser")

// ErrSSORequired signals SSO authentication is required.
var ErrSSORequired = errors.New("sso authentication required")

// ErrManagedIdentityInvalid signals invalid managed identity token.
var ErrManagedIdentityInvalid = errors.New("invalid managed identity token")

// ErrSignatureInvalid signals invalid request signature.
var ErrSignatureInvalid = errors.New("invalid request signature")
