package notify

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// fastRetry shaves the backoff timers down so tests don't sleep for
// real seconds. Same MaxAttempts as DefaultRetry to keep behaviors
// otherwise comparable.
func fastRetry(maxAttempts int) RetryPolicy {
	return RetryPolicy{
		MaxAttempts: maxAttempts,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    10 * time.Millisecond,
	}
}

func TestTransport_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := NewTransport(nil, fastRetry(3))
	build := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
	}
	check := func(_ context.Context, status int, body []byte) error {
		if status == http.StatusOK {
			return nil
		}
		return errors.New("unexpected")
	}
	if err := tr.Send(context.Background(), "test", build, check); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestTransport_RetryThenSuccess(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := NewTransport(nil, fastRetry(5))
	build := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
	}
	check := func(_ context.Context, status int, body []byte) error {
		if status >= 200 && status < 300 {
			return nil
		}
		return errors.New("not 2xx")
	}
	if err := tr.Send(context.Background(), "retry", build, check); err != nil {
		t.Fatalf("want nil after 3 attempts, got %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("want 3 attempts (2 fails + 1 success), got %d", attempts.Load())
	}
}

func TestTransport_NilSleepUsesTimerFallback(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := NewTransport(srv.Client(), RetryPolicy{MaxAttempts: 2, BaseDelay: 0})
	tr.sleep = nil
	build := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
	}
	check := func(_ context.Context, status int, body []byte) error {
		if status >= 200 && status < 300 {
			return nil
		}
		return errors.New("retry-me")
	}

	if err := tr.Send(context.Background(), "nil-sleep", build, check); err != nil {
		t.Fatalf("want nil after retry, got %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("want 2 attempts, got %d", attempts.Load())
	}
}

func TestTransport_UsesRetryAfterWhenRetryable(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := NewTransport(nil, RetryPolicy{
		MaxAttempts: 2,
		BaseDelay:   time.Minute,
		MaxDelay:    1500 * time.Millisecond,
	})
	var delays []time.Duration
	tr.sleep = func(ctx context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}
	build := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
	}
	check := func(_ context.Context, status int, body []byte) error {
		if status >= 200 && status < 300 {
			return nil
		}
		return errors.New("retry-me")
	}

	if err := tr.Send(context.Background(), "retry-after", build, check); err != nil {
		t.Fatalf("want nil after retry, got %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("want 2 attempts, got %d", attempts.Load())
	}
	if len(delays) != 1 || delays[0] != 1500*time.Millisecond {
		t.Fatalf("delays = %v, want [1.5s] from clamped Retry-After", delays)
	}
}

func TestTransport_RecordsOutboundDeliveryMetrics(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	retryable := metrics.OutboundDeliveryAttemptsTotal.WithLabelValues("slack", "retryable", "429")
	success := metrics.OutboundDeliveryAttemptsTotal.WithLabelValues("slack", "success", "200")
	retryAfter := metrics.OutboundRetryAfterTotal.WithLabelValues("slack")
	duration := metrics.OutboundDeliveryDuration.WithLabelValues("slack", "success")
	beforeRetryable := counterValue(t, retryable)
	beforeSuccess := counterValue(t, success)
	beforeRetryAfter := counterValue(t, retryAfter)
	beforeDuration := histogramCount(t, duration)

	tr := NewTransport(srv.Client(), RetryPolicy{
		MaxAttempts: 2,
		BaseDelay:   time.Minute,
		MaxDelay:    5 * time.Second,
	})
	tr.sleep = func(ctx context.Context, delay time.Duration) error {
		if delay != 2*time.Second {
			t.Fatalf("delay = %v, want Retry-After 2s", delay)
		}
		return nil
	}
	build := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
	}
	check := func(_ context.Context, status int, body []byte) error {
		if status >= 200 && status < 300 {
			return nil
		}
		return errors.New("retry-me")
	}

	if err := tr.Send(context.Background(), "digest-slack-tenant", build, check); err != nil {
		t.Fatalf("want nil after retry, got %v", err)
	}
	if got := counterValue(t, retryable); got != beforeRetryable+1 {
		t.Fatalf("retryable attempts = %v, want %v", got, beforeRetryable+1)
	}
	if got := counterValue(t, success); got != beforeSuccess+1 {
		t.Fatalf("success attempts = %v, want %v", got, beforeSuccess+1)
	}
	if got := counterValue(t, retryAfter); got != beforeRetryAfter+1 {
		t.Fatalf("retry-after total = %v, want %v", got, beforeRetryAfter+1)
	}
	if got := histogramCount(t, duration); got != beforeDuration+1 {
		t.Fatalf("success duration count = %d, want %d", got, beforeDuration+1)
	}
}

func TestTransport_IgnoresRetryAfterOnTerminal(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	tr := NewTransport(nil, fastRetry(3))
	tr.sleep = func(ctx context.Context, delay time.Duration) error {
		t.Fatalf("terminal response must not sleep before retry, got delay %s", delay)
		return nil
	}
	build := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
	}
	check := func(_ context.Context, status int, body []byte) error {
		if status == http.StatusForbidden {
			return ErrTerminal
		}
		return nil
	}

	err := tr.Send(context.Background(), "terminal-retry-after", build, check)
	if !errors.Is(err, ErrTerminal) {
		t.Fatalf("want ErrTerminal, got %v", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("terminal must stop after 1 attempt, got %d", attempts.Load())
	}
}

func counterValue(t *testing.T, counter prometheus.Counter) float64 {
	t.Helper()
	metric := ptrext.Of(dto.Metric{})
	if err := counter.Write(metric); err != nil {
		t.Fatalf("write counter metric: %v", err)
	}
	if metric.Counter == nil {
		return 0
	}
	return metric.GetCounter().GetValue()
}

func histogramCount(t *testing.T, observer prometheus.Observer) uint64 {
	t.Helper()
	metric, ok := observer.(prometheus.Metric)
	if !ok {
		t.Fatal("observer does not implement prometheus.Metric")
	}
	dtoMetric := ptrext.Of(dto.Metric{})
	if err := metric.Write(dtoMetric); err != nil {
		t.Fatalf("write histogram metric: %v", err)
	}
	if dtoMetric.Histogram == nil {
		return 0
	}
	return dtoMetric.GetHistogram().GetSampleCount()
}

func TestTransport_TerminalShortCircuits(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	tr := NewTransport(nil, fastRetry(5))
	build := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
	}
	check := func(_ context.Context, status int, body []byte) error {
		if status >= 200 && status < 300 {
			return nil
		}
		if status >= 400 && status < 500 {
			return ErrTerminal
		}
		return errors.New("retry-me")
	}
	err := tr.Send(context.Background(), "terminal", build, check)
	if !errors.Is(err, ErrTerminal) {
		t.Fatalf("want ErrTerminal, got %v", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("terminal must short-circuit: want 1 attempt, got %d", attempts.Load())
	}
}

func TestTransport_NoRetrySingleAttempt(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	tr := NewTransport(nil, NoRetry())
	build := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
	}
	check := func(_ context.Context, status int, body []byte) error {
		if status >= 200 && status < 300 {
			return nil
		}
		return errors.New("nope")
	}
	if err := tr.Send(context.Background(), "no-retry", build, check); err == nil {
		t.Fatalf("want err on 503, got nil")
	}
	if attempts.Load() != 1 {
		t.Fatalf("NoRetry must do exactly 1 attempt, got %d", attempts.Load())
	}
}

func TestTransport_ContextCancellationStopsLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	tr := NewTransport(nil, RetryPolicy{
		MaxAttempts: 10,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    1 * time.Second,
	})
	build := func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader([]byte(`{}`)))
	}
	check := func(_ context.Context, status int, body []byte) error { return errors.New("503") }

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := tr.Send(ctx, "cancel", build, check)
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("want ctx err, got %v", err)
	}
}

func TestRetryPolicy_BackoffDoubles(t *testing.T) {
	p := RetryPolicy{BaseDelay: 1 * time.Second, MaxDelay: 1 * time.Minute}
	cases := []struct {
		n    int
		want time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
	}
	for _, c := range cases {
		if got := p.backoff(c.n); got != c.want {
			t.Errorf("backoff(%d): want %v, got %v", c.n, c.want, got)
		}
	}
}

func TestRetryPolicy_BackoffClampsAtMaxDelay(t *testing.T) {
	p := RetryPolicy{BaseDelay: 10 * time.Second, MaxDelay: 30 * time.Second}
	// 10 * 2^4 = 160s > 30s cap
	if got := p.backoff(5); got != 30*time.Second {
		t.Errorf("clamp: want 30s, got %v", got)
	}
}

func TestDefaultRetry(t *testing.T) {
	t.Parallel()
	p := DefaultRetry()
	if p.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", p.MaxAttempts)
	}
	if p.BaseDelay != 1*time.Second {
		t.Errorf("BaseDelay = %v, want 1s", p.BaseDelay)
	}
	if p.MaxDelay != 10*time.Minute {
		t.Errorf("MaxDelay = %v, want 10m", p.MaxDelay)
	}
}
