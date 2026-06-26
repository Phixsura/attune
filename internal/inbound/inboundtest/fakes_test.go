// SPDX-License-Identifier: Apache-2.0

package inboundtest

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
)

// -----------------------------------------------------------------------
// FakeIngest
// -----------------------------------------------------------------------

func TestFakeIngest_RecordsCalls(t *testing.T) {
	t.Parallel()
	fi := &FakeIngest{} // ptrext:allow mutex-identity
	in := domain.IngestInput{Source: "test", Content: "hello"}
	id, err := fi.Ingest(context.Background(), "t1", uuid.Nil, in)
	require.NoError(t, err)
	require.Equal(t, int64(1), id)
	require.Len(t, fi.Calls, 1)
	require.Equal(t, "t1", fi.Calls[0].TenantID)
	require.Equal(t, uuid.Nil, fi.Calls[0].KeyID)
	require.Equal(t, "test", fi.Calls[0].In.Source)
}

func TestFakeIngest_MultipleCalls(t *testing.T) {
	t.Parallel()
	fi := &FakeIngest{} // ptrext:allow mutex-identity
	in := domain.IngestInput{Source: "test"}

	id1, err := fi.Ingest(context.Background(), "t1", uuid.Nil, in)
	require.NoError(t, err)
	require.Equal(t, int64(1), id1)

	id2, err := fi.Ingest(context.Background(), "t2", uuid.Nil, in)
	require.NoError(t, err)
	require.Equal(t, int64(2), id2)
	require.Len(t, fi.Calls, 2)
}

func TestFakeIngest_ReturnsAndClearsNextErr(t *testing.T) {
	t.Parallel()
	fi := &FakeIngest{NextErr: errors.New("fail")} // ptrext:allow mutex-identity
	in := domain.IngestInput{Source: "test"}

	_, err := fi.Ingest(context.Background(), "t1", uuid.Nil, in)
	require.Error(t, err)
	require.Equal(t, "fail", err.Error())
	require.Empty(t, fi.Calls, "should not record call on error")

	// NextErr is cleared after first use
	id, err := fi.Ingest(context.Background(), "t1", uuid.Nil, in)
	require.NoError(t, err)
	require.Equal(t, int64(1), id)
}

// -----------------------------------------------------------------------
// FakeSources
// -----------------------------------------------------------------------

func TestFakeSources_PutAndGet(t *testing.T) {
	t.Parallel()
	fs := NewFakeSources()
	src := inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Channel:  "email",
		Slug:     "inbox",
		Enabled:  true,
	}
	fs.Put("demo", src)

	got, err := fs.Get(context.Background(), "s1")
	require.NoError(t, err)
	require.Equal(t, "s1", got.ID)
	require.Equal(t, "email", got.Channel)
}

func TestFakeSources_GetNotFound(t *testing.T) {
	t.Parallel()
	fs := NewFakeSources()
	_, err := fs.Get(context.Background(), "nonexistent")
	require.Error(t, err)
}

func TestFakeSources_GetBySlugs(t *testing.T) {
	t.Parallel()
	fs := NewFakeSources()
	src := inbound.Source{
		ID:       "s1",
		TenantID: "t1",
		Channel:  "email",
		Slug:     "inbox",
	}
	fs.Put("demo", src)

	got, err := fs.GetBySlugs(context.Background(), "demo", "email", "inbox")
	require.NoError(t, err)
	require.Equal(t, "s1", got.ID)
}

func TestFakeSources_GetBySlugsNotFound(t *testing.T) {
	t.Parallel()
	fs := NewFakeSources()
	_, err := fs.GetBySlugs(context.Background(), "no", "no", "no")
	require.Error(t, err)
}

func TestFakeSources_List(t *testing.T) {
	t.Parallel()
	fs := NewFakeSources()
	fs.Put("demo", inbound.Source{ID: "s1", Channel: "email", Slug: "a"})
	fs.Put("demo", inbound.Source{ID: "s2", Channel: "email", Slug: "b"})
	fs.Put("demo", inbound.Source{ID: "s3", Channel: "webhook", Slug: "c"})

	got, err := fs.List(context.Background(), "email")
	require.NoError(t, err)
	require.Len(t, got, 2, "should list only email sources")

	got2, err := fs.List(context.Background(), "webhook")
	require.NoError(t, err)
	require.Len(t, got2, 1)

	got3, err := fs.List(context.Background(), "nonexistent")
	require.NoError(t, err)
	require.Empty(t, got3)
}

func TestFakeSources_UpdateState(t *testing.T) {
	t.Parallel()
	fs := NewFakeSources()
	fs.Put("demo", inbound.Source{ID: "s1", Channel: "email"})

	state := inbound.SourceState{LastUID: 42, LastError: "test"}
	err := fs.UpdateState(context.Background(), "s1", state)
	require.NoError(t, err)

	got, err := fs.Get(context.Background(), "s1")
	require.NoError(t, err)
	require.Equal(t, int64(42), got.State.LastUID)
	require.Equal(t, "test", got.State.LastError)
}

func TestFakeSources_UpdateStateNotFound(t *testing.T) {
	t.Parallel()
	fs := NewFakeSources()
	err := fs.UpdateState(context.Background(), "nonexistent", inbound.SourceState{})
	require.Error(t, err)
}

func TestFakeSources_SetEnabled(t *testing.T) {
	t.Parallel()
	fs := NewFakeSources()
	fs.Put("demo", inbound.Source{ID: "s1", Channel: "email", Enabled: true})

	err := fs.SetEnabled(context.Background(), "s1", false, "test reason")
	require.NoError(t, err)

	got, err := fs.Get(context.Background(), "s1")
	require.NoError(t, err)
	require.False(t, got.Enabled)
}

func TestFakeSources_SetEnabledNotFound(t *testing.T) {
	t.Parallel()
	fs := NewFakeSources()
	err := fs.SetEnabled(context.Background(), "nonexistent", false, "reason")
	require.Error(t, err)
}

// -----------------------------------------------------------------------
// FakeSecrets
// -----------------------------------------------------------------------

func TestFakeSecrets_EncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()
	s := FakeSecrets{}
	plain := []byte("hello world")
	enc, err := s.Encrypt(plain)
	require.NoError(t, err)
	require.NotEqual(t, plain, enc)

	dec, err := s.Decrypt(enc)
	require.NoError(t, err)
	require.Equal(t, plain, dec)
}

func TestFakeSecrets_EncryptOversizeInput(t *testing.T) {
	t.Parallel()
	s := FakeSecrets{}
	big := make([]byte, (1<<20)+1)
	_, err := s.Encrypt(big)
	require.Error(t, err)
}

func TestFakeSecrets_DecryptTooShort(t *testing.T) {
	t.Parallel()
	s := FakeSecrets{}
	_, err := s.Decrypt([]byte{0x01})
	require.Error(t, err)
}

func TestFakeSecrets_DecryptEmpty(t *testing.T) {
	t.Parallel()
	s := FakeSecrets{}
	_, err := s.Decrypt(nil)
	require.Error(t, err)
}

// -----------------------------------------------------------------------
// FakeMetrics
// -----------------------------------------------------------------------

func TestFakeMetrics_Total(t *testing.T) {
	t.Parallel()
	m := &FakeMetrics{} // ptrext:allow test-fixture
	m.Total("email", "t1", "inbox", "ok")
	require.Len(t, m.Totals, 1)
	require.Equal(t, "email|t1|inbox|ok", m.Totals[0])
}

func TestFakeMetrics_Latency(t *testing.T) {
	t.Parallel()
	m := &FakeMetrics{} // ptrext:allow test-fixture
	m.Latency("email", "t1", "inbox", 1.5)
	require.Len(t, m.Latencies, 1)
	require.Equal(t, "email|t1|inbox", m.Latencies[0])
	require.Equal(t, 1.5, m.LatencySeconds[0])
}

func TestFakeMetrics_SetSourceState(t *testing.T) {
	t.Parallel()
	m := &FakeMetrics{} // ptrext:allow test-fixture
	m.SetSourceState("email", "t1", "inbox", "enabled", true)
	m.SetSourceState("email", "t1", "inbox", "enabled", false)
	require.Len(t, m.StateCalls, 2)
	require.Equal(t, "email|t1|inbox|enabled=on", m.StateCalls[0])
	require.Equal(t, "email|t1|inbox|enabled=off", m.StateCalls[1])
}

func TestFakeMetrics_SetPollLag(t *testing.T) {
	t.Parallel()
	m := &FakeMetrics{} // ptrext:allow test-fixture
	m.SetPollLag("email", "t1", "inbox", 5.0)
	require.Len(t, m.PollLags, 1)
	require.Equal(t, "email|t1|inbox", m.PollLags[0])
}

// -----------------------------------------------------------------------
// FakeMux
// -----------------------------------------------------------------------

func TestFakeMux_RecordsRoutes(t *testing.T) {
	t.Parallel()
	m := &FakeMux{} // ptrext:allow test-fixture
	m.Method("POST", "/webhook/{source}", nil)
	require.Len(t, m.Routes, 1)
	require.Equal(t, "POST", m.Routes[0].Method)
	require.Equal(t, "/webhook/{source}", m.Routes[0].Pattern)
}

// -----------------------------------------------------------------------
// DepsFor
// -----------------------------------------------------------------------

func TestDepsFor_NilArgs(t *testing.T) {
	t.Parallel()
	deps := DepsFor(nil, nil, nil)
	require.NotNil(t, deps.Mux)
	require.NotNil(t, deps.Ingest)
	require.NotNil(t, deps.Sources)
	require.NotNil(t, deps.Secrets)
	require.NotNil(t, deps.Metrics)
}

func TestDepsFor_CustomArgs(t *testing.T) {
	t.Parallel()
	fi := &FakeIngest{} // ptrext:allow mutex-identity
	fs := NewFakeSources()
	fm := &FakeMux{} // ptrext:allow test-fixture
	deps := DepsFor(fi, fs, fm)
	require.Equal(t, fm, deps.Mux)
	require.Equal(t, fs, deps.Sources)
}
