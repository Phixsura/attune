// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package apikey

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

// ---------- toProtoAPIKey ----------

func TestToProtoAPIKey_AllFields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 15, 10, 30, 0, 0, time.UTC)
	lastUsed := created.Add(2 * time.Hour)
	revoked := created.Add(24 * time.Hour)
	expires := created.Add(90 * 24 * time.Hour)
	row := apikeyrepo.APIKeyListRow{
		ID:           id,
		KeyPrefix:    "fbk_live_prefix",
		Label:        "Full Key",
		Description:  "A fully populated key",
		IsActive:     false,
		CreatedAt:    created,
		LastUsedAt:   ptrext.Of(lastUsed),
		RevokedAt:    ptrext.Of(revoked),
		ExpiresAt:    ptrext.Of(expires),
		UsageCount:   42,
		AllowedCIDRs: []string{"10.0.0.0/8"},
		RateLimitRPM: ptrext.Of(1000),
	}
	scopes := []domain.Scope{domain.ScopeIngestWrite, domain.ScopeFeedbackRead}

	got := toProtoAPIKey(row, scopes)

	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, "fbk_live_prefix", got.GetKeyPrefix())
	require.Equal(t, "Full Key", got.GetLabel())
	require.Equal(t, "A fully populated key", got.GetDescription())
	require.False(t, got.GetIsActive())
	require.Equal(t, created.Format(time.RFC3339), got.GetCreatedAt())
	require.Equal(t, lastUsed.Format(time.RFC3339), got.GetLastUsedAt())
	require.Equal(t, revoked.Format(time.RFC3339), got.GetRevokedAt())
	require.Equal(t, expires.Format(time.RFC3339), got.GetExpiresAt())
	require.Equal(t, int64(42), got.GetUsageCount())
	require.Equal(t, []string{"10.0.0.0/8"}, got.GetAllowedCidrs())
	require.Equal(t, int32(1000), got.GetRateLimitRpm())
	require.Equal(t, []string{"ingest:write", "feedback:read"}, got.GetScopes())
}

func TestToProtoAPIKey_MinimalFields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	row := apikeyrepo.APIKeyListRow{
		ID:        id,
		KeyPrefix: "fbk_live_min",
		Label:     "Minimal",
		IsActive:  true,
		CreatedAt: created,
	}

	got := toProtoAPIKey(row, nil)

	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, "Minimal", got.GetLabel())
	require.True(t, got.GetIsActive())
	require.Empty(t, got.GetDescription())
	require.Empty(t, got.GetLastUsedAt())
	require.Empty(t, got.GetRevokedAt())
	require.Empty(t, got.GetExpiresAt())
	require.Equal(t, int32(0), got.GetRateLimitRpm())
	require.Empty(t, got.GetScopes())
}

func TestToProtoAPIKey_EmptyScopes(t *testing.T) {
	t.Parallel()

	row := apikeyrepo.APIKeyListRow{
		ID:        uuid.New(),
		CreatedAt: time.Now(),
	}
	got := toProtoAPIKey(row, []domain.Scope{})

	require.Empty(t, got.GetScopes())
}

// ---------- toProtoSigningKey ----------

func TestToProtoSigningKey_Nil(t *testing.T) {
	t.Parallel()

	got := toProtoSigningKey(nil)
	require.Nil(t, got)
}

func TestToProtoSigningKey_WithExpiry(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	keyID := uuid.New()
	created := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	expires := created.Add(365 * 24 * time.Hour)

	got := toProtoSigningKey(&apikeyrepo.SigningKey{
		ID:             id,
		KeyID:          keyID,
		Algorithm:      "ES256",
		PublicKeyPEM:   "-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----",
		KeyFingerprint: "sha256:abc",
		IsActive:       true,
		CreatedAt:      created,
		ExpiresAt:      ptrext.Of(expires),
	})

	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, keyID.String(), got.GetKeyId())
	require.Equal(t, "ES256", got.GetAlgorithm())
	require.Contains(t, got.GetPublicKeyPem(), "BEGIN PUBLIC KEY")
	require.Equal(t, "sha256:abc", got.GetKeyFingerprint())
	require.True(t, got.GetIsActive())
	require.Equal(t, created.Format(time.RFC3339), got.GetCreatedAt())
	require.Equal(t, expires.Format(time.RFC3339), got.GetExpiresAt())
}

func TestToProtoSigningKey_WithoutExpiry(t *testing.T) {
	t.Parallel()

	got := toProtoSigningKey(&apikeyrepo.SigningKey{
		ID:        uuid.New(),
		KeyID:     uuid.New(),
		Algorithm: "RS256",
		CreatedAt: time.Now(),
		ExpiresAt: nil,
	})

	require.Empty(t, got.GetExpiresAt())
}

// ---------- toProtoManagedIdentity ----------

func TestToProtoManagedIdentity_Nil(t *testing.T) {
	t.Parallel()

	got := toProtoManagedIdentity(nil)
	require.Nil(t, got)
}

func TestToProtoManagedIdentity_AllOptionals(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	lastUsed := created.Add(time.Hour)

	got := toProtoManagedIdentity(&apikeyrepo.ManagedIdentity{
		ID:             id,
		Name:           "gcp-identity",
		Provider:       domain.IdentityProviderGCPWorkload,
		ExternalID:     "sa@project.iam.gserviceaccount.com",
		Audience:       "https://api.example.com",
		Issuer:         "https://accounts.google.com",
		SubjectPattern: "sa@*.iam.gserviceaccount.com",
		AllowedScopes:  []string{"ingest:write"},
		IsActive:       true,
		CreatedAt:      created,
		LastUsedAt:     ptrext.Of(lastUsed),
	})

	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, "gcp-identity", got.GetName())
	require.Equal(t, "gcp_workload", got.GetProvider())
	require.Equal(t, "sa@project.iam.gserviceaccount.com", got.GetExternalId())
	require.Equal(t, "https://api.example.com", got.GetAudience())
	require.Equal(t, "https://accounts.google.com", got.GetIssuer())
	require.Equal(t, "sa@*.iam.gserviceaccount.com", got.GetSubjectPattern())
	require.Equal(t, []string{"ingest:write"}, got.GetAllowedScopes())
	require.True(t, got.GetIsActive())
	require.Equal(t, created.Format(time.RFC3339), got.GetCreatedAt())
	require.Equal(t, lastUsed.Format(time.RFC3339), got.GetLastUsedAt())
}

func TestToProtoManagedIdentity_MinimalFields(t *testing.T) {
	t.Parallel()

	got := toProtoManagedIdentity(&apikeyrepo.ManagedIdentity{
		ID:         uuid.New(),
		Name:       "minimal",
		Provider:   domain.IdentityProviderAWSIAM,
		ExternalID: "arn:aws:iam::123:role/x",
		CreatedAt:  time.Now(),
	})

	require.Empty(t, got.GetAudience())
	require.Empty(t, got.GetIssuer())
	require.Empty(t, got.GetSubjectPattern())
	require.Empty(t, got.GetLastUsedAt())
}

// ---------- toProtoSIEMIntegration ----------

func TestToProtoSIEMIntegration_Nil(t *testing.T) {
	t.Parallel()

	got := toProtoSIEMIntegration(nil)
	require.Nil(t, got)
}

func TestToProtoSIEMIntegration_AllFields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	lastSent := created.Add(time.Hour)

	got := toProtoSIEMIntegration(&apikeyrepo.SIEMIntegration{
		ID:                   id,
		Provider:             domain.SIEMProviderSplunk,
		Name:                 "Splunk HEC",
		EndpointURL:          "https://splunk.example.com:8088/services/collector",
		EventTypes:           []string{"key.created", "key.revoked"},
		BatchSize:            200,
		FlushIntervalSeconds: 30,
		IsActive:             true,
		CreatedAt:            created,
		LastSentAt:           ptrext.Of(lastSent),
		LastError:            "timeout",
	})

	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, "splunk", got.GetProvider())
	require.Equal(t, "Splunk HEC", got.GetName())
	require.Equal(t, "https://splunk.example.com:8088/services/collector", got.GetEndpointUrl())
	require.Equal(t, []string{"key.created", "key.revoked"}, got.GetEventTypes())
	require.Equal(t, int32(200), got.GetBatchSize())
	require.Equal(t, int32(30), got.GetFlushIntervalSeconds())
	require.True(t, got.GetIsActive())
	require.Equal(t, created.Format(time.RFC3339), got.GetCreatedAt())
	require.Equal(t, lastSent.Format(time.RFC3339), got.GetLastSentAt())
	require.Equal(t, "timeout", got.GetLastError())
}

func TestToProtoSIEMIntegration_MinimalFields(t *testing.T) {
	t.Parallel()

	got := toProtoSIEMIntegration(&apikeyrepo.SIEMIntegration{
		ID:        uuid.New(),
		Provider:  domain.SIEMProviderDatadog,
		Name:      "minimal",
		CreatedAt: time.Now(),
	})

	require.Empty(t, got.GetLastSentAt())
	require.Empty(t, got.GetLastError())
}

// ---------- toProtoAIAgentConfig ----------

func TestToProtoAIAgentConfig_Nil(t *testing.T) {
	t.Parallel()

	got := toProtoAIAgentConfig(nil)
	require.Nil(t, got)
}

func TestToProtoAIAgentConfig_AllFields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)

	got := toProtoAIAgentConfig(&apikeyrepo.AIAgentConfig{
		ID:                   id,
		Name:                 "MCP Agent",
		AgentType:            domain.AIAgentTypeMCPServer,
		AllowedScopes:        []string{"feedback:read", "ingest:write"},
		MaxTokensPerRequest:  50000,
		MaxRequestsPerMinute: 120,
		AllowedModels:        []string{"claude-3", "gpt-4"},
		RequireHumanApproval: true,
		ApprovalTimeoutSecs:  600,
		AuditAllRequests:     true,
		IsActive:             true,
		CreatedAt:            created,
	})

	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, "MCP Agent", got.GetName())
	require.Equal(t, "mcp_server", got.GetAgentType())
	require.Equal(t, []string{"feedback:read", "ingest:write"}, got.GetAllowedScopes())
	require.Equal(t, int32(50000), got.GetMaxTokensPerRequest())
	require.Equal(t, int32(120), got.GetMaxRequestsPerMinute())
	require.Equal(t, []string{"claude-3", "gpt-4"}, got.GetAllowedModels())
	require.True(t, got.GetRequireHumanApproval())
	require.Equal(t, int32(600), got.GetApprovalTimeoutSeconds())
	require.True(t, got.GetAuditAllRequests())
	require.True(t, got.GetIsActive())
	require.Equal(t, created.Format(time.RFC3339), got.GetCreatedAt())
}

// ---------- toProtoRotationSchedule ----------

func TestToProtoRotationSchedule_Nil(t *testing.T) {
	t.Parallel()

	got := toProtoRotationSchedule(nil)
	require.Nil(t, got)
}

func TestToProtoRotationSchedule_WithLastRotated(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	keyID := uuid.New()
	created := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	nextRotation := created.Add(90 * 24 * time.Hour)
	lastRotated := created.Add(-30 * 24 * time.Hour)

	got := toProtoRotationSchedule(&apikeyrepo.RotationSchedule{
		ID:                   id,
		KeyID:                keyID,
		RotationIntervalDays: 90,
		GracePeriodHours:     168,
		AutoNotify:           true,
		NotifyDaysBefore:     []int{30, 7, 1},
		NextRotationAt:       nextRotation,
		LastRotatedAt:        ptrext.Of(lastRotated),
		IsActive:             true,
		CreatedAt:            created,
	})

	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, keyID.String(), got.GetKeyId())
	require.Equal(t, int32(90), got.GetRotationIntervalDays())
	require.Equal(t, int32(168), got.GetGracePeriodHours())
	require.True(t, got.GetAutoNotify())
	require.Equal(t, []int32{30, 7, 1}, got.GetNotifyDaysBefore())
	require.Equal(t, nextRotation.UTC().Format(time.RFC3339), got.GetNextRotationAt())
	require.Equal(t, lastRotated.UTC().Format(time.RFC3339), got.GetLastRotatedAt())
	require.True(t, got.GetIsActive())
	require.Equal(t, created.Format(time.RFC3339), got.GetCreatedAt())
}

func TestToProtoRotationSchedule_WithoutLastRotated(t *testing.T) {
	t.Parallel()

	got := toProtoRotationSchedule(&apikeyrepo.RotationSchedule{
		ID:                   uuid.New(),
		KeyID:                uuid.New(),
		RotationIntervalDays: 30,
		NextRotationAt:       time.Now(),
		LastRotatedAt:        nil,
		CreatedAt:            time.Now(),
	})

	require.Empty(t, got.GetLastRotatedAt())
}

// ---------- computeKeyFingerprint ----------

func TestComputeKeyFingerprint_Deterministic(t *testing.T) {
	t.Parallel()

	pem := "-----BEGIN PUBLIC KEY-----\ntest-key-data\n-----END PUBLIC KEY-----"

	fp1 := computeKeyFingerprint(pem)
	fp2 := computeKeyFingerprint(pem)

	require.Equal(t, fp1, fp2)

	hash := sha256.Sum256([]byte(pem))
	expected := hex.EncodeToString(hash[:])
	require.Equal(t, expected, fp1)
}

func TestComputeKeyFingerprint_DifferentInputsDiffer(t *testing.T) {
	t.Parallel()

	fp1 := computeKeyFingerprint("key-a")
	fp2 := computeKeyFingerprint("key-b")

	require.NotEqual(t, fp1, fp2)
}
