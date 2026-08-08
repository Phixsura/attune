// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

const (
	criticalLowScoreSLA = 24 * time.Hour
	highLowScoreSLA     = 48 * time.Hour
	mediumLowScoreSLA   = 72 * time.Hour
	lowLowScoreSLA      = 7 * 24 * time.Hour
)

func lowScoreReviewSeed(
	campaign repo.Campaign,
	score int,
	submittedAt time.Time,
) *repo.LowScoreReviewSeed {
	if score > campaign.LowScoreThreshold {
		return nil
	}
	severity := defaultLowScoreSeverity(score, campaign.LowScoreThreshold)
	dueAt := submittedAt.Add(lowScoreSLA(severity))
	return ptrext.Of(repo.LowScoreReviewSeed{
		Severity:  severity,
		DueAt:     ptrext.Of(dueAt),
		UpdatedBy: "system",
	})
}

func defaultLowScoreSeverity(score int, threshold int) string {
	if score <= 1 {
		return repo.SeverityCritical
	}
	if score < threshold {
		return repo.SeverityHigh
	}
	return repo.SeverityMedium
}

func lowScoreSLA(severity string) time.Duration {
	switch severity {
	case repo.SeverityCritical:
		return criticalLowScoreSLA
	case repo.SeverityHigh:
		return highLowScoreSLA
	case repo.SeverityLow:
		return lowLowScoreSLA
	default:
		return mediumLowScoreSLA
	}
}

func applyReviewUpdate(current repo.LowScoreReview, in ReviewInput) (repo.LowScoreReview, error) {
	next := current
	if in.Status != "" {
		next.Status = normalizeReviewStatus(in.Status)
	}
	if in.Severity != "" {
		next.Severity = normalizeSeverity(in.Severity)
	}
	if in.OwnerMemberIDSet {
		next.OwnerMemberID = in.OwnerMemberID
	}
	if in.RootCause != nil {
		next.RootCause = boundedString(strings.TrimSpace(ptrext.Indirect(in.RootCause)), 120)
	}
	if in.ActionTaken != nil {
		next.ActionTaken = boundedString(strings.TrimSpace(ptrext.Indirect(in.ActionTaken)), 5000)
	}
	if in.CustomerContacted != nil {
		if current.CustomerContacted && !ptrext.Indirect(in.CustomerContacted) {
			return repo.LowScoreReview{}, ErrValidation
		}
		next.CustomerContacted = ptrext.Indirect(in.CustomerContacted)
	}
	if in.DueAtSet {
		next.DueAt = in.DueAt
	}
	next.UpdatedBy = strings.TrimSpace(in.ActorID)
	if !validReviewStatus(next.Status) || !validSeverity(next.Severity) || next.UpdatedBy == "" {
		return repo.LowScoreReview{}, ErrValidation
	}
	return next, nil
}

func normalizeReviewStatus(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.ReviewOpen:
		return repo.ReviewOpen
	case repo.ReviewInReview:
		return repo.ReviewInReview
	case repo.ReviewResolved:
		return repo.ReviewResolved
	case repo.ReviewDismissed:
		return repo.ReviewDismissed
	default:
		return ""
	}
}

func normalizeSeverity(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.SeverityLow:
		return repo.SeverityLow
	case repo.SeverityMedium:
		return repo.SeverityMedium
	case repo.SeverityHigh:
		return repo.SeverityHigh
	case repo.SeverityCritical:
		return repo.SeverityCritical
	default:
		return ""
	}
}

func validReviewStatus(value string) bool {
	return value == repo.ReviewOpen ||
		value == repo.ReviewInReview ||
		value == repo.ReviewResolved ||
		value == repo.ReviewDismissed
}

func validSeverity(value string) bool {
	return value == repo.SeverityLow ||
		value == repo.SeverityMedium ||
		value == repo.SeverityHigh ||
		value == repo.SeverityCritical
}
