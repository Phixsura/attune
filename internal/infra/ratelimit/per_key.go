// PerKeyLimiter enforces each API key's own rate_limit_rpm (a variable,
// per-key limit), complementing the per-tenant Limiter in limiter.go. It is a
// no-op for keys without an rpm. Same design trade-offs as Limiter: in-memory
// token buckets (golang.org/x/time/rate), per-process state, no new infra.
package ratelimit

import (
	"fmt"
	"math"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// PerKeyLimiter keys a token bucket by API-key id, each at that key's own rpm.
// Only keys with rate_limit_rpm set get a bucket, so cardinality is bounded by
// the (small) set of rpm-configured keys; like Limiter, buckets aren't evicted.
type PerKeyLimiter struct {
	disabled bool
	onLimit  func(tenantID string) // metric hook; nil-safe

	mu   sync.RWMutex
	keys map[uuid.UUID]*keyBucket
}

type keyBucket struct {
	rpm int // the rpm this bucket was built for; rebuilt if the key's rpm changes
	lim *rate.Limiter
}

// NewPerKey returns a PerKeyLimiter. disabled=true allows every request
// (test/migration escape hatch). onLimit fires once per rejection.
func NewPerKey(disabled bool, onLimit func(string)) *PerKeyLimiter {
	return ptrext.Of(PerKeyLimiter{
		disabled: disabled,
		onLimit:  onLimit,
		keys:     make(map[uuid.UUID]*keyBucket),
	})
}

// perKeyBurst caps the instantaneous burst at one minute's worth of the rate
// (min 1) — so a 1-rpm key admits 1 then throttles.
func perKeyBurst(rpm int) int {
	if rpm < 1 {
		return 1
	}
	return rpm
}

func (l *PerKeyLimiter) limiterFor(keyID uuid.UUID, rpm int) *rate.Limiter {
	l.mu.RLock()
	b, ok := l.keys[keyID]
	l.mu.RUnlock()
	if ok && b.rpm == rpm {
		return b.lim
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if b, ok = l.keys[keyID]; ok && b.rpm == rpm {
		return b.lim
	}
	nb := ptrext.Of(keyBucket{rpm: rpm, lim: rate.NewLimiter(rate.Limit(float64(rpm)/60.0), perKeyBurst(rpm))})
	l.keys[keyID] = nb
	return nb.lim
}

// Allow reports whether one request for keyID should be admitted. rpm<=0 or a
// nil key bypasses (no per-key limit configured).
func (l *PerKeyLimiter) Allow(keyID uuid.UUID, rpm int, tenantID string) bool {
	if l.disabled || rpm <= 0 || keyID == uuid.Nil {
		return true
	}
	if !l.limiterFor(keyID, rpm).Allow() {
		if l.onLimit != nil {
			l.onLimit(tenantID)
		}
		return false
	}
	return true
}

func (l *PerKeyLimiter) retryAfterSeconds(keyID uuid.UUID, rpm int) int {
	if l.disabled || rpm <= 0 || keyID == uuid.Nil {
		return 0
	}
	res := l.limiterFor(keyID, rpm).Reserve()
	if !res.OK() {
		return 1
	}
	delay := res.Delay()
	res.Cancel()
	if delay <= 0 {
		return 1
	}
	return int(math.Ceil(delay.Seconds()))
}

// Middleware enforces the per-key limit. Mount AFTER the api-key middleware so
// AuthCtx (KeyID + RateLimitRPM) is on the context. Keys without an rpm pass
// straight through; the per-tenant Limiter still applies independently.
func (l *PerKeyLimiter) Middleware(next http.Handler) http.Handler {
	const where = "ratelimit.PerKeyMiddleware"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		auth := apikey.FromContext(ctx)
		if auth == nil || auth.RateLimitRPM == nil {
			next.ServeHTTP(w, r)
			return
		}
		rpm := ptrext.Indirect(auth.RateLimitRPM)
		if !l.Allow(auth.KeyID, rpm, auth.TenantID) {
			logext.Warnf(ctx, "[%s] reject: per-key rate limited,key_id:%s,rpm:%d,path:%s",
				where, auth.KeyID, rpm, r.URL.Path)
			retryAfter := l.retryAfterSeconds(auth.KeyID, rpm)
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			dispatcher.Reject(ctx, w, http.StatusTooManyRequests,
				attunev1.ErrorCode_RATE_LIMITED, retryAfterMessage(retryAfter))
			return
		}
		next.ServeHTTP(w, r)
	})
}
