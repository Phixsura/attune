// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

type portalRateLimitEntry struct {
	mu         sync.Mutex
	limiter    *rate.Limiter
	lastAccess time.Time
}

type portalAnonymousLimiter struct {
	perMinute  int
	burst      int
	disabled   bool
	trustedHop int
	keyFunc    func(*http.Request) string
	nowFunc    func() time.Time

	mu      sync.RWMutex
	clients map[string]*portalRateLimitEntry
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

func newPortalSurveyTokenLimiter(perMinute, burst int, disabled bool) *portalAnonymousLimiter {
	return newPortalLimiter(perMinute, burst, disabled, 0, portalSurveyTokenRateKey)
}

func newPortalSurveyProviderEventLimiter(perMinute, burst int, disabled bool) *portalAnonymousLimiter {
	return newPortalLimiter(perMinute, burst, disabled, 0, portalSurveyProviderEventRateKey)
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
		nowFunc:    time.Now,
		clients:    make(map[string]*portalRateLimitEntry),
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
		logext.Warnf(r.Context(), "[portal.ratelimit] reject,path:%s,rate_key:%s", portalSafePathForLog(r), key)
		dispatcher.Reject(r.Context(), w, http.StatusTooManyRequests,
			attunev1.ErrorCode_RATE_LIMITED, fmt.Sprintf("request too frequent, retry in %d seconds", retryAfter))
	})
}

func (l *portalAnonymousLimiter) StartCleanup(ctx context.Context, interval, maxIdleTime time.Duration) {
	if l == nil || l.disabled || interval <= 0 || maxIdleTime <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				l.cleanup(maxIdleTime)
			}
		}
	}()
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
	now := l.now()
	l.mu.RLock()
	entry, ok := l.clients[key]
	l.mu.RUnlock()
	if ok {
		entry.touch(now)
		return entry.limiter
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok = l.clients[key]; ok {
		entry.touch(now)
		return entry.limiter
	}
	entry = ptrext.Of(portalRateLimitEntry{
		limiter:    rate.NewLimiter(rate.Limit(float64(l.perMinute)/60.0), l.burst),
		lastAccess: now,
	})
	l.clients[key] = entry
	return entry.limiter
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

func (l *portalAnonymousLimiter) cleanup(maxIdleTime time.Duration) {
	if l == nil || maxIdleTime <= 0 {
		return
	}
	cutoff := l.now().Add(-maxIdleTime)
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, entry := range l.clients {
		if entry.idleBefore(cutoff) {
			delete(l.clients, key)
		}
	}
}

func (l *portalAnonymousLimiter) now() time.Time {
	if l == nil || l.nowFunc == nil {
		return time.Now()
	}
	return l.nowFunc()
}

func (e *portalRateLimitEntry) touch(now time.Time) {
	e.mu.Lock()
	e.lastAccess = now
	e.mu.Unlock()
}

func (e *portalRateLimitEntry) idleBefore(cutoff time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastAccess.Before(cutoff)
}

func portalTenantClientRateKey(r *http.Request, trustedHop int) string {
	tenantSlug := strings.TrimSpace(chi.URLParam(r, "tenant_slug"))
	clientIP := portalRateKeyHash(nethardening.ClientIP(r, trustedHop))
	if tenantSlug == "" {
		return "client:" + clientIP
	}
	return "tenant:" + tenantSlug + "|client:" + clientIP
}

func portalSurveyTokenRateKey(r *http.Request) string {
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if token == "" {
		return "survey-token:missing"
	}
	return "survey-token:" + portalRateKeyHash(token)
}

func portalSurveyProviderEventRateKey(r *http.Request) string {
	tenantID := strings.TrimSpace(chi.URLParam(r, "tenant_id"))
	senderID := strings.TrimSpace(chi.URLParam(r, "sender_id"))
	if tenantID == "" && senderID == "" {
		return "survey-provider-event:missing"
	}
	return "survey-provider-event:tenant:" + portalRateKeyHash(tenantID) + "|sender:" + portalRateKeyHash(senderID)
}

func portalRateKeyHash(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:8])
}

func portalSafePathForLog(r *http.Request) string {
	path := r.URL.EscapedPath()
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if token == "" {
		return path
	}
	path = strings.ReplaceAll(path, token, "<token>")
	path = strings.ReplaceAll(path, url.PathEscape(token), "<token>")
	return path
}
