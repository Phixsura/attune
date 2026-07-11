// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"math"
	"net/http"
	"sync"

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

	mu      sync.RWMutex
	clients map[string]*rate.Limiter
}

func newPortalAnonymousLimiter(perMinute, burst int, disabled bool, trustedHop int) *portalAnonymousLimiter {
	return ptrext.Of(portalAnonymousLimiter{
		perMinute:  perMinute,
		burst:      burst,
		disabled:   disabled,
		trustedHop: trustedHop,
		clients:    make(map[string]*rate.Limiter),
	})
}

func (l *portalAnonymousLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l == nil || l.disabled || l.perMinute <= 0 {
			next.ServeHTTP(w, r)
			return
		}
		key := nethardening.ClientIP(r, l.trustedHop)
		limiter := l.limiterFor(key)
		if limiter.Allow() {
			next.ServeHTTP(w, r)
			return
		}
		retryAfter := l.retryAfterSeconds(limiter)
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
		logext.Warnf(r.Context(), "[portal.ratelimit] reject,path:%s,client_ip:%s", r.URL.Path, key)
		dispatcher.Reject(r.Context(), w, http.StatusTooManyRequests,
			attunev1.ErrorCode_RATE_LIMITED, fmt.Sprintf("request too frequent, retry in %d seconds", retryAfter))
	})
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
