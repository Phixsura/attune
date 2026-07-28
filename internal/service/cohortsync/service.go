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
	RecoverStaleRuns(ctx context.Context, timeout time.Duration) (int64, error)
	CountActiveMembers(ctx context.Context, tenantID string, cohortID uuid.UUID) (int, error)
	InsertRun(ctx context.Context, run repo.SyncRun) (*repo.SyncRun, error)
	InsertExclusiveRun(ctx context.Context, run repo.SyncRun) (*repo.SyncRun, error)
	FinishRun(ctx context.Context, id uuid.UUID, status string, added, removed, total int, errorMessage string) error
	ListRuns(ctx context.Context, tenantID string, cohortID uuid.UUID, limit int) ([]repo.SyncRun, error)
	HasRunningRun(ctx context.Context, tenantID string, cohortID uuid.UUID) (bool, error)
	ApplyMembershipDelta(ctx context.Context, in repo.ApplyInput) (repo.ApplyResult, error)
	RecordEvent(ctx context.Context, in repo.SyncEvent) (*repo.SyncEvent, error)
	UpdateEventStatus(ctx context.Context, id uuid.UUID, status string, runID *uuid.UUID, failureReason string) error
	ListEvents(ctx context.Context, tenantID string, sourceID uuid.UUID, limit int) ([]repo.SyncEvent, error)
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
	PullCredential string // Provider's API key + secret (e.g. "api_key:secret_key")
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

	var pullCred secretstore.EncryptedValue
	if pc := strings.TrimSpace(in.PullCredential); pc != "" {
		pullCred, err = s.store.EncryptValue([]byte(pc), sourcePullAAD(in.TenantID, id, in.Provider))
		if err != nil {
			return nil, fmt.Errorf("encrypt cohort pull credential: %w", err)
		}
	}

	row, err := s.repo.CreateSource(ctx, repo.Source{
		ID:                       id,
		TenantID:                 in.TenantID,
		Provider:                 in.Provider,
		Name:                     in.Name,
		AuthType:                 in.AuthType,
		CredentialKeyID:          encrypted.KeyID,
		CredentialCiphertext:     encrypted.Ciphertext,
		BaseURL:                  in.BaseURL,
		ProviderConfig:           []byte(cfg),
		WebhookSecretKeyID:       webhookSecret.KeyID,
		WebhookSecretCiphertext:  webhookSecret.Ciphertext,
		PullCredentialKeyID:      pullCred.KeyID,
		PullCredentialCiphertext: pullCred.Ciphertext,
		Enabled:                  in.Enabled,
		Status:                   sourceStatus(in.Enabled),
		CreatedBy:                in.Actor.ID,
		UpdatedBy:                in.Actor.ID,
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

// UpdateSourceInput is the input for updating a cohort source.
type UpdateSourceInput struct {
	TenantID       string
	ID             uuid.UUID
	Name           *string
	Enabled        *bool
	Credential     *string
	PullCredential *string
	BaseURL        *string
	ProviderConfig *string
	Actor          Actor
	AuditActor     auditlogsvc.Actor
}

// UpdateSource updates mutable fields of a cohort source.
func (s *Service) UpdateSource(ctx context.Context, in UpdateSourceInput) (*repo.Source, error) {
	current, err := s.repo.GetSource(ctx, in.TenantID, in.ID)
	if err != nil {
		return nil, err
	}
	next := current // keep pointer
	applyScalarFields(in, next)
	if err := s.applySourceConfig(in, next); err != nil {
		return nil, err
	}
	if err := s.applySourceCredentials(in, next); err != nil {
		return nil, err
	}
	if next.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	next.UpdatedBy = in.Actor.ID
	updated, err := s.repo.UpdateSource(ctx, ptrext.Indirect(next))
	if err != nil {
		return nil, err
	}
	s.record(ctx, in.AuditActor, in.TenantID, "cohort_source.update",
		"cohort_source", updated.ID.String(), "Updated cohort source",
		sourceAudit(current), sourceAudit(updated))
	return updated, nil
}

func applyScalarFields(in UpdateSourceInput, next *repo.Source) { // ptrext:allow mutating-helper
	if in.Name != nil {
		next.Name = strings.TrimSpace(ptrext.Indirect(in.Name))
	}
	if in.Enabled != nil {
		next.Enabled = ptrext.Indirect(in.Enabled)
		next.Status = sourceStatus(next.Enabled)
	}
}

func (s *Service) applySourceConfig(in UpdateSourceInput, next *repo.Source) error { // ptrext:allow mutating-helper
	if in.BaseURL != nil {
		next.BaseURL = strings.TrimSpace(ptrext.Indirect(in.BaseURL))
		if next.BaseURL != "" {
			if err := cohortsync.ValidateProviderURL(next.BaseURL); err != nil {
				return fmt.Errorf("%w: base_url: %s", ErrValidation, err.Error())
			}
		}
	}
	if in.ProviderConfig != nil {
		cfg, err := normalizeJSONObject(ptrext.Indirect(in.ProviderConfig))
		if err != nil {
			return err
		}
		next.ProviderConfig = []byte(cfg)
	}
	return nil
}

func (s *Service) applySourceCredentials(in UpdateSourceInput, next *repo.Source) error { // ptrext:allow mutating-helper
	if in.Credential != nil {
		cred := strings.TrimSpace(ptrext.Indirect(in.Credential))
		if cred == "" {
			return fmt.Errorf("%w: credential is required", ErrValidation)
		}
		encrypted, err := s.store.EncryptValue([]byte(cred), sourceAAD(next.TenantID, next.ID, next.Provider))
		if err != nil {
			return fmt.Errorf("encrypt cohort credential: %w", err)
		}
		next.CredentialKeyID = encrypted.KeyID
		next.CredentialCiphertext = encrypted.Ciphertext
	}
	if in.PullCredential != nil {
		pc := strings.TrimSpace(ptrext.Indirect(in.PullCredential))
		if pc != "" {
			enc, err := s.store.EncryptValue([]byte(pc), sourcePullAAD(next.TenantID, next.ID, next.Provider))
			if err != nil {
				return fmt.Errorf("encrypt pull credential: %w", err)
			}
			next.PullCredentialKeyID = enc.KeyID
			next.PullCredentialCiphertext = enc.Ciphertext
		} else {
			next.PullCredentialKeyID = ""
			next.PullCredentialCiphertext = nil
		}
	}
	return nil
}

// TestSource verifies connectivity to the provider by calling the adapter's Check method.
func (s *Service) TestSource(ctx context.Context, tenantID string, id uuid.UUID, auditActor auditlogsvc.Actor) (cohortsync.CheckResult, error) {
	source, err := s.repo.GetSource(ctx, tenantID, id)
	if err != nil {
		return cohortsync.CheckResult{}, err
	}
	provider, ok := cohortsync.Lookup(source.Provider)
	if !ok {
		result := cohortsync.CheckResult{OK: false, Error: cohortsync.UnavailableError(source.Provider).Error()}
		return result, cohortsync.UnavailableError(source.Provider)
	}
	pullCred, err := s.DecryptPullCredential(ptrext.Indirect(source))
	if err != nil {
		return cohortsync.CheckResult{OK: false, Error: "pull credential not configured"}, nil
	}
	result, checkErr := provider.Check(ctx, cohortsync.Connection{
		ID:             source.ID.String(),
		TenantID:       source.TenantID,
		Provider:       source.Provider,
		Name:           source.Name,
		AuthType:       source.AuthType,
		BaseURL:        source.BaseURL,
		ProviderConfig: source.ProviderConfig,
		Credential:     pullCred,
	})
	if checkErr != nil && result.Error == "" {
		result.Error = redact(checkErr.Error())
	}
	s.record(ctx, auditActor, tenantID, "cohort_source.update",
		"cohort_source", id.String(), "Tested cohort source", nil,
		map[string]any{"provider": source.Provider, "ok": result.OK, "error": result.Error})
	return result, checkErr
}

// DecryptCredential decrypts the stored credential for a source.
func (s *Service) DecryptCredential(source repo.Source) ([]byte, error) {
	return s.store.DecryptValue(secretstore.EncryptedValue{
		KeyID:      source.CredentialKeyID,
		Ciphertext: source.CredentialCiphertext,
	}, sourceAAD(source.TenantID, source.ID, source.Provider))
}

// DecryptPullCredential decrypts the pull credential (provider API key + secret).
func (s *Service) DecryptPullCredential(source repo.Source) ([]byte, error) {
	if source.PullCredentialKeyID == "" || len(source.PullCredentialCiphertext) == 0 {
		return nil, fmt.Errorf("%w: pull credential is not configured", ErrValidation)
	}
	return s.store.DecryptValue(secretstore.EncryptedValue{
		KeyID:      source.PullCredentialKeyID,
		Ciphertext: source.PullCredentialCiphertext,
	}, sourcePullAAD(source.TenantID, source.ID, source.Provider))
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
	syncStart := time.Now()
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

	adds, removes := splitDeltas(payload.Deltas)
	removeIDs := make([]string, 0, len(removes))
	for _, rm := range removes {
		removeIDs = append(removeIDs, rm.ExternalUserID)
	}

	result, err := s.repo.ApplyMembershipDelta(ctx, repo.ApplyInput{
		TenantID:     tenantID,
		CohortID:     cohort.ID,
		SourceID:     sourceID,
		Trigger:      "webhook",
		Members:      adds,
		RemoveIDs:    removeIDs,
		StaleTTLDays: cohort.StaleTTLDays,
	})
	if err != nil {
		_ = s.repo.UpdateSourceSyncStatus(ctx, tenantID, sourceID, redact(err.Error()))
		recordSyncMetrics(source.Provider, "webhook", "failed", 0, 0, 0)
		return nil, err
	}

	recordSyncMetrics(source.Provider, "webhook", "succeeded", result.MembersAdded, int(result.Removed), result.MemberCount)
	observeDuration(source.Provider, "webhook", syncStart)

	return ptrext.Of(SyncRunResult{
		Run:     result.Run,
		Cohort:  ptrext.Indirect(cohort),
		Added:   result.MembersAdded,
		Removed: int(result.Removed),
	}), nil
}

// ApplyFullSnapshot processes a full membership snapshot (Mixpanel "members" action).
// The trigger parameter distinguishes webhook-initiated ("webhook") from operator-
// initiated ("manual") syncs in metrics and run records.
func (s *Service) ApplyFullSnapshot(ctx context.Context, tenantID string, sourceID uuid.UUID, payload cohortsync.SyncPayload, trigger string) (*SyncRunResult, error) {
	syncStart := time.Now()
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

	adds, _ := splitDeltas(payload.Deltas)

	// Full-snapshot uses InsertExclusiveRun inside ApplyMembershipDelta
	// (the partial unique index enforces at most one running run).
	// We pass OlderThan = now so MarkDeparted runs after upsert — the
	// upserted members have last_seen_at = NOW() which is >= OlderThan,
	// so only genuinely absent members get marked departed.
	result, err := s.repo.ApplyMembershipDelta(ctx, repo.ApplyInput{
		TenantID:     tenantID,
		CohortID:     cohort.ID,
		SourceID:     sourceID,
		Trigger:      trigger,
		Members:      adds,
		StaleTTLDays: cohort.StaleTTLDays,
		OlderThan:    time.Now(),
		IsSnapshot:   true,
	})
	if err != nil {
		_ = s.repo.UpdateSourceSyncStatus(ctx, tenantID, sourceID, redact(err.Error()))
		recordSyncMetrics(source.Provider, trigger, "failed", 0, 0, 0)
		return nil, err
	}

	recordSyncMetrics(source.Provider, trigger, "succeeded", result.MembersAdded, int(result.Removed), result.MemberCount)
	observeDuration(source.Provider, trigger, syncStart)

	return ptrext.Of(SyncRunResult{
		Run:     result.Run,
		Cohort:  ptrext.Indirect(cohort),
		Added:   result.MembersAdded,
		Removed: int(result.Removed),
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

	// Best-effort pre-check to fail fast before credential decrypt + provider
	// pull. The real atomic guard is InsertExclusiveRun inside ApplyFullSnapshot.
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

	pullCred, err := s.DecryptPullCredential(ptrext.Indirect(source))
	if err != nil {
		return nil, fmt.Errorf("decrypt pull credential: %w", err)
	}

	payload, err := provider.PullCohort(ctx, cohortsync.Connection{
		ID:             source.ID.String(),
		TenantID:       source.TenantID,
		Provider:       source.Provider,
		Name:           source.Name,
		AuthType:       source.AuthType,
		BaseURL:        source.BaseURL,
		ProviderConfig: source.ProviderConfig,
		Credential:     pullCred,
	}, cohort.ExternalCohortID)
	if err != nil {
		errMsg := redact(err.Error())
		_ = s.repo.UpdateSourceSyncStatus(ctx, tenantID, source.ID, errMsg)
		recordSyncMetrics(source.Provider, "manual", "failed", 0, 0, 0)
		return nil, err
	}

	result, err := s.ApplyFullSnapshot(ctx, tenantID, source.ID, payload, "manual")
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

// HealthSummary is a snapshot of cohort sync health for a tenant.
type HealthSummary struct {
	SourceCount        int
	ActiveSources      int
	ErrorSources       int
	CohortCount        int
	TotalActiveMembers int
}

// Health returns a health summary for the tenant.
func (s *Service) Health(ctx context.Context, tenantID string) (HealthSummary, error) {
	sources, err := s.repo.ListSources(ctx, tenantID)
	if err != nil {
		return HealthSummary{}, err
	}
	var h HealthSummary
	h.SourceCount = len(sources)
	for _, src := range sources {
		switch src.Status {
		case "active":
			h.ActiveSources++
		case "error":
			h.ErrorSources++
		}
	}
	cohorts, err := s.repo.ListAllCohorts(ctx, tenantID)
	if err != nil {
		return HealthSummary{}, err
	}
	h.CohortCount = len(cohorts)
	for _, c := range cohorts {
		h.TotalActiveMembers += c.MemberCount
	}
	return h, nil
}

// ---------- Events ----------

// RecordEvent records a webhook delivery for dedup. Returns repo.ErrDuplicateEvent if duplicate.
func (s *Service) RecordEvent(ctx context.Context, in repo.SyncEvent) (*repo.SyncEvent, error) {
	return s.repo.RecordEvent(ctx, in)
}

// UpdateEventStatus updates an event's status after processing.
func (s *Service) UpdateEventStatus(ctx context.Context, id uuid.UUID, status string, runID *uuid.UUID, failureReason string) error {
	return s.repo.UpdateEventStatus(ctx, id, status, runID, failureReason)
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

func sourcePullAAD(tenantID string, id uuid.UUID, provider string) []byte {
	return []byte("cohort_sources:" + tenantID + ":" + id.String() + ":" + provider + ":pull_credential")
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

func observeDuration(provider, trigger string, start time.Time) {
	metrics.CohortSyncRunDurationSeconds.WithLabelValues(provider, trigger).Observe(time.Since(start).Seconds())
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
