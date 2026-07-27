// SPDX-License-Identifier: Apache-2.0

// Package cohortsync coordinates cohort source management, membership delta
// application, sync run recording, and stale TTL enforcement.
package cohortsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/cohortsync"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/cohortsync"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// ErrValidation is returned for invalid input.
var ErrValidation = errors.New("cohort sync validation failed")

// Store is the credential encrypt/decrypt interface.
type Store interface {
	EncryptValue(plaintext, aad []byte) (secretstore.EncryptedValue, error)
	DecryptValue(value secretstore.EncryptedValue, aad []byte) ([]byte, error)
}

// Repo is the consumer-defined persistence interface.
type Repo interface {
	CreateSource(ctx context.Context, in repo.Source) (*repo.Source, error)
	GetSource(ctx context.Context, tenantID string, id uuid.UUID) (*repo.Source, error)
	ListSources(ctx context.Context, tenantID string) ([]repo.Source, error)
	UpdateSource(ctx context.Context, in repo.Source) (*repo.Source, error)
	DeleteSource(ctx context.Context, tenantID string, id uuid.UUID) error
	UpdateSourceSyncStatus(ctx context.Context, tenantID string, id uuid.UUID, lastError string) error
	UpsertCohort(ctx context.Context, in repo.Cohort) (*repo.Cohort, error)
	GetCohort(ctx context.Context, tenantID string, id uuid.UUID) (*repo.Cohort, error)
	GetCohortByExternalID(ctx context.Context, tenantID string, sourceID uuid.UUID, externalCohortID string) (*repo.Cohort, error)
	ListCohorts(ctx context.Context, tenantID string, sourceID uuid.UUID) ([]repo.Cohort, error)
	ListAllCohorts(ctx context.Context, tenantID string) ([]repo.Cohort, error)
	UpdateCohort(ctx context.Context, in repo.Cohort) (*repo.Cohort, error)
	UpdateCohortSyncResult(ctx context.Context, tenantID string, cohortID uuid.UUID, memberCount int, lastError string) error
	UpsertMemberships(ctx context.Context, tenantID string, cohortID uuid.UUID, members []repo.MembershipUpsert) (touched int, err error)
	MarkDeparted(ctx context.Context, tenantID string, cohortID uuid.UUID, staleTTLDays int, olderThan time.Time) (int64, error)
	MarkMembersDeparted(ctx context.Context, tenantID string, cohortID uuid.UUID, staleTTLDays int, externalUserIDs []string) (int64, error)
	CleanExpired(ctx context.Context) (int64, error)
	CountActiveMembers(ctx context.Context, tenantID string, cohortID uuid.UUID) (int, error)
	InsertRun(ctx context.Context, run repo.SyncRun) (*repo.SyncRun, error)
	FinishRun(ctx context.Context, id uuid.UUID, status string, added, removed, total int, errorMessage string) error
	ListRuns(ctx context.Context, tenantID string, cohortID uuid.UUID, limit int) ([]repo.SyncRun, error)
	HasRunningRun(ctx context.Context, tenantID string, cohortID uuid.UUID) (bool, error)
}

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

// Actor identifies who performed an action.
type Actor struct {
	Type string
	ID   string
}

// Service is the cohort sync orchestration layer.
type Service struct {
	repo  Repo
	store Store
	audit auditRecorder
}

// New builds a cohort sync service.
func New(repo Repo, store Store) *Service {
	return ptrext.Of(Service{repo: repo, store: store})
}

// SetAuditLogger wires the audit recorder.
func (s *Service) SetAuditLogger(audit auditRecorder) {
	s.audit = audit
}

// ---------- Source CRUD ----------

// CreateSourceInput is the input for creating a cohort source.
type CreateSourceInput struct {
	TenantID       string
	Provider       string
	Name           string
	AuthType       string
	Credential     string
	WebhookSecret  string
	BaseURL        string
	ProviderConfig string
	Enabled        bool
	Actor          Actor
	AuditActor     auditlogsvc.Actor
}

// CreateSource creates and encrypts a new cohort source.
func (s *Service) CreateSource(ctx context.Context, in CreateSourceInput) (*repo.Source, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Provider = strings.TrimSpace(strings.ToLower(in.Provider))
	in.Name = strings.TrimSpace(in.Name)
	in.AuthType = strings.TrimSpace(strings.ToLower(in.AuthType))
	in.Credential = strings.TrimSpace(in.Credential)
	in.BaseURL = strings.TrimSpace(in.BaseURL)

	if err := validateSourceShape(in.Provider, in.Name, in.AuthType); err != nil {
		return nil, err
	}
	if in.Credential == "" {
		return nil, fmt.Errorf("%w: credential is required", ErrValidation)
	}
	if in.BaseURL != "" {
		if err := cohortsync.ValidateProviderURL(in.BaseURL); err != nil {
			return nil, fmt.Errorf("%w: base_url: %s", ErrValidation, err.Error())
		}
	}
	cfg, err := normalizeJSONObject(in.ProviderConfig)
	if err != nil {
		return nil, err
	}

	id := uuid.New()
	encrypted, err := s.store.EncryptValue([]byte(in.Credential), sourceAAD(in.TenantID, id, in.Provider))
	if err != nil {
		return nil, fmt.Errorf("encrypt cohort credential: %w", err)
	}

	var webhookSecret secretstore.EncryptedValue
	if ws := strings.TrimSpace(in.WebhookSecret); ws != "" {
		webhookSecret, err = s.store.EncryptValue([]byte(ws), sourceWebhookAAD(in.TenantID, id, in.Provider))
		if err != nil {
			return nil, fmt.Errorf("encrypt cohort webhook secret: %w", err)
		}
	}

	row, err := s.repo.CreateSource(ctx, repo.Source{
		ID:                      id,
		TenantID:                in.TenantID,
		Provider:                in.Provider,
		Name:                    in.Name,
		AuthType:                in.AuthType,
		CredentialKeyID:         encrypted.KeyID,
		CredentialCiphertext:    encrypted.Ciphertext,
		BaseURL:                 in.BaseURL,
		ProviderConfig:          []byte(cfg),
		WebhookSecretKeyID:      webhookSecret.KeyID,
		WebhookSecretCiphertext: webhookSecret.Ciphertext,
		Enabled:                 in.Enabled,
		Status:                  sourceStatus(in.Enabled),
		CreatedBy:               in.Actor.ID,
		UpdatedBy:               in.Actor.ID,
	})
	if err != nil {
		return nil, err
	}
	s.record(ctx, in.AuditActor, in.TenantID, "cohort_source.create",
		"cohort_source", row.ID.String(), "Created cohort source", nil, sourceAudit(row))
	return row, nil
}

// GetSource retrieves a cohort source.
func (s *Service) GetSource(ctx context.Context, tenantID string, id uuid.UUID) (*repo.Source, error) {
	return s.repo.GetSource(ctx, tenantID, id)
}

// ListSources lists cohort sources for a tenant.
func (s *Service) ListSources(ctx context.Context, tenantID string) ([]repo.Source, error) {
	return s.repo.ListSources(ctx, tenantID)
}

// DeleteSource deletes a cohort source.
func (s *Service) DeleteSource(ctx context.Context, tenantID string, id uuid.UUID, actor Actor, auditActor auditlogsvc.Actor) error {
	before, err := s.repo.GetSource(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteSource(ctx, tenantID, id); err != nil {
		return err
	}
	s.record(ctx, auditActor, tenantID, "cohort_source.delete",
		"cohort_source", id.String(), "Deleted cohort source", sourceAudit(before), nil)
	return nil
}

// DecryptCredential decrypts the stored credential for a source.
func (s *Service) DecryptCredential(source repo.Source) ([]byte, error) {
	return s.store.DecryptValue(secretstore.EncryptedValue{
		KeyID:      source.CredentialKeyID,
		Ciphertext: source.CredentialCiphertext,
	}, sourceAAD(source.TenantID, source.ID, source.Provider))
}

// ---------- Cohort management ----------

// ListCohorts lists cohorts for a source.
func (s *Service) ListCohorts(ctx context.Context, tenantID string, sourceID uuid.UUID) ([]repo.Cohort, error) {
	return s.repo.ListCohorts(ctx, tenantID, sourceID)
}

// ListAllCohorts lists all cohorts across all sources for a tenant.
func (s *Service) ListAllCohorts(ctx context.Context, tenantID string) ([]repo.Cohort, error) {
	return s.repo.ListAllCohorts(ctx, tenantID)
}

// UpdateCohortInput is the input for updating a cohort.
type UpdateCohortInput struct {
	TenantID     string
	ID           uuid.UUID
	Name         *string
	Description  *string
	StaleTTLDays *int
	Enabled      *bool
	Actor        Actor
	AuditActor   auditlogsvc.Actor
}

// UpdateCohort updates mutable cohort fields.
func (s *Service) UpdateCohort(ctx context.Context, in UpdateCohortInput) (*repo.Cohort, error) {
	current, err := s.repo.GetCohort(ctx, in.TenantID, in.ID)
	if err != nil {
		return nil, err
	}
	next := ptrext.Indirect(current)
	if in.Name != nil {
		next.Name = strings.TrimSpace(ptrext.Indirect(in.Name))
	}
	if in.Description != nil {
		next.Description = strings.TrimSpace(ptrext.Indirect(in.Description))
	}
	if in.StaleTTLDays != nil {
		next.StaleTTLDays = ptrext.Indirect(in.StaleTTLDays)
	}
	if in.Enabled != nil {
		next.Enabled = ptrext.Indirect(in.Enabled)
	}
	if next.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	if next.StaleTTLDays < 1 || next.StaleTTLDays > 365 {
		return nil, fmt.Errorf("%w: stale_ttl_days must be 1..365", ErrValidation)
	}
	updated, err := s.repo.UpdateCohort(ctx, next)
	if err != nil {
		return nil, err
	}
	s.record(ctx, in.AuditActor, in.TenantID, "cohort.update",
		"cohort", updated.ID.String(), "Updated cohort", cohortAudit(current), cohortAudit(updated))
	return updated, nil
}

// ---------- Delta application ----------

// SyncRunResult captures the outcome of a sync operation.
type SyncRunResult struct {
	Run     repo.SyncRun
	Cohort  repo.Cohort
	Added   int
	Removed int
}

// ApplyDelta processes an incremental membership update from a provider webhook.
func (s *Service) ApplyDelta(ctx context.Context, tenantID string, sourceID uuid.UUID, payload cohortsync.SyncPayload) (*SyncRunResult, error) {
	source, err := s.repo.GetSource(ctx, tenantID, sourceID)
	if err != nil {
		return nil, err
	}
	if !source.Enabled {
		return s.skipDisabledRun(ctx, tenantID, sourceID, payload)
	}

	cohort, err := s.ensureCohort(ctx, tenantID, sourceID, payload)
	if err != nil {
		return nil, err
	}
	if !cohort.Enabled {
		return s.skipDisabledCohortRun(ctx, tenantID, cohort, payload)
	}

	run, err := s.repo.InsertRun(ctx, repo.SyncRun{
		ID:       uuid.New(),
		TenantID: tenantID,
		CohortID: cohort.ID,
		Trigger:  "webhook",
		Status:   "running",
	})
	if err != nil {
		return nil, err
	}

	adds, removes := splitDeltas(payload.Deltas)
	added, err := s.repo.UpsertMemberships(ctx, tenantID, cohort.ID, adds)
	if err != nil {
		return nil, s.failRun(ctx, run.ID, err)
	}

	var removed int64
	if len(removes) > 0 {
		removeIDs := make([]string, 0, len(removes))
		for _, rm := range removes {
			removeIDs = append(removeIDs, rm.ExternalUserID)
		}
		removed, err = s.repo.MarkMembersDeparted(ctx, tenantID, cohort.ID, cohort.StaleTTLDays, removeIDs)
		if err != nil {
			return nil, s.failRun(ctx, run.ID, err)
		}
	}

	memberCount, err := s.repo.CountActiveMembers(ctx, tenantID, cohort.ID)
	if err != nil {
		return nil, s.failRun(ctx, run.ID, err)
	}
	if err := s.repo.UpdateCohortSyncResult(ctx, tenantID, cohort.ID, memberCount, ""); err != nil {
		logext.Warnf(ctx, "[cohortsync.ApplyDelta] update cohort sync result failed,err:%s", err.Error())
	}
	if err := s.repo.UpdateSourceSyncStatus(ctx, tenantID, sourceID, ""); err != nil {
		logext.Warnf(ctx, "[cohortsync.ApplyDelta] update source sync status failed,err:%s", err.Error())
	}

	finishErr := s.repo.FinishRun(ctx, run.ID, "succeeded", added, int(removed), memberCount, "")
	if finishErr != nil {
		logext.Warnf(ctx, "[cohortsync.ApplyDelta] finish run failed,err:%s", finishErr.Error())
	}

	recordSyncMetrics(source.Provider, "webhook", "succeeded", added, int(removed), memberCount)

	return ptrext.Of(SyncRunResult{
		Run:     ptrext.Indirect(run),
		Cohort:  ptrext.Indirect(cohort),
		Added:   added,
		Removed: int(removed),
	}), nil
}

// ApplyFullSnapshot processes a full membership snapshot (Mixpanel "members" action).
func (s *Service) ApplyFullSnapshot(ctx context.Context, tenantID string, sourceID uuid.UUID, payload cohortsync.SyncPayload) (*SyncRunResult, error) {
	source, err := s.repo.GetSource(ctx, tenantID, sourceID)
	if err != nil {
		return nil, err
	}
	if !source.Enabled {
		return s.skipDisabledRun(ctx, tenantID, sourceID, payload)
	}

	cohort, err := s.ensureCohort(ctx, tenantID, sourceID, payload)
	if err != nil {
		return nil, err
	}
	if !cohort.Enabled {
		return s.skipDisabledCohortRun(ctx, tenantID, cohort, payload)
	}

	running, err := s.repo.HasRunningRun(ctx, tenantID, cohort.ID)
	if err != nil {
		return nil, err
	}
	if running {
		return nil, fmt.Errorf("%w: a sync is already running for this cohort", repo.ErrConflict)
	}

	run, err := s.repo.InsertRun(ctx, repo.SyncRun{
		ID:       uuid.New(),
		TenantID: tenantID,
		CohortID: cohort.ID,
		Trigger:  "webhook",
		Status:   "running",
	})
	if err != nil {
		return nil, err
	}

	adds, _ := splitDeltas(payload.Deltas)
	added, err := s.repo.UpsertMemberships(ctx, tenantID, cohort.ID, adds)
	if err != nil {
		return nil, s.failRun(ctx, run.ID, err)
	}

	// Reconciliation: mark members not seen in this snapshot as departed.
	removed, err := s.repo.MarkDeparted(ctx, tenantID, cohort.ID, cohort.StaleTTLDays, run.StartedAt)
	if err != nil {
		return nil, s.failRun(ctx, run.ID, err)
	}

	memberCount, err := s.repo.CountActiveMembers(ctx, tenantID, cohort.ID)
	if err != nil {
		return nil, s.failRun(ctx, run.ID, err)
	}
	if err := s.repo.UpdateCohortSyncResult(ctx, tenantID, cohort.ID, memberCount, ""); err != nil {
		logext.Warnf(ctx, "[cohortsync.ApplyFullSnapshot] update cohort sync result failed,err:%s", err.Error())
	}
	if err := s.repo.UpdateSourceSyncStatus(ctx, tenantID, sourceID, ""); err != nil {
		logext.Warnf(ctx, "[cohortsync.ApplyFullSnapshot] update source sync status failed,err:%s", err.Error())
	}

	finishErr := s.repo.FinishRun(ctx, run.ID, "succeeded", added, int(removed), memberCount, "")
	if finishErr != nil {
		logext.Warnf(ctx, "[cohortsync.ApplyFullSnapshot] finish run failed,err:%s", finishErr.Error())
	}

	recordSyncMetrics(source.Provider, "webhook", "succeeded", added, int(removed), memberCount)

	return ptrext.Of(SyncRunResult{
		Run:     ptrext.Indirect(run),
		Cohort:  ptrext.Indirect(cohort),
		Added:   added,
		Removed: int(removed),
	}), nil
}

// SyncNow triggers an on-demand pull for a cohort.
func (s *Service) SyncNow(ctx context.Context, tenantID string, cohortID uuid.UUID, actor Actor, auditActor auditlogsvc.Actor) (*SyncRunResult, error) {
	cohort, err := s.repo.GetCohort(ctx, tenantID, cohortID)
	if err != nil {
		return nil, err
	}
	source, err := s.repo.GetSource(ctx, tenantID, cohort.CohortSourceID)
	if err != nil {
		return nil, err
	}

	running, err := s.repo.HasRunningRun(ctx, tenantID, cohortID)
	if err != nil {
		return nil, err
	}
	if running {
		return nil, fmt.Errorf("%w: a sync is already running for this cohort", repo.ErrConflict)
	}

	provider, ok := cohortsync.Lookup(source.Provider)
	if !ok {
		return nil, cohortsync.UnavailableError(source.Provider)
	}

	credential, err := s.DecryptCredential(ptrext.Indirect(source))
	if err != nil {
		return nil, fmt.Errorf("decrypt cohort credential: %w", err)
	}

	payload, err := provider.PullCohort(ctx, cohortsync.Connection{
		ID:             source.ID.String(),
		TenantID:       source.TenantID,
		Provider:       source.Provider,
		Name:           source.Name,
		AuthType:       source.AuthType,
		BaseURL:        source.BaseURL,
		ProviderConfig: source.ProviderConfig,
		Credential:     credential,
	}, cohort.ExternalCohortID)
	if err != nil {
		errMsg := redact(err.Error())
		_ = s.repo.UpdateSourceSyncStatus(ctx, tenantID, source.ID, errMsg)
		return nil, err
	}

	result, err := s.ApplyFullSnapshot(ctx, tenantID, source.ID, payload)
	if err != nil {
		return nil, err
	}

	s.record(ctx, auditActor, tenantID, "cohort.sync",
		"cohort", cohortID.String(), "Manual cohort sync triggered", nil,
		map[string]any{"added": result.Added, "removed": result.Removed, "actor": actor.ID})

	return result, nil
}

// CleanExpired removes memberships whose TTL has passed.
func (s *Service) CleanExpired(ctx context.Context) (int64, error) {
	return s.repo.CleanExpired(ctx)
}

// ListRuns returns sync runs for a cohort.
func (s *Service) ListRuns(ctx context.Context, tenantID string, cohortID uuid.UUID, limit int) ([]repo.SyncRun, error) {
	return s.repo.ListRuns(ctx, tenantID, cohortID, limit)
}

// ---------- helpers ----------

func (s *Service) ensureCohort(ctx context.Context, tenantID string, sourceID uuid.UUID, payload cohortsync.SyncPayload) (*repo.Cohort, error) {
	cohort, err := s.repo.GetCohortByExternalID(ctx, tenantID, sourceID, payload.ExternalCohortID)
	if err == nil {
		return cohort, nil
	}
	if !errors.Is(err, repo.ErrCohortNotFound) {
		return nil, err
	}
	name := strings.TrimSpace(payload.CohortName)
	if name == "" {
		name = payload.ExternalCohortID
	}
	return s.repo.UpsertCohort(ctx, repo.Cohort{
		ID:               uuid.New(),
		TenantID:         tenantID,
		CohortSourceID:   sourceID,
		ExternalCohortID: payload.ExternalCohortID,
		Name:             name,
		StaleTTLDays:     30,
		Enabled:          true,
	})
}

func (s *Service) skipDisabledRun(ctx context.Context, tenantID string, sourceID uuid.UUID, payload cohortsync.SyncPayload) (*SyncRunResult, error) {
	cohort, err := s.ensureCohort(ctx, tenantID, sourceID, payload)
	if err != nil {
		return nil, err
	}
	return s.skipDisabledCohortRun(ctx, tenantID, cohort, payload)
}

func (s *Service) skipDisabledCohortRun(ctx context.Context, tenantID string, cohort *repo.Cohort, _ cohortsync.SyncPayload) (*SyncRunResult, error) {
	logext.Warnf(ctx, "[cohortsync] skipping sync for disabled cohort,tenant_id:%s,cohort_id:%s", tenantID, cohort.ID.String())
	run, err := s.repo.InsertRun(ctx, repo.SyncRun{
		ID:       uuid.New(),
		TenantID: tenantID,
		CohortID: cohort.ID,
		Trigger:  "webhook",
		Status:   "running",
	})
	if err != nil {
		return nil, err
	}
	_ = s.repo.FinishRun(ctx, run.ID, "skipped", 0, 0, cohort.MemberCount, "cohort or source is disabled")
	return ptrext.Of(SyncRunResult{Run: ptrext.Indirect(run), Cohort: ptrext.Indirect(cohort)}), nil
}

func (s *Service) failRun(ctx context.Context, runID uuid.UUID, cause error) error {
	msg := cause.Error()
	if utf8.RuneCountInString(msg) > 2000 {
		runes := []rune(msg)
		msg = string(runes[:2000])
	}
	finishErr := s.repo.FinishRun(ctx, runID, "failed", 0, 0, 0, msg)
	if finishErr != nil {
		logext.Warnf(ctx, "[cohortsync] finish run (fail) failed,err:%s", finishErr.Error())
	}
	return cause
}

func splitDeltas(deltas []cohortsync.MemberDelta) (adds []repo.MembershipUpsert, removes []repo.MembershipUpsert) {
	for _, d := range deltas {
		m := repo.MembershipUpsert{
			ExternalUserID: d.ExternalUserID,
			Email:          d.Email,
			DisplayName:    d.DisplayName,
		}
		if len(d.Properties) > 0 {
			if raw, err := json.Marshal(d.Properties); err == nil {
				m.UserProperties = raw
			}
		}
		switch d.Action {
		case "remove":
			removes = append(removes, m)
		default:
			adds = append(adds, m)
		}
	}
	return adds, removes
}

func validateSourceShape(provider, name, authType string) error {
	if err := cohortsync.ValidateProviderToken(provider); err != nil {
		return fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if name == "" || len(name) > 200 || !utf8.ValidString(name) {
		return fmt.Errorf("%w: name must be 1..200 valid UTF-8 bytes", ErrValidation)
	}
	switch authType {
	case "api_key", "token", "basic":
	default:
		return fmt.Errorf("%w: invalid auth_type", ErrValidation)
	}
	return nil
}

func normalizeJSONObject(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}", nil
	}
	if len(raw) > 32*1024 {
		return "", fmt.Errorf("%w: provider_config is too large", ErrValidation)
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil { // ptrext:allow unmarshal-out-param
		return "", fmt.Errorf("%w: provider_config must be JSON", ErrValidation)
	}
	if _, ok := v.(map[string]any); !ok {
		return "", fmt.Errorf("%w: provider_config must be a JSON object", ErrValidation)
	}
	return raw, nil
}

func sourceStatus(enabled bool) string {
	if enabled {
		return "active"
	}
	return "disabled"
}

func sourceAAD(tenantID string, id uuid.UUID, provider string) []byte {
	return []byte("cohort_sources:" + tenantID + ":" + id.String() + ":" + provider)
}

func sourceWebhookAAD(tenantID string, id uuid.UUID, provider string) []byte {
	return []byte("cohort_sources:" + tenantID + ":" + id.String() + ":" + provider + ":webhook_secret")
}

func (s *Service) record(ctx context.Context, actor auditlogsvc.Actor, tenantID, action, targetType, targetID, summary string, before, after any) {
	if s.audit == nil {
		return
	}
	if actor.Type == "" {
		actor.Type = "admin"
	}
	_ = s.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   tenantID,
		Actor:      actor,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Summary:    summary,
		Before:     before,
		After:      after,
	})
}

func sourceAudit(src *repo.Source) map[string]any {
	if src == nil {
		return nil
	}
	return map[string]any{
		"id":       src.ID.String(),
		"provider": src.Provider,
		"name":     src.Name,
		"enabled":  src.Enabled,
		"status":   src.Status,
		"base_url": src.BaseURL,
	}
}

func recordSyncMetrics(provider, trigger, status string, added, removed, activeMembers int) {
	metrics.CohortSyncRunsTotal.WithLabelValues(provider, trigger, status).Inc()
	if added > 0 {
		metrics.CohortSyncMembersChangedTotal.WithLabelValues(provider, "add").Add(float64(added))
	}
	if removed > 0 {
		metrics.CohortSyncMembersChangedTotal.WithLabelValues(provider, "remove").Add(float64(removed))
	}
	metrics.CohortSyncActiveMembers.WithLabelValues(provider).Set(float64(activeMembers))
}

var urlPattern = regexp.MustCompile(`https?://[^\s"'<>)]+`)

// redact replaces URLs in error messages with their redacted form to prevent
// accidental credential leakage (e.g. base_url with query-string tokens).
func redact(s string) string {
	s = strings.TrimSpace(s)
	return urlPattern.ReplaceAllStringFunc(s, func(raw string) string {
		suffix := ""
		for len(raw) > 0 && strings.ContainsAny(raw[len(raw)-1:], ".,;:!?") {
			suffix = raw[len(raw)-1:] + suffix
			raw = raw[:len(raw)-1]
		}
		return nethardening.RedactURL(raw) + suffix
	})
}

func cohortAudit(c *repo.Cohort) map[string]any {
	if c == nil {
		return nil
	}
	return map[string]any{
		"id":                 c.ID.String(),
		"name":               c.Name,
		"external_cohort_id": c.ExternalCohortID,
		"enabled":            c.Enabled,
		"stale_ttl_days":     c.StaleTTLDays,
		"member_count":       c.MemberCount,
	}
}
