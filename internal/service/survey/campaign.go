// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"reflect"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

func (s *Service) normalizeNewCampaign(in CampaignInput) (repo.Campaign, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	actorID := strings.TrimSpace(in.ActorID)
	if tenantID == "" || actorID == "" || in.Name == nil {
		return repo.Campaign{}, ErrValidation
	}
	campaign := repo.Campaign{
		ID:                            uuid.New(),
		TenantID:                      tenantID,
		Name:                          strings.TrimSpace(ptrext.Indirect(in.Name)),
		SurveyType:                    normalizeSurveyType(in.SurveyType),
		Status:                        normalizeCampaignStatus(in.Status, true),
		TriggerEvent:                  normalizeTriggerEvent(in.TriggerEvent),
		DistributionMode:              normalizeDistributionMode(in.DistributionMode),
		DedupePolicy:                  normalizeDedupePolicy(in.DedupePolicy),
		TriggerFilter:                 normalizeObject(in.TriggerFilter),
		Content:                       normalizeContent(in.SurveyType, in.Content),
		Locale:                        normalizedLocale(ptrext.Indirect(in.Locale), "en"),
		ContentVersion:                1,
		SamplingPercent:               numberOr(in.SamplingPercent, 100),
		MinDaysBetweenContact:         intOr(in.MinDaysBetweenContact, 30),
		ExpiresAfterDays:              intOr(in.ExpiresAfterDays, 14),
		MaxDailyInvitations:           intOr(in.MaxDailyInvitations, 0),
		LowScoreThreshold:             intOr(in.LowScoreThreshold, 3),
		RequireRecentCustomerActivity: boolOr(in.RequireRecentCustomerActivity, true),
		RecentActivityDays:            intOr(in.RecentActivityDays, 30),
		SuppressAutoResolved:          boolOr(in.SuppressAutoResolved, true),
		CreatedBy:                     actorID,
		UpdatedBy:                     actorID,
	}
	if err := validateCampaign(campaign); err != nil {
		return repo.Campaign{}, err
	}
	return campaign, nil
}

func (s *Service) applyCampaignUpdate(current repo.Campaign, in CampaignInput) (repo.Campaign, error) {
	next := ptrext.Of(current)
	if in.Name != nil {
		next.Name = strings.TrimSpace(ptrext.Indirect(in.Name))
	}
	applyCampaignEnumUpdate(next, in)
	applyCampaignContentUpdate(next, current, in)
	applyCampaignLimitUpdate(next, in)
	applyCampaignFlagUpdate(next, in)
	if in.Locale != nil {
		next.Locale = normalizedLocale(ptrext.Indirect(in.Locale), next.Locale)
	}
	next.UpdatedBy = strings.TrimSpace(in.ActorID)
	if next.UpdatedBy == "" {
		return repo.Campaign{}, ErrValidation
	}
	if next.Status != repo.StatusArchived {
		next.ArchivedAt = nil
	}
	updated := ptrext.Indirect(next)
	if err := validateCampaign(updated); err != nil {
		return repo.Campaign{}, err
	}
	return updated, nil
}

func applyCampaignEnumUpdate(next *repo.Campaign, in CampaignInput) {
	if in.Status != "" {
		next.Status = normalizeCampaignStatus(in.Status, false)
	}
	if in.TriggerEvent != "" {
		next.TriggerEvent = normalizeTriggerEvent(in.TriggerEvent)
	}
	if in.DistributionMode != "" {
		next.DistributionMode = normalizeDistributionMode(in.DistributionMode)
	}
	if in.DedupePolicy != "" {
		next.DedupePolicy = normalizeDedupePolicy(in.DedupePolicy)
	}
}

func applyCampaignContentUpdate(next *repo.Campaign, current repo.Campaign, in CampaignInput) {
	if in.TriggerFilterSet {
		next.TriggerFilter = normalizeObject(in.TriggerFilter)
	}
	if !in.ContentSet {
		return
	}
	next.Content = normalizeContent(next.SurveyType, in.Content)
	if !reflect.DeepEqual(current.Content, next.Content) {
		next.ContentVersion++
	}
}

func applyCampaignLimitUpdate(next *repo.Campaign, in CampaignInput) {
	if in.SamplingPercent != nil {
		next.SamplingPercent = ptrext.Indirect(in.SamplingPercent)
	}
	if in.MinDaysBetweenContact != nil {
		next.MinDaysBetweenContact = ptrext.Indirect(in.MinDaysBetweenContact)
	}
	if in.ExpiresAfterDays != nil {
		next.ExpiresAfterDays = ptrext.Indirect(in.ExpiresAfterDays)
	}
	if in.MaxDailyInvitations != nil {
		next.MaxDailyInvitations = ptrext.Indirect(in.MaxDailyInvitations)
	}
	if in.LowScoreThreshold != nil {
		next.LowScoreThreshold = ptrext.Indirect(in.LowScoreThreshold)
	}
	if in.RecentActivityDays != nil {
		next.RecentActivityDays = ptrext.Indirect(in.RecentActivityDays)
	}
}

func applyCampaignFlagUpdate(next *repo.Campaign, in CampaignInput) {
	if in.RequireRecentCustomerActivity != nil {
		next.RequireRecentCustomerActivity = ptrext.Indirect(in.RequireRecentCustomerActivity)
	}
	if in.SuppressAutoResolved != nil {
		next.SuppressAutoResolved = ptrext.Indirect(in.SuppressAutoResolved)
	}
}

func validateCampaign(c repo.Campaign) error {
	if !validCampaignIdentity(c) || !validCampaignEnums(c) || !validCampaignLimits(c) {
		return ErrValidation
	}
	return validateLowScoreThreshold(c.SurveyType, c.LowScoreThreshold)
}

func validCampaignIdentity(c repo.Campaign) bool {
	return c.TenantID != "" && c.Name != "" && c.CreatedBy != "" && c.UpdatedBy != ""
}

func validCampaignEnums(c repo.Campaign) bool {
	return c.Status != repo.StatusArchived &&
		validSurveyType(c.SurveyType) &&
		validCampaignStatus(c.Status) &&
		validTriggerEvent(c.TriggerEvent) &&
		validDistributionMode(c.DistributionMode) &&
		validDedupePolicy(c.DedupePolicy)
}

func validCampaignLimits(c repo.Campaign) bool {
	return c.SamplingPercent >= 0 &&
		c.SamplingPercent <= 100 &&
		c.MinDaysBetweenContact >= 0 &&
		c.ExpiresAfterDays >= 1 &&
		c.ExpiresAfterDays <= 365 &&
		c.MaxDailyInvitations >= 0 &&
		c.RecentActivityDays >= 0 &&
		c.RecentActivityDays <= 3650
}

func normalizeSurveyType(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.TypeCSAT:
		return repo.TypeCSAT
	case repo.TypeCES:
		return repo.TypeCES
	default:
		return ""
	}
}

func normalizeCampaignStatus(raw string, creating bool) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "":
		return repo.StatusDraft
	case repo.StatusDraft:
		return repo.StatusDraft
	case repo.StatusActive:
		return repo.StatusActive
	case repo.StatusArchived:
		if creating {
			return ""
		}
		return repo.StatusArchived
	default:
		return ""
	}
}

func normalizeTriggerEvent(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.TriggerWorkflowTransition:
		return repo.TriggerWorkflowTransition
	case repo.TriggerReplySent:
		return repo.TriggerReplySent
	case repo.TriggerManualLink:
		return repo.TriggerManualLink
	case repo.TriggerRequestResolved:
		return repo.TriggerRequestResolved
	default:
		return ""
	}
}

func normalizeDistributionMode(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.DistributionContactEmail:
		return repo.DistributionContactEmail
	case repo.DistributionSourceLink:
		return repo.DistributionSourceLink
	default:
		return ""
	}
}

func normalizeDedupePolicy(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "":
		return repo.DedupeOnePerSource
	case repo.DedupeOnePerSource:
		return repo.DedupeOnePerSource
	case repo.DedupeOnePerResolution:
		return repo.DedupeOnePerResolution
	case repo.DedupeOnePerTrigger:
		return repo.DedupeOnePerTrigger
	default:
		return ""
	}
}

func validSurveyType(value string) bool {
	return value == repo.TypeCSAT || value == repo.TypeCES
}

func validCampaignStatus(value string) bool {
	return value == repo.StatusDraft || value == repo.StatusActive
}

func validTriggerEvent(value string) bool {
	return value == repo.TriggerWorkflowTransition ||
		value == repo.TriggerReplySent ||
		value == repo.TriggerManualLink ||
		value == repo.TriggerRequestResolved
}

func validDistributionMode(value string) bool {
	return value == repo.DistributionContactEmail || value == repo.DistributionSourceLink
}

func validDedupePolicy(value string) bool {
	return value == repo.DedupeOnePerSource || value == repo.DedupeOnePerResolution || value == repo.DedupeOnePerTrigger
}
