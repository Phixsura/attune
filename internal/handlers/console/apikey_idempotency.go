package console

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/idempotency"
)

var apiKeyIdempotencyKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

func withAPIKeyIdempotency(store idempotency.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if store == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const where = "console.withAPIKeyIdempotency"

			key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !apiKeyIdempotencyKeyRe.MatchString(key) {
				dispatcher.Reject(r.Context(), w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST,
					"idempotency key must be 8-64 chars of [A-Za-z0-9_-]")
				return
			}

			auth, ok := apikey.FromContextSafe(r.Context())
			if !ok {
				dispatcher.Reject(r.Context(), w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "missing api key")
				return
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				logext.Warnf(r.Context(), "[%s] read body failed,path:%s,err:%+v", where, r.URL.Path, err.Error())
				dispatcher.Reject(r.Context(), w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "failed to read request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			requestHash, err := hashAPIKeyIdempotencyRequest(r.Method, r.URL.Path, r.Header.Get("Content-Type"), body)
			if err != nil {
				logext.Warnf(r.Context(), "[%s] hash request failed,path:%s,err:%+v", where, r.URL.Path, err.Error())
				dispatcher.Reject(r.Context(), w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "request body must be valid JSON")
				return
			}

			keyState, acquired, err := acquireAPIKeyIdempotency(r, store, auth.TenantID, key, requestHash)
			if err != nil {
				if errors.Is(err, idempotency.ErrHashMismatch) {
					dispatcher.Reject(r.Context(), w, http.StatusConflict, attunev1.ErrorCode_IDEMPOTENCY_CONFLICT,
						"idempotency key used with different request parameters")
				} else {
					logext.Errorf(r.Context(), "[%s] acquire failed,tenant_id:%s,path:%s,err:%+v",
						where, auth.TenantID, r.URL.Path, err.Error())
					dispatcher.Reject(r.Context(), w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL,
						"idempotency lookup failed")
				}
				return
			}
			if !acquired {
				switch keyState.Status {
				case idempotency.StatusCompleted:
					writeCachedAPIKeyIdempotentResponse(w, keyState)
				case idempotency.StatusPending:
					dispatcher.Reject(r.Context(), w, http.StatusConflict, attunev1.ErrorCode_REQUEST_IN_PROGRESS,
						"request with this idempotency key is already in progress")
				default:
					dispatcher.Reject(r.Context(), w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL,
						"idempotency state is invalid")
				}
				return
			}

			rec := newBufferedResponseWriter(w)
			next.ServeHTTP(rec, r)

			if shouldCacheAPIKeyIdempotentStatus(rec.status) {
				if err := store.Complete(r.Context(), auth.TenantID, key, rec.status, rec.body.Bytes()); err != nil {
					logext.Warnf(r.Context(), "[%s] complete failed,tenant_id:%s,path:%s,err:%+v",
						where, auth.TenantID, r.URL.Path, err.Error())
				}
				return
			}
			if err := store.Fail(r.Context(), auth.TenantID, key); err != nil {
				logext.Warnf(r.Context(), "[%s] fail failed,tenant_id:%s,path:%s,err:%+v",
					where, auth.TenantID, r.URL.Path, err.Error())
			}
		})
	}
}

func acquireAPIKeyIdempotency(
	r *http.Request,
	store idempotency.Store,
	tenantID, key string,
	requestHash []byte,
) (*idempotency.Key, bool, error) {
	keyState, acquired, err := store.Acquire(r.Context(), tenantID, key, requestHash, 0)
	if !errors.Is(err, idempotency.ErrExpired) {
		return keyState, acquired, err
	}
	if delErr := store.Delete(r.Context(), tenantID, key); delErr != nil {
		return nil, false, delErr
	}
	return store.Acquire(r.Context(), tenantID, key, requestHash, 0)
}

func hashAPIKeyIdempotencyRequest(method, path, contentType string, body []byte) ([]byte, error) {
	canonicalBody, err := canonicalizeAPIKeyIdempotencyBody(contentType, body)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(append([]byte(method+" "+path+"\n"), canonicalBody...))
	return sum[:], nil
}

func canonicalizeAPIKeyIdempotencyBody(contentType string, body []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "application/json") {
		return trimmed, nil
	}
	var v any
	if err := json.Unmarshal(trimmed, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func shouldCacheAPIKeyIdempotentStatus(status int) bool {
	if status == 0 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests {
		return false
	}
	return status < 500
}

func writeCachedAPIKeyIdempotentResponse(w http.ResponseWriter, key *idempotency.Key) {
	if len(key.ResponseBody) > 0 && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(key.ResponseCode)
	if len(key.ResponseBody) > 0 {
		_, _ = w.Write(key.ResponseBody)
	}
}

type bufferedResponseWriter struct {
	http.ResponseWriter
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func newBufferedResponseWriter(w http.ResponseWriter) *bufferedResponseWriter {
	return ptrext.Of(bufferedResponseWriter{ResponseWriter: w, status: http.StatusOK})
}

func (w *bufferedResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *bufferedResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *bufferedResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	_, _ = w.body.Write(p)
	return w.ResponseWriter.Write(p)
}
