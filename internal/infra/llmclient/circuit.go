// SPDX-License-Identifier: Apache-2.0

package llmclient

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/Phixsura/attune/internal/infra/circuitbreaker"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// CircuitBreakerClient wraps an LLMClient with circuit breaker protection.
// It fast-fails when the upstream provider is unhealthy, preventing
// cascading failures and wasted tokens.
type CircuitBreakerClient struct {
	inner   LLMClient
	breaker *circuitbreaker.Breaker
}

// NewCircuitBreakerClient wraps the given client with circuit breaker protection.
func NewCircuitBreakerClient(inner LLMClient, name string) *CircuitBreakerClient {
	cfg := circuitbreaker.DefaultConfig(name)
	return ptrext.Of(CircuitBreakerClient{
		inner:   inner,
		breaker: circuitbreaker.New(cfg),
	})
}

// NewCircuitBreakerClientWithConfig wraps with custom circuit breaker config.
func NewCircuitBreakerClientWithConfig(inner LLMClient, cfg circuitbreaker.Config) *CircuitBreakerClient {
	return ptrext.Of(CircuitBreakerClient{
		inner:   inner,
		breaker: circuitbreaker.New(cfg),
	})
}

// Complete implements LLMClient.
func (c *CircuitBreakerClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	done, err := c.breaker.Allow()
	if err != nil {
		return CompletionResponse{}, err
	}

	resp, err := c.inner.Complete(ctx, req)
	done(err == nil || !isCircuitBreakerError(err))
	return resp, err
}

// Close implements LLMClient.
func (c *CircuitBreakerClient) Close() error {
	return c.inner.Close()
}

// State returns the current circuit state.
func (c *CircuitBreakerClient) State() circuitbreaker.State {
	return c.breaker.State()
}

// Reset forces the circuit to closed state.
func (c *CircuitBreakerClient) Reset() {
	c.breaker.Reset()
}

// circuitBreakerPatterns are substrings that indicate an infrastructure failure.
var circuitBreakerPatterns = []string{
	"connection refused", "no such host", "network is unreachable",
	"connection reset", "EOF",
	"500", "502", "503", "504", "529",
	"rate limit", "overloaded",
}

// isCircuitBreakerError determines if an error should count toward
// the circuit breaker threshold. We only count infrastructure failures
// (timeouts, connection errors, 5xx), not client errors (4xx, invalid input).
func isCircuitBreakerError(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	errStr := err.Error()
	for _, pattern := range circuitBreakerPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}
