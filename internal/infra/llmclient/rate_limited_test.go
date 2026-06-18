package llmclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestNewRateLimitedClientReturnsOriginalWhenDisabled(t *testing.T) {
	t.Parallel()
	base := fakeLLMClient{}
	got := NewRateLimitedClient(base, 0, 0)
	if got != base {
		t.Fatal("expected disabled limiter to return original client")
	}
}

func TestRateLimitedClientReturnsCancellationSentinel(t *testing.T) {
	t.Parallel()
	client := NewRateLimitedClient(fakeLLMClient{}, 1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Complete(ctx, CompletionRequest{})
	if !errors.Is(err, ErrRateLimitWaitCanceled) {
		t.Fatalf("err = %v, want ErrRateLimitWaitCanceled", err)
	}
}

func TestRateLimitedClientWaitsBeforeDelegating(t *testing.T) {
	t.Parallel()
	base := fakeLLMClient{}
	client := NewRateLimitedClient(base, 1, 1)

	if _, err := client.Complete(context.Background(), CompletionRequest{}); err != nil {
		t.Fatalf("first Complete err = %v", err)
	}

	start := time.Now()
	if _, err := client.Complete(context.Background(), CompletionRequest{}); err != nil {
		t.Fatalf("second Complete err = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("second Complete elapsed = %s, want about 1s wait", elapsed)
	}
}

func TestMutableRateLimitedClientSameConfigDoesNotResetTokens(t *testing.T) {
	t.Parallel()
	client := NewMutableRateLimitedClient(fakeLLMClient{}, RateLimitConfig{
		Enabled: true,
		QPS:     1,
		Burst:   1,
	})

	if _, err := client.Complete(context.Background(), CompletionRequest{}); err != nil {
		t.Fatalf("first Complete err = %v", err)
	}
	if err := client.Configure(RateLimitConfig{
		Enabled: true,
		QPS:     1,
		Burst:   1,
	}); err != nil {
		t.Fatalf("Configure err = %v", err)
	}

	start := time.Now()
	if _, err := client.Complete(context.Background(), CompletionRequest{}); err != nil {
		t.Fatalf("second Complete err = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("second Complete elapsed = %s, want about 1s wait after no-op Configure", elapsed)
	}
}

func TestMutableRateLimitedClientSnapshotAndBurstNormalization(t *testing.T) {
	t.Parallel()

	client := NewMutableRateLimitedClient(fakeLLMClient{}, RateLimitConfig{
		Enabled: true,
		QPS:     0.4,
		Burst:   0,
	})

	snap := client.Snapshot()
	if !snap.Enabled {
		t.Fatal("expected limiter snapshot enabled")
	}
	if snap.QPS != 0.4 {
		t.Fatalf("snapshot qps = %v, want 0.4", snap.QPS)
	}
	if snap.Burst != 1 {
		t.Fatalf("snapshot burst = %d, want normalized 1", snap.Burst)
	}
}

func TestMutableRateLimitedClientCloseDelegatesAndNilIsSafe(t *testing.T) {
	t.Parallel()

	base := ptrext.Of(closingLLMClient{})
	client := NewMutableRateLimitedClient(base, RateLimitConfig{
		Enabled: true,
		QPS:     2,
		Burst:   2,
	})

	if err := client.Close(); err != nil {
		t.Fatalf("Close err = %v", err)
	}
	if base.closed != 1 {
		t.Fatalf("close count = %d, want 1", base.closed)
	}

	var nilClient *MutableRateLimitedClient
	if err := nilClient.Close(); err != nil {
		t.Fatalf("nil Close err = %v", err)
	}
	if err := nilClient.Configure(RateLimitConfig{}); err != nil {
		t.Fatalf("nil Configure err = %v", err)
	}
}

type fakeLLMClient struct{}

func (fakeLLMClient) Complete(context.Context, CompletionRequest) (CompletionResponse, error) {
	return CompletionResponse{}, nil
}

func (fakeLLMClient) Close() error { return nil }

type closingLLMClient struct{ closed int }

func (c *closingLLMClient) Complete(context.Context, CompletionRequest) (CompletionResponse, error) {
	return CompletionResponse{}, nil
}

func (c *closingLLMClient) Close() error {
	c.closed++
	return nil
}
