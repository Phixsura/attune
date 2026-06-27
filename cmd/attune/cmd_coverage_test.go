// ptrext:file-allow test fixtures

package main

import (
	"context"
	"flag"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/config"
	llmconfigrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	llmconfigsvc "github.com/Phixsura/attune/internal/service/llmconfig"
)

// ── signalContext ─────────────────────────────────────────────────────

func TestSignalContext_CancelCallable(t *testing.T) {
	ctx, cancel := signalContext()
	defer cancel()
	require.NotNil(t, ctx)
	require.NoError(t, ctx.Err())
	cancel()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

// ── sleepBeforeShutdown ───────────────────────────────────────────────

func TestSleepBeforeShutdown_ZeroDuration(t *testing.T) {
	sleepBeforeShutdown(0) // must return immediately
}

func TestSleepBeforeShutdown_NegativeDuration(t *testing.T) {
	sleepBeforeShutdown(-1 * time.Second) // must return immediately
}

func TestSleepBeforeShutdown_PositiveDuration(t *testing.T) {
	start := time.Now()
	sleepBeforeShutdown(10 * time.Millisecond)
	require.GreaterOrEqual(t, time.Since(start), 10*time.Millisecond)
}

// ── secretsUsage ──────────────────────────────────────────────────────

func TestSecretsUsage_Message(t *testing.T) {
	err := secretsUsage()
	require.Error(t, err)
	require.Contains(t, err.Error(), "generate-keyset")
	require.Contains(t, err.Error(), "keyset-info")
	require.Contains(t, err.Error(), "add-key")
	require.Contains(t, err.Error(), "set-primary")
	require.Contains(t, err.Error(), "delete-key")
	require.Contains(t, err.Error(), "reencrypt")
	require.Contains(t, err.Error(), "retire-key")
}

// ── runSecrets dispatch ───────────────────────────────────────────────

func TestRunSecrets_SetPrimaryNoKeyID(t *testing.T) {
	err := runSecretsSetPrimary([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--key-id is required")
}

func TestRunSecrets_DeleteKeyNoKeyID(t *testing.T) {
	err := runSecretsDeleteKey([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--key-id is required")
}

func TestRunSecrets_RetireKeyNoKeyID(t *testing.T) {
	err := runSecretsRetireKey([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--key-id is required")
}

// ── runEmbed dispatch ─────────────────────────────────────────────────

func TestRunEmbed_UnknownVerb(t *testing.T) {
	err := runEmbed([]string{"invalid"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown embed subcommand "invalid"`)
}

func TestRunEmbed_BackfillNoTenant(t *testing.T) {
	err := runEmbed([]string{"backfill"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--tenant is required")
}

func TestRunEmbed_ResetNoTenant(t *testing.T) {
	err := runEmbed([]string{"reset"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--tenant is required")
}

func TestRunEmbed_StatusNoTenant(t *testing.T) {
	err := runEmbed([]string{"status"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--tenant is required")
}

// ── runLLMAbilities dispatch ──────────────────────────────────────────

func TestRunLLMAbilities_NoArgs(t *testing.T) {
	err := runLLMAbilities(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage: attune llm abilities")
}

func TestRunLLMAbilities_UnknownVerb(t *testing.T) {
	err := runLLMAbilities([]string{"invalid"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown llm abilities verb "invalid"`)
}

// ── runLLMRoutes dispatch ─────────────────────────────────────────────

func TestRunLLMRoutes_NoArgs(t *testing.T) {
	err := runLLMRoutes(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage: attune llm routes")
}

func TestRunLLMRoutes_UnknownVerb(t *testing.T) {
	err := runLLMRoutes([]string{"invalid"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown llm routes verb "invalid"`)
}

// ── runLLMChannels dispatch ───────────────────────────────────────────

func TestRunLLMChannels_NoArgs(t *testing.T) {
	err := runLLMChannels(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage: attune llm channels")
}

// ── runOutbox dispatch ────────────────────────────────────────────────

func TestRunOutbox_UnknownSubcommand_Message(t *testing.T) {
	err := runOutbox([]string{"nope"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown outbox subcommand "nope"`)
}

// ── runTenant dispatch ────────────────────────────────────────────────

func TestRunTenantCreate_NoSlug(t *testing.T) {
	err := runTenantCreate([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--slug is required")
}

// ── runDoctor ─────────────────────────────────────────────────────────

func TestOpenDoctorPool_NilConfig(t *testing.T) {
	pool := openDoctorPool(context.Background(), nil)
	require.Nil(t, pool)
}

// ── channelViews / channelView ────────────────────────────────────────

func TestChannelViews_MapsAll(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	rows := []llmconfigrepo.Channel{
		{
			ID:       id1,
			Name:     "chan1",
			Protocol: "openai-compat",
			BaseURL:  "https://api.example.com",
			AuthMode: "bearer",
			Status:   "enabled",
			Priority: 1,
			Weight:   10,
		},
		{
			ID:                   id2,
			Name:                 "chan2",
			Protocol:             "anthropic",
			BaseURL:              "https://anthropic.example.com",
			AuthMode:             "none",
			CredentialKeyID:      "key-42",
			CredentialCiphertext: []byte("cipher"),
			Status:               "disabled",
			Priority:             0,
			Weight:               5,
			TimeoutSeconds:       30,
		},
	}
	views := channelViews(rows)
	require.Len(t, views, 2)

	require.Equal(t, id1.String(), views[0].ID)
	require.Equal(t, "chan1", views[0].Name)
	require.Equal(t, "openai-compat", views[0].Protocol)
	require.False(t, views[0].HasAPIKey, "no ciphertext means HasAPIKey=false")

	require.Equal(t, id2.String(), views[1].ID)
	require.Equal(t, "anthropic", views[1].Protocol)
	require.True(t, views[1].HasAPIKey, "ciphertext present means HasAPIKey=true")
	require.Equal(t, "key-42", views[1].CredentialKeyID)
	require.Equal(t, 30, views[1].TimeoutSeconds)
}

// ── channelTestView ───────────────────────────────────────────────────

func TestChannelTestView_OKTrue(t *testing.T) {
	id := uuid.New()
	result := llmconfigsvc.ChannelTestResult{
		Channel: llmconfigrepo.Channel{
			ID:       id,
			Name:     "test-ch",
			Protocol: "openai-compat",
			Status:   "enabled",
		},
		ProviderModel: "gpt-4",
		Text:          "Hello world",
		InputTokens:   10,
		OutputTokens:  5,
		LatencyMS:     200,
	}
	view := channelTestView(result, true)
	require.True(t, view.OK)
	require.Equal(t, "gpt-4", view.ProviderModel)
	require.Equal(t, "Hello world", view.Text)
	require.Equal(t, int32(10), view.InputTokens)
	require.Equal(t, int32(5), view.OutputTokens)
	require.Equal(t, int64(200), view.LatencyMS)
	require.Equal(t, id.String(), view.Channel.ID)
}

func TestChannelTestView_OKFalse(t *testing.T) {
	result := llmconfigsvc.ChannelTestResult{
		Channel:       llmconfigrepo.Channel{ID: uuid.New()},
		ProviderModel: "claude-opus-4-20250514",
	}
	view := channelTestView(result, false)
	require.False(t, view.OK)
	require.Equal(t, "claude-opus-4-20250514", view.ProviderModel)
}

// ── channelFlagSet ────────────────────────────────────────────────────

func TestChannelFlagSet_DefaultValues(t *testing.T) {
	fs, input, apiKey, id := channelFlagSet("test-cmd")
	require.NotNil(t, fs)
	require.NotNil(t, input)
	require.NotNil(t, apiKey)
	require.NotNil(t, id)

	// Parse with no flags to get defaults
	err := fs.Parse([]string{})
	require.NoError(t, err)

	require.Equal(t, "", input.Name)
	require.Equal(t, "", input.Protocol)
	require.Equal(t, "bearer", input.AuthMode)
	require.Equal(t, "enabled", input.Status)
	require.Equal(t, 0, input.Priority)
	require.NotNil(t, input.Weight)
	require.Equal(t, 1, *input.Weight)
	require.NotNil(t, input.TimeoutSeconds)
	require.Equal(t, 60, *input.TimeoutSeconds)
	require.Nil(t, apiKey.ptr(), "unset apiKey should return nil ptr")
}

func TestChannelFlagSet_WithFlags(t *testing.T) {
	fs, input, apiKey, id := channelFlagSet("test-cmd")
	err := fs.Parse([]string{
		"--name", "my-channel",
		"--protocol", "anthropic",
		"--base-url", "https://api.anthropic.com",
		"--auth-mode", "none",
		"--status", "disabled",
		"--priority", "5",
		"--weight", "3",
		"--timeout-seconds", "120",
		"--api-key", "sk-test",
		"--id", "abc-123",
	})
	require.NoError(t, err)
	require.Equal(t, "my-channel", input.Name)
	require.Equal(t, "anthropic", input.Protocol)
	require.Equal(t, "https://api.anthropic.com", input.BaseURL)
	require.Equal(t, "none", input.AuthMode)
	require.Equal(t, "disabled", input.Status)
	require.Equal(t, 5, input.Priority)
	require.NotNil(t, input.Weight)
	require.Equal(t, 3, *input.Weight)
	require.NotNil(t, input.TimeoutSeconds)
	require.Equal(t, 120, *input.TimeoutSeconds)
	require.NotNil(t, apiKey.ptr())
	require.Equal(t, "sk-test", *apiKey.ptr())
	require.Equal(t, "abc-123", *id)
}

// ── visitedFlags ──────────────────────────────────────────────────────

func TestVisitedFlags_TracksSetFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	_ = fs.String("alpha", "", "")
	_ = fs.String("beta", "", "")
	_ = fs.Int("gamma", 0, "")
	err := fs.Parse([]string{"--alpha", "val1", "--gamma", "42"})
	require.NoError(t, err)

	got := visitedFlags(fs)
	_, hasAlpha := got["alpha"]
	_, hasBeta := got["beta"]
	_, hasGamma := got["gamma"]
	require.True(t, hasAlpha)
	require.False(t, hasBeta)
	require.True(t, hasGamma)
}

// ── channelInputFromExisting ──────────────────────────────────────────

func TestChannelInputFromExisting_MapsAllFields(t *testing.T) {
	ch := llmconfigrepo.Channel{
		ID:             uuid.New(),
		Name:           "test-channel",
		Protocol:       "gemini",
		BaseURL:        "https://gemini.example.com",
		AuthMode:       "bearer",
		Status:         "draining",
		Priority:       3,
		Weight:         7,
		TimeoutSeconds: 45,
	}
	got := channelInputFromExisting(ch)
	require.Equal(t, ch.Name, got.Name)
	require.Equal(t, ch.Protocol, got.Protocol)
	require.Equal(t, ch.BaseURL, got.BaseURL)
	require.Equal(t, ch.AuthMode, got.AuthMode)
	require.Equal(t, ch.Status, got.Status)
	require.Equal(t, ch.Priority, got.Priority)
	require.NotNil(t, got.Weight)
	require.Equal(t, 7, *got.Weight)
	require.NotNil(t, got.TimeoutSeconds)
	require.Equal(t, 45, *got.TimeoutSeconds)
}

// ── applyChannelFlagOverrides ─────────────────────────────────────────

func TestApplyChannelFlagOverrides_SelectiveOverride(t *testing.T) {
	dst := llmconfigsvc.ChannelInput{
		Name:     "original",
		Protocol: "openai-compat",
		BaseURL:  "https://original.com",
		AuthMode: "bearer",
		Status:   "enabled",
		Priority: 1,
	}
	src := llmconfigsvc.ChannelInput{
		Name:     "updated",
		Protocol: "anthropic",
		BaseURL:  "https://updated.com",
		AuthMode: "none",
		Status:   "disabled",
		Priority: 5,
	}
	// Only name and status were explicitly set
	set := map[string]struct{}{
		"name":   {},
		"status": {},
	}
	got := applyChannelFlagOverrides(dst, src, set)
	require.Equal(t, "updated", got.Name, "name was in set")
	require.Equal(t, "openai-compat", got.Protocol, "protocol was not in set")
	require.Equal(t, "https://original.com", got.BaseURL, "base-url was not in set")
	require.Equal(t, "bearer", got.AuthMode, "auth-mode was not in set")
	require.Equal(t, "disabled", got.Status, "status was in set")
	require.Equal(t, 1, got.Priority, "priority was not in set")
}

// ── optionalStringFlag ────────────────────────────────────────────────

func TestOptionalStringFlag_StringDefault(t *testing.T) {
	var f optionalStringFlag
	require.Equal(t, "", f.String())
	require.False(t, f.set)
}

func TestOptionalStringFlag_SetAndGet(t *testing.T) {
	var f optionalStringFlag
	err := f.Set("my-value")
	require.NoError(t, err)
	require.True(t, f.set)
	require.Equal(t, "my-value", f.String())
	ptr := f.ptr()
	require.NotNil(t, ptr)
	require.Equal(t, "my-value", *ptr)
}

func TestOptionalStringFlag_PtrUnset(t *testing.T) {
	var f optionalStringFlag
	require.Nil(t, f.ptr())
}

// ── server constants ──────────────────────────────────────────────────

func TestDatabaseConnectConstants(t *testing.T) {
	require.Equal(t, 60*time.Second, databaseConnectTimeout)
	require.Equal(t, 2*time.Second, databaseConnectRetryInterval)
}

func TestIdempotencyKeyConstants(t *testing.T) {
	require.Equal(t, 48*time.Hour, idempotencyKeyRetention)
	require.Equal(t, 6*time.Hour, idempotencyKeyPruneInterval)
}

func TestMCPPrunerConstants(t *testing.T) {
	require.Equal(t, 1*time.Hour, mcpPruneInterval)
	require.Equal(t, 7*24*time.Hour, mcpSessionIdleLimit)
}

func TestEvalSuggestionsConstants_Coverage(t *testing.T) {
	require.Equal(t, 30*24*time.Hour, evalSuggestionsWindow)
	require.Equal(t, 20, evalSuggestionsSample)
	require.Equal(t, 60*time.Second, evalSuggestionsTimeout)
}

// ── subcommands table ─────────────────────────────────────────────────

func TestSubcommands_AllKeysPresent(t *testing.T) {
	expected := []string{
		"server", "doctor", "keys", "tenant", "eval",
		"outbox", "secrets", "llm", "embed",
		"migrations", "restore-drill",
	}
	for _, key := range expected {
		t.Run(key, func(t *testing.T) {
			_, ok := subcommands[key]
			require.True(t, ok, "subcommands should contain %q", key)
		})
	}
}

func TestSubcommands_NoExtraKeys(t *testing.T) {
	expected := map[string]struct{}{
		"server": {}, "doctor": {}, "keys": {}, "tenant": {}, "eval": {},
		"outbox": {}, "secrets": {}, "llm": {}, "embed": {},
		"migrations": {}, "restore-drill": {}, "audit": {}, "auth": {},
	}
	for key := range subcommands {
		_, ok := expected[key]
		require.True(t, ok, "unexpected subcommand %q", key)
	}
}

// ── drainAwareReadiness ───────────────────────────────────────────────

func TestDrainAwareReadiness_BeginDrainChangesState(t *testing.T) {
	r := newDrainAwareReadiness(&fakeChecker{})
	require.NoError(t, r.Ping(context.Background()))
	r.BeginDrain()
	err := r.Ping(context.Background())
	require.ErrorIs(t, err, errServerDraining)
}

type fakeChecker struct{ err error }

func (f *fakeChecker) Ping(_ context.Context) error { return f.err }

// ── runLLM dispatch ───────────────────────────────────────────────────

func TestRunLLM_SingleArgChannels(t *testing.T) {
	// "channels" with no verb should trigger an error
	err := runLLM([]string{"channels"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage: attune llm channels")
}

func TestRunLLM_SingleArgAbilities(t *testing.T) {
	err := runLLM([]string{"abilities"})
	require.Error(t, err)
}

func TestRunLLM_SingleArgRoutes(t *testing.T) {
	err := runLLM([]string{"routes"})
	require.Error(t, err)
}

// ── writeReport ───────────────────────────────────────────────────────

func TestWriteReport_EmptyOutput(t *testing.T) {
	// stdout path: just confirm it does not error
	err := writeReport("test markdown", "")
	require.NoError(t, err)
}

func TestWriteReport_ToTempFile(t *testing.T) {
	path := t.TempDir() + "/report.md"
	err := writeReport("## Report\nsome content", path)
	require.NoError(t, err)
}

// ── openOutput ────────────────────────────────────────────────────────

func TestOpenOutput_EmptyPathReturnsStdout(t *testing.T) {
	w, closer, err := openOutput("")
	require.NoError(t, err)
	require.NotNil(t, w)
	closer() // no-op
}

func TestOpenOutput_ValidPath(t *testing.T) {
	path := t.TempDir() + "/test-output.csv"
	w, closer, err := openOutput(path)
	require.NoError(t, err)
	require.NotNil(t, w)
	defer closer()
	_, writeErr := w.Write([]byte("hello"))
	require.NoError(t, writeErr)
}

// ── runKeys ───────────────────────────────────────────────────────────

func TestRunKeys_NoArgsError(t *testing.T) {
	err := runKeys(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage: attune keys issue")
}

func TestRunKeys_UnknownVerb_Message(t *testing.T) {
	err := runKeys([]string{"revoke"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage: attune keys issue")
}

func TestRunKeys_IssueMissingTenantFlag(t *testing.T) {
	err := runKeys([]string{"issue"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--tenant is required")
}

// ── runSecrets dispatch extras ────────────────────────────────────────

func TestRunSecrets_SetPrimaryDispatch(t *testing.T) {
	err := runSecrets([]string{"set-primary"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--key-id is required")
}

func TestRunSecrets_DeleteKeyDispatch(t *testing.T) {
	err := runSecrets([]string{"delete-key"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--key-id is required")
}

func TestRunSecrets_RetireKeyDispatch(t *testing.T) {
	err := runSecrets([]string{"retire-key"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--key-id is required")
}

// ── LLM channel UUID validation ───────────────────────────────────────

func TestRunLLMChannelGet_BadUUID(t *testing.T) {
	err := runLLMChannelGet([]string{"--id", "not-a-uuid"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--id must be a UUID")
}

func TestRunLLMChannelDelete_BadUUID(t *testing.T) {
	err := runLLMChannelDelete([]string{"--id", "not-a-uuid"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--id must be a UUID")
}

func TestRunLLMChannelTest_BadUUID(t *testing.T) {
	err := runLLMChannelTest([]string{"--id", "not-a-uuid"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--id must be a UUID")
}

func TestRunLLMChannelUpdate_BadUUID(t *testing.T) {
	err := runLLMChannelUpdate([]string{"--id", "not-a-uuid"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--id must be a UUID")
}

func TestRunLLMAbilitiesList_BadUUID(t *testing.T) {
	err := runLLMAbilitiesList([]string{"--channel", "not-a-uuid"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--channel must be a UUID")
}

func TestRunLLMAbilityUpsert_BadUUID(t *testing.T) {
	err := runLLMAbilityUpsert([]string{"--channel", "not-a-uuid"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--channel must be a UUID")
}

func TestRunLLMAbilityDelete_BadUUID(t *testing.T) {
	err := runLLMAbilityDelete([]string{"--channel", "not-a-uuid"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--channel must be a UUID")
}

// ── inboundWiring ─────────────────────────────────────────────────────

func TestErrServerDraining_Message(t *testing.T) {
	require.Equal(t, "server draining", errServerDraining.Error())
}

// ── safePercent ───────────────────────────────────────────────────────

func TestSafePercent_FullCoverage(t *testing.T) {
	p := safePercent(100, 100)
	require.NotNil(t, p)
	require.InDelta(t, 100.0, *p, 0.01)
}

func TestSafePercent_PartialCoverage(t *testing.T) {
	p := safePercent(25, 50)
	require.NotNil(t, p)
	require.InDelta(t, 50.0, *p, 0.01)
}

// ── logDatabasePingRetry ──────────────────────────────────────────────

func TestLogDatabasePingRetry_VariousAttempts(t *testing.T) {
	ctx := context.Background()
	err := assert.AnError

	// These should not panic for any attempt number
	logDatabasePingRetry(ctx, 1, 60*time.Second, 2*time.Second, err)
	logDatabasePingRetry(ctx, 2, 60*time.Second, 2*time.Second, err)
	logDatabasePingRetry(ctx, 5, 60*time.Second, 2*time.Second, err)
	logDatabasePingRetry(ctx, 10, 60*time.Second, 2*time.Second, err)
	logDatabasePingRetry(ctx, 15, 60*time.Second, 2*time.Second, err)
}

func TestLogDatabasePingRetry_ZeroInterval(t *testing.T) {
	// Shouldn't panic with zero interval (maxAttempts clamps to 1)
	logDatabasePingRetry(context.Background(), 1, 10*time.Second, 0, assert.AnError)
}

// ── printUsage ────────────────────────────────────────────────────────

func TestPrintUsage_DoesNotPanic(t *testing.T) {
	require.NotPanics(t, func() {
		printUsage()
	})
}

// ── parseSince ───────────────────────────────────────────────────────

func TestParseSince_EmptyDefaultsTo30DaysAgo(t *testing.T) {
	got, err := parseSince("")
	require.NoError(t, err)
	diff := time.Since(got)
	require.InDelta(t, 30*24, diff.Hours(), 1)
}

func TestParseSince_DateOnly(t *testing.T) {
	got, err := parseSince("2026-05-01")
	require.NoError(t, err)
	require.Equal(t, 2026, got.Year())
	require.Equal(t, time.May, got.Month())
	require.Equal(t, 1, got.Day())
}

func TestParseSince_RFC3339(t *testing.T) {
	got, err := parseSince("2026-05-01T12:30:00Z")
	require.NoError(t, err)
	require.Equal(t, 2026, got.Year())
	require.Equal(t, 12, got.Hour())
}

func TestParseSince_InvalidFormat(t *testing.T) {
	_, err := parseSince("not-a-date")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--since")
}

func TestParseSince_PartialDate(t *testing.T) {
	_, err := parseSince("2026-05")
	require.Error(t, err)
}

// ── parseGlobalArgs ──────────────────────────────────────────────────

func TestParseGlobalArgs_NoArgs(t *testing.T) {
	got, err := parseGlobalArgs(nil)
	require.NoError(t, err)
	require.Equal(t, []string{}, got)
}

func TestParseGlobalArgs_ConfigFlagConsumed(t *testing.T) {
	got, err := parseGlobalArgs([]string{"--config", "/tmp/test.yaml", "server"})
	require.NoError(t, err)
	require.Equal(t, []string{"server"}, got)
}

func TestParseGlobalArgs_ConfigEqualsConsumed(t *testing.T) {
	got, err := parseGlobalArgs([]string{"--config=/tmp/test.yaml", "server"})
	require.NoError(t, err)
	require.Equal(t, []string{"server"}, got)
}

func TestParseGlobalArgs_ConfigWithoutValueErrors(t *testing.T) {
	_, err := parseGlobalArgs([]string{"--config"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--config requires a path")
}

func TestParseGlobalArgs_NonConfigArgsPassThrough(t *testing.T) {
	got, err := parseGlobalArgs([]string{"keys", "issue", "--tenant", "demo"})
	require.NoError(t, err)
	require.Equal(t, []string{"keys", "issue", "--tenant", "demo"}, got)
}

// ── migrations helper functions ──────────────────────────────────────

func TestPrintMigrationsUsage_DoesNotPanic(t *testing.T) {
	err := printMigrationsUsage()
	require.NoError(t, err)
}

func TestFormatAppliedAt_Nil(t *testing.T) {
	got := formatAppliedAt(nil)
	require.Equal(t, "", got)
}

func TestFormatAppliedAt_ValidRFC3339(t *testing.T) {
	s := "2026-06-15T14:30:00Z"
	got := formatAppliedAt(&s) // ptrext:allow test fixture
	require.Equal(t, "2026-06-15 14:30:00", got)
}

func TestFormatAppliedAt_InvalidString(t *testing.T) {
	s := "not-a-date"
	got := formatAppliedAt(&s) // ptrext:allow test fixture
	require.Equal(t, "", got)
}

func TestFormatDuration_Nil(t *testing.T) {
	got := formatDuration(nil)
	require.Equal(t, "", got)
}

func TestFormatDuration_ValidValue(t *testing.T) {
	v := 42
	got := formatDuration(&v) // ptrext:allow test fixture
	require.Equal(t, "42ms", got)
}

func TestTruncateFilename_Short(t *testing.T) {
	got := truncateFilename("short.sql", 45)
	require.Equal(t, "short.sql", got)
}

func TestTruncateFilename_ExactLength(t *testing.T) {
	name := "exactly_forty_five_characters_long_filename.s"
	require.Equal(t, 45, len(name))
	got := truncateFilename(name, 45)
	require.Equal(t, name, got)
}

func TestTruncateFilename_Long(t *testing.T) {
	name := "this_is_a_very_long_filename_that_exceeds_the_maximum_allowed.sql"
	got := truncateFilename(name, 45)
	require.Len(t, got, 45)
	require.True(t, got[len(got)-3:] == "...")
}

func TestParseRepairFlags_Defaults(t *testing.T) {
	opts, err := parseRepairFlags([]string{})
	require.NoError(t, err)
	require.Equal(t, 0, opts.version)
	require.False(t, opts.force)
}

func TestParseRepairFlags_WithValues(t *testing.T) {
	opts, err := parseRepairFlags([]string{"--version", "5", "--force"})
	require.NoError(t, err)
	require.Equal(t, 5, opts.version)
	require.True(t, opts.force)
}

func TestParseBaselineFlags_Defaults(t *testing.T) {
	opts, err := parseBaselineFlags([]string{})
	require.NoError(t, err)
	require.Equal(t, 0, opts.version)
	require.False(t, opts.force)
}

func TestParseBaselineFlags_WithValues(t *testing.T) {
	opts, err := parseBaselineFlags([]string{"--version", "10", "--force"})
	require.NoError(t, err)
	require.Equal(t, 10, opts.version)
	require.True(t, opts.force)
}

func TestRunMigrationsCmd_NoArgs(t *testing.T) {
	// No args triggers usage (which returns nil)
	err := runMigrationsCmd(nil)
	require.NoError(t, err)
}

func TestRunMigrationsCmd_HelpFlag(t *testing.T) {
	err := runMigrationsCmd([]string{"help"})
	require.NoError(t, err)
}

func TestRunMigrationsCmd_UnknownSubcommand(t *testing.T) {
	err := runMigrationsCmd([]string{"invalid"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown subcommand: invalid")
}

func TestRunMigrationsRepair_MissingVersion(t *testing.T) {
	err := runMigrationsRepair([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--version is required")
}

func TestPrintCheckResult_PassedDoesNotPanic(t *testing.T) {
	require.NotPanics(t, func() {
		printCheckResult("Test", true, "")
	})
}

func TestPrintCheckResult_FailedDoesNotPanic(t *testing.T) {
	require.NotPanics(t, func() {
		printCheckResult("Test", false, "something went wrong\ndetails here")
	})
}

func TestConfirmRepair_SuccessAndForce(t *testing.T) {
	// When migration is successful and force=true, should return true
	got := confirmRepair(1, "001_test.sql", true, true)
	require.True(t, got)
}

func TestConfirmRepair_DirtyAndForce(t *testing.T) {
	// When migration is dirty and force=true, should return true
	got := confirmRepair(2, "002_test.sql", false, true)
	require.True(t, got)
}

// ── sortedKeys ───────────────────────────────────────────────────────

func TestSortedKeys_ReturnsAlphaOrder(t *testing.T) {
	m := map[string]string{"c": "C", "a": "A", "b": "B"}
	got := sortedKeys(m)
	require.Equal(t, []string{"a", "b", "c"}, got)
}

func TestSortedKeys_EmptyMap(t *testing.T) {
	got := sortedKeys(map[string]string{})
	require.Empty(t, got)
}

// ── buildRateLimiter ─────────────────────────────────────────────────

func TestBuildRateLimiter_Enabled(t *testing.T) {
	cfg := &config.Config{
		RateLimitPerMinute: 60,
		RateLimitBurst:     10,
		RateLimitDisabled:  false,
	}
	limiter := buildRateLimiter(cfg)
	require.NotNil(t, limiter)
}
