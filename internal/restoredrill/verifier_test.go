// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ---------------------------------------------------------------------------
// Options.recoveryDurations
// ---------------------------------------------------------------------------

func TestRecoveryDurations(t *testing.T) {
	t.Parallel()

	t.Run("both nil when nothing supplied", func(t *testing.T) {
		t.Parallel()
		opts := Options{}
		rpo, rto := opts.recoveryDurations(time.Now())
		require.Nil(t, rpo)
		require.Nil(t, rto)
	})

	t.Run("rpo derived from backup taken at", func(t *testing.T) {
		t.Parallel()
		start := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
		backup := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
		opts := Options{BackupTakenAt: ptrext.Of(backup)}
		rpo, rto := opts.recoveryDurations(start)
		require.NotNil(t, rpo)
		require.Equal(t, 2*time.Hour, *rpo) // ptrext:allow test
		require.Nil(t, rto)
	})

	t.Run("clock skew clamps rpo to zero", func(t *testing.T) {
		t.Parallel()
		start := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
		future := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
		opts := Options{BackupTakenAt: ptrext.Of(future)}
		rpo, _ := opts.recoveryDurations(start)
		require.NotNil(t, rpo)
		require.Equal(t, time.Duration(0), *rpo) // ptrext:allow test
	})

	t.Run("rto passed through from options", func(t *testing.T) {
		t.Parallel()
		dur := 5 * time.Minute
		opts := Options{RestoreDuration: ptrext.Of(dur)}
		_, rto := opts.recoveryDurations(time.Now())
		require.NotNil(t, rto)
		require.Equal(t, dur, *rto) // ptrext:allow test
	})

	t.Run("both supplied", func(t *testing.T) {
		t.Parallel()
		start := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
		backup := time.Date(2026, 6, 26, 11, 0, 0, 0, time.UTC)
		dur := 3 * time.Minute
		opts := Options{
			BackupTakenAt:   ptrext.Of(backup),
			RestoreDuration: ptrext.Of(dur),
		}
		rpo, rto := opts.recoveryDurations(start)
		require.NotNil(t, rpo)
		require.NotNil(t, rto)
		require.Equal(t, time.Hour, *rpo) // ptrext:allow test
		require.Equal(t, dur, *rto)       // ptrext:allow test
	})
}

// ---------------------------------------------------------------------------
// liveKeyIDs — filters enabled keys only
// ---------------------------------------------------------------------------

func TestLiveKeyIDs(t *testing.T) {
	t.Parallel()

	t.Run("returns only enabled keys", func(t *testing.T) {
		t.Parallel()
		store := newTestStore(t)
		keys := store.Keys()
		require.NotEmpty(t, keys, "test store must have at least one key")

		ids := liveKeyIDs(store)
		require.NotEmpty(t, ids)
		// Every id returned must correspond to an ENABLED key in the store.
		keyMap := make(map[string]string)
		for _, k := range keys {
			keyMap[k.KeyID] = k.Status
		}
		for _, id := range ids {
			require.Equal(t, "ENABLED", keyMap[id])
		}
	})
}

// ---------------------------------------------------------------------------
// llmChannelAAD
// ---------------------------------------------------------------------------

func TestLLMChannelAAD(t *testing.T) {
	t.Parallel()

	aad := llmChannelAAD("chan-123")
	require.NotEmpty(t, aad)
	// Must match the writer's convention.
	expected := secretstore.AssociatedData("llm_channel", "chan-123", "api_key")
	require.Equal(t, expected, aad)
}

// ---------------------------------------------------------------------------
// zero — wipes buffer
// ---------------------------------------------------------------------------

func TestZero(t *testing.T) {
	t.Parallel()

	buf := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	zero(buf)
	for i, b := range buf {
		require.Equal(t, byte(0), b, "byte at index %d not zeroed", i)
	}
}

func TestZero_EmptySlice(t *testing.T) {
	t.Parallel()
	zero(nil)      // must not panic
	zero([]byte{}) // must not panic
}

// ---------------------------------------------------------------------------
// CountedTables — verify it contains expected tables
// ---------------------------------------------------------------------------

func TestCountedTables(t *testing.T) {
	t.Parallel()
	require.Contains(t, CountedTables, "tenants")
	require.Contains(t, CountedTables, "user_feedback")
	require.Contains(t, CountedTables, "inbound_sources")
	require.Contains(t, CountedTables, "llm_channels")
	require.Len(t, CountedTables, 4)
}

// ---------------------------------------------------------------------------
// decryptLLMChannel — additional edge cases
// ---------------------------------------------------------------------------

func TestDecryptLLMChannel_WrongAADFails(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	val, err := store.EncryptValue([]byte("sk-test-key"),
		secretstore.AssociatedData("llm_channel", testChannelID, "api_key"))
	require.NoError(t, err)

	// Decrypt with a different channel id (wrong AAD) must fail.
	err = decryptLLMChannel(store, "wrong-channel-id", val.KeyID, val.Ciphertext)
	require.Error(t, err)
	require.Contains(t, err.Error(), "wrong-channel-id")
}

// ---------------------------------------------------------------------------
// decryptInboundWebhook — edge cases
// ---------------------------------------------------------------------------

func TestDecryptInboundWebhook_CorruptedOuterEnvelope(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	err := decryptInboundWebhook(store, []byte("not-encrypted-at-all"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "envelope")
}

func TestDecryptInboundWebhook_BadJSONInner(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	// Encrypt non-JSON bytes as the outer envelope.
	outer, err := store.Encrypt([]byte("this is not json"))
	require.NoError(t, err)

	err = decryptInboundWebhook(store, outer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not valid JSON")
}

func TestDecryptInboundWebhook_EmptyCurrentSecret(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	// Build an envelope where current secret is empty.
	env := inboundWebhookSecrets{SecretCurrentEncrypted: nil}
	plain, err := json.Marshal(env)
	require.NoError(t, err)
	outer, err := store.Encrypt(plain)
	require.NoError(t, err)

	err = decryptInboundWebhook(store, outer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no current secret")
}

func TestDecryptInboundWebhook_CurrentSecretDecryptFails(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	// Build an envelope where current secret ciphertext is bogus.
	env := inboundWebhookSecrets{SecretCurrentEncrypted: []byte("bogus-ciphertext")}
	plain, err := json.Marshal(env)
	require.NoError(t, err)
	outer, err := store.Encrypt(plain)
	require.NoError(t, err)

	err = decryptInboundWebhook(store, outer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "current secret")
}

func TestDecryptInboundWebhook_PreviousSecretDecryptFails(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	// Build valid current, but bogus previous.
	cur, err := store.Encrypt([]byte("good-current-signing-secret-32by"))
	require.NoError(t, err)
	env := inboundWebhookSecrets{
		SecretCurrentEncrypted:  cur,
		SecretPreviousEncrypted: []byte("bogus-previous"),
	}
	plain, err := json.Marshal(env)
	require.NoError(t, err)
	outer, err := store.Encrypt(plain)
	require.NoError(t, err)

	err = decryptInboundWebhook(store, outer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "previous secret")
}

// ---------------------------------------------------------------------------
// decryptInboundEmail — edge cases
// ---------------------------------------------------------------------------

func TestDecryptInboundEmail_CorruptedOuterEnvelope(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	err := decryptInboundEmail(store, []byte("not-encrypted"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "envelope")
}

func TestDecryptInboundEmail_BadJSONInner(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	outer, err := store.Encrypt([]byte("not-json"))
	require.NoError(t, err)

	err = decryptInboundEmail(store, outer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not valid JSON")
}

func TestDecryptInboundEmail_EmptyPassword(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	env := inboundEmailSecrets{PasswordEncrypted: nil}
	plain, err := json.Marshal(env)
	require.NoError(t, err)
	outer, err := store.Encrypt(plain)
	require.NoError(t, err)

	err = decryptInboundEmail(store, outer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no password")
}

func TestDecryptInboundEmail_PasswordDecryptFails(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	env := inboundEmailSecrets{PasswordEncrypted: []byte("bogus")}
	plain, err := json.Marshal(env)
	require.NoError(t, err)
	outer, err := store.Encrypt(plain)
	require.NoError(t, err)

	err = decryptInboundEmail(store, outer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "password")
}

// ---------------------------------------------------------------------------
// evaluateRowCounts — additional edge cases
// ---------------------------------------------------------------------------

func TestEvaluateRowCounts_EmptyRestoreAndBaseline(t *testing.T) {
	t.Parallel()
	// Both empty means no data loss (both have 0 rows).
	got := evaluateRowCounts(
		map[string]int64{"user_feedback": 0, "tenants": 0},
		map[string]int64{"user_feedback": 0, "tenants": 0},
	)
	require.Equal(t, StatusPass, got.Status)
}

func TestEvaluateRowCounts_MultipleDataLossTables(t *testing.T) {
	t.Parallel()
	got := evaluateRowCounts(
		map[string]int64{"user_feedback": 0, "tenants": 0},
		map[string]int64{"user_feedback": 100, "tenants": 50},
	)
	require.Equal(t, StatusFail, got.Status)
	require.Contains(t, got.Message, "data loss")
}

func TestEvaluateRowCounts_DataLossTakesPriorityOverShort(t *testing.T) {
	t.Parallel()
	// One table has data loss (empty) and another is just short; data loss wins.
	got := evaluateRowCounts(
		map[string]int64{"user_feedback": 0, "tenants": 1},
		map[string]int64{"user_feedback": 100, "tenants": 10000},
	)
	require.Equal(t, StatusFail, got.Status)
	require.Contains(t, got.Message, "data loss")
}

func TestEvaluateRowCounts_ExactMatch(t *testing.T) {
	t.Parallel()
	got := evaluateRowCounts(
		map[string]int64{"user_feedback": 100, "tenants": 5},
		map[string]int64{"user_feedback": 100, "tenants": 5},
	)
	require.Equal(t, StatusPass, got.Status)
}

func TestEvaluateRowCounts_DetailCarriesBothMaps(t *testing.T) {
	t.Parallel()
	restored := map[string]int64{"user_feedback": 8}
	baseline := map[string]int64{"user_feedback": 10}
	got := evaluateRowCounts(restored, baseline)
	detail, ok := got.Detail.(rowCountsDetail)
	require.True(t, ok)
	require.Equal(t, restored, detail.Restored)
	require.Equal(t, baseline, detail.Baseline)
}

// ---------------------------------------------------------------------------
// evaluateConstraints — additional edge cases
// ---------------------------------------------------------------------------

func TestEvaluateConstraints_Parallel(t *testing.T) {
	t.Parallel()

	t.Run("no baseline is skipped", func(t *testing.T) {
		t.Parallel()
		got := evaluateConstraints(5, nil)
		require.Equal(t, StatusSkip, got.Status)
		require.Equal(t, "constraints", got.Name)
	})

	t.Run("matches baseline passes", func(t *testing.T) {
		t.Parallel()
		got := evaluateConstraints(1, ptrext.Of(1))
		require.Equal(t, StatusPass, got.Status)
	})

	t.Run("below baseline passes", func(t *testing.T) {
		t.Parallel()
		got := evaluateConstraints(0, ptrext.Of(2))
		require.Equal(t, StatusPass, got.Status)
	})

	t.Run("above baseline fails", func(t *testing.T) {
		t.Parallel()
		got := evaluateConstraints(3, ptrext.Of(1))
		require.Equal(t, StatusFail, got.Status)
		require.Contains(t, got.Message, "introduced")
	})

	t.Run("zero baseline zero restored passes", func(t *testing.T) {
		t.Parallel()
		got := evaluateConstraints(0, ptrext.Of(0))
		require.Equal(t, StatusPass, got.Status)
	})
}

// ---------------------------------------------------------------------------
// evaluateSequences — additional edge cases
// ---------------------------------------------------------------------------

func TestEvaluateSequences_Parallel(t *testing.T) {
	t.Parallel()

	t.Run("none behind passes", func(t *testing.T) {
		t.Parallel()
		got := evaluateSequences(nil)
		require.Equal(t, StatusPass, got.Status)
		require.Equal(t, "sequences", got.Name)
	})

	t.Run("behind fails and sorts names", func(t *testing.T) {
		t.Parallel()
		got := evaluateSequences([]string{"public.z_seq", "public.a_seq"})
		require.Equal(t, StatusFail, got.Status)
		// Sorted: a_seq before z_seq.
		idx := strings.Index(got.Message, "a_seq")
		require.Greater(t, idx, -1)
		require.Less(t, idx, strings.Index(got.Message, "z_seq"))
	})
}

// ---------------------------------------------------------------------------
// evaluateEncoding — additional edge cases
// ---------------------------------------------------------------------------

func TestEvaluateEncoding_Parallel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		enc     string
		want    Status
		wantMsg string
	}{
		{"UTF8 passes", "UTF8", StatusPass, ""},
		{"utf8 case-insensitive passes", "utf8", StatusPass, ""},
		{"Utf8 mixed case passes", "Utf8", StatusPass, ""},
		{"LATIN1 fails", "LATIN1", StatusFail, "LATIN1"},
		{"SQL_ASCII fails", "SQL_ASCII", StatusFail, "SQL_ASCII"},
		{"empty string fails", "", StatusFail, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := evaluateEncoding(tc.enc)
			require.Equal(t, "encoding", got.Name)
			require.Equal(t, tc.want, got.Status)
			if tc.wantMsg != "" {
				require.Contains(t, got.Message, tc.wantMsg)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// evaluateMatviews — additional edge cases
// ---------------------------------------------------------------------------

func TestEvaluateMatviews_Parallel(t *testing.T) {
	t.Parallel()

	t.Run("none unpopulated passes", func(t *testing.T) {
		t.Parallel()
		got := evaluateMatviews(nil)
		require.Equal(t, StatusPass, got.Status)
		require.Equal(t, "materialized_views", got.Name)
	})

	t.Run("empty slice passes", func(t *testing.T) {
		t.Parallel()
		got := evaluateMatviews([]string{})
		require.Equal(t, StatusPass, got.Status)
	})

	t.Run("unpopulated views fail and sort names", func(t *testing.T) {
		t.Parallel()
		got := evaluateMatviews([]string{"public.mv_b", "public.mv_a"})
		require.Equal(t, StatusFail, got.Status)
		require.Contains(t, got.Message, "2 unpopulated")
		// Names should be sorted.
		require.Less(t, strings.Index(got.Message, "mv_a"), strings.Index(got.Message, "mv_b"))
	})
}

// ---------------------------------------------------------------------------
// evaluateExtensions — additional edge cases
// ---------------------------------------------------------------------------

func TestEvaluateExtensions_Parallel(t *testing.T) {
	t.Parallel()

	t.Run("no baseline skips", func(t *testing.T) {
		t.Parallel()
		got := evaluateExtensions([]string{"vector"}, nil)
		require.Equal(t, StatusSkip, got.Status)
		require.Equal(t, "extensions", got.Name)
	})

	t.Run("all present passes", func(t *testing.T) {
		t.Parallel()
		got := evaluateExtensions([]string{"plpgsql", "vector"}, []string{"vector", "plpgsql"})
		require.Equal(t, StatusPass, got.Status)
	})

	t.Run("extra extensions in restore still passes", func(t *testing.T) {
		t.Parallel()
		got := evaluateExtensions([]string{"plpgsql", "vector", "amcheck"}, []string{"vector"})
		require.Equal(t, StatusPass, got.Status)
	})

	t.Run("missing extension fails", func(t *testing.T) {
		t.Parallel()
		got := evaluateExtensions([]string{"plpgsql"}, []string{"plpgsql", "vector"})
		require.Equal(t, StatusFail, got.Status)
		require.Contains(t, got.Message, "vector")
	})

	t.Run("multiple missing extensions fail and sort", func(t *testing.T) {
		t.Parallel()
		got := evaluateExtensions(nil, []string{"vector", "amcheck"})
		require.Equal(t, StatusFail, got.Status)
		require.Contains(t, got.Message, "2 extension")
		require.Less(t, strings.Index(got.Message, "amcheck"), strings.Index(got.Message, "vector"))
	})

	t.Run("empty baseline passes", func(t *testing.T) {
		t.Parallel()
		got := evaluateExtensions([]string{"plpgsql"}, []string{})
		require.Equal(t, StatusPass, got.Status)
	})
}

// ---------------------------------------------------------------------------
// humanDur
// ---------------------------------------------------------------------------

func TestSecondsPtr_NegativeClampedToZero(t *testing.T) {
	t.Parallel()
	neg := -5 * time.Second
	got := secondsPtr(ptrext.Of(neg))
	require.NotNil(t, got)
	require.Equal(t, int64(0), ptrext.Indirect(got))
}

// ---------------------------------------------------------------------------
// secondsOf — additional edge cases
// ---------------------------------------------------------------------------

func TestSecondsOf_PositiveRounds(t *testing.T) {
	t.Parallel()
	got := secondsOf(65*time.Second + 400*time.Millisecond)
	require.NotNil(t, got)
	require.Equal(t, int64(65), ptrext.Indirect(got))
}

// ---------------------------------------------------------------------------
// evaluateRecoveryObjectives — additional edge cases
// ---------------------------------------------------------------------------

func TestEvaluateRecoveryObjectives_RPOOnlyNoTarget(t *testing.T) {
	t.Parallel()
	got := evaluateRecoveryObjectives(ptrext.Of(24*time.Hour), nil, 0, 0)
	require.Equal(t, StatusPass, got.Status)
	require.Contains(t, got.Message, "RPO")
}

func TestEvaluateRecoveryObjectives_RTOOnlyNoTarget(t *testing.T) {
	t.Parallel()
	got := evaluateRecoveryObjectives(nil, ptrext.Of(5*time.Minute), 0, 0)
	require.Equal(t, StatusPass, got.Status)
	require.Contains(t, got.Message, "RTO")
}

func TestEvaluateRecoveryObjectives_BothBreachesReportedTogether(t *testing.T) {
	t.Parallel()
	got := evaluateRecoveryObjectives(
		ptrext.Of(48*time.Hour), ptrext.Of(2*time.Hour),
		24*time.Hour, 30*time.Minute,
	)
	require.Equal(t, StatusWarn, got.Status)
	require.Contains(t, got.Message, "RPO")
	require.Contains(t, got.Message, "RTO")
}

func TestEvaluateRecoveryObjectives_DetailCarriesTargets(t *testing.T) {
	t.Parallel()
	got := evaluateRecoveryObjectives(
		ptrext.Of(time.Hour), ptrext.Of(5*time.Minute),
		24*time.Hour, 30*time.Minute,
	)
	detail, ok := got.Detail.(recoveryDetail)
	require.True(t, ok)
	require.NotNil(t, detail.RPOSeconds)
	require.NotNil(t, detail.RTOSeconds)
	require.NotNil(t, detail.RPOTargetSeconds)
	require.NotNil(t, detail.RTOTargetSeconds)
}

func TestEvaluateRecoveryObjectives_DetailNilTargetsWhenZero(t *testing.T) {
	t.Parallel()
	got := evaluateRecoveryObjectives(
		ptrext.Of(time.Hour), nil, 0, 0,
	)
	detail, ok := got.Detail.(recoveryDetail)
	require.True(t, ok)
	require.Nil(t, detail.RPOTargetSeconds)
	require.Nil(t, detail.RTOTargetSeconds)
}

// ---------------------------------------------------------------------------
// missingKeyRefs — additional edge cases
// ---------------------------------------------------------------------------

func TestMissingKeyRefs_Parallel(t *testing.T) {
	t.Parallel()

	t.Run("both empty yields nil", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, missingKeyRefs(nil, nil))
	})

	t.Run("all referenced are empty string yields nil", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, missingKeyRefs([]string{"", ""}, []string{"1"}))
	})
}

// ---------------------------------------------------------------------------
// Aggregate — additional edge cases
// ---------------------------------------------------------------------------

func TestAggregate_Parallel(t *testing.T) {
	t.Parallel()

	t.Run("single pass", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, StatusPass, Aggregate([]CheckResult{{Status: StatusPass}}))
	})

	t.Run("single fail", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, StatusFail, Aggregate([]CheckResult{{Status: StatusFail}}))
	})

	t.Run("single skip", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, StatusSkip, Aggregate([]CheckResult{{Status: StatusSkip}}))
	})

	t.Run("single warn", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, StatusWarn, Aggregate([]CheckResult{{Status: StatusWarn}}))
	})
}

// ---------------------------------------------------------------------------
// classifySchema — additional edge cases with t.Parallel
// ---------------------------------------------------------------------------

func TestClassifySchema_SchemaDetail(t *testing.T) {
	t.Parallel()
	got := classifySchema(schemaInputs{Total: 24, Applied: 24, Pending: 0, MaxApplied: 24})
	detail, ok := got.Detail.(schemaDetail)
	require.True(t, ok)
	require.Equal(t, 24, detail.Total)
	require.Equal(t, 24, detail.Applied)
	require.Equal(t, 0, detail.Pending)
}

func TestClassifySchema_DirtyTakesPriorityOverEverything(t *testing.T) {
	t.Parallel()
	// Dirty + checksum drift + manifest reorder: dirty wins.
	got := classifySchema(schemaInputs{
		Total:       24,
		Applied:     24,
		DirtyErr:    database.ErrDirtyMigration{Version: 15, Filename: "015_x.sql"},
		ChecksumErr: database.ErrChecksumDrift{Drifted: []database.ChecksumDrift{{Version: 3}}},
		ManifestErr: database.ErrManifestReorder{Stored: "a", Computed: "b"},
	})
	require.Equal(t, StatusFail, got.Status)
	require.Contains(t, got.Message, "dirty")
}

func TestClassifySchema_PendingMessageFormat(t *testing.T) {
	t.Parallel()
	got := classifySchema(schemaInputs{Total: 24, Applied: 20, Pending: 4})
	require.Equal(t, StatusWarn, got.Status)
	require.Contains(t, got.Message, "4 migration")
	require.Contains(t, got.Message, "20/24")
}

func TestClassifySchema_PassMessageFormat(t *testing.T) {
	t.Parallel()
	got := classifySchema(schemaInputs{Total: 24, Applied: 24, Pending: 0, MaxApplied: 24})
	require.Equal(t, StatusPass, got.Status)
	require.Contains(t, got.Message, "24/24")
	require.Contains(t, got.Message, "checksums")
}

// ---------------------------------------------------------------------------
// rowCountFloor constant
// ---------------------------------------------------------------------------

func TestRowCountFloor(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0.5, rowCountFloor)
}

// ---------------------------------------------------------------------------
// connectTimeout constant
// ---------------------------------------------------------------------------

func TestConnectTimeout(t *testing.T) {
	t.Parallel()
	require.Equal(t, 10*time.Second, connectTimeout)
}

// ---------------------------------------------------------------------------
// inboundWebhookSecrets / inboundEmailSecrets JSON round-trip
// ---------------------------------------------------------------------------

func TestInboundWebhookSecrets_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	orig := inboundWebhookSecrets{
		SecretCurrentEncrypted:  []byte("cur"),
		SecretPreviousEncrypted: []byte("prev"),
	}
	data, err := json.Marshal(orig)
	require.NoError(t, err)

	var decoded inboundWebhookSecrets
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, orig.SecretCurrentEncrypted, decoded.SecretCurrentEncrypted)
	require.Equal(t, orig.SecretPreviousEncrypted, decoded.SecretPreviousEncrypted)
}

func TestInboundEmailSecrets_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	orig := inboundEmailSecrets{PasswordEncrypted: []byte("pw")}
	data, err := json.Marshal(orig)
	require.NoError(t, err)

	var decoded inboundEmailSecrets
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, orig.PasswordEncrypted, decoded.PasswordEncrypted)
}

// ---------------------------------------------------------------------------
// decryptDetail JSON tags
// ---------------------------------------------------------------------------

func TestDecryptDetail_JSONTags(t *testing.T) {
	t.Parallel()
	d := decryptDetail{
		LLMTotal:       3,
		LLMChecked:     2,
		InboundTotal:   1,
		InboundChecked: 1,
		Decrypted:      3,
	}
	data, err := json.Marshal(d)
	require.NoError(t, err)
	s := string(data)
	require.Contains(t, s, "llm_total")
	require.Contains(t, s, "llm_checked")
	require.Contains(t, s, "inbound_total")
	require.Contains(t, s, "inbound_checked")
	require.Contains(t, s, "decrypted")
}

// ---------------------------------------------------------------------------
// DrillReport JSON serialization
// ---------------------------------------------------------------------------

func TestDrillReport_JSONOmitsEmpty(t *testing.T) {
	t.Parallel()
	rep := DrillReport{
		Status:    StatusPass,
		StartedAt: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
		Checks:    []CheckResult{{Name: "conn", Status: StatusPass, Message: "ok"}},
	}
	data, err := json.Marshal(rep)
	require.NoError(t, err)
	s := string(data)
	// omitempty fields should not appear when zero/nil.
	require.NotContains(t, s, "backup_ref")
	require.NotContains(t, s, "rpo_seconds")
	require.NotContains(t, s, "rto_seconds")
	require.NotContains(t, s, "attune_version")
}

func TestDrillReport_JSONIncludesWhenSet(t *testing.T) {
	t.Parallel()
	rep := DrillReport{
		Status:        StatusWarn,
		StartedAt:     time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
		BackupRef:     "backup-2026-06-26",
		RPOSeconds:    ptrext.Of(int64(3600)),
		RTOSeconds:    ptrext.Of(int64(120)),
		AttuneVersion: "v0.5.0",
		Checks:        []CheckResult{},
	}
	data, err := json.Marshal(rep)
	require.NoError(t, err)
	s := string(data)
	require.Contains(t, s, "backup_ref")
	require.Contains(t, s, "rpo_seconds")
	require.Contains(t, s, "rto_seconds")
	require.Contains(t, s, "attune_version")
}

// ---------------------------------------------------------------------------
// recoveryDetail JSON serialization
// ---------------------------------------------------------------------------

func TestRecoveryDetail_JSONOmitsNil(t *testing.T) {
	t.Parallel()
	d := recoveryDetail{
		RPOSeconds: ptrext.Of(int64(3600)),
	}
	data, err := json.Marshal(d)
	require.NoError(t, err)
	s := string(data)
	require.Contains(t, s, "rpo_seconds")
	require.NotContains(t, s, "rto_seconds")
	require.NotContains(t, s, "rpo_target_seconds")
	require.NotContains(t, s, "rto_target_seconds")
}

// ---------------------------------------------------------------------------
// rowCountsDetail JSON serialization
// ---------------------------------------------------------------------------

func TestRowCountsDetail_JSONOmitsNilBaseline(t *testing.T) {
	t.Parallel()
	d := rowCountsDetail{
		Restored: map[string]int64{"tenants": 5},
	}
	data, err := json.Marshal(d)
	require.NoError(t, err)
	s := string(data)
	require.Contains(t, s, "restored")
	require.NotContains(t, s, "baseline")
}

// ---------------------------------------------------------------------------
// schemaDetail JSON serialization
// ---------------------------------------------------------------------------

func TestSchemaDetail_JSONTags(t *testing.T) {
	t.Parallel()
	d := schemaDetail{Total: 24, Applied: 20, Pending: 4}
	data, err := json.Marshal(d)
	require.NoError(t, err)
	s := string(data)
	require.Contains(t, s, `"total":24`)
	require.Contains(t, s, `"applied":20`)
	require.Contains(t, s, `"pending":4`)
}

// ---------------------------------------------------------------------------
// deepDetail JSON serialization
// ---------------------------------------------------------------------------

func TestDeepDetail_JSONTags(t *testing.T) {
	t.Parallel()
	d := deepDetail{IndexesChecked: 10, IndexesTotal: 12}
	data, err := json.Marshal(d)
	require.NoError(t, err)
	s := string(data)
	require.Contains(t, s, `"indexes_checked":10`)
	require.Contains(t, s, `"indexes_total":12`)
}

// ---------------------------------------------------------------------------
// Options struct
// ---------------------------------------------------------------------------

func TestOptions_ZeroValue(t *testing.T) {
	t.Parallel()
	var opts Options
	require.Empty(t, opts.BackupRef)
	require.Nil(t, opts.Baseline)
	require.Nil(t, opts.BaselineUnvalidated)
	require.Nil(t, opts.BaselineExtensions)
	require.False(t, opts.Deep)
	require.Nil(t, opts.BackupTakenAt)
	require.Nil(t, opts.RestoreDuration)
	require.Zero(t, opts.RPOTarget)
	require.Zero(t, opts.RTOTarget)
}

// ---------------------------------------------------------------------------
// Status constants
// ---------------------------------------------------------------------------

func TestStatusConstants(t *testing.T) {
	t.Parallel()
	require.Equal(t, Status("pass"), StatusPass)
	require.Equal(t, Status("warn"), StatusWarn)
	require.Equal(t, Status("fail"), StatusFail)
	require.Equal(t, Status("skipped"), StatusSkip)
}
