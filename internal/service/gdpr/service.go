// SPDX-License-Identifier: Apache-2.0

package gdpr

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	gdprrepo "github.com/Phixsura/attune/internal/repo/gdpr"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type repo interface {
	Export(ctx context.Context, tenantID, subjectKey string) (*gdprrepo.ExportData, error)
	Delete(ctx context.Context, tenantID, subjectKey string) (*gdprrepo.DeleteResult, error)
}

type requestStore interface {
	ListRequests(ctx context.Context, filter gdprrepo.ListRequestFilter) (gdprrepo.ListRequestResult, error)
	GetOperationsSummary(ctx context.Context, tenantID string) (*gdprrepo.OperationsSummary, error)
	CreateDeleteRequest(ctx context.Context, tenantID, subjectKey, subjectHash, createdBy string, executeAfter time.Time) (*gdprrepo.DeleteResult, error)
	CancelDeleteRequest(ctx context.Context, tenantID, requestID string) (*gdprrepo.Request, error)
}

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

type Service struct {
	repo               repo
	audit              auditRecorder
	auditRetentionDays int
	auditPruneInterval time.Duration
	exportTTL          time.Duration
	stepUpTTL          time.Duration
	deleteGraceWindow  time.Duration
}

type Option func(*Service)

func WithAuditRetention(days int, pruneInterval time.Duration) Option {
	return func(s *Service) {
		s.auditRetentionDays = days
		s.auditPruneInterval = pruneInterval
	}
}

func WithExportTTL(ttl time.Duration) Option {
	return func(s *Service) {
		if ttl > 0 {
			s.exportTTL = ttl
		}
	}
}

func WithStepUpTTL(ttl time.Duration) Option {
	return func(s *Service) {
		if ttl > 0 {
			s.stepUpTTL = ttl
		}
	}
}

func WithDeleteGraceWindow(window time.Duration) Option {
	return func(s *Service) {
		if window > 0 {
			s.deleteGraceWindow = window
		}
	}
}

func New(repo repo, audit auditRecorder, opts ...Option) *Service {
	svc := ptrext.Of(Service{
		repo:               repo,
		audit:              audit,
		auditRetentionDays: 365,
		auditPruneInterval: 24 * time.Hour,
		exportTTL:          DefaultExportTTL,
		stepUpTTL:          15 * time.Minute,
		deleteGraceWindow:  15 * time.Minute,
	})
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

type ExportBundle struct {
	Filename string
	Data     []byte
	Counts   gdprrepo.Counts
}

func (s *Service) Export(ctx context.Context, tenantID, rawSubjectKey string, actor auditlogsvc.Actor) (*ExportBundle, error) {
	subjectKey := strings.TrimSpace(rawSubjectKey)
	if subjectKey == "" {
		return nil, ErrInvalidSubjectKey
	}
	data, err := s.repo.Export(ctx, tenantID, subjectKey)
	if err != nil {
		return nil, translateRepoError(err)
	}
	archive, err := buildZIP(tenantID, data)
	if err != nil {
		return nil, fmt.Errorf("build export zip: %w", err)
	}
	if err := s.record(ctx, tenantID, subjectKey, actor, "gdpr.export", "Exported GDPR bundle for subject", nil, map[string]any{
		"subject_hash":         subjectkey.Hash(tenantID, subjectKey),
		"feedback_count":       data.Counts.FeedbackCount,
		"tag_assignment_count": data.Counts.TagAssignmentCount,
		"feedback_audit_count": data.Counts.FeedbackAuditCount,
		"llm_audit_count":      data.Counts.LLMAuditCount,
	}); err != nil {
		return nil, err
	}
	return ptrext.Of(ExportBundle{
		Filename: zipFilename(subjectKey, data.GeneratedAt),
		Data:     archive,
		Counts:   data.Counts,
	}), nil
}

func (s *Service) Delete(ctx context.Context, tenantID, rawSubjectKey string, actor auditlogsvc.Actor) (*gdprrepo.DeleteResult, error) {
	subjectKey := strings.TrimSpace(rawSubjectKey)
	if subjectKey == "" {
		return nil, ErrInvalidSubjectKey
	}
	subjectHash := subjectkey.Hash(tenantID, subjectKey)
	store, ok := s.repo.(requestStore)
	if !ok {
		result, err := s.repo.Delete(ctx, tenantID, subjectKey)
		if err != nil {
			return nil, translateRepoError(err)
		}
		if err := s.RecordDeleteCompletion(ctx, tenantID, subjectKey, actor, result); err != nil {
			return nil, err
		}
		return result, nil
	}
	executeAfter := time.Now().UTC().Add(s.deleteGraceWindow)
	result, err := store.CreateDeleteRequest(ctx, tenantID, subjectKey, subjectHash, actor.ID, executeAfter)
	if err != nil {
		return nil, translateRepoError(err)
	}
	if err := s.record(ctx, tenantID, subjectKey, actor, "gdpr.delete.requested", "Scheduled GDPR subject delete", nil, map[string]any{
		"request_id":           result.RequestID,
		"subject_hash":         subjectHash,
		"feedback_count":       result.Counts.FeedbackCount,
		"tag_assignment_count": result.Counts.TagAssignmentCount,
		"feedback_audit_count": result.Counts.FeedbackAuditCount,
		"llm_audit_count":      result.Counts.LLMAuditCount,
		"execute_after":        executeAfter.Format(time.RFC3339),
	}); err != nil {
		return nil, err
	}
	result.Status = gdprrepo.RequestStatusScheduled
	result.ExecuteAfter = ptrext.Of(executeAfter)
	return result, nil
}

func (s *Service) CancelDeleteRequest(ctx context.Context, tenantID, requestID string, actor auditlogsvc.Actor) error {
	store, ok := s.repo.(requestStore)
	if !ok {
		return fmt.Errorf("gdpr request store is not configured")
	}
	req, err := store.CancelDeleteRequest(ctx, tenantID, requestID)
	if err != nil {
		return err
	}
	if err := s.record(ctx, tenantID, req.SubjectKey, actor, "gdpr.delete.cancelled", "Cancelled scheduled GDPR subject delete", nil, map[string]any{
		"request_id":   req.ID,
		"subject_hash": req.SubjectHash,
	}); err != nil {
		return err
	}
	return nil
}

func (s *Service) RecordDeleteCompletion(ctx context.Context, tenantID, subjectKey string, actor auditlogsvc.Actor, result *gdprrepo.DeleteResult) error {
	subjectHash := subjectkey.Hash(tenantID, subjectKey)
	return s.record(ctx, tenantID, subjectKey, actor, "gdpr.delete", "Deleted GDPR subject data", map[string]any{
		"feedback_count":       result.Counts.FeedbackCount,
		"tag_assignment_count": result.Counts.TagAssignmentCount,
		"feedback_audit_count": result.Counts.FeedbackAuditCount,
		"llm_audit_count":      result.Counts.LLMAuditCount,
	}, map[string]any{
		"subject_hash":         subjectHash,
		"feedback_count":       result.Counts.FeedbackCount,
		"tag_assignment_count": result.Counts.TagAssignmentCount,
		"feedback_audit_count": result.Counts.FeedbackAuditCount,
		"llm_audit_count":      result.Counts.LLMAuditCount,
	})
}

var ErrInvalidSubjectKey = errors.New("subject_key is required")

func translateRepoError(err error) error {
	switch {
	case errors.Is(err, gdprrepo.ErrSubjectNotFound):
		return ErrSubjectNotFound
	case errors.Is(err, ErrInvalidSubjectKey):
		return err
	default:
		return err
	}
}

var ErrSubjectNotFound = errors.New("gdpr subject not found")

type StepUpStatus struct {
	Satisfied       bool
	PasswordAllowed bool
	Method          string
	VerifiedAt      *time.Time
	ExpiresAt       *time.Time
}

func (s *Service) ListRequests(ctx context.Context, tenantID, cursor string, limit int, requestType string) (*attunev1.ListGdprRequestsResponse, error) {
	store, ok := s.repo.(requestStore)
	if !ok {
		return nil, fmt.Errorf("gdpr request store is not configured")
	}
	result, err := store.ListRequests(ctx, gdprrepo.ListRequestFilter{
		TenantID:    tenantID,
		Cursor:      cursor,
		Limit:       limit,
		RequestType: requestType,
	})
	if err != nil {
		return nil, err
	}
	items := make([]*attunev1.GdprRequestSummary, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, requestSummaryProto(item))
	}
	resp := ptrext.Of(attunev1.ListGdprRequestsResponse{Items: items})
	if result.NextCursor != "" {
		resp.NextCursor = ptrext.Of(result.NextCursor)
	}
	return resp, nil
}

func (s *Service) GetOperations(ctx context.Context, tenantID string, stepUp StepUpStatus) (*attunev1.GdprOperationsResponse, error) {
	store, ok := s.repo.(requestStore)
	if !ok {
		return nil, fmt.Errorf("gdpr request store is not configured")
	}
	summary, err := store.GetOperationsSummary(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	resp := ptrext.Of(attunev1.GdprOperationsResponse{
		StepUp:                        stepUpStatusProto(stepUp, s.stepUpTTL),
		ExportTtlSeconds:              int32(s.exportTTL.Seconds()),
		AuditRetentionDays:            int32(s.auditRetentionDays),
		AuditPruneIntervalSeconds:     int32(s.auditPruneInterval.Seconds()),
		QueuedRequestCount:            int32(summary.QueuedRequestCount),
		ActiveRequestCount:            int32(summary.ActiveRequestCount),
		ReadyExportCount:              int32(summary.ReadyExportCount),
		HashedAuditResidue:            true,
		BackupsMayRetainUntilRotation: true,
		LegalHoldSupported:            false,
		DeleteGraceWindowSeconds:      int32(s.deleteGraceWindow.Seconds()),
		ScheduledDeleteCount:          int32(summary.ScheduledDeleteCount),
	})
	if summary.NextExportExpiryAt != nil {
		resp.NextExportExpiryAt = ptrext.Of(summary.NextExportExpiryAt.UTC().Format(time.RFC3339))
	}
	return resp, nil
}

func (s *Service) record(
	ctx context.Context,
	tenantID, subjectKey string,
	actor auditlogsvc.Actor,
	action, summary string,
	before, after any,
) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   tenantID,
		Actor:      actor,
		Action:     action,
		TargetType: "gdpr_subject",
		TargetID:   subjectkey.Hash(tenantID, subjectKey),
		Summary:    summary,
		Before:     before,
		After:      after,
	})
}

func buildZIP(tenantID string, data *gdprrepo.ExportData) ([]byte, error) {
	buf := ptrext.Of(bytes.Buffer{})
	zw := zip.NewWriter(buf)
	defer zw.Close()

	manifest := map[string]any{
		"tenant_id":       tenantID,
		"subject_key":     data.SubjectKey,
		"subject_display": data.SubjectDisplay,
		"generated_at":    data.GeneratedAt.UTC().Format(time.RFC3339),
		"schema_version":  "gdpr-export-v1",
		"counts": map[string]int{
			"feedback":           data.Counts.FeedbackCount,
			"feedback_tags":      data.Counts.TagAssignmentCount,
			"feedback_audit_log": data.Counts.FeedbackAuditCount,
			"llm_audit":          data.Counts.LLMAuditCount,
		},
	}
	if err := writeJSONFile(zw, "manifest.json", manifest); err != nil {
		return nil, err
	}
	if err := writeJSONLinesFile(zw, "feedback.jsonl", data.FeedbackRows); err != nil {
		return nil, err
	}
	if err := writeJSONLinesFile(zw, "feedback_tags.jsonl", data.FeedbackTagRows); err != nil {
		return nil, err
	}
	if err := writeJSONLinesFile(zw, "feedback_audit_log.jsonl", data.FeedbackAuditRows); err != nil {
		return nil, err
	}
	if err := writeJSONLinesFile(zw, "llm_audit.jsonl", data.LLMAuditRows); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeJSONFile(zw *zip.Writer, name string, value any) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = f.Write(append(payload, '\n'))
	return err
}

func writeJSONLinesFile(zw *zip.Writer, name string, rows []json.RawMessage) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := f.Write(row); err != nil {
			return err
		}
		if _, err := f.Write([]byte{'\n'}); err != nil {
			return err
		}
	}
	return nil
}

func zipFilename(subjectKey string, generatedAt time.Time) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", "@", "_at_")
	return fmt.Sprintf("gdpr-export-%s-%s.zip", replacer.Replace(subjectKey), generatedAt.UTC().Format("20060102-150405"))
}

func requestSummaryProto(item gdprrepo.Request) *attunev1.GdprRequestSummary {
	resp := ptrext.Of(attunev1.GdprRequestSummary{
		RequestId:          item.ID,
		RequestType:        requestTypeProto(item.RequestType),
		Status:             requestStatusProto(item.Status),
		SubjectKey:         item.SubjectKey,
		SubjectDisplay:     item.SubjectDisplay,
		CreatedBy:          item.CreatedBy,
		FeedbackCount:      int32(item.Counts.FeedbackCount),
		TagAssignmentCount: int32(item.Counts.TagAssignmentCount),
		FeedbackAuditCount: int32(item.Counts.FeedbackAuditCount),
		LlmAuditCount:      int32(item.Counts.LLMAuditCount),
		CreatedAt:          item.CreatedAt.UTC().Format(time.RFC3339),
	})
	if item.StartedAt != nil {
		resp.StartedAt = ptrext.Of(item.StartedAt.UTC().Format(time.RFC3339))
	}
	if item.CompletedAt != nil {
		resp.CompletedAt = ptrext.Of(item.CompletedAt.UTC().Format(time.RFC3339))
	}
	if item.ExpiresAt != nil {
		resp.ExpiresAt = ptrext.Of(item.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if item.ExecuteAfter != nil {
		resp.ExecuteAfter = ptrext.Of(item.ExecuteAfter.UTC().Format(time.RFC3339))
	}
	if item.DownloadedAt != nil {
		resp.DownloadedAt = ptrext.Of(item.DownloadedAt.UTC().Format(time.RFC3339))
	}
	if item.CancelledAt != nil {
		resp.CancelledAt = ptrext.Of(item.CancelledAt.UTC().Format(time.RFC3339))
	}
	if item.RevokedAt != nil {
		resp.RevokedAt = ptrext.Of(item.RevokedAt.UTC().Format(time.RFC3339))
	}
	if item.ArchiveFilename != "" {
		resp.ArchiveFilename = ptrext.Of(item.ArchiveFilename)
	}
	if item.Error != "" {
		resp.Error = ptrext.Of(item.Error)
	}
	return resp
}

func requestTypeProto(kind gdprrepo.RequestType) attunev1.GdprRequestType {
	switch kind {
	case gdprrepo.RequestTypeExport:
		return attunev1.GdprRequestType_GDPR_REQUEST_TYPE_EXPORT
	case gdprrepo.RequestTypeDelete:
		return attunev1.GdprRequestType_GDPR_REQUEST_TYPE_DELETE
	default:
		return attunev1.GdprRequestType_GDPR_REQUEST_TYPE_UNSPECIFIED
	}
}

func requestStatusProto(status gdprrepo.RequestStatus) attunev1.GdprRequestStatus {
	switch status {
	case gdprrepo.RequestStatusQueued:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_QUEUED
	case gdprrepo.RequestStatusRunning:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_RUNNING
	case gdprrepo.RequestStatusReady:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_READY
	case gdprrepo.RequestStatusDownloaded:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_DOWNLOADED
	case gdprrepo.RequestStatusCompleted:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_COMPLETED
	case gdprrepo.RequestStatusFailed:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_FAILED
	case gdprrepo.RequestStatusExpired:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_EXPIRED
	case gdprrepo.RequestStatusScheduled:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_SCHEDULED
	case gdprrepo.RequestStatusCancelled:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_CANCELLED
	case gdprrepo.RequestStatusRevoked:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_REVOKED
	default:
		return attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_UNSPECIFIED
	}
}

func stepUpStatusProto(status StepUpStatus, ttl time.Duration) *attunev1.GdprStepUpStatus {
	resp := ptrext.Of(attunev1.GdprStepUpStatus{
		Satisfied:       status.Satisfied,
		PasswordAllowed: status.PasswordAllowed,
		Method:          status.Method,
		TtlSeconds:      int32(ttl.Seconds()),
	})
	if status.VerifiedAt != nil {
		resp.VerifiedAt = ptrext.Of(status.VerifiedAt.UTC().Format(time.RFC3339))
	}
	if status.ExpiresAt != nil {
		resp.ExpiresAt = ptrext.Of(status.ExpiresAt.UTC().Format(time.RFC3339))
	}
	return resp
}
