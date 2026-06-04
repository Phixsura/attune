// Package ratelimit is the per-tenant token bucket guarding the ingest
// API. Phase 3.3 ships a single global default; per-SKU + per-tenant
// overrides come at Wave 3 billing.
//
// Design：
//   - Token bucket via golang.org/x/time/rate (battle-tested algorithm
//     used by net/http2, grpc, kubernetes).
//   - One bucket per tenant_id, keyed in a sync.Map. Buckets never
//     evict: Y1 < 400 tenants ≈ < 50KB memory, GC isn't worth the code.
//   - State is in-memory only. A listen restart drops all bucket
//     state — short-lived bursts immediately after deploy can exceed
//     the limit. Acceptable trade-off: zero new infra (no Redis), zero
//     DB writes per request.
//   - Rate is "requests per minute"; burst is the bucket capacity.
//     A sustained 60/min limit with 300 burst means: clients can fire
//     300 in a single instant, then must average 1/sec thereafter.
package ratelimit

import (
	"net/http"
	"sync"

	"golang.org/x/time/rate"

	"github.com/wanmuchengchuan/listen/internal/infra/apikey"
	"github.com/wanmuchengchuan/listen/internal/logext"
)

// Limiter is the per-tenant rate limiter. Construct once, share via
// middleware. Zero-value is invalid — always go through New.
type Limiter struct {
	perMinute int
	burst     int
	disabled  bool
	onLimit   func(tenantID string) // metric hook; nil-safe

	mu      sync.RWMutex
	tenants map[string]*rate.Limiter
}

// New returns a Limiter. perMinute=0 or disabled=true means every
// request is allowed (escape hatch for tests / migration / "we'll
// turn this on later" moments). onLimit fires once each time a
// request is rejected — wire to a Prometheus counter.
func New(perMinute, burst int, disabled bool, onLimit func(string)) *Limiter {
	return &Limiter{
		perMinute: perMinute,
		burst:     burst,
		disabled:  disabled,
		onLimit:   onLimit,
		tenants:   make(map[string]*rate.Limiter),
	}
}

// limiterFor returns (or lazily creates) the per-tenant limiter. Hot
// path: first lookup is read-locked; the rare create path takes the
// write lock + re-checks under it (single-flight on first request per
// tenant).
func (l *Limiter) limiterFor(tenantID string) *rate.Limiter {
	l.mu.RLock()
	lim, ok := l.tenants[tenantID]
	l.mu.RUnlock()
	if ok {
		return lim
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if lim, ok = l.tenants[tenantID]; ok {
		return lim
	}
	// rate.Limit is "events per second". Convert from per-minute.
	lim = rate.NewLimiter(rate.Limit(float64(l.perMinute)/60.0), l.burst)
	l.tenants[tenantID] = lim
	return lim
}

// Allow reports whether one request should be admitted. Unauthenticated
// requests (no tenant in context) bypass — the api-key middleware is
// already going to 401 them.
func (l *Limiter) Allow(tenantID string) bool {
	if l.disabled || l.perMinute <= 0 || tenantID == "" {
		return true
	}
	if !l.limiterFor(tenantID).Allow() {
		if l.onLimit != nil {
			l.onLimit(tenantID)
		}
		return false
	}
	return true
}

// Middleware returns the HTTP wrapper. Mount AFTER api-key middleware
// so context already carries tenant_id.
//
// Hot path: success silent (tight loop discipline);只 log reject。
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	const where = "ratelimit.Middleware"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenantID, _ := apikey.TenantIDFromContext(ctx)
		if !l.Allow(tenantID) {
			logext.Warnf(ctx, "[%s] reject: rate limited,tenant_id:%s,path:%s",
				where, tenantID, r.URL.Path)
			w.Header().Set("Retry-After", "30")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate_limited","message":"提交过快，请 30 秒后再试"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
