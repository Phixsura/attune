// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

type portalAnonymousLimiter struct {
	perMinute  int
	burst      int
	disabled   bool
	trustedHop int
	keyFunc    func(*http.Request) string

	mu      sync.RWMutex
	clients map[string]*rate.Limiter
}

func newPortalAnonymousLimiter(perMinute, burst int, disabled bool, trustedHop int) *portalAnonymousLimiter {
	return newPortalLimiter(perMinute, burst, disabled, trustedHop, func(r *http.Request) string {
		return portalTenantClientRateKey(r, trustedHop)
	})
}

func newPortalSubmissionLimiter(perMinute, burst int, disabled bool, trustedHop int) *portalAnonymousLimiter {
	return newPortalLimiter(perMinute, burst, disabled, trustedHop, func(r *http.Request) string {
		return portalTenantClientRateKey(r, trustedHop)
	})
}

func newPortalLimiter(
	perMinute, burst int,
	disabled bool,
	trustedHop int,
	keyFunc func(*http.Request) string,
) *portalAnonymousLimiter {
	return ptrext.Of(portalAnonymousLimiter{
		perMinute:  perMinute,
		burst:      burst,
		disabled:   disabled,
		trustedHop: trustedHop,
		keyFunc:    keyFunc,
		clients:    make(map[string]*rate.Limiter),
	})
}

func (l *portalAnonymousLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l == nil || l.disabled || l.perMinute <= 0 {
			next.ServeHTTP(w, r)
			return
		}
		key := l.key(r)
		limiter := l.limiterFor(key)
		if limiter.Allow() {
			next.ServeHTTP(w, r)
			return
		}
		retryAfter := l.retryAfterSeconds(limiter)
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
		logext.Warnf(r.Context(), "[portal.ratelimit] reject,path:%s,rate_key:%s", r.URL.Path, key)
		dispatcher.Reject(r.Context(), w, http.StatusTooManyRequests,
			attunev1.ErrorCode_RATE_LIMITED, fmt.Sprintf("request too frequent, retry in %d seconds", retryAfter))
	})
}

func (l *portalAnonymousLimiter) key(r *http.Request) string {
	if l == nil {
		return ""
	}
	if l.keyFunc != nil {
		return l.keyFunc(r)
	}
	return nethardening.ClientIP(r, l.trustedHop)
}

func (l *portalAnonymousLimiter) limiterFor(key string) *rate.Limiter {
	l.mu.RLock()
	limiter, ok := l.clients[key]
	l.mu.RUnlock()
	if ok {
		return limiter
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if limiter, ok = l.clients[key]; ok {
		return limiter
	}
	limiter = rate.NewLimiter(rate.Limit(float64(l.perMinute)/60.0), l.burst)
	l.clients[key] = limiter
	return limiter
}

func (l *portalAnonymousLimiter) retryAfterSeconds(limiter *rate.Limiter) int {
	reservation := limiter.Reserve()
	if !reservation.OK() {
		return 1
	}
	delay := reservation.Delay()
	reservation.Cancel()
	if delay <= 0 {
		return 1
	}
	return int(math.Ceil(delay.Seconds()))
}

func portalTenantClientRateKey(r *http.Request, trustedHop int) string {
	tenantSlug := strings.TrimSpace(chi.URLParam(r, "tenant_slug"))
	clientIP := nethardening.ClientIP(r, trustedHop)
	if tenantSlug == "" {
		return clientIP
	}
	return tenantSlug + "|" + clientIP
}
