package attune

import (
	"context"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// BinaryResponse is a raw download response returned by archive / CSV helpers.
type BinaryResponse struct {
	ContentType string
	Filename    string
	Data        []byte
}

func resolveRequestConfig(opts []RequestOption) (requestConfig, error) {
	var rc requestConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&rc)
		}
	}
	if rc.idempotencyKey != "" && hasHeaderControlChar(rc.idempotencyKey) {
		return requestConfig{}, &AttuneError{
			Code:    CodeBadRequest,
			Message: "idempotencyKey contains invalid characters",
		}
	}
	return rc, nil
}

func resolveRetryablePOSTKey(opts []RequestOption) (string, error) {
	rc, err := resolveRequestConfig(opts)
	if err != nil {
		return "", err
	}
	if rc.idempotencyKey != "" {
		return rc.idempotencyKey, nil
	}
	return newIdempotencyKey(), nil
}

func (c *Client) doBytes(ctx context.Context, method, path, key string) (*BinaryResponse, error) {
	retrySafe := method != http.MethodPost || key != ""
	var last *attemptError
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt-1, c.rand)
			if last != nil && last.hasRetryAfter {
				delay = last.retryAfter
			}
			if err := c.sleep(ctx, delay); err != nil {
				return nil, &AttuneError{Code: CodeAborted, Message: err.Error(), cause: err}
			}
		}
		resp, ae := c.doBytesOnce(ctx, method, path, key)
		if ae == nil {
			return resp, nil
		}
		last = ae
		if !ae.retryable || !retrySafe {
			return nil, ae.err
		}
	}
	return nil, last.err
}

func (c *Client) doBytesOnce(parent context.Context, method, path, key string) (*BinaryResponse, *attemptError) {
	ctx := parent
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, c.timeout)
		defer cancel()
	}

	req, err := c.newRequest(ctx, method, path, nil, key)
	if err != nil {
		return nil, &attemptError{err: &AttuneError{Code: CodeBadRequest, Message: err.Error(), cause: err}}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, c.classifyTransport(parent, ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, readErrResult := readCappedBody(resp)
	if readErrResult != nil {
		return nil, readErrResult
	}

	if resp.StatusCode/100 == 2 {
		return &BinaryResponse{
			ContentType: resp.Header.Get("Content-Type"),
			Filename:    filenameFromHeader(resp.Header.Get("Content-Disposition")),
			Data:        append([]byte(nil), data...),
		}, nil
	}

	var env attunev1.ErrorResponse
	var envPtr *attunev1.ErrorResponse
	if protojsonUnmarshal.Unmarshal(data, &env) == nil && env.GetCode() != "" {
		envPtr = &env
	}
	ae := errorFromResponse(resp.StatusCode, envPtr, resp.Header)
	ra, hasRA := ParseRetryAfter(resp.Header, time.Now())
	return nil, &attemptError{
		err:           ae,
		retryable:     IsRetryable(resp.StatusCode),
		retryAfter:    ra,
		hasRetryAfter: hasRA,
	}
}

func filenameFromHeader(header string) string {
	if strings.TrimSpace(header) == "" {
		return ""
	}
	rawStar, hasRawStar := rawDispositionParam(header, "filename*")
	if hasRawStar {
		if decoded, ok := decodeContentDispositionFilename(rawStar); ok {
			return decoded
		}
	}
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		if hasRawStar {
			return undecodedContentDispositionFilename(rawStar)
		}
		return ""
	}
	if filename := strings.TrimSpace(params["filename"]); filename != "" {
		return filename
	}
	if decoded, ok := decodeContentDispositionFilename(params["filename*"]); ok {
		return decoded
	}
	if hasRawStar {
		return undecodedContentDispositionFilename(rawStar)
	}
	return undecodedContentDispositionFilename(params["filename*"])
}

func rawDispositionParam(header, name string) (string, bool) {
	prefix := strings.ToLower(name) + "="
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if len(part) < len(prefix) || !strings.EqualFold(part[:len(prefix)], prefix) {
			continue
		}
		return strings.TrimSpace(part[len(prefix):]), true
	}
	return "", false
}

func undecodedContentDispositionFilename(raw string) string {
	raw = strings.Trim(strings.TrimSpace(raw), `"`)
	if raw == "" {
		return ""
	}
	if first := strings.IndexByte(raw, '\''); first >= 0 {
		if second := strings.IndexByte(raw[first+1:], '\''); second >= 0 {
			raw = raw[first+1+second+1:]
		}
	}
	return raw
}

func decodeContentDispositionFilename(raw string) (string, bool) {
	raw = undecodedContentDispositionFilename(raw)
	if raw == "" {
		return "", false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", false
	}
	return decoded, true
}
