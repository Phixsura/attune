// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/defaults"
)

type portalLimiterSet struct {
	read                 *portalAnonymousLimiter
	write                *portalAnonymousLimiter
	surveyRead           *portalAnonymousLimiter
	surveyWrite          *portalAnonymousLimiter
	surveyProviderEvents *portalAnonymousLimiter
}

func newPortalLimiterSet(ctx context.Context, cfg *config.Config) portalLimiterSet {
	limiters := portalLimiterSet{
		read:                 newPortalAnonymousLimiter(cfg.RateLimitPerMinute, cfg.RateLimitBurst, cfg.RateLimitDisabled, cfg.Security.TrustedProxyHops),
		write:                newPortalSubmissionLimiter(cfg.RateLimitPerMinute, cfg.RateLimitBurst, cfg.RateLimitDisabled, cfg.Security.TrustedProxyHops),
		surveyRead:           newPortalSurveyTokenLimiter(cfg.RateLimitPerMinute, cfg.RateLimitBurst, cfg.RateLimitDisabled),
		surveyWrite:          newPortalSurveyTokenLimiter(cfg.RateLimitPerMinute, cfg.RateLimitBurst, cfg.RateLimitDisabled),
		surveyProviderEvents: newPortalSurveyProviderEventLimiter(cfg.RateLimitPerMinute, cfg.RateLimitBurst, cfg.RateLimitDisabled),
	}
	limiters.startCleanup(ctx)
	return limiters
}

func (s portalLimiterSet) startCleanup(ctx context.Context) {
	s.read.StartCleanup(ctx, defaults.RateLimitCleanupInterval, defaults.RateLimitMaxIdleTime)
	s.write.StartCleanup(ctx, defaults.RateLimitCleanupInterval, defaults.RateLimitMaxIdleTime)
	s.surveyRead.StartCleanup(ctx, defaults.RateLimitCleanupInterval, defaults.RateLimitMaxIdleTime)
	s.surveyWrite.StartCleanup(ctx, defaults.RateLimitCleanupInterval, defaults.RateLimitMaxIdleTime)
	s.surveyProviderEvents.StartCleanup(ctx, defaults.RateLimitCleanupInterval, defaults.RateLimitMaxIdleTime)
}
