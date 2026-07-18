package httpretry

import (
	"context"
	"errors"
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

func TestDefaultRetryable_NetworkAndNilResponse(t *testing.T) {
	t.Parallel()

	require.True(t, DefaultRetryable(nil, errors.New("dial refused")))
	require.False(t, DefaultRetryable(nil, nil))
}

func TestDo_ReturnsRequestBodyFactoryError(t *testing.T) {
	t.Parallel()

	bodyErr := errors.New("rewind failed")
	req, err := http.NewRequest(http.MethodPost, "https://example.com", strings.NewReader("payload"))
	require.NoError(t, err)
	req.GetBody = func() (io.ReadCloser, error) {
		return nil, bodyErr
	}

	resp, err := Do(context.Background(), http.DefaultClient, req, DefaultConfig())

	require.Nil(t, resp)
	require.ErrorIs(t, err, bodyErr)
}

func TestDo_ReturnsLastNetworkErrorAfterRetries(t *testing.T) {
	t.Parallel()

	networkErr := errors.New("temporary network failure")
	client := ptrext.Of(http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return nil, networkErr
		}),
	})
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	resp, err := Do(context.Background(), client, req, Config{
		MaxRetries:    0,
		BaseDelay:     time.Millisecond,
		MaxDelay:      time.Millisecond,
		Multiplier:    1,
		RetryableFunc: DefaultRetryable,
	})

	require.Nil(t, resp)
	require.ErrorIs(t, err, networkErr)
}

func TestDo_ClosesRetryableResponseBody(t *testing.T) {
	t.Parallel()

	body := ptrext.Of(closeTrackingBody{Reader: strings.NewReader("retry me")})
	client := ptrext.Of(http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return ptrext.Of(http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       body,
				Header:     make(http.Header),
			}), nil
		}),
	})
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	_, err = Do(context.Background(), client, req, Config{
		MaxRetries:    0,
		BaseDelay:     time.Millisecond,
		MaxDelay:      time.Millisecond,
		Multiplier:    1,
		RetryableFunc: DefaultRetryable,
	})

	require.EqualError(t, err, "max retries exceeded")
	require.True(t, body.closed)
}

type closeTrackingBody struct {
	*strings.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return nil
}
