// SPDX-License-Identifier: Apache-2.0

package llmclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/circuitbreaker"
)

type mockLLMClient struct {
	responses []CompletionResponse
	errs      []error
	calls     int
}

func newMockLLMClient(responses []CompletionResponse, errs []error) *mockLLMClient {
	return &mockLLMClient{responses: responses, errs: errs} // ptrext:allow test-fixture
}

func (m *mockLLMClient) Complete(_ context.Context, _ CompletionRequest) (CompletionResponse, error) {
	idx := m.calls
	m.calls++
	if idx < len(m.errs) && m.errs[idx] != nil {
		return CompletionResponse{}, m.errs[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return CompletionResponse{Text: "ok"}, nil
}

func (m *mockLLMClient) Close() error {
	return nil
}

func TestCircuitBreakerClient_NormalOperation(t *testing.T) {
	t.Parallel()
	mock := newMockLLMClient([]CompletionResponse{{Text: "hello"}}, nil)
	client := NewCircuitBreakerClient(mock, "test")

	resp, err := client.Complete(context.Background(), CompletionRequest{})
	require.NoError(t, err)
	require.Equal(t, "hello", resp.Text)
	require.Equal(t, circuitbreaker.StateClosed, client.State())
}

func TestCircuitBreakerClient_OpensOnFailures(t *testing.T) {
	t.Parallel()
	mock := newMockLLMClient(nil, []error{
		errors.New("connection refused"),
		errors.New("connection refused"),
		errors.New("connection refused"),
		errors.New("connection refused"),
		errors.New("connection refused"),
	})

	cfg := circuitbreaker.Config{
		Name:             "test",
		FailureThreshold: 5,
		OpenDuration:     10 * time.Second,
	}
	client := NewCircuitBreakerClientWithConfig(mock, cfg)

	for i := 0; i < 5; i++ {
		_, err := client.Complete(context.Background(), CompletionRequest{})
		require.Error(t, err)
	}

	require.Equal(t, circuitbreaker.StateOpen, client.State())

	_, err := client.Complete(context.Background(), CompletionRequest{})
	require.ErrorIs(t, err, circuitbreaker.ErrCircuitOpen)
}

func TestCircuitBreakerClient_ClientErrorsDoNotTripBreaker(t *testing.T) {
	t.Parallel()
	mock := newMockLLMClient(nil, []error{
		errors.New("400 bad request"),
		errors.New("401 unauthorized"),
		errors.New("invalid input"),
		errors.New("model not found"),
		errors.New("context length exceeded"),
	})

	cfg := circuitbreaker.Config{
		Name:             "test",
		FailureThreshold: 3,
	}
	client := NewCircuitBreakerClientWithConfig(mock, cfg)

	for i := 0; i < 5; i++ {
		_, err := client.Complete(context.Background(), CompletionRequest{})
		require.Error(t, err)
	}

	require.Equal(t, circuitbreaker.StateClosed, client.State())
}

func TestCircuitBreakerClient_Reset(t *testing.T) {
	t.Parallel()
	mock := newMockLLMClient(nil, []error{
		errors.New("503 service unavailable"),
		errors.New("503 service unavailable"),
		errors.New("503 service unavailable"),
	})

	cfg := circuitbreaker.Config{
		Name:             "test",
		FailureThreshold: 3,
		OpenDuration:     1 * time.Hour,
	}
	client := NewCircuitBreakerClientWithConfig(mock, cfg)

	for i := 0; i < 3; i++ {
		_, _ = client.Complete(context.Background(), CompletionRequest{})
	}
	require.Equal(t, circuitbreaker.StateOpen, client.State())

	client.Reset()
	require.Equal(t, circuitbreaker.StateClosed, client.State())
}

func TestIsCircuitBreakerError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"connection refused", errors.New("connection refused"), true},
		{"no such host", errors.New("no such host"), true},
		{"503 service unavailable", errors.New("503 service unavailable"), true},
		{"500 internal server error", errors.New("500 internal server error"), true},
		{"rate limit exceeded", errors.New("rate limit exceeded"), true},
		{"400 bad request", errors.New("400 bad request"), false},
		{"401 unauthorized", errors.New("401 unauthorized"), false},
		{"invalid model", errors.New("invalid model"), false},
		{"content policy violation", errors.New("content policy violation"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, isCircuitBreakerError(tt.err))
		})
	}
}
