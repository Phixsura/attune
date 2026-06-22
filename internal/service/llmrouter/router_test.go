// SPDX-License-Identifier: Apache-2.0

package llmrouter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	llmrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
)

type fakeRepo struct {
	candidates []llmrepo.Candidate
	err        error
}

func (r fakeRepo) ResolveCandidates(context.Context, string, string) ([]llmrepo.Candidate, error) {
	return r.candidates, r.err
}

type fakeStore struct {
	wantAAD string
}

func (s fakeStore) DecryptValue(_ secretstore.EncryptedValue, aad []byte) ([]byte, error) {
	if string(aad) != s.wantAAD {
		return nil, errors.New("unexpected aad")
	}
	return []byte("sk-test"), nil
}

func TestRouterNotConfigured(t *testing.T) {
	router := New(fakeRepo{err: llmrepo.ErrNoCandidates}, fakeStore{})
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "tenant-1", Purpose: "enrich"},
	})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v; want ErrNotConfigured", err)
	}
}

func TestAPIKeyForChannelUsesChannelAAD(t *testing.T) {
	id := uuid.New()
	router := New(fakeRepo{}, fakeStore{
		wantAAD: string(secretstore.AssociatedData("llm_channel", id.String(), "api_key")),
	})
	got, err := router.apiKeyForChannel(llmrepo.Channel{
		ID:                   id,
		AuthMode:             llmrepo.AuthModeBearer,
		CredentialKeyID:      "123",
		CredentialCiphertext: []byte("ciphertext"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk-test" {
		t.Fatalf("api key = %q", got)
	}
}

func TestSelectCandidateKeepsTopPriorityGroup(t *testing.T) {
	low := candidate("low", 1, 100)
	high := candidate("high", 2, 1)
	got := candidateAttempts([]llmrepo.Candidate{high, low})
	if len(got) != 2 {
		t.Fatalf("attempts = %#v; want 2", got)
	}
	if got[0].Channel.Name != "high" {
		t.Fatalf("first attempt %q; want high", got[0].Channel.Name)
	}
}

func TestRouterFailsOverToLowerPriorityCandidate(t *testing.T) {
	bad := candidate("bad", 2, 1)
	bad.Channel.BaseURL = "bad"
	bad.Ability.ProviderModel = "bad-model"
	good := candidate("good", 1, 1)
	good.Channel.BaseURL = "good"
	good.Ability.ProviderModel = "good-model"
	router := newRouter(fakeRepo{candidates: []llmrepo.Candidate{bad, good}}, fakeStore{}, func(_, baseURL, _ string) (llmclient.LLMClient, error) {
		return fakeClient{complete: func(context.Context, llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
			if baseURL == "bad" {
				return llmclient.CompletionResponse{}, errors.New("bad channel")
			}
			return llmclient.CompletionResponse{Text: "ok"}, nil
		}}, nil
	})

	resp, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "tenant-1", Purpose: "enrich"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "ok" || resp.Route.ChannelName != "good" || resp.Route.ProviderModel != "good-model" {
		t.Fatalf("response = %#v; want good failover route", resp)
	}
}

func TestRouterAppliesChannelTimeout(t *testing.T) {
	row := candidate("timed", 1, 1)
	row.Channel.TimeoutSeconds = 7
	sawDeadline := false
	router := newRouter(fakeRepo{candidates: []llmrepo.Candidate{row}}, fakeStore{}, func(_, _, _ string) (llmclient.LLMClient, error) {
		return fakeClient{complete: func(ctx context.Context, _ llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
			deadline, ok := ctx.Deadline()
			sawDeadline = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 7*time.Second
			return llmclient.CompletionResponse{Text: "ok"}, nil
		}}, nil
	})

	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "tenant-1", Purpose: "enrich"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawDeadline {
		t.Fatal("expected provider call context to carry channel timeout deadline")
	}
}

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

type fakeClient struct {
	complete func(context.Context, llmclient.CompletionRequest) (llmclient.CompletionResponse, error)
}

func (c fakeClient) Complete(ctx context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	return c.complete(ctx, req)
}

func (c fakeClient) Close() error { return nil }

func TestRouter_NilRouter(t *testing.T) {
	var router *Router
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v; want ErrNotConfigured", err)
	}
}

func TestRouter_Close(t *testing.T) {
	router := New(fakeRepo{}, fakeStore{})
	if err := router.Close(); err != nil {
		t.Fatalf("Close() = %v; want nil", err)
	}
}

func TestRouter_RepoError(t *testing.T) {
	router := New(fakeRepo{err: errors.New("db error")}, fakeStore{})
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "tenant-1", Purpose: "enrich"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "db error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRouter_EmptyPurpose(t *testing.T) {
	router := New(fakeRepo{
		candidates: []llmrepo.Candidate{},
		err:        llmrepo.ErrNoCandidates,
	}, fakeStore{})

	// Test that empty purpose still works (defaults to "enrich" internally)
	_, err := router.Complete(context.Background(), llmclient.CompletionRequest{
		Guard: llmclient.GuardMetadata{TenantID: "tenant-1", Purpose: ""},
	})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v; want ErrNotConfigured", err)
	}
}

func TestRouteMetadata(t *testing.T) {
	c := candidate("test-channel", 2, 100)
	meta := routeMetadata(c)
	if meta.ChannelName != "test-channel" {
		t.Errorf("ChannelName = %q; want test-channel", meta.ChannelName)
	}
	if meta.ProviderModel != "test-channel-model" {
		t.Errorf("ProviderModel = %q; want test-channel-model", meta.ProviderModel)
	}
}

func TestCandidateAttempts_PreservesInputOrder(t *testing.T) {
	// candidateAttempts assumes DB has pre-sorted by priority desc; it groups
	// same-priority candidates together, so we pass in sorted order.
	c1 := candidate("first", 3, 1)
	c2 := candidate("second", 2, 1)
	c3 := candidate("third", 1, 1)

	attempts := candidateAttempts([]llmrepo.Candidate{c1, c2, c3})

	if len(attempts) != 3 {
		t.Fatalf("got %d attempts, want 3", len(attempts))
	}
	if attempts[0].Channel.Name != "first" {
		t.Errorf("first attempt %q; want first", attempts[0].Channel.Name)
	}
}

func TestCandidateAttempts_EmptySlice(t *testing.T) {
	attempts := candidateAttempts(nil)
	if len(attempts) != 0 {
		t.Fatalf("got %d attempts, want 0", len(attempts))
	}
}
