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
