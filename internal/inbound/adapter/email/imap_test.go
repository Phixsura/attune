// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// -----------------------------------------------------------------------
// wipe
// -----------------------------------------------------------------------

func TestWipe_ZerosSlice(t *testing.T) {
	t.Parallel()
	b := []byte("secret-password")
	wipe(b)
	for i, v := range b {
		require.Equal(t, byte(0), v, "byte %d should be zeroed", i)
	}
}

func TestWipe_EmptySlice(t *testing.T) {
	t.Parallel()
	// Should not panic on empty/nil slices
	wipe(nil)
	wipe([]byte{})
}

// -----------------------------------------------------------------------
// newSearchCriteria
// -----------------------------------------------------------------------

func TestNewSearchCriteria_ZeroLastUID(t *testing.T) {
	t.Parallel()
	c := newSearchCriteria(0)
	require.NotNil(t, c)
	require.Empty(t, c.UID, "lastUID==0 should produce no UID filter")
}

func TestNewSearchCriteria_NonZeroLastUID(t *testing.T) {
	t.Parallel()
	c := newSearchCriteria(42)
	require.NotNil(t, c)
	require.NotEmpty(t, c.UID, "lastUID>0 should produce a UID filter")
}

// -----------------------------------------------------------------------
// ShutdownTimeout
// -----------------------------------------------------------------------

func TestShutdownTimeout(t *testing.T) {
	t.Parallel()
	a := NewAdapter()
	st, ok := a.(inbound.ShutdownTimeouter)
	require.True(t, ok, "email adapter should implement ShutdownTimeouter")
	require.Equal(t, 10*time.Second, st.ShutdownTimeout())
}

// -----------------------------------------------------------------------
// persistPollResult — tests that override nowFn are NOT parallel because
// nowFn is a package-level variable.
// -----------------------------------------------------------------------

func TestPersistPollResult_NewUIDs(t *testing.T) {
	// NOT parallel: overrides package-level nowFn
	origNow := nowFn
	now := time.Unix(1_700_000_000, 0)
	nowFn = func() time.Time { return now }
	defer func() { nowFn = origNow }()

	sources := inboundtest.NewFakeSources()
	sources.Put("demo", inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Channel:  channelName,
		Slug:     "inbox",
		Enabled:  true,
		State:    inbound.SourceState{LastUID: 10},
	})

	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps:          inbound.Deps{Sources: sources, Metrics: ptrext.Of(inboundtest.FakeMetrics{})},
		lastSuccessAt: map[string]time.Time{},
	}

	src := inbound.Source{
		ID:    "s1",
		State: inbound.SourceState{LastUID: 10},
	}
	a.persistPollResult(context.Background(), src, 20)

	got, err := sources.Get(context.Background(), "s1")
	require.NoError(t, err)
	require.Equal(t, int64(20), got.State.LastUID)
}

func TestPersistPollResult_StaleError(t *testing.T) {
	t.Parallel()
	sources := inboundtest.NewFakeSources()
	sources.Put("demo", inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Channel:  channelName,
		Slug:     "inbox",
		Enabled:  true,
		State: inbound.SourceState{
			LastUID:   10,
			LastError: "old error",
		},
	})

	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps:          inbound.Deps{Sources: sources, Metrics: ptrext.Of(inboundtest.FakeMetrics{})},
		lastSuccessAt: map[string]time.Time{},
	}

	src := inbound.Source{
		ID: "s1",
		State: inbound.SourceState{
			LastUID:   10,
			LastError: "old error",
		},
	}
	a.persistPollResult(context.Background(), src, 10)

	got, err := sources.Get(context.Background(), "s1")
	require.NoError(t, err)
	require.Equal(t, "", got.State.LastError, "stale error should be cleared")
}

func TestPersistPollResult_NoChange(t *testing.T) {
	t.Parallel()
	sources := inboundtest.NewFakeSources()
	sources.Put("demo", inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Channel:  channelName,
		Slug:     "inbox",
		Enabled:  true,
		State:    inbound.SourceState{LastUID: 10},
	})

	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps:          inbound.Deps{Sources: sources, Metrics: ptrext.Of(inboundtest.FakeMetrics{})},
		lastSuccessAt: map[string]time.Time{},
	}

	src := inbound.Source{
		ID:    "s1",
		State: inbound.SourceState{LastUID: 10},
	}
	a.persistPollResult(context.Background(), src, 10)

	got, err := sources.Get(context.Background(), "s1")
	require.NoError(t, err)
	require.Equal(t, int64(10), got.State.LastUID, "UID should be unchanged")
}

// -----------------------------------------------------------------------
// seedFirstPollCursor
// -----------------------------------------------------------------------

func TestSeedFirstPollCursor_AlreadyHasUID(t *testing.T) {
	t.Parallel()
	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps:          inbound.Deps{Sources: inboundtest.NewFakeSources()},
		lastSuccessAt: map[string]time.Time{},
	}

	src := inbound.Source{
		ID:    "s1",
		State: inbound.SourceState{LastUID: 42},
	}
	got := a.seedFirstPollCursor(context.Background(), src, "now", nil)
	require.Equal(t, int64(42), got.State.LastUID, "already-seeded source should be unchanged")
}

func TestSeedFirstPollCursor_AllUnseen(t *testing.T) {
	t.Parallel()
	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps:          inbound.Deps{Sources: inboundtest.NewFakeSources()},
		lastSuccessAt: map[string]time.Time{},
	}

	src := inbound.Source{
		ID:    "s1",
		State: inbound.SourceState{LastUID: 0},
	}
	got := a.seedFirstPollCursor(context.Background(), src, "all_unseen", nil)
	require.Equal(t, int64(0), got.State.LastUID, "all_unseen should keep LastUID=0")
}

func TestSeedFirstPollCursor_NowWithSelectData(t *testing.T) {
	t.Parallel()
	sources := inboundtest.NewFakeSources()
	sources.Put("demo", inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Channel:  channelName,
		Slug:     "inbox",
		Enabled:  true,
	})

	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps:          inbound.Deps{Sources: sources},
		lastSuccessAt: map[string]time.Time{},
	}

	src := inbound.Source{
		ID:    "s1",
		State: inbound.SourceState{LastUID: 0},
	}
	selectData := ptrext.Of(imap.SelectData{UIDNext: 100})
	got := a.seedFirstPollCursor(context.Background(), src, "now", selectData)
	require.Equal(t, int64(99), got.State.LastUID, "should seed to UIDNext-1")
}

func TestSeedFirstPollCursor_NowWithNilSelectData(t *testing.T) {
	t.Parallel()
	sources := inboundtest.NewFakeSources()
	sources.Put("demo", inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Channel:  channelName,
		Slug:     "inbox",
		Enabled:  true,
	})

	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps:          inbound.Deps{Sources: sources},
		lastSuccessAt: map[string]time.Time{},
	}

	src := inbound.Source{
		ID:    "s1",
		State: inbound.SourceState{LastUID: 0},
	}
	got := a.seedFirstPollCursor(context.Background(), src, "now", nil)
	require.Equal(t, int64(0), got.State.LastUID, "nil selectData should seed to 0")
}

// -----------------------------------------------------------------------
// transientError
// -----------------------------------------------------------------------

func TestTransientError(t *testing.T) {
	t.Parallel()
	sources := inboundtest.NewFakeSources()
	sources.Put("demo", inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Channel:  channelName,
		Slug:     "inbox",
		Enabled:  true,
	})
	metrics := ptrext.Of(inboundtest.FakeMetrics{})

	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps:          inbound.Deps{Sources: sources, Metrics: metrics},
		lastSuccessAt: map[string]time.Time{},
	}

	src := inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Slug:     "inbox",
		State:    inbound.SourceState{LastUID: 10},
	}
	a.transientError(context.Background(), src, "dial: imap server unreachable")

	got, err := sources.Get(context.Background(), "s1")
	require.NoError(t, err)
	require.Equal(t, "dial: imap server unreachable", got.State.LastError)
	require.Equal(t, int64(10), got.State.LastUID, "UID should be preserved")
	require.Contains(t, metrics.Totals[0], "transient_err")
}

// -----------------------------------------------------------------------
// markPollSuccess — nil lastSuccessAt map
// -----------------------------------------------------------------------

func TestMarkPollSuccess_NilMap(t *testing.T) {
	t.Parallel()
	a := &adapter{} // ptrext:allow inbound-adapter-mutex-identity
	a.lastSuccessAt = nil
	now := time.Unix(1_700_000_000, 0)
	a.markPollSuccess("test", now)
	require.NotNil(t, a.lastSuccessAt)
	require.Equal(t, now, a.lastSuccessAt["test"])
}

// -----------------------------------------------------------------------
// Start — idempotent (already started)
// -----------------------------------------------------------------------

func TestStart_Idempotent(t *testing.T) {
	t.Parallel()
	a := NewAdapter()
	deps := inboundtest.DepsFor(nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := a.Start(ctx, deps)
	require.NoError(t, err)

	// Second start should be a no-op
	err = a.Start(ctx, deps)
	require.NoError(t, err)

	require.NoError(t, a.Shutdown(context.Background()))
}

// -----------------------------------------------------------------------
// pollSource — tests that override nowFn are NOT parallel.
// -----------------------------------------------------------------------

func TestPollSource_BadConfig(t *testing.T) {
	// NOT parallel: overrides package-level nowFn
	origNow := nowFn
	now := time.Unix(1_700_000_000, 0)
	nowFn = func() time.Time { return now }
	defer func() { nowFn = origNow }()

	sources := inboundtest.NewFakeSources()
	sources.Put("demo", inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Channel:  channelName,
		Slug:     "inbox",
		Enabled:  true,
		Config:   []byte{0x00},
	})
	metrics := ptrext.Of(inboundtest.FakeMetrics{})

	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps: inbound.Deps{
			Sources: sources,
			Secrets: inboundtest.FakeSecrets{},
			Metrics: metrics,
		},
		lastSuccessAt: map[string]time.Time{},
	}

	src := inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Slug:     "inbox",
		Config:   []byte{0x00},
	}
	a.pollSource(context.Background(), src)

	got, err := sources.Get(context.Background(), "s1")
	require.NoError(t, err)
	require.Equal(t, "decrypt config", got.State.LastError)
}

func TestPollSource_BlockedHost(t *testing.T) {
	// NOT parallel: overrides package-level nowFn
	origNow := nowFn
	now := time.Unix(1_700_000_000, 0)
	nowFn = func() time.Time { return now }
	defer func() { nowFn = origNow }()

	secrets := inboundtest.FakeSecrets{}
	pw, err := secrets.Encrypt([]byte("pw"))
	require.NoError(t, err)
	cfg := Config{
		Version:           ConfigVersion,
		Host:              "", // empty host is blocked
		Port:              993,
		Username:          "user@example.com",
		PasswordEncrypted: pw,
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	enc, err := secrets.Encrypt(raw)
	require.NoError(t, err)

	sources := inboundtest.NewFakeSources()
	sources.Put("demo", inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Channel:  channelName,
		Slug:     "inbox",
		Enabled:  true,
		Config:   enc,
	})
	metrics := ptrext.Of(inboundtest.FakeMetrics{})

	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps: inbound.Deps{
			Sources: sources,
			Secrets: secrets,
			Metrics: metrics,
		},
		lastSuccessAt: map[string]time.Time{},
	}

	src := inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Slug:     "inbox",
		Config:   enc,
	}
	a.pollSource(context.Background(), src)

	got, err := sources.Get(context.Background(), "s1")
	require.NoError(t, err)
	require.False(t, got.Enabled)
	require.Contains(t, metrics.Totals[0], "validate_err")
}
