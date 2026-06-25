// SPDX-License-Identifier: Apache-2.0

// Package httpretry provides HTTP request retry utilities.
package httpretry

import (
	"context"
	"errors"
	"math"
	"net/http"
	"time"
)

// Config configures retry behavior.
type Config struct {
	MaxRetries    int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	Multiplier    float64
	RetryableFunc func(resp *http.Response, err error) bool
}

// DefaultConfig returns sensible defaults for HTTP retries.
func DefaultConfig() Config {
	return Config{
		MaxRetries:    3,
		BaseDelay:     1 * time.Second,
		MaxDelay:      30 * time.Second,
		Multiplier:    2.0,
		RetryableFunc: DefaultRetryable,
	}
}

// DefaultRetryable returns true for retryable errors and status codes.
func DefaultRetryable(resp *http.Response, err error) bool {
	if err != nil {
		return true // network errors are retryable
	}
	if resp == nil {
		return false
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		http.StatusBadGateway:
		return true
	default:
		return false
	}
}

// Do executes an HTTP request with retries.
func Do(ctx context.Context, client *http.Client, req *http.Request, cfg Config) (*http.Response, error) {
	var lastErr error
	var lastResp *http.Response

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := calculateDelay(attempt, cfg)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		// Clone request for retry
		reqCopy := req.Clone(ctx)
		if req.Body != nil && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			reqCopy.Body = body
		}

		resp, err := client.Do(reqCopy)
		lastErr = err
		lastResp = resp

		if !cfg.RetryableFunc(resp, err) {
			return resp, err
		}

		// Close response body before retry
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return lastResp, errors.New("max retries exceeded")
}

func calculateDelay(attempt int, cfg Config) time.Duration {
	delay := float64(cfg.BaseDelay) * math.Pow(cfg.Multiplier, float64(attempt-1))
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}
	return time.Duration(delay)
}
