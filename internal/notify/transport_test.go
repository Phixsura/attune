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
	check := func(status int, body []byte) error {
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
	check := func(status int, body []byte) error {
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
	check := func(status int, body []byte) error {
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
	check := func(status int, body []byte) error {
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
	check := func(status int, body []byte) error { return errors.New("503") }

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
