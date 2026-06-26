// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test mock fixtures
package llmrouter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	llmrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
)

// TestMain relaxes the outbound egress policy so httptest servers on loopback
// are reachable. The default (production) policy blocks loopback as SSRF.
func TestMain(m *testing.M) {
	llmclient.SetEgressPolicy(nethardening.Policy{AllowLoopback: true, AllowPrivate: true})
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// test doubles
// ---------------------------------------------------------------------------

type fakeRepo struct {
	candidates []llmrepo.Candidate
	err        error
	// capturePurpose records the purpose arg passed to ResolveCandidates.
	capturePurpose string
}

func (r *fakeRepo) ResolveCandidates(_ context.Context, _ string, purpose string) ([]llmrepo.Candidate, error) {
	r.capturePurpose = purpose
	return r.candidates, r.err
}

type fakeStore struct {
	plaintext []byte
	err       error
	wantAAD   string
}

func (s fakeStore) DecryptValue(_ secretstore.EncryptedValue, aad []byte) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.wantAAD != "" && string(aad) != s.wantAAD {
		return nil, errors.New("unexpected aad")
	}
	if s.plaintext != nil {
		return s.plaintext, nil
	}
	return []byte("sk-test"), nil
}

type fakeClient struct {
	complete func(context.Context, llmclient.CompletionRequest) (llmclient.CompletionResponse, error)
}

func (c fakeClient) Complete(ctx context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	return c.complete(ctx, req)
}

func (c fakeClient) Close() error { return nil }

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func candidate(name string, priority, weight int) llmrepo.Candidate {
	id := uuid.New()
	return llmrepo.Candidate{
		Channel: llmrepo.Channel{
			ID:       id,
			Name:     name,
			Protocol: llmrepo.ProtocolOpenAICompat,
			AuthMode: llmrepo.AuthModeNone,
			Priority: priority,
			Weight:   weight,
		},
		Ability: llmrepo.Ability{
			ChannelID:     id,
			LogicalModel:  "enrich-default",
			ProviderModel: name + "-model",
			Priority:      priority,
			Weight:        weight,
		},
		Route: llmrepo.Route{
			Purpose:      "enrich",
			LogicalModel: "enrich-default",
		},
	}
}

func okFactory(_, _, _ string) (llmclient.LLMClient, error) {
	return fakeClient{complete: func(context.Context, llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
		return llmclient.CompletionResponse{Text: "ok"}, nil
	}}, nil
}

func failFactory(_, _, _ string) (llmclient.LLMClient, error) {
	return nil, errors.New("factory boom")
}

// ---------------------------------------------------------------------------
// Complete tests
// ---------------------------------------------------------------------------

func TestComplete_NilRouter(t *testing.T) {
	t.Parallel()
	var router *Router
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{})
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestComplete_NilRepo(t *testing.T) {
	t.Parallel()
	router := newRouter(nil, fakeStore{}, okFactory)
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{})
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestComplete_NilStore(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{}
	router := newRouter(repo, nil, okFactory)
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{})
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestComplete_ErrNoCandidates(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{err: llmrepo.ErrNoCandidates}
	router := newRouter(repo, fakeStore{}, okFactory)
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestComplete_RepoError(t *testing.T) {
	t.Parallel()
	dbErr := errors.New("db unavailable")
	repo := &fakeRepo{err: dbErr}
	router := newRouter(repo, fakeStore{}, okFactory)
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	require.ErrorIs(t, err, dbErr)
}

func TestComplete_DefaultPurpose(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{
		candidates: []llmrepo.Candidate{candidate("ch", 1, 1)},
	}
	router := newRouter(repo, fakeStore{}, okFactory)
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: ""},
	})
	require.NoError(t, err)
	require.Equal(t, "enrich", repo.capturePurpose)
}

func TestComplete_ExplicitPurpose(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{
		candidates: []llmrepo.Candidate{candidate("ch", 1, 1)},
	}
	router := newRouter(repo, fakeStore{}, okFactory)
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "eval"},
	})
	require.NoError(t, err)
	require.Equal(t, "eval", repo.capturePurpose)
}

func TestComplete_Success(t *testing.T) {
	t.Parallel()
	c := candidate("good", 1, 1)
	repo := &fakeRepo{candidates: []llmrepo.Candidate{c}}
	router := newRouter(repo, fakeStore{}, func(_, _, _ string) (llmclient.LLMClient, error) {
		return fakeClient{complete: func(_ context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
			require.Equal(t, "good-model", req.Model)
			return llmclient.CompletionResponse{Text: "hello"}, nil
		}}, nil
	})
	resp, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	require.NoError(t, err)
	require.Equal(t, "hello", resp.Text)
	require.Equal(t, "good", resp.Route.ChannelName)
	require.Equal(t, "good-model", resp.Route.ProviderModel)
}

func TestComplete_FactoryError(t *testing.T) {
	t.Parallel()
	c := candidate("ch", 1, 1)
	repo := &fakeRepo{candidates: []llmrepo.Candidate{c}}
	router := newRouter(repo, fakeStore{}, failFactory)
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "factory boom")
}

func TestComplete_DecryptError(t *testing.T) {
	t.Parallel()
	c := candidate("ch", 1, 1)
	c.Channel.AuthMode = llmrepo.AuthModeBearer
	repo := &fakeRepo{candidates: []llmrepo.Candidate{c}}
	store := fakeStore{err: errors.New("decrypt failed")}
	router := newRouter(repo, store, okFactory)
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decrypt failed")
}

func TestComplete_AllCandidatesFail(t *testing.T) {
	t.Parallel()
	c1 := candidate("a", 2, 1)
	c2 := candidate("b", 1, 1)
	repo := &fakeRepo{candidates: []llmrepo.Candidate{c1, c2}}
	router := newRouter(repo, fakeStore{}, func(_, _, _ string) (llmclient.LLMClient, error) {
		return fakeClient{complete: func(context.Context, llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
			return llmclient.CompletionResponse{}, errors.New("provider down")
		}}, nil
	})
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "provider down")
}

func TestComplete_EmptyCandidates_ErrNotConfigured(t *testing.T) {
	t.Parallel()
	// ResolveCandidates returns empty slice without error -- the loop body
	// never executes, so lastErr stays nil and the guard sets ErrNotConfigured.
	repo := &fakeRepo{candidates: []llmrepo.Candidate{}}
	router := newRouter(repo, fakeStore{}, okFactory)
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestComplete_FailoverSuccess(t *testing.T) {
	t.Parallel()
	bad := candidate("bad", 2, 1)
	bad.Channel.BaseURL = "bad"
	good := candidate("good", 1, 1)
	good.Channel.BaseURL = "good"
	repo := &fakeRepo{candidates: []llmrepo.Candidate{bad, good}}
	router := newRouter(repo, fakeStore{}, func(_, baseURL, _ string) (llmclient.LLMClient, error) {
		return fakeClient{complete: func(context.Context, llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
			if baseURL == "bad" {
				return llmclient.CompletionResponse{}, errors.New("bad channel")
			}
			return llmclient.CompletionResponse{Text: "ok"}, nil
		}}, nil
	})
	resp, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	require.NoError(t, err)
	require.Equal(t, "ok", resp.Text)
	require.Equal(t, "good", resp.Route.ChannelName)
}

func TestComplete_NilBackendFactory(t *testing.T) {
	t.Parallel()
	// When backendFactory is nil, completeWithCandidate falls back to newBackend.
	// Since the channel uses an unsupported protocol, newBackend returns an error.
	c := candidate("ch", 1, 1)
	c.Channel.Protocol = "unsupported-proto"
	repo := &fakeRepo{candidates: []llmrepo.Candidate{c}}
	router := newRouter(repo, fakeStore{}, nil)
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported protocol")
}

func TestComplete_ChannelTimeout(t *testing.T) {
	t.Parallel()
	c := candidate("timed", 1, 1)
	c.Channel.TimeoutSeconds = 5
	repo := &fakeRepo{candidates: []llmrepo.Candidate{c}}
	sawDeadline := false
	router := newRouter(repo, fakeStore{}, func(_, _, _ string) (llmclient.LLMClient, error) {
		return fakeClient{complete: func(ctx context.Context, _ llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
			deadline, ok := ctx.Deadline()
			sawDeadline = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 5*time.Second
			return llmclient.CompletionResponse{Text: "ok"}, nil
		}}, nil
	})
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	require.NoError(t, err)
	require.True(t, sawDeadline, "expected context deadline from channel timeout")
}

func TestComplete_ZeroTimeout(t *testing.T) {
	t.Parallel()
	c := candidate("notimed", 1, 1)
	c.Channel.TimeoutSeconds = 0
	repo := &fakeRepo{candidates: []llmrepo.Candidate{c}}
	sawNoDeadline := false
	router := newRouter(repo, fakeStore{}, func(_, _, _ string) (llmclient.LLMClient, error) {
		return fakeClient{complete: func(ctx context.Context, _ llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
			_, ok := ctx.Deadline()
			sawNoDeadline = !ok
			return llmclient.CompletionResponse{Text: "ok"}, nil
		}}, nil
	})
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	require.NoError(t, err)
	require.True(t, sawNoDeadline, "expected no deadline when timeout is zero")
}

func TestComplete_RoutesModel(t *testing.T) {
	t.Parallel()
	c := candidate("ch", 1, 1)
	c.Ability.ProviderModel = "gpt-4o-mini"
	repo := &fakeRepo{candidates: []llmrepo.Candidate{c}}
	var gotModel string
	router := newRouter(repo, fakeStore{}, func(_, _, _ string) (llmclient.LLMClient, error) {
		return fakeClient{complete: func(_ context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
			gotModel = req.Model
			return llmclient.CompletionResponse{Text: "ok"}, nil
		}}, nil
	})
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Model: "original-model",
		Guard: llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	require.NoError(t, err)
	require.Equal(t, "gpt-4o-mini", gotModel)
}

// ---------------------------------------------------------------------------
// Embed tests
// ---------------------------------------------------------------------------

func TestEmbed_NilRouter(t *testing.T) {
	t.Parallel()
	var router *Router
	_, err := router.Embed(context.Background(), EmbeddingRequest{TenantID: "t1"})
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestEmbed_NilRepo(t *testing.T) {
	t.Parallel()
	router := newRouter(nil, fakeStore{}, okFactory)
	_, err := router.Embed(context.Background(), EmbeddingRequest{TenantID: "t1"})
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestEmbed_NilStore(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{}
	router := newRouter(repo, nil, okFactory)
	_, err := router.Embed(context.Background(), EmbeddingRequest{TenantID: "t1"})
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestEmbed_ErrNoCandidates(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{err: llmrepo.ErrNoCandidates}
	router := newRouter(repo, fakeStore{}, okFactory)
	_, err := router.Embed(context.Background(), EmbeddingRequest{TenantID: "t1"})
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestEmbed_RepoError(t *testing.T) {
	t.Parallel()
	dbErr := errors.New("db unavailable")
	repo := &fakeRepo{err: dbErr}
	router := newRouter(repo, fakeStore{}, okFactory)
	_, err := router.Embed(context.Background(), EmbeddingRequest{TenantID: "t1"})
	require.ErrorIs(t, err, dbErr)
}

func TestEmbed_AlwaysUsesEmbedPurpose(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{err: llmrepo.ErrNoCandidates}
	router := newRouter(repo, fakeStore{}, okFactory)
	_, _ = router.Embed(context.Background(), EmbeddingRequest{TenantID: "t1"})
	require.Equal(t, "embed", repo.capturePurpose)
}

func TestEmbed_DecryptError(t *testing.T) {
	t.Parallel()
	c := candidate("ch", 1, 1)
	c.Channel.AuthMode = llmrepo.AuthModeBearer
	repo := &fakeRepo{candidates: []llmrepo.Candidate{c}}
	store := fakeStore{err: errors.New("decrypt failed")}
	router := newRouter(repo, store, okFactory)
	_, err := router.Embed(context.Background(), EmbeddingRequest{TenantID: "t1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decrypt failed")
}

func TestEmbed_AllCandidatesFail(t *testing.T) {
	t.Parallel()
	c := candidate("ch", 1, 1)
	c.Channel.AuthMode = llmrepo.AuthModeBearer
	repo := &fakeRepo{candidates: []llmrepo.Candidate{c}}
	store := fakeStore{err: errors.New("bad key")}
	router := newRouter(repo, store, okFactory)
	_, err := router.Embed(context.Background(), EmbeddingRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestEmbed_EmptyCandidates_ErrNotConfigured(t *testing.T) {
	t.Parallel()
	// ResolveCandidates returns empty slice without error -- the loop body
	// never executes, so lastErr stays nil and the guard sets ErrNotConfigured.
	repo := &fakeRepo{candidates: []llmrepo.Candidate{}}
	router := newRouter(repo, fakeStore{}, okFactory)
	_, err := router.Embed(context.Background(), EmbeddingRequest{TenantID: "t1"})
	require.ErrorIs(t, err, ErrNotConfigured)
}

// ---------------------------------------------------------------------------
// Close tests
// ---------------------------------------------------------------------------

func TestClose_ReturnsNil(t *testing.T) {
	t.Parallel()
	router := New(&fakeRepo{}, fakeStore{})
	require.NoError(t, router.Close())
}

// ---------------------------------------------------------------------------
// apiKeyForChannel tests
// ---------------------------------------------------------------------------

func TestAPIKeyForChannel_AuthModeNone(t *testing.T) {
	t.Parallel()
	router := New(&fakeRepo{}, fakeStore{})
	got, err := router.apiKeyForChannel(llmrepo.Channel{
		AuthMode: llmrepo.AuthModeNone,
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestAPIKeyForChannel_BearerSuccess(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	store := fakeStore{
		wantAAD: string(secretstore.AssociatedData("llm_channel", id.String(), "api_key")),
	}
	router := New(&fakeRepo{}, store)
	got, err := router.apiKeyForChannel(llmrepo.Channel{
		ID:                   id,
		AuthMode:             llmrepo.AuthModeBearer,
		CredentialKeyID:      "key-123",
		CredentialCiphertext: []byte("encrypted"),
	})
	require.NoError(t, err)
	require.Equal(t, "sk-test", got)
}

func TestAPIKeyForChannel_DecryptFailure(t *testing.T) {
	t.Parallel()
	store := fakeStore{err: errors.New("keyset unavailable")}
	router := New(&fakeRepo{}, store)
	_, err := router.apiKeyForChannel(llmrepo.Channel{
		ID:       uuid.New(),
		AuthMode: llmrepo.AuthModeBearer,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "keyset unavailable")
}

// ---------------------------------------------------------------------------
// candidateAttempts tests
// ---------------------------------------------------------------------------

func TestCandidateAttempts_Nil(t *testing.T) {
	t.Parallel()
	require.Empty(t, candidateAttempts(nil))
}

func TestCandidateAttempts_SingleCandidate(t *testing.T) {
	t.Parallel()
	c := candidate("only", 1, 1)
	got := candidateAttempts([]llmrepo.Candidate{c})
	require.Len(t, got, 1)
	require.Equal(t, "only", got[0].Channel.Name)
}

func TestCandidateAttempts_HighPriorityFirst(t *testing.T) {
	t.Parallel()
	low := candidate("low", 1, 100)
	high := candidate("high", 2, 1)
	got := candidateAttempts([]llmrepo.Candidate{high, low})
	require.Len(t, got, 2)
	require.Equal(t, "high", got[0].Channel.Name)
}

func TestCandidateAttempts_ThreeGroupsPreserveOrder(t *testing.T) {
	t.Parallel()
	c1 := candidate("first", 3, 1)
	c2 := candidate("second", 2, 1)
	c3 := candidate("third", 1, 1)
	attempts := candidateAttempts([]llmrepo.Candidate{c1, c2, c3})
	require.Len(t, attempts, 3)
	require.Equal(t, "first", attempts[0].Channel.Name)
	require.Equal(t, "second", attempts[1].Channel.Name)
	require.Equal(t, "third", attempts[2].Channel.Name)
}

func TestCandidateAttempts_SamePriorityGroupShuffled(t *testing.T) {
	t.Parallel()
	// Same priority, same weight -- all appear in the output exactly once.
	var cs []llmrepo.Candidate
	for i := range 5 {
		c := candidate("c"+string(rune('A'+i)), 1, 1)
		// Ensure channel and ability priorities match for same group.
		c.Channel.Priority = 1
		c.Ability.Priority = 1
		cs = append(cs, c)
	}
	got := candidateAttempts(cs)
	require.Len(t, got, 5)
	// All names should be present (no duplicates, no drops).
	names := make(map[string]bool)
	for _, c := range got {
		names[c.Channel.Name] = true
	}
	require.Len(t, names, 5)
}

// ---------------------------------------------------------------------------
// candidateWeight tests
// ---------------------------------------------------------------------------

func TestCandidateWeight_Normal(t *testing.T) {
	t.Parallel()
	c := candidate("ch", 1, 3)
	c.Ability.Weight = 4
	c.Channel.Weight = 5
	require.Equal(t, 20, candidateWeight(c))
}

func TestCandidateWeight_NegativeAbilityWeight(t *testing.T) {
	t.Parallel()
	c := candidate("ch", 1, 1)
	c.Ability.Weight = -1
	c.Channel.Weight = 5
	require.Equal(t, 0, candidateWeight(c))
}

func TestCandidateWeight_NegativeChannelWeight(t *testing.T) {
	t.Parallel()
	c := candidate("ch", 1, 1)
	c.Ability.Weight = 5
	c.Channel.Weight = -1
	require.Equal(t, 0, candidateWeight(c))
}

func TestCandidateWeight_BothNegative(t *testing.T) {
	t.Parallel()
	c := candidate("ch", 1, 1)
	c.Ability.Weight = -3
	c.Channel.Weight = -2
	require.Equal(t, 0, candidateWeight(c))
}

func TestCandidateWeight_ZeroAbility(t *testing.T) {
	t.Parallel()
	c := candidate("ch", 1, 1)
	c.Ability.Weight = 0
	c.Channel.Weight = 10
	require.Equal(t, 0, candidateWeight(c))
}

func TestCandidateWeight_ZeroChannel(t *testing.T) {
	t.Parallel()
	c := candidate("ch", 1, 1)
	c.Ability.Weight = 10
	c.Channel.Weight = 0
	require.Equal(t, 0, candidateWeight(c))
}

// ---------------------------------------------------------------------------
// weightedPickIndex tests
// ---------------------------------------------------------------------------

func TestWeightedPickIndex_SingleCandidate(t *testing.T) {
	t.Parallel()
	c := candidate("only", 1, 5)
	require.Equal(t, 0, weightedPickIndex([]llmrepo.Candidate{c}))
}

func TestWeightedPickIndex_ZeroTotalWeight(t *testing.T) {
	t.Parallel()
	// All candidates have zero effective weight -- random fallback.
	c1 := candidate("a", 1, 0)
	c2 := candidate("b", 1, 0)
	idx := weightedPickIndex([]llmrepo.Candidate{c1, c2})
	require.True(t, idx == 0 || idx == 1)
}

func TestWeightedPickIndex_NegativeWeightsFallback(t *testing.T) {
	t.Parallel()
	c1 := candidate("a", 1, 1)
	c1.Ability.Weight = -1
	c2 := candidate("b", 1, 1)
	c2.Ability.Weight = -1
	idx := weightedPickIndex([]llmrepo.Candidate{c1, c2})
	require.True(t, idx == 0 || idx == 1)
}

func TestWeightedPickIndex_HeavilyWeighted(t *testing.T) {
	t.Parallel()
	// One candidate with overwhelming weight should be picked consistently.
	heavy := candidate("heavy", 1, 1000)
	light := candidate("light", 1, 1)
	// Run multiple iterations to raise confidence; the heavy candidate should
	// dominate. We verify it is picked at least once.
	pickedHeavy := false
	for range 20 {
		if weightedPickIndex([]llmrepo.Candidate{heavy, light}) == 0 {
			pickedHeavy = true
			break
		}
	}
	require.True(t, pickedHeavy, "heavy-weight candidate should be picked at least once in 20 iterations")
}

// ---------------------------------------------------------------------------
// splitTopPriorityGroup tests
// ---------------------------------------------------------------------------

func TestSplitTopPriorityGroup_AllSamePriority(t *testing.T) {
	t.Parallel()
	cs := []llmrepo.Candidate{
		candidate("a", 2, 1),
		candidate("b", 2, 1),
		candidate("c", 2, 1),
	}
	top, rest := splitTopPriorityGroup(cs)
	require.Len(t, top, 3)
	require.Empty(t, rest)
}

func TestSplitTopPriorityGroup_TwoGroups(t *testing.T) {
	t.Parallel()
	cs := []llmrepo.Candidate{
		candidate("a", 2, 1),
		candidate("b", 2, 1),
		candidate("c", 1, 1),
	}
	top, rest := splitTopPriorityGroup(cs)
	require.Len(t, top, 2)
	require.Len(t, rest, 1)
	require.Equal(t, "c", rest[0].Channel.Name)
}

func TestSplitTopPriorityGroup_DifferentChannelPriority(t *testing.T) {
	t.Parallel()
	// Same ability priority but different channel priority splits the group.
	c1 := candidate("a", 2, 1)
	c2 := candidate("b", 2, 1)
	c2.Channel.Priority = 1
	cs := []llmrepo.Candidate{c1, c2}
	top, rest := splitTopPriorityGroup(cs)
	require.Len(t, top, 1)
	require.Len(t, rest, 1)
}

// ---------------------------------------------------------------------------
// routeMetadata tests
// ---------------------------------------------------------------------------

func TestRouteMetadata_AllFields(t *testing.T) {
	t.Parallel()
	c := candidate("test-channel", 2, 100)
	c.Channel.Protocol = llmrepo.ProtocolAnthropic
	c.Route.LogicalModel = "eval-model"
	meta := routeMetadata(c)
	require.Equal(t, c.Channel.ID.String(), meta.ChannelID)
	require.Equal(t, "test-channel", meta.ChannelName)
	require.Equal(t, llmrepo.ProtocolAnthropic, meta.Protocol)
	require.Equal(t, "eval-model", meta.LogicalModel)
	require.Equal(t, "test-channel-model", meta.ProviderModel)
}

// ---------------------------------------------------------------------------
// newBackend tests (via unsupported protocol)
// ---------------------------------------------------------------------------

func TestNewBackend_UnsupportedProtocol(t *testing.T) {
	t.Parallel()
	_, err := newBackend("unknown-proto", "http://localhost", "key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported protocol")
}

func TestNewBackend_OpenAICompat(t *testing.T) {
	t.Parallel()
	client, err := newBackend(llmrepo.ProtocolOpenAICompat, "http://localhost:9999", "sk-test")
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NoError(t, client.Close())
}

func TestNewBackend_OpenAIResponses(t *testing.T) {
	t.Parallel()
	client, err := newBackend(llmrepo.ProtocolOpenAIResponses, "http://localhost:9999", "sk-test")
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NoError(t, client.Close())
}

func TestNewBackend_Anthropic(t *testing.T) {
	t.Parallel()
	client, err := newBackend(llmrepo.ProtocolAnthropic, "http://localhost:9999", "sk-test")
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NoError(t, client.Close())
}

func TestNewBackend_Gemini(t *testing.T) {
	t.Parallel()
	client, err := newBackend(llmrepo.ProtocolGemini, "http://localhost:9999", "sk-test")
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NoError(t, client.Close())
}

// ---------------------------------------------------------------------------
// embedWithCandidate tests (via httptest)
// ---------------------------------------------------------------------------

func TestEmbedWithCandidate_Success(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object": "list",
			"data": [{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]}],
			"model": "text-embedding-3-small",
			"usage": {"prompt_tokens":5,"total_tokens":5}
		}`))
	}))
	defer ts.Close()

	c := candidate("embed-ch", 1, 1)
	c.Channel.BaseURL = ts.URL
	router := newRouter(&fakeRepo{}, fakeStore{}, okFactory)
	resp, err := router.embedWithCandidate(context.Background(), c, EmbeddingRequest{
		TenantID: "t1",
		Input:    []string{"hello"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Embeddings, 1)
	require.Equal(t, []float32{0.1, 0.2, 0.3}, resp.Embeddings[0])
	require.Equal(t, int32(5), resp.Usage.PromptTokens)
	require.Equal(t, "embed-ch", resp.Route.ChannelName)
}

func TestEmbedWithCandidate_Timeout(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object": "list",
			"data": [{"object":"embedding","index":0,"embedding":[0.1]}],
			"model": "m",
			"usage": {"prompt_tokens":1,"total_tokens":1}
		}`))
	}))
	defer ts.Close()

	c := candidate("timed-embed", 1, 1)
	c.Channel.BaseURL = ts.URL
	c.Channel.TimeoutSeconds = 30
	router := newRouter(&fakeRepo{}, fakeStore{}, okFactory)
	resp, err := router.embedWithCandidate(context.Background(), c, EmbeddingRequest{
		TenantID: "t1",
		Input:    []string{"test"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Embeddings, 1)
}

func TestEmbedWithCandidate_EmbedError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"provider down"}`))
	}))
	defer ts.Close()

	c := candidate("embed-err", 1, 1)
	c.Channel.BaseURL = ts.URL
	router := newRouter(&fakeRepo{}, fakeStore{}, okFactory)
	_, err := router.embedWithCandidate(context.Background(), c, EmbeddingRequest{
		TenantID: "t1",
		Input:    []string{"hello"},
	})
	require.Error(t, err)
}

func TestEmbed_SuccessEndToEnd(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object": "list",
			"data": [{"object":"embedding","index":0,"embedding":[0.5,0.6]}],
			"model": "text-embedding-3-small",
			"usage": {"prompt_tokens":3,"total_tokens":3}
		}`))
	}))
	defer ts.Close()

	c := candidate("embed-ch", 1, 1)
	c.Channel.BaseURL = ts.URL
	repo := &fakeRepo{candidates: []llmrepo.Candidate{c}}
	router := newRouter(repo, fakeStore{}, okFactory)
	resp, err := router.Embed(context.Background(), EmbeddingRequest{
		TenantID: "t1",
		Input:    []string{"hello"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Embeddings, 1)
	require.Equal(t, []float32{0.5, 0.6}, resp.Embeddings[0])
	require.Equal(t, "embed-ch", resp.Route.ChannelName)
}
