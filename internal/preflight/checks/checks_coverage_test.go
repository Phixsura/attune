// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/preflight"
	"github.com/Phixsura/attune/internal/restoredrill"
)

// --- config checks ---

func TestConfigParse_NilCfg_HasRemediation(t *testing.T) {
	t.Parallel()

	r := checkConfigParse(context.Background(), &preflight.Environment{})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.NotEmpty(t, r.Remediation)
	require.Equal(t, "config:parse", r.Name)
	require.Equal(t, preflight.CategoryConfig, r.Category)
}

func TestConfigParse_Valid_NoRemediation(t *testing.T) {
	t.Parallel()

	r := checkConfigParse(context.Background(), &preflight.Environment{
		Cfg: &config.Config{},
	})
	require.Equal(t, preflight.StatusPass, r.Status)
	require.Empty(t, r.Remediation)
	require.Contains(t, r.Message, "loaded")
}

func TestConfigBaseURL_EmptyRemediation(t *testing.T) {
	t.Parallel()

	r := checkConfigBaseURL(context.Background(), &preflight.Environment{
		Cfg: &config.Config{},
	})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.NotEmpty(t, r.Remediation)
	require.Contains(t, r.Remediation, "console.base_url")
}

func TestConfigBaseURL_InvalidURLRemediation(t *testing.T) {
	t.Parallel()

	r := checkConfigBaseURL(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleBaseURL: "://bad"},
	})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Message, "not a valid URL")
}

func TestConfigBaseURL_HTTPSScheme(t *testing.T) {
	t.Parallel()

	r := checkConfigBaseURL(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleBaseURL: "https://example.com"},
	})
	require.Equal(t, preflight.StatusPass, r.Status)
	require.Contains(t, r.Message, "(https)")
}

func TestTLSConsistency_CannotParseBaseURL(t *testing.T) {
	t.Parallel()

	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleBaseURL: "://bad"},
	})
	require.Equal(t, preflight.StatusSkipped, r.Status)
	require.Contains(t, r.Message, "Cannot parse")
}

func TestTLSConsistency_HTTPMessageContent(t *testing.T) {
	t.Parallel()

	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleBaseURL: "http://example.com"},
	})
	require.Equal(t, preflight.StatusWarn, r.Status)
	require.Contains(t, r.Message, "Non-HTTPS")
	require.Contains(t, r.Message, "cleartext")
}

func TestTLSConsistency_HTTPS_CleanPass(t *testing.T) {
	t.Parallel()

	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleBaseURL: "https://example.com"},
	})
	require.Equal(t, preflight.StatusPass, r.Status)
	require.Contains(t, r.Message, "No insecure flags")
}

func TestTLSConsistency_LoopbackFlagMessage(t *testing.T) {
	t.Parallel()

	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{
			ConsoleBaseURL: "https://example.com",
			Security:       config.SecurityConfig{AllowLoopbackEgress: true},
		},
	})
	require.Equal(t, preflight.StatusWarn, r.Status)
	require.Contains(t, r.Message, "allow_loopback_egress")
	require.Contains(t, r.Remediation, "SSRF")
}

func TestTLSConsistency_PrivateEgressFlagMessage(t *testing.T) {
	t.Parallel()

	r := checkTLSConsistency(context.Background(), &preflight.Environment{
		Cfg: &config.Config{
			ConsoleBaseURL: "https://example.com",
			Security:       config.SecurityConfig{AllowPrivateEgress: true},
		},
	})
	require.Equal(t, preflight.StatusWarn, r.Status)
	require.Contains(t, r.Message, "allow_private_egress")
}

// --- auth checks ---

func TestSessionKey_Empty_HasRemediation(t *testing.T) {
	t.Parallel()

	r := checkSessionKey(context.Background(), &preflight.Environment{
		Cfg: &config.Config{},
	})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Remediation, "32 bytes")
}

func TestSessionKey_TooShort_ShowsLength(t *testing.T) {
	t.Parallel()

	r := checkSessionKey(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleSessionKey: "shortkey"},
	})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Message, "8 bytes")
}

func TestSessionKey_ExactlyThirtyTwo(t *testing.T) {
	t.Parallel()

	key := strings.Repeat("a", 32)
	r := checkSessionKey(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleSessionKey: key},
	})
	require.Equal(t, preflight.StatusPass, r.Status)
	require.Contains(t, r.Message, "32 bytes")
}

func TestSessionKey_LongKey(t *testing.T) {
	t.Parallel()

	key := strings.Repeat("x", 64)
	r := checkSessionKey(context.Background(), &preflight.Environment{
		Cfg: &config.Config{ConsoleSessionKey: key},
	})
	require.Equal(t, preflight.StatusPass, r.Status)
	require.Contains(t, r.Message, "64 bytes")
}

func TestOIDCReachable_EnabledEmptyIssuer_HasRemediation(t *testing.T) {
	t.Parallel()

	r := checkOIDCReachable(context.Background(), &preflight.Environment{
		Cfg: &config.Config{OIDC: config.OIDCConfig{Enabled: true}},
	})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Remediation, "oidc.issuer_url")
}

func TestOIDCReachable_InvalidIssuerURL(t *testing.T) {
	t.Parallel()

	// A URL with control characters should fail NewRequestWithContext
	r := checkOIDCReachable(context.Background(), &preflight.Environment{
		Cfg: &config.Config{OIDC: config.OIDCConfig{
			Enabled:   true,
			IssuerURL: "http://example.com\x00bad",
		}},
	})
	require.Equal(t, preflight.StatusFail, r.Status)
}

func TestOIDCReachable_Disabled_ShowsMessage(t *testing.T) {
	t.Parallel()

	r := checkOIDCReachable(context.Background(), &preflight.Environment{
		Cfg: &config.Config{OIDC: config.OIDCConfig{Enabled: false}},
	})
	require.Equal(t, preflight.StatusSkipped, r.Status)
	require.Equal(t, "OIDC not configured", r.Message)
}

// --- database checks ---

func TestDBConnectivity_NilPool_Remediation(t *testing.T) {
	t.Parallel()

	r := checkDBConnectivity(context.Background(), &preflight.Environment{})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Message, "not available")
	require.Contains(t, r.Remediation, "database.url")
}

func TestPgvector_NilPool_Remediation(t *testing.T) {
	t.Parallel()

	r := checkPgvector(context.Background(), &preflight.Environment{})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Remediation, "connectivity")
}

// --- encryption checks ---

func TestTinkKeyset_NilCfg_HasRemediation(t *testing.T) {
	t.Parallel()

	r := checkTinkKeyset(context.Background(), &preflight.Environment{})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Equal(t, "Config not loaded", r.Message)
}

func TestTinkKeyset_Empty_HasRemediation(t *testing.T) {
	t.Parallel()

	r := checkTinkKeyset(context.Background(), &preflight.Environment{
		Cfg: &config.Config{},
	})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Remediation, "generate-keyset")
}

func TestTinkKeyset_InvalidJSON_Remediation(t *testing.T) {
	t.Parallel()

	r := checkTinkKeyset(context.Background(), &preflight.Environment{
		Cfg: &config.Config{Secrets: config.SecretsConfig{TinkKeyset: "{invalid"}},
	})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Message, "load failed")
}

// --- backup checks ---

func TestGradeRestoreDrill_NoDrillMessage(t *testing.T) {
	t.Parallel()

	r := gradeRestoreDrill(false, "", 0, 7*24*time.Hour)
	require.Equal(t, preflight.StatusWarn, r.Status)
	require.Contains(t, r.Message, "No restore drill")
	require.Contains(t, r.Remediation, "restore-drill run")
}

func TestGradeRestoreDrill_FailedMessage(t *testing.T) {
	t.Parallel()

	r := gradeRestoreDrill(true, restoredrill.StatusFail, 2*24*time.Hour, 7*24*time.Hour)
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Message, "FAILED")
	require.Contains(t, r.Message, "2 days ago")
}

func TestGradeRestoreDrill_WarnMessage(t *testing.T) {
	t.Parallel()

	r := gradeRestoreDrill(true, restoredrill.StatusWarn, time.Hour, 7*24*time.Hour)
	require.Equal(t, preflight.StatusWarn, r.Status)
	require.Contains(t, r.Message, "passed with warnings")
}

func TestGradeRestoreDrill_PassedFreshMessage(t *testing.T) {
	t.Parallel()

	r := gradeRestoreDrill(true, restoredrill.StatusPass, 2*24*time.Hour, 7*24*time.Hour)
	require.Equal(t, preflight.StatusPass, r.Status)
	require.Contains(t, r.Message, "passed")
	require.Contains(t, r.Message, "2 days ago")
}

func TestGradeRestoreDrill_StaleMessage(t *testing.T) {
	t.Parallel()

	r := gradeRestoreDrill(true, restoredrill.StatusPass, 30*24*time.Hour, 7*24*time.Hour)
	require.Equal(t, preflight.StatusWarn, r.Status)
	require.Contains(t, r.Message, "stale")
	require.Contains(t, r.Remediation, "fresh drill")
}

func TestGradeRestoreDrill_FutureTimestamp(t *testing.T) {
	t.Parallel()

	r := gradeRestoreDrill(true, restoredrill.StatusPass, -time.Hour, 7*24*time.Hour)
	require.Equal(t, preflight.StatusWarn, r.Status)
	require.Contains(t, r.Message, "future timestamp")
}

func TestGradeRestoreDrill_UnknownStatus(t *testing.T) {
	t.Parallel()

	r := gradeRestoreDrill(true, restoredrill.Status("mystery"), time.Hour, 7*24*time.Hour)
	require.Equal(t, preflight.StatusWarn, r.Status)
	require.Contains(t, r.Message, "did not verify recoverability")
}

func TestAgoString_Today(t *testing.T) {
	t.Parallel()

	require.Equal(t, "today", agoString(0))
	require.Equal(t, "today", agoString(12*time.Hour))
}

func TestAgoString_OneDayAgo(t *testing.T) {
	t.Parallel()

	require.Equal(t, "1 day ago", agoString(36*time.Hour))
}

func TestAgoString_MultipleDays(t *testing.T) {
	t.Parallel()

	require.Equal(t, "5 days ago", agoString(5*24*time.Hour))
	require.Equal(t, "30 days ago", agoString(30*24*time.Hour))
}

func TestAgoString_NegativeDuration(t *testing.T) {
	t.Parallel()

	// Negative duration returns "today" (days <= 0)
	require.Equal(t, "today", agoString(-time.Hour))
}

// --- worker checks ---

func TestEnricherWorkers_NilConfig_Message(t *testing.T) {
	t.Parallel()

	r := checkEnricherWorkers(context.Background(), &preflight.Environment{})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Equal(t, "Config not loaded", r.Message)
}

func TestEnricherWorkers_Zero_HasRemediation(t *testing.T) {
	t.Parallel()

	r := checkEnricherWorkers(context.Background(), &preflight.Environment{
		Cfg: &config.Config{EnricherWorkers: 0},
	})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Message, "0")
	require.Contains(t, r.Remediation, "positive integer")
}

func TestEnricherWorkers_Negative_ShowsValue(t *testing.T) {
	t.Parallel()

	r := checkEnricherWorkers(context.Background(), &preflight.Environment{
		Cfg: &config.Config{EnricherWorkers: -5},
	})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Message, "-5")
}

func TestEnricherWorkers_Positive_ShowsCount(t *testing.T) {
	t.Parallel()

	r := checkEnricherWorkers(context.Background(), &preflight.Environment{
		Cfg: &config.Config{EnricherWorkers: 8},
	})
	require.Equal(t, preflight.StatusPass, r.Status)
	require.Contains(t, r.Message, "8 enricher worker(s)")
}

func TestEnricherWorkers_One(t *testing.T) {
	t.Parallel()

	r := checkEnricherWorkers(context.Background(), &preflight.Environment{
		Cfg: &config.Config{EnricherWorkers: 1},
	})
	require.Equal(t, preflight.StatusPass, r.Status)
	require.Contains(t, r.Message, "1 enricher worker(s)")
}

// --- migration checks ---

func TestMigrationPending_NilPool_NameAndCategory(t *testing.T) {
	t.Parallel()

	r := checkMigrationPending(context.Background(), &preflight.Environment{})
	require.Equal(t, "migration:pending", r.Name)
	require.Equal(t, preflight.CategoryMigration, r.Category)
	require.Equal(t, preflight.StatusFail, r.Status)
}

func TestMigrationIntegrity_NilPool_SkipsChecksum(t *testing.T) {
	t.Parallel()

	r := checkMigrationIntegrity(context.Background(), &preflight.Environment{})
	// Without pool, it should skip the checksum part but still check duplicates
	require.Contains(t, []preflight.Status{preflight.StatusPass, preflight.StatusSkipped}, r.Status)
}

func TestMigrationDirty_NilPool_Skipped(t *testing.T) {
	t.Parallel()

	r := checkMigrationDirty(context.Background(), &preflight.Environment{})
	require.Equal(t, preflight.StatusSkipped, r.Status)
	require.Contains(t, r.Message, "not available")
}

func TestMigrationManifest_NilPool_Skipped(t *testing.T) {
	t.Parallel()

	r := checkMigrationManifest(context.Background(), &preflight.Environment{})
	require.Equal(t, preflight.StatusSkipped, r.Status)
	require.Contains(t, r.Message, "not available")
}

func TestSetMigrationTotal_ZeroAndBack(t *testing.T) {
	t.Parallel()

	// Store and restore original value
	original := migrationTotal.Load()
	defer SetMigrationTotal(int(original))

	SetMigrationTotal(42)
	require.Equal(t, int32(42), migrationTotal.Load())

	SetMigrationTotal(0)
	require.Equal(t, int32(0), migrationTotal.Load())
}

// --- metrics check ---

func TestMetricsRegistry_MessageContent(t *testing.T) {
	t.Parallel()

	r := checkMetricsRegistry(context.Background(), &preflight.Environment{})
	// In test environment, metrics are registered by import
	if r.Status == preflight.StatusPass {
		require.Contains(t, r.Message, "metric families registered")
	}
}

func TestMetricsRegistry_NameAndCategory(t *testing.T) {
	t.Parallel()

	r := checkMetricsRegistry(context.Background(), &preflight.Environment{})
	require.Equal(t, "metrics:registry", r.Name)
	require.Equal(t, preflight.CategoryMetrics, r.Category)
}

// --- checkRestoreDrill nil pool ---

func TestCheckRestoreDrill_NilPool_Skipped(t *testing.T) {
	t.Parallel()

	r := checkRestoreDrill(context.Background(), &preflight.Environment{})
	require.Equal(t, preflight.StatusSkipped, r.Status)
	require.Contains(t, r.Message, "not available")
}

// --- restoreDrillFreshness constant ---

func TestRestoreDrillFreshness_SevenDays(t *testing.T) {
	t.Parallel()

	require.Equal(t, 7*24*time.Hour, restoreDrillFreshness)
}
