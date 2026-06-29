// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestChannel_ID(t *testing.T) {
	t.Parallel()
	ch := ptrext.Of(channel{})
	assert.Equal(t, "email", ch.ID())
}

func TestChannel_RenderEvent_StubReturnsNil(t *testing.T) {
	t.Parallel()
	ch := ptrext.Of(channel{})
	rendered, err := ch.RenderEvent(ptrext.Of(outbound.Envelope{}), outbound.Target{})
	require.NoError(t, err)

	req, err := rendered.Build(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, req)

	err = rendered.Check(context.Background(), 200, nil)
	assert.NoError(t, err)
}

func TestChannel_RenderDigest_StubReturnsNil(t *testing.T) {
	t.Parallel()
	ch := ptrext.Of(channel{})
	rendered, err := ch.RenderDigest(nil, outbound.Target{})
	require.NoError(t, err)

	req, err := rendered.Build(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, req)
}

func TestParseConfig_HappyPath(t *testing.T) {
	t.Parallel()
	dst := outbound.Target{
		Secret: "smtp-password-123",
		Config: map[string]any{
			"from":              "noreply@example.com",
			"to":                "team@example.com",
			"smtp_host":         "smtp.example.com",
			"smtp_port":         "465",
			"smtp_username":     "apikey",
			"smtp_implicit_tls": true,
		},
	}

	cfg, err := parseConfig(dst)
	require.NoError(t, err)
	assert.Equal(t, "smtp.example.com", cfg.host)
	assert.Equal(t, "465", cfg.port)
	assert.True(t, cfg.implicitTLS)
	assert.Equal(t, "apikey", cfg.username)
	assert.Equal(t, "smtp-password-123", cfg.password)
	assert.Equal(t, "noreply@example.com", cfg.from)
	assert.Equal(t, "team@example.com", cfg.to)
}

func TestParseConfig_DefaultPort(t *testing.T) {
	t.Parallel()
	dst := outbound.Target{
		Config: map[string]any{
			"from":      "noreply@example.com",
			"to":        "team@example.com",
			"smtp_host": "smtp.example.com",
		},
	}

	cfg, err := parseConfig(dst)
	require.NoError(t, err)
	assert.Equal(t, "587", cfg.port)
	assert.False(t, cfg.implicitTLS)
}

func TestParseConfig_ToFallsBackToURL(t *testing.T) {
	t.Parallel()
	dst := outbound.Target{
		URL: "alerts@example.com",
		Config: map[string]any{
			"from":      "noreply@example.com",
			"smtp_host": "smtp.example.com",
		},
	}

	cfg, err := parseConfig(dst)
	require.NoError(t, err)
	assert.Equal(t, "alerts@example.com", cfg.to)
}

func TestParseConfig_MissingFrom(t *testing.T) {
	t.Parallel()
	dst := outbound.Target{
		Config: map[string]any{
			"to":        "team@example.com",
			"smtp_host": "smtp.example.com",
		},
	}

	_, err := parseConfig(dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "from address")
}

func TestParseConfig_MissingTo(t *testing.T) {
	t.Parallel()
	dst := outbound.Target{
		Config: map[string]any{
			"from":      "noreply@example.com",
			"smtp_host": "smtp.example.com",
		},
	}

	_, err := parseConfig(dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "to address")
}

func TestParseConfig_MissingHost(t *testing.T) {
	t.Parallel()
	dst := outbound.Target{
		Config: map[string]any{
			"from": "noreply@example.com",
			"to":   "team@example.com",
		},
	}

	_, err := parseConfig(dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "smtp_host")
}

func TestBuildRFC822(t *testing.T) {
	t.Parallel()
	msg := buildRFC822("from@test.com", "to@test.com", "Hello", "Body text")
	s := string(msg)

	assert.Contains(t, s, "From: from@test.com\r\n")
	assert.Contains(t, s, "To: to@test.com\r\n")
	assert.Contains(t, s, "Subject: Hello\r\n")
	assert.Contains(t, s, "Content-Type: text/plain; charset=utf-8\r\n")
	assert.Contains(t, s, "X-Mailer: attune/1.0\r\n")
	assert.Contains(t, s, "\r\n\r\nBody text")
}

func TestRenderEventEmail_Basic(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Timestamp: "2026-01-01T00:00:00Z",
		Feedback: map[string]any{
			"title":   "Login broken",
			"content": "Cannot login since yesterday",
			"source":  "web",
		},
	})

	subject, body := renderEventEmail(env)
	assert.Equal(t, "[Attune] Login broken", subject)
	assert.Contains(t, body, "Login broken")
	assert.Contains(t, body, "Cannot login since yesterday")
	assert.Contains(t, body, "web")
}

func TestRenderEventEmail_Urgent(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Timestamp: "2026-01-01T00:00:00Z",
		Feedback: map[string]any{
			"title":     "Data loss",
			"content":   "Records disappeared",
			"source":    "api",
			"is_urgent": true,
		},
	})

	subject, body := renderEventEmail(env)
	assert.Contains(t, subject, "[Urgent]")
	assert.Contains(t, body, "[Urgent] Data loss")
}

func TestRenderEventEmail_EnrichedNested(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Timestamp: "2026-01-01T00:00:00Z",
		Feedback: map[string]any{
			"content": "Something wrong",
			"source":  "api",
			"enriched": map[string]any{
				"title":     "Enriched Title",
				"is_urgent": true,
				"attrs": map[string]any{
					"severity": "critical",
					"category": "bug",
				},
			},
		},
	})

	subject, body := renderEventEmail(env)
	assert.Contains(t, subject, "[Urgent]")
	assert.Contains(t, subject, "Enriched Title")
	assert.Contains(t, body, "Severity: critical")
	assert.Contains(t, body, "Category: bug")
}

func TestRenderEventEmail_DefaultTitle(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Timestamp: "2026-01-01T00:00:00Z",
		Feedback: map[string]any{
			"content": "some content",
			"source":  "web",
		},
	})

	subject, _ := renderEventEmail(env)
	assert.Equal(t, "[Attune] New Feedback", subject)
}

func TestRenderDigestEmail_StructuredView(t *testing.T) {
	t.Parallel()
	view := digestView{
		TenantID: "t1",
		RunDate:  "2026-01-01",
		Result: digestResult{
			Stats: digestStats{Total: 42, Enriched: 30, Urgent: 3},
			Themes: []digestTheme{
				{Title: "Login issues", Count: 10, ExampleTitles: []string{"Cannot login"}},
				{Title: "UI bugs", Count: 5},
			},
		},
	}

	subject, body := renderDigestEmail(view)
	assert.Contains(t, subject, "2026-01-01")
	assert.Contains(t, body, "Total: 42 feedback")
	assert.Contains(t, body, "30 enriched")
	assert.Contains(t, body, "3 urgent")
	assert.Contains(t, body, "Login issues")
	assert.Contains(t, body, "Cannot login")
	assert.Contains(t, body, "UI bugs")
}

func TestRenderDigestEmail_ItemsFallback(t *testing.T) {
	t.Parallel()
	view := digestView{
		TenantID: "t1",
		RunDate:  "2026-01-01",
		Result: digestResult{
			Stats: digestStats{Total: 2},
			Items: []digestItem{
				{ID: 1, Title: "First"},
				{ID: 2, Title: "Second"},
			},
		},
	}

	_, body := renderDigestEmail(view)
	assert.Contains(t, body, "#1 First")
	assert.Contains(t, body, "#2 Second")
}

func TestRenderDigestEmail_UnknownShape(t *testing.T) {
	t.Parallel()
	subject, body := renderDigestEmail(map[string]any{"custom": "data"})
	assert.Equal(t, "[Attune] Daily Digest", subject)
	assert.Contains(t, body, "custom")
}

func TestSendEvent_ConfigError(t *testing.T) {
	t.Parallel()
	ch := ptrext.Of(channel{})
	env := ptrext.Of(outbound.Envelope{
		Feedback: map[string]any{"content": "test", "source": "web"},
	})
	dst := outbound.Target{Config: map[string]any{}}

	err := ch.SendEvent(context.Background(), env, dst)
	require.Error(t, err)
	assert.ErrorIs(t, err, outbound.ErrTerminal)
}

func TestSendDigest_ConfigError(t *testing.T) {
	t.Parallel()
	ch := ptrext.Of(channel{})
	dst := outbound.Target{Config: map[string]any{}}

	err := ch.SendDigest(context.Background(), nil, dst)
	require.Error(t, err)
	assert.ErrorIs(t, err, outbound.ErrTerminal)
}

func TestChannel_ImplementsDirectSenders(t *testing.T) {
	t.Parallel()
	ch := ptrext.Of(channel{})
	var _ outbound.DirectEventSender = ch
	var _ outbound.DirectDigestSender = ch
}

func TestChannel_RegistrationIntegrity(t *testing.T) {
	t.Parallel()
	entries := outbound.Channels()

	var found bool
	for _, e := range entries {
		if e.ID == "email" {
			found = true
			assert.True(t, e.SupportsEvent)
			assert.True(t, e.SupportsDigest)
			break
		}
	}
	assert.True(t, found, "email channel must be registered")
}
