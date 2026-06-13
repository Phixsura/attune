// SPDX-License-Identifier: Apache-2.0

// Package llmrouter resolves DB-managed LLM routes into concrete provider
// clients.
package llmrouter

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	llmrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
)

var ErrNotConfigured = errors.New("llm_not_configured")

type Repo interface {
	ResolveCandidates(ctx context.Context, tenantID, purpose string) ([]llmrepo.Candidate, error)
}

type Store interface {
	DecryptValue(value secretstore.EncryptedValue, aad []byte) ([]byte, error)
}

type backendFactory func(protocol, baseURL, apiKey string) (llmclient.LLMClient, error)

type Router struct {
	repo           Repo
	store          Store
	backendFactory backendFactory
}

func New(repo Repo, store Store) *Router {
	return newRouter(repo, store, newBackend)
}

func newRouter(repo Repo, store Store, factory backendFactory) *Router {
	return ptrext.Of(Router{repo: repo, store: store, backendFactory: factory})
}

func (r *Router) Complete(
	ctx context.Context,
	req llmclient.CompletionRequest,
) (llmclient.CompletionResponse, error) {
	if r == nil || r.repo == nil || r.store == nil {
		return llmclient.CompletionResponse{}, ErrNotConfigured
	}
	purpose := req.Guard.Purpose
	if purpose == "" {
		purpose = "enrich"
	}
	candidates, err := r.repo.ResolveCandidates(ctx, req.Guard.TenantID, purpose)
	if err != nil {
		if errors.Is(err, llmrepo.ErrNoCandidates) {
			return llmclient.CompletionResponse{}, ErrNotConfigured
		}
		return llmclient.CompletionResponse{}, err
	}
	var lastResp llmclient.CompletionResponse
	var lastErr error
	for _, candidate := range candidateAttempts(candidates) {
		resp, err := r.completeWithCandidate(ctx, candidate, req)
		if err == nil {
			return resp, nil
		}
		lastResp = resp
		lastErr = err
	}
	if lastErr == nil {
		lastErr = ErrNotConfigured
	}
	return lastResp, lastErr
}

func (r *Router) Close() error {
	return nil
}

// EmbeddingRequest carries the parameters for embedding generation.
type EmbeddingRequest struct {
	TenantID   string
	Input      []string
	Dimensions int
	UserID     string
}

// EmbeddingResponse is the routed embedding result.
type EmbeddingResponse struct {
	Embeddings [][]float32
	Usage      llmclient.EmbeddingUsage
	Route      llmclient.RouteMetadata
}

// Embed routes an embedding request to the appropriate provider based on
// llm_routes with purpose='embed'.
func (r *Router) Embed(
	ctx context.Context,
	req EmbeddingRequest,
) (EmbeddingResponse, error) {
	if r == nil || r.repo == nil || r.store == nil {
		return EmbeddingResponse{}, ErrNotConfigured
	}
	candidates, err := r.repo.ResolveCandidates(ctx, req.TenantID, "embed")
	if err != nil {
		if errors.Is(err, llmrepo.ErrNoCandidates) {
			return EmbeddingResponse{}, ErrNotConfigured
		}
		return EmbeddingResponse{}, err
	}
	var lastResp EmbeddingResponse
	var lastErr error
	for _, candidate := range candidateAttempts(candidates) {
		resp, err := r.embedWithCandidate(ctx, candidate, req)
		if err == nil {
			return resp, nil
		}
		lastResp = resp
		lastErr = err
	}
	if lastErr == nil {
		lastErr = ErrNotConfigured
	}
	return lastResp, lastErr
}

func (r *Router) embedWithCandidate(
	ctx context.Context,
	c llmrepo.Candidate,
	req EmbeddingRequest,
) (EmbeddingResponse, error) {
	resp := EmbeddingResponse{Route: routeMetadata(c)}
	apiKey, err := r.apiKeyForChannel(c.Channel)
	if err != nil {
		return resp, err
	}
	backend := llmclient.NewOpenAIEmbeddingClient(c.Channel.BaseURL, apiKey)
	defer backend.Close()
	callCtx := ctx
	cancel := func() {}
	if c.Channel.TimeoutSeconds > 0 {
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(c.Channel.TimeoutSeconds)*time.Second)
	}
	defer cancel()
	embResp, err := backend.Embed(callCtx, llmclient.EmbeddingRequest{
		Model:      c.Ability.ProviderModel,
		Input:      req.Input,
		Dimensions: req.Dimensions,
		UserID:     req.UserID,
	})
	if err != nil {
		return resp, err
	}
	resp.Embeddings = embResp.Embeddings
	resp.Usage = embResp.Usage
	return resp, nil
}

func (r *Router) completeWithCandidate(
	ctx context.Context,
	c llmrepo.Candidate,
	req llmclient.CompletionRequest,
) (llmclient.CompletionResponse, error) {
	resp := llmclient.CompletionResponse{Route: routeMetadata(c)}
	apiKey, err := r.apiKeyForChannel(c.Channel)
	if err != nil {
		return resp, err
	}
	factory := r.backendFactory
	if factory == nil {
		factory = newBackend
	}
	backend, err := factory(c.Channel.Protocol, c.Channel.BaseURL, apiKey)
	if err != nil {
		return resp, err
	}
	defer backend.Close()
	routed := req
	routed.Model = c.Ability.ProviderModel
	callCtx := ctx
	cancel := func() {}
	if c.Channel.TimeoutSeconds > 0 {
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(c.Channel.TimeoutSeconds)*time.Second)
	}
	defer cancel()
	resp, err = backend.Complete(callCtx, routed)
	resp.Route = routeMetadata(c)
	return resp, err
}

func (r *Router) apiKeyForChannel(ch llmrepo.Channel) (string, error) {
	if ch.AuthMode == llmrepo.AuthModeNone {
		return "", nil
	}
	plaintext, err := r.store.DecryptValue(secretstore.EncryptedValue{
		KeyID:      ch.CredentialKeyID,
		Ciphertext: ch.CredentialCiphertext,
	}, secretstore.AssociatedData("llm_channel", ch.ID.String(), "api_key"))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func newBackend(protocol, baseURL, apiKey string) (llmclient.LLMClient, error) {
	switch protocol {
	case llmrepo.ProtocolOpenAIResponses:
		return llmclient.NewOpenAIResponses(baseURL, apiKey)
	case llmrepo.ProtocolAnthropic:
		return llmclient.NewAnthropic(baseURL, apiKey)
	case llmrepo.ProtocolGemini:
		return llmclient.NewGemini(baseURL, apiKey)
	case llmrepo.ProtocolOpenAICompat:
		return llmclient.NewOpenAICompat(baseURL, apiKey)
	default:
		return nil, fmt.Errorf("llmrouter: unsupported protocol %q", protocol)
	}
}

func candidateAttempts(candidates []llmrepo.Candidate) []llmrepo.Candidate {
	if len(candidates) <= 1 {
		return candidates
	}
	out := make([]llmrepo.Candidate, 0, len(candidates))
	for len(candidates) > 0 {
		group, rest := splitTopPriorityGroup(candidates)
		for len(group) > 0 {
			idx := weightedPickIndex(group)
			out = append(out, group[idx])
			group = append(group[:idx], group[idx+1:]...)
		}
		candidates = rest
	}
	return out
}

func splitTopPriorityGroup(candidates []llmrepo.Candidate) ([]llmrepo.Candidate, []llmrepo.Candidate) {
	topAbility := candidates[0].Ability.Priority
	topChannel := candidates[0].Channel.Priority
	end := 0
	for _, c := range candidates {
		if c.Ability.Priority != topAbility || c.Channel.Priority != topChannel {
			break
		}
		end++
	}
	return candidates[:end], candidates[end:]
}

func weightedPickIndex(candidates []llmrepo.Candidate) int {
	total := 0
	for _, c := range candidates {
		total += candidateWeight(c)
	}
	if total <= 0 {
		return rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(candidates))
	}
	n := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(total)
	for i, c := range candidates {
		n -= candidateWeight(c)
		if n < 0 {
			return i
		}
	}
	return 0
}

func candidateWeight(c llmrepo.Candidate) int {
	abilityWeight := c.Ability.Weight
	channelWeight := c.Channel.Weight
	if abilityWeight < 0 {
		abilityWeight = 0
	}
	if channelWeight < 0 {
		channelWeight = 0
	}
	return abilityWeight * channelWeight
}

func routeMetadata(c llmrepo.Candidate) llmclient.RouteMetadata {
	return llmclient.RouteMetadata{
		ChannelID:     c.Channel.ID.String(),
		ChannelName:   c.Channel.Name,
		Protocol:      c.Channel.Protocol,
		LogicalModel:  c.Route.LogicalModel,
		ProviderModel: c.Ability.ProviderModel,
	}
}
