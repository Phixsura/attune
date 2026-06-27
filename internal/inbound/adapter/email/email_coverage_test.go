// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package email

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// -----------------------------------------------------------------------
// parseRFC822 — uncovered branches
// -----------------------------------------------------------------------

func TestParseRFC822_CompletelyMalformed(t *testing.T) {
	t.Parallel()
	// Garbage input that message.Read cannot parse at all — covers the
	// early-return error branch at line 35-37.
	_, err := parseRFC822([]byte("not a valid RFC822 message at all"))
	// The go-message library may or may not error on minimal input; the
	// key assertion is no panic.
	_ = err
}

func TestParseRFC822_EmptyInput(t *testing.T) {
	t.Parallel()
	_, err := parseRFC822([]byte{})
	_ = err
}

func TestParseRFC822_MultipartHTMLOnly(t *testing.T) {
	t.Parallel()
	// A multipart message with ONLY an HTML part — the parser should
	// iterate all parts, find no text/plain, and return with empty TextBody
	// rather than handing raw HTML to downstream. Covers the "no text/plain
	// part found" return at lines 69-71.
	raw := strings.Join([]string{
		"From: html-only@example.com",
		"To: feedback@team.com",
		"Subject: HTML-only feedback",
		"Message-ID: <html-only@example.com>",
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=\"htmlbound\"",
		"Date: Sun, 8 Jun 2026 12:10:00 +0000",
		"",
		"--htmlbound",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<p>Only HTML here, no <b>plain</b> text part.</p>",
		"",
		"--htmlbound--",
	}, "\r\n")

	p, err := parseRFC822([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, "html-only@example.com", p.From)
	require.Equal(t, "HTML-only feedback", p.Subject)
	require.Equal(t, "<html-only@example.com>", p.MessageID)
	// TextBody must be empty — no text/plain part existed.
	require.Empty(t, p.TextBody,
		"multipart with only HTML parts should leave TextBody empty")
}

func TestParseRFC822_NonMultipartPlainBody(t *testing.T) {
	t.Parallel()
	// A simple non-multipart message with an explicit Content-Type header.
	// Covers the io.ReadAll(msg.Body) path at lines 74-79 (the non-multipart
	// branch after the multipart reader check).
	raw := strings.Join([]string{
		"From: simple@example.com",
		"To: feedback@team.com",
		"Subject: Simple plain body",
		"Message-ID: <plain-body@example.com>",
		"Content-Type: text/plain; charset=utf-8",
		"Date: Sun, 8 Jun 2026 12:10:00 +0000",
		"",
		"This is a simple plain text body.",
	}, "\r\n")

	p, err := parseRFC822([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, "simple@example.com", p.From)
	require.Equal(t, "Simple plain body", p.Subject)
	require.Contains(t, p.TextBody, "simple plain text body")
}

func TestParseRFC822_MultipartHTMLFirstTextSecond(t *testing.T) {
	t.Parallel()
	// Multipart with HTML first, text/plain second — should still prefer
	// text/plain. Exercises the multipart iteration loop skipping
	// non-text/plain parts before finding the match.
	raw := strings.Join([]string{
		"From: mixed@example.com",
		"To: feedback@team.com",
		"Subject: Mixed order",
		"Message-ID: <mixed@example.com>",
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=\"mixbound\"",
		"Date: Sun, 8 Jun 2026 12:10:00 +0000",
		"",
		"--mixbound",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<p>HTML part comes first.</p>",
		"",
		"--mixbound",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Plain text part comes second.",
		"",
		"--mixbound--",
	}, "\r\n")

	p, err := parseRFC822([]byte(raw))
	require.NoError(t, err)
	require.Contains(t, p.TextBody, "Plain text part comes second",
		"should prefer text/plain even when it appears after HTML")
	require.NotContains(t, p.TextBody, "<p>",
		"should not contain HTML tags")
}

func TestParseRFC822_NoReferencesOrInReplyTo(t *testing.T) {
	t.Parallel()
	// A message with no References or In-Reply-To headers.
	raw := strings.Join([]string{
		"From: norefs@example.com",
		"To: feedback@team.com",
		"Subject: No references header",
		"Message-ID: <norefs@example.com>",
		"Content-Type: text/plain; charset=utf-8",
		"Date: Sun, 8 Jun 2026 12:10:00 +0000",
		"",
		"Body text.",
	}, "\r\n")

	p, err := parseRFC822([]byte(raw))
	require.NoError(t, err)
	require.Empty(t, p.References, "no References header means empty slice")
	require.Empty(t, p.InReplyTo, "no In-Reply-To header means empty string")
}

func TestParseRFC822_MultipleReferences(t *testing.T) {
	t.Parallel()
	// A message with multiple space-separated References.
	raw := strings.Join([]string{
		"From: multirefs@example.com",
		"To: feedback@team.com",
		"Subject: Multiple refs",
		"Message-ID: <d@example.com>",
		"In-Reply-To: <c@example.com>",
		"References: <a@example.com> <b@example.com> <c@example.com>",
		"Content-Type: text/plain; charset=utf-8",
		"Date: Sun, 8 Jun 2026 12:10:00 +0000",
		"",
		"Body.",
	}, "\r\n")

	p, err := parseRFC822([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, "<c@example.com>", p.InReplyTo)
	require.Len(t, p.References, 3)
	require.Equal(t, "<a@example.com>", p.References[0])
	require.Equal(t, "<b@example.com>", p.References[1])
	require.Equal(t, "<c@example.com>", p.References[2])
}

func TestParseRFC822_MultipartWithMultipleNonTextParts(t *testing.T) {
	t.Parallel()
	// A multipart/mixed with an attachment-like part and no text/plain.
	// Exercises the full multipart iteration exhausting all parts without
	// finding text/plain.
	raw := strings.Join([]string{
		"From: attach@example.com",
		"To: feedback@team.com",
		"Subject: Attachment only",
		"Message-ID: <attach@example.com>",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=\"attbound\"",
		"Date: Sun, 8 Jun 2026 12:10:00 +0000",
		"",
		"--attbound",
		"Content-Type: application/octet-stream",
		"Content-Disposition: attachment; filename=\"data.bin\"",
		"",
		"binarydata",
		"",
		"--attbound",
		"Content-Type: image/png",
		"Content-Disposition: attachment; filename=\"logo.png\"",
		"",
		"fakepng",
		"",
		"--attbound--",
	}, "\r\n")

	p, err := parseRFC822([]byte(raw))
	require.NoError(t, err)
	require.Empty(t, p.TextBody,
		"multipart with no text/plain should leave TextBody empty")
}

// -----------------------------------------------------------------------
// ValidateOutboundHost — uncovered branches
// -----------------------------------------------------------------------

func TestValidateOutboundHost_MulticastV4(t *testing.T) {
	t.Parallel()
	// 239.255.255.250 is in the IPv4 multicast range but NOT in the
	// link-local multicast sub-range (224.0.0.0/24). This hits the
	// IsMulticast() check at lines 56-58 in host_guard.go without
	// being caught earlier by IsLinkLocalMulticast().
	err := ValidateOutboundHost("239.255.255.250")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrBlockedHost))
	require.Contains(t, err.Error(), "multicast")
}

func TestValidateOutboundHost_UnresolvableDNS(t *testing.T) {
	t.Parallel()
	// A hostname that won't resolve in DNS — covers the fail-open path
	// at lines 43-47 in host_guard.go (the "let the dial fail naturally"
	// branch). The .invalid TLD is reserved by RFC 2606 for exactly this.
	err := ValidateOutboundHost("this-host-does-not-exist.invalid")
	// Fail-open: should return nil so the dial can produce a natural error.
	require.NoError(t, err, "unresolvable host should pass (fail-open)")
}

// -----------------------------------------------------------------------
// seedFirstPollCursor — uncovered branches
// -----------------------------------------------------------------------

func TestSeedFirstPollCursor_UIDNextZero(t *testing.T) {
	t.Parallel()
	// selectData is non-nil but UIDNext=0 — seedUID stays at its var
	// zero (int64(0)), the < 0 guard is not triggered.
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
	selectData := ptrext.Of(imap.SelectData{UIDNext: 0})
	got := a.seedFirstPollCursor(context.Background(), src, "now", selectData)
	require.Equal(t, int64(0), got.State.LastUID,
		"UIDNext=0 should seed to 0")
}

func TestSeedFirstPollCursor_UIDNextOne(t *testing.T) {
	t.Parallel()
	// UIDNext=1 yields seedUID=0.
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
	selectData := ptrext.Of(imap.SelectData{UIDNext: 1})
	got := a.seedFirstPollCursor(context.Background(), src, "now", selectData)
	require.Equal(t, int64(0), got.State.LastUID, "UIDNext=1 should seed to 0")
}

func TestSeedFirstPollCursor_UpdateStateError(t *testing.T) {
	t.Parallel()
	// Source ID not in the fake store — UpdateState returns an error.
	// Covers the error logging path at lines 186-189.
	sources := inboundtest.NewFakeSources()
	// Deliberately do NOT put the source in the store so UpdateState fails.

	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps:          inbound.Deps{Sources: sources},
		lastSuccessAt: map[string]time.Time{},
	}

	src := inbound.Source{
		ID:    "nonexistent",
		State: inbound.SourceState{LastUID: 0},
	}
	selectData := ptrext.Of(imap.SelectData{UIDNext: 50})
	got := a.seedFirstPollCursor(context.Background(), src, "now", selectData)
	// Even when UpdateState fails, the in-memory source should still be
	// updated (the caller relies on the return value, not the DB state).
	require.Equal(t, int64(49), got.State.LastUID,
		"should still set in-memory LastUID despite UpdateState error")
}

func TestSeedFirstPollCursor_AllUnseenCaseInsensitive(t *testing.T) {
	t.Parallel()
	// "ALL_UNSEEN" (uppercase) should match via strings.EqualFold.
	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps:          inbound.Deps{Sources: inboundtest.NewFakeSources()},
		lastSuccessAt: map[string]time.Time{},
	}

	src := inbound.Source{
		ID:    "s1",
		State: inbound.SourceState{LastUID: 0},
	}
	got := a.seedFirstPollCursor(context.Background(), src, "ALL_UNSEEN", nil)
	require.Equal(t, int64(0), got.State.LastUID,
		"ALL_UNSEEN should skip seeding")
}

// -----------------------------------------------------------------------
// pollLoop — cancel and iteration paths
// -----------------------------------------------------------------------

// errorSources is a SourceStore that returns an error on List.
type errorSources struct {
	*inboundtest.FakeSources
	listErr error
}

func (e *errorSources) List(_ context.Context, _ string) ([]inbound.Source, error) {
	return nil, e.listErr
}

func TestPollLoop_CancelledContext(t *testing.T) {
	t.Parallel()
	// A pre-cancelled context should cause pollLoop to exit on the first
	// iteration's select case (ctx.Done).
	sources := inboundtest.NewFakeSources()
	metrics := ptrext.Of(inboundtest.FakeMetrics{})

	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps: inbound.Deps{
			Sources: sources,
			Metrics: metrics,
		},
		lastSuccessAt: map[string]time.Time{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	a.wg.Add(1)
	done := make(chan struct{})
	go func() {
		a.pollLoop(ctx)
		close(done)
	}()

	select {
	case <-done:
		// pollLoop exited.
	case <-time.After(5 * time.Second):
		t.Fatal("pollLoop did not exit after context cancel")
	}
}

func TestPollLoop_ListError(t *testing.T) {
	t.Parallel()
	// When List returns an error, pollLoop should log and continue to the
	// select. We cancel right after to verify it does not crash.
	// Covers the error path at lines 28-30.
	errSources := &errorSources{ // ptrext:allow interface-embedding
		FakeSources: inboundtest.NewFakeSources(),
		listErr:     errors.New("db connection lost"),
	}
	metrics := ptrext.Of(inboundtest.FakeMetrics{})

	a := &adapter{ // ptrext:allow inbound-adapter-mutex-identity
		deps: inbound.Deps{
			Sources: errSources,
			Metrics: metrics,
		},
		lastSuccessAt: map[string]time.Time{},
	}

	ctx, cancel := context.WithCancel(context.Background())

	a.wg.Add(1)
	done := make(chan struct{})
	go func() {
		a.pollLoop(ctx)
		close(done)
	}()

	// Give pollLoop one tick to hit the error path, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// pollLoop exited.
	case <-time.After(5 * time.Second):
		t.Fatal("pollLoop did not exit after cancel")
	}
}

func TestPollLoop_SkipsDisabledSources(t *testing.T) {
	t.Parallel()
	// When a disabled source appears in the list (stale read), pollLoop
	// skips it. Covers the !src.Enabled check at lines 35-39.
	sources := inboundtest.NewFakeSources()
	sources.Put("demo", inbound.Source{
		ID:       "s-disabled",
		TenantID: "t1",
		Channel:  channelName,
		Slug:     "inbox",
		Enabled:  false, // disabled — should be skipped
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

	ctx, cancel := context.WithCancel(context.Background())

	a.wg.Add(1)
	done := make(chan struct{})
	go func() {
		a.pollLoop(ctx)
		close(done)
	}()

	// Let one tick pass, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// pollLoop exited.
	case <-time.After(5 * time.Second):
		t.Fatal("pollLoop did not exit after cancel")
	}
	// Metrics should be empty — the disabled source was skipped entirely,
	// so no pollSource calls were made.
	require.Empty(t, metrics.Totals,
		"disabled source should not trigger any metric emissions")
}

// -----------------------------------------------------------------------
// pollSource — dial failure path
// -----------------------------------------------------------------------

// dialIMAPFailFunc creates a dialIMAP replacement that always returns err.
func dialIMAPFailFunc(err error) func(context.Context, string, *imapclient.Options) (*imapclient.Client, error) {
	return func(_ context.Context, _ string, _ *imapclient.Options) (*imapclient.Client, error) {
		return nil, err
	}
}

func TestPollSource_DialFailure(t *testing.T) {
	// NOT parallel: overrides package-level dialIMAP + nowFn.
	origDial := dialIMAP
	defer func() { dialIMAP = origDial }()
	dialIMAP = dialIMAPFailFunc(errors.New("connection refused"))

	origNow := nowFn
	now := time.Unix(1_700_000_000, 0)
	nowFn = func() time.Time { return now }
	defer func() { nowFn = origNow }()

	secrets := inboundtest.FakeSecrets{}
	pw, err := secrets.Encrypt([]byte("pw"))
	require.NoError(t, err)
	cfg := Config{
		Version:           ConfigVersion,
		Host:              "127.0.0.1",
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
	require.Contains(t, got.State.LastError, "dial: imap server unreachable")
	require.Contains(t, metrics.Totals[0], "transient_err")
}

// Compile-time assertion that our test types satisfy their interfaces.
var _ inbound.SourceStore = (*errorSources)(nil)
