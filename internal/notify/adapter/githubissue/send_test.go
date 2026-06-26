// SPDX-License-Identifier: Apache-2.0

package githubissue

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/notify"
)

func validEnvelopeJSON() []byte {
	env := attuneEnvelope{
		Version:   "1",
		EventType: "feedback.enriched",
		TraceID:   "trace-1",
		Feedback: attuneFeedback{
			ID:       42,
			TenantID: "t1",
			Content:  "Great product!",
			Source:   "api",
			UserID:   "u1",
			Enriched: attuneEnriched{
				Title:     "Positive feedback",
				Rationale: "User praised the product",
			},
		},
	}
	b, _ := json.Marshal(env)
	return b
}

func TestCheckGitHubResponse_Created(t *testing.T) {
	t.Parallel()
	env := attuneEnvelope{
		Feedback: attuneFeedback{ID: 1, Enriched: attuneEnriched{Title: "t"}},
	}
	checker := checkGitHubResponse("test-label", env)
	body := []byte(`{"number":99,"html_url":"https://github.com/foo/bar/issues/99"}`)
	err := checker(t.Context(), http.StatusCreated, body)
	assert.NoError(t, err)
}

func TestCheckGitHubResponse_TooManyRequests(t *testing.T) {
	t.Parallel()
	env := attuneEnvelope{
		Feedback: attuneFeedback{ID: 2, Enriched: attuneEnriched{Title: "t"}},
	}
	checker := checkGitHubResponse("test-label", env)
	err := checker(t.Context(), http.StatusTooManyRequests, []byte("rate limited"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, notify.ErrTerminal)
}

func TestCheckGitHubResponse_RequestTimeout(t *testing.T) {
	t.Parallel()
	env := attuneEnvelope{
		Feedback: attuneFeedback{ID: 3, Enriched: attuneEnriched{Title: "t"}},
	}
	checker := checkGitHubResponse("test-label", env)
	err := checker(t.Context(), http.StatusRequestTimeout, []byte("timeout"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, notify.ErrTerminal)
}

func TestCheckGitHubResponse_ClientError(t *testing.T) {
	t.Parallel()
	env := attuneEnvelope{
		Feedback: attuneFeedback{ID: 4, Enriched: attuneEnriched{Title: "t"}},
	}
	checker := checkGitHubResponse("test-label", env)
	err := checker(t.Context(), http.StatusForbidden, []byte("forbidden"))
	require.Error(t, err)
	assert.ErrorIs(t, err, notify.ErrTerminal)
}

func TestCheckGitHubResponse_ServerError(t *testing.T) {
	t.Parallel()
	env := attuneEnvelope{
		Feedback: attuneFeedback{ID: 5, Enriched: attuneEnriched{Title: "t"}},
	}
	checker := checkGitHubResponse("test-label", env)
	err := checker(t.Context(), http.StatusInternalServerError, []byte("server error"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, notify.ErrTerminal)
}

// TestSendGitHubIssue groups tests that modify githubAPIBaseForTest.
// Not parallel at the top level because they share the package var.
func TestSendGitHubIssue(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":1,"html_url":"https://github.com/o/r/issues/1"}`))
		}))
		defer srv.Close()

		old := githubAPIBaseForTest
		githubAPIBaseForTest = srv.URL
		defer func() { githubAPIBaseForTest = old }()

		transport := notify.NewTransport(srv.Client(), notify.NoRetry())
		err := SendGitHubIssue(t.Context(), transport, "https://github.com/owner/repo", "ghp_token", validEnvelopeJSON())
		assert.NoError(t, err)
	})

	t.Run("bad payload", func(t *testing.T) {
		transport := notify.NewTransport(nil, notify.NoRetry())
		err := SendGitHubIssue(t.Context(), transport, "https://github.com/owner/repo", "token", []byte("not json"))
		require.Error(t, err)
		assert.ErrorIs(t, err, notify.ErrTerminal)
	})

	t.Run("bad repo URL", func(t *testing.T) {
		transport := notify.NewTransport(nil, notify.NoRetry())
		err := SendGitHubIssue(t.Context(), transport, "https://notgithub.com/x/y", "token", validEnvelopeJSON())
		require.Error(t, err)
		assert.ErrorIs(t, err, notify.ErrTerminal)
	})

	t.Run("server error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal server error"))
		}))
		defer srv.Close()

		old := githubAPIBaseForTest
		githubAPIBaseForTest = srv.URL
		defer func() { githubAPIBaseForTest = old }()

		transport := notify.NewTransport(srv.Client(), notify.NoRetry())
		err := SendGitHubIssue(t.Context(), transport, "https://github.com/owner/repo", "ghp_token", validEnvelopeJSON())
		require.Error(t, err)
	})

	t.Run("client error terminal", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"validation failed"}`))
		}))
		defer srv.Close()

		old := githubAPIBaseForTest
		githubAPIBaseForTest = srv.URL
		defer func() { githubAPIBaseForTest = old }()

		transport := notify.NewTransport(srv.Client(), notify.NoRetry())
		err := SendGitHubIssue(t.Context(), transport, "https://github.com/owner/repo", "ghp_token", validEnvelopeJSON())
		require.Error(t, err)
		assert.ErrorIs(t, err, notify.ErrTerminal)
	})
}
