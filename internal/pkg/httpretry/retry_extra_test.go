package httpretry

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	require.Equal(t, 3, cfg.MaxRetries)
	require.Equal(t, 1*time.Second, cfg.BaseDelay)
	require.Equal(t, 30*time.Second, cfg.MaxDelay)
	require.Equal(t, 2.0, cfg.Multiplier)
	require.NotNil(t, cfg.RetryableFunc)
}

func TestCalculateDelay(t *testing.T) {
	t.Parallel()

	cfg := Config{
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   350 * time.Millisecond,
		Multiplier: 2,
	}

	require.Equal(t, 100*time.Millisecond, calculateDelay(1, cfg))
	require.Equal(t, 200*time.Millisecond, calculateDelay(2, cfg))
	require.Equal(t, 350*time.Millisecond, calculateDelay(4, cfg))
}

func TestDo_MaxRetriesExceeded(t *testing.T) {
	t.Parallel()

	client := ptrext.Of(http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return ptrext.Of(http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}), nil
		}),
	})
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	_, err = Do(context.Background(), client, req, Config{
		MaxRetries:    1,
		BaseDelay:     time.Millisecond,
		MaxDelay:      time.Millisecond,
		Multiplier:    1,
		RetryableFunc: func(*http.Response, error) bool { return true },
	})

	require.Error(t, err)
	require.EqualError(t, err, "max retries exceeded")
}
