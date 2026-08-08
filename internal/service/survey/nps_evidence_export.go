// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

const (
	npsEvidenceExportVersion = "3"
	npsEvidenceExportTTL     = 30 * 24 * time.Hour
)

func supportedNPSEvidenceExportVersion(version string) bool {
	return version == "1" || version == "2" || version == npsEvidenceExportVersion
}

// CreateNPSCampaignRunEvidenceExport freezes the exact aggregate artifact
// returned to an operator. Subsequent downloads use the stored bytes and do
// not re-evaluate a live response ledger.
func (s *Service) CreateNPSCampaignRunEvidenceExport(
	ctx context.Context,
	tenantID string,
	campaignID, runID uuid.UUID,
	createdByType, createdBy string,
) (repo.NPSCampaignRunEvidenceExport, error) {
	item, _, err := s.CreateNPSCampaignRunEvidenceExportWithRequestKey(
		ctx, tenantID, campaignID, runID, uuid.New(), createdByType, createdBy,
	)
	return item, err
}

// CreateNPSCampaignRunEvidenceExportWithRequestKey creates one bounded,
// replayable evidence snapshot. A matching request key returns the original
// artifact without recomputing or emitting a second creation event.
func (s *Service) CreateNPSCampaignRunEvidenceExportWithRequestKey(
	ctx context.Context,
	tenantID string,
	campaignID, runID, clientRequestKey uuid.UUID,
	createdByType, createdBy string,
) (repo.NPSCampaignRunEvidenceExport, bool, error) {
	if err := validateNPSCampaignRunEvidenceExportRequest(tenantID, campaignID, runID, clientRequestKey, createdBy); err != nil {
		return repo.NPSCampaignRunEvidenceExport{}, false, err
	}
	exportRepo, err := s.npsEvidenceExportRepo()
	if err != nil {
		return repo.NPSCampaignRunEvidenceExport{}, false, err
	}
	existing, found, err := s.findNPSCampaignRunEvidenceExport(ctx, exportRepo, tenantID, campaignID, runID, clientRequestKey)
	if err != nil {
		return repo.NPSCampaignRunEvidenceExport{}, false, err
	}
	if found {
		return existing, true, nil
	}
	item, err := s.buildNPSCampaignRunEvidenceExport(ctx, tenantID, campaignID, runID, clientRequestKey, createdByType, createdBy)
	if err != nil {
		return repo.NPSCampaignRunEvidenceExport{}, false, err
	}
	item, err = exportRepo.CreateNPSCampaignRunEvidenceExport(ctx, item)
	if err != nil {
		if existing, replayed := s.replayNPSCampaignRunEvidenceExportAfterConflict(ctx, exportRepo, tenantID, campaignID, runID, clientRequestKey); replayed {
			return existing, true, nil
		}
		return repo.NPSCampaignRunEvidenceExport{}, false, mapRepoError(err)
	}
	return item, false, nil
}

func validateNPSCampaignRunEvidenceExportRequest(
	tenantID string,
	campaignID, runID, clientRequestKey uuid.UUID,
	createdBy string,
) error {
	if strings.TrimSpace(tenantID) == "" || campaignID == uuid.Nil || runID == uuid.Nil || clientRequestKey == uuid.Nil || strings.TrimSpace(createdBy) == "" {
		return ErrValidation
	}
	return nil
}

func (s *Service) findNPSCampaignRunEvidenceExport(
	ctx context.Context,
	exportRepo repo.NPSCampaignRunEvidenceExportRepo,
	tenantID string,
	campaignID, runID, clientRequestKey uuid.UUID,
) (repo.NPSCampaignRunEvidenceExport, bool, error) {
	item, err := exportRepo.FindNPSCampaignRunEvidenceExportByRequestKey(ctx, tenantID, campaignID, runID, clientRequestKey)
	switch {
	case errors.Is(err, repo.ErrNotFound):
		return repo.NPSCampaignRunEvidenceExport{}, false, nil
	case err != nil:
		return repo.NPSCampaignRunEvidenceExport{}, false, mapRepoError(err)
	case !supportedNPSEvidenceExportVersion(item.ReportVersion):
		return repo.NPSCampaignRunEvidenceExport{}, false, ErrConflict
	default:
		return item, true, nil
	}
}

func (s *Service) replayNPSCampaignRunEvidenceExportAfterConflict(
	ctx context.Context,
	exportRepo repo.NPSCampaignRunEvidenceExportRepo,
	tenantID string,
	campaignID, runID, clientRequestKey uuid.UUID,
) (repo.NPSCampaignRunEvidenceExport, bool) {
	item, found, err := s.findNPSCampaignRunEvidenceExport(ctx, exportRepo, tenantID, campaignID, runID, clientRequestKey)
	return item, err == nil && found
}

func (s *Service) buildNPSCampaignRunEvidenceExport(
	ctx context.Context,
	tenantID string,
	campaignID, runID, clientRequestKey uuid.UUID,
	createdByType, createdBy string,
) (repo.NPSCampaignRunEvidenceExport, error) {
	evidence, err := s.NPSCampaignRunEvidence(ctx, tenantID, campaignID, runID)
	if err != nil {
		return repo.NPSCampaignRunEvidenceExport{}, err
	}
	generatedAt := s.currentTime()
	data, err := BuildNPSCampaignRunEvidenceCSV(evidence, generatedAt)
	if err != nil {
		return repo.NPSCampaignRunEvidenceExport{}, fmt.Errorf("build NPS evidence export: %w", err)
	}
	digest := sha256.Sum256(data)
	actorType := strings.TrimSpace(createdByType)
	if actorType == "" {
		actorType = "admin"
	}
	return repo.NPSCampaignRunEvidenceExport{
		ID:               uuid.New(),
		TenantID:         strings.TrimSpace(tenantID),
		CampaignID:       campaignID,
		RunID:            runID,
		ClientRequestKey: clientRequestKey,
		ReportVersion:    npsEvidenceExportVersion,
		GeneratedAt:      generatedAt,
		Artifact:         data,
		ArtifactSHA256:   fmt.Sprintf("sha256:%x", digest),
		CreatedByType:    actorType,
		CreatedBy:        strings.TrimSpace(createdBy),
		ExpiresAt:        generatedAt.Add(npsEvidenceExportTTL),
	}, nil
}

func (s *Service) ListNPSCampaignRunEvidenceExports(
	ctx context.Context,
	tenantID string,
	campaignID, runID uuid.UUID,
	limit int,
) ([]repo.NPSCampaignRunEvidenceExportSummary, error) {
	if strings.TrimSpace(tenantID) == "" || campaignID == uuid.Nil || runID == uuid.Nil {
		return nil, ErrValidation
	}
	exportRepo, err := s.npsEvidenceExportRepo()
	if err != nil {
		return nil, err
	}
	items, err := exportRepo.ListNPSCampaignRunEvidenceExports(ctx, tenantID, campaignID, runID, limit)
	return items, mapRepoError(err)
}

func (s *Service) DownloadNPSCampaignRunEvidenceExport(
	ctx context.Context,
	tenantID string,
	campaignID, runID, exportID uuid.UUID,
) (repo.NPSCampaignRunEvidenceExport, error) {
	if strings.TrimSpace(tenantID) == "" || campaignID == uuid.Nil || runID == uuid.Nil || exportID == uuid.Nil {
		return repo.NPSCampaignRunEvidenceExport{}, ErrValidation
	}
	exportRepo, err := s.npsEvidenceExportRepo()
	if err != nil {
		return repo.NPSCampaignRunEvidenceExport{}, err
	}
	item, err := exportRepo.MarkNPSCampaignRunEvidenceExportDownloaded(ctx, tenantID, campaignID, runID, exportID)
	if err != nil {
		return repo.NPSCampaignRunEvidenceExport{}, mapRepoError(err)
	}
	if !item.ExpiresAt.IsZero() && !item.ExpiresAt.After(s.currentTime()) {
		return repo.NPSCampaignRunEvidenceExport{}, ErrExpired
	}
	return item, nil
}

// PurgeExpiredNPSCampaignRunEvidenceExports removes expired report bytes while
// leaving the audit trail untouched. The optional interface keeps lightweight
// service fakes usable when the cleanup worker is not under test.
func (s *Service) PurgeExpiredNPSCampaignRunEvidenceExports(
	ctx context.Context,
	now time.Time,
	limit int,
) (map[string]int64, error) {
	purger, ok := s.repo.(repo.NPSCampaignRunEvidenceExportPurger)
	if !ok {
		return map[string]int64{}, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return purger.PurgeExpiredNPSCampaignRunEvidenceExports(ctx, now, limit)
}

func (s *Service) currentTime() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) npsEvidenceExportRepo() (repo.NPSCampaignRunEvidenceExportRepo, error) {
	item, ok := s.repo.(repo.NPSCampaignRunEvidenceExportRepo)
	if !ok {
		return nil, ErrDisabled
	}
	return item, nil
}

// BuildNPSCampaignRunEvidenceCSV is the versioned, aggregate-only report
// serializer. It intentionally has no respondent-level inputs.
func BuildNPSCampaignRunEvidenceCSV(evidence NPSCampaignRunEvidence, generatedAt time.Time) ([]byte, error) {
	run := evidence.Run
	analytics := evidence.Analytics
	scoreCounts := make(map[int]int, 11)
	for _, bucket := range analytics.ScoreDistribution {
		if bucket.Score >= 0 && bucket.Score <= 10 {
			scoreCounts[bucket.Score] = bucket.Count
		}
	}
	npsResponseCount := run.DetractorCount + run.PassiveCount + run.PromoterCount
	npsAvailable := run.NPSAvailable || npsResponseCount > 0
	row := []string{
		npsEvidenceExportVersion,
		generatedAt.UTC().Format(time.RFC3339),
		run.CampaignID.String(),
		run.ID.String(),
		strconv.Itoa(run.Sequence),
		run.Status,
		run.MeasurementKey,
		run.MeasurementReadiness,
		run.ScheduledAt.UTC().Format(time.RFC3339),
		formatEvidenceTime(run.OpenedAt),
		formatEvidenceTime(run.ClosesAt),
		strconv.Itoa(run.EvaluatedCount),
		strconv.Itoa(run.EligibleCount),
		strconv.Itoa(run.InvitationCount),
		strconv.Itoa(run.DeliveredCount),
		strconv.Itoa(run.StartedCount),
		strconv.Itoa(run.CompletedCount),
		formatEvidenceRatio(run.HostedVisitRate),
		formatEvidenceRatio(run.CompletionRate),
		formatEvidenceRatio(run.CompletedResponseRate),
		formatEvidenceFloat(run.NPS),
		strconv.FormatBool(npsAvailable),
		strconv.Itoa(run.DetractorCount),
		strconv.Itoa(run.PassiveCount),
		strconv.Itoa(run.PromoterCount),
		strconv.Itoa(run.RedactedResponseCount),
		strconv.Itoa(analytics.QualityFlaggedResponseCount),
		strconv.Itoa(run.MinimumCompletedResponses),
		strconv.Itoa(run.MinimumResponseRatePercent),
		strconv.Itoa(run.SamplePlanningPopulationCount),
		strconv.Itoa(run.SamplePlanningRequiredCompletedResponses),
		strconv.Itoa(run.SamplePlanningInvitationTarget),
		strconv.FormatBool(run.InvitationCountBelowSamplePlanningTarget),
		strconv.Itoa(run.CollectionDays),
		strconv.Itoa(run.MaximumRunRecipients),
		strconv.Itoa(run.ContactCooldownDays),
		strconv.Itoa(run.RecurrenceSamplingPercent),
	}
	for score := 0; score <= 10; score++ {
		row = append(row, strconv.Itoa(scoreCounts[score]))
	}
	buffer := ptrext.Of(bytes.Buffer{})
	writer := csv.NewWriter(buffer)
	header := []string{
		"report_version", "generated_at", "campaign_id", "run_id", "run_sequence", "status",
		"measurement_key", "measurement_readiness", "scheduled_at", "opened_at", "closes_at",
		"evaluated_count", "eligible_count", "invitation_count", "delivered_count", "started_count",
		"completed_count", "hosted_visit_rate", "page_visit_completion_rate", "submitted_response_rate",
		"nps", "nps_available", "detractor_count", "passive_count", "promoter_count", "redacted_response_count", "quality_flagged_response_count",
		"minimum_completed_responses", "minimum_response_rate_percent", "sample_population_count",
		"sample_required_completed_responses", "sample_invitation_target", "sample_target_shortfall",
		"collection_days", "maximum_run_recipients", "contact_cooldown_days", "recurrence_sampling_percent",
	}
	for score := 0; score <= 10; score++ {
		header = append(header, "score_"+strconv.Itoa(score)+"_count")
	}
	if err := writer.Write(header); err != nil {
		return nil, err
	}
	if err := writer.Write(row); err != nil {
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func formatEvidenceTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func formatEvidenceRatio(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func formatEvidenceFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}
