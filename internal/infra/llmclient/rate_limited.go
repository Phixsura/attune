package llmclient

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ErrRateLimitWaitCanceled indicates the request never reached a provider
// because it was canceled while waiting for a local rate-limit slot.
var ErrRateLimitWaitCanceled = errors.New("llm rate-limit wait canceled")

type RateLimitConfig struct {
	Enabled bool
	QPS     float64
	Burst   int
}

type RateLimitSnapshot struct {
	Enabled bool
	QPS     float64
	Burst   int
}

type limiterState struct {
	enabled bool
	qps     float64
	burst   int
	limiter *rate.Limiter
}

type MutableRateLimitedClient struct {
	next LLMClient
	mu   sync.RWMutex
	cur  limiterState
}

// NewMutableRateLimitedClient wraps next with a mutable local QPS limiter.
func NewMutableRateLimitedClient(next LLMClient, cfg RateLimitConfig) *MutableRateLimitedClient {
	c := ptrext.Of(MutableRateLimitedClient{next: next})
	_ = c.Configure(cfg)
	return c
}

// NewRateLimitedClient preserves the previous one-shot API.
func NewRateLimitedClient(next LLMClient, qps float64, burst int) LLMClient {
	if next == nil || qps <= 0 {
		return next
	}
	return NewMutableRateLimitedClient(next, RateLimitConfig{
		Enabled: true,
		QPS:     qps,
		Burst:   burst,
	})
}

func (c *MutableRateLimitedClient) Configure(cfg RateLimitConfig) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	state := limiterState{
		enabled: cfg.Enabled && cfg.QPS > 0,
		qps:     cfg.QPS,
		burst:   cfg.Burst,
	}
	if state.enabled {
		if state.burst <= 0 {
			state.burst = int(math.Ceil(cfg.QPS))
		}
		if state.burst < 1 {
			state.burst = 1
		}
		state.limiter = rate.NewLimiter(rate.Limit(cfg.QPS), state.burst)
	}
	if sameLimiterConfig(c.cur, state) {
		return nil
	}
	c.cur = state
	return nil
}

func (c *MutableRateLimitedClient) Snapshot() RateLimitSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return RateLimitSnapshot{
		Enabled: c.cur.enabled,
		QPS:     c.cur.qps,
		Burst:   c.cur.burst,
	}
}

func (c *MutableRateLimitedClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if c == nil || c.next == nil {
		return CompletionResponse{}, nil
	}
	c.mu.RLock()
	state := c.cur
	c.mu.RUnlock()
	if !state.enabled || state.limiter == nil {
		return c.next.Complete(ctx, req)
	}
	start := time.Now()
	if err := state.limiter.Wait(ctx); err != nil {
		metrics.LLMRateLimitWaitSeconds.Observe(time.Since(start).Seconds())
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CompletionResponse{}, fmt.Errorf("%w: %w", ErrRateLimitWaitCanceled, err)
		}
		return CompletionResponse{}, fmt.Errorf("llm rate-limit wait: %w", err)
	}
	metrics.LLMRateLimitWaitSeconds.Observe(time.Since(start).Seconds())
	return c.next.Complete(ctx, req)
}

func (c *MutableRateLimitedClient) Close() error {
	if c == nil || c.next == nil {
		return nil
	}
	return c.next.Close()
}

func sameLimiterConfig(a, b limiterState) bool {
	return a.enabled == b.enabled && a.qps == b.qps && a.burst == b.burst
}

var _ LLMClient = ptrext.Of(MutableRateLimitedClient{})
