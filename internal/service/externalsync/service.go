// SPDX-License-Identifier: Apache-2.0

// Package externalsync coordinates external connection configuration, sync-run
// orchestration, and provider adapter dispatch.
package externalsync

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/externalsync"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/externalsync"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

var (
	ErrValidation          = errors.New("external sync validation failed")
	ErrProviderUnavailable = externalsync.ErrProviderUnavailable
	ErrWebhookSignature    = errors.New("external sync webhook signature verification failed")
)

const invalidStatus = "\x00invalid"

var urlPattern = regexp.MustCompile(`https?://[^\s"'<>)]+`)

type Store interface {
	EncryptValue(plaintext, aad []byte) (secretstore.EncryptedValue, error)
	DecryptValue(value secretstore.EncryptedValue, aad []byte) ([]byte, error)
}

type Repo interface {
	ListConnections(ctx context.Context, tenantID string) ([]repo.Connection, error)
	GetConnection(ctx context.Context, tenantID string, id uuid.UUID) (*repo.Connection, error)
	CreateConnection(ctx context.Context, in repo.Connection) (*repo.Connection, error)
	UpdateConnection(ctx context.Context, in repo.Connection, updateCredential, updateWebhookSecret bool) (*repo.Connection, error)
	DeleteConnection(ctx context.Context, tenantID string, id uuid.UUID, actor string) error
	UpdateConnectionTestResult(ctx context.Context, tenantID string, id uuid.UUID, ok bool, lastError string) (*repo.Connection, error)
	ResumeConnection(ctx context.Context, tenantID string, id uuid.UUID, actor string) (*repo.Connection, error)
	ListMappings(ctx context.Context, tenantID string, connectionID uuid.UUID) ([]repo.Mapping, error)
	GetMapping(ctx context.Context, tenantID string, id uuid.UUID) (*repo.Mapping, error)
	ResolveRunMapping(ctx context.Context, tenantID string, connectionID uuid.UUID, mappingID *uuid.UUID) (*repo.Mapping, error)
	UpdateMapping(ctx context.Context, in repo.Mapping) (*repo.Mapping, error)
	ResetCursor(ctx context.Context, tenantID string, mappingID uuid.UUID, actor string) (*repo.ResetCursorResult, error)
	EnqueueBackfill(ctx context.Context, tenantID string, mappingID uuid.UUID, actor string, resetCursor bool) (*repo.BackfillResult, error)
	InsertRun(ctx context.Context, run repo.SyncRun) (*repo.SyncRun, error)
	ListRuns(ctx context.Context, filter repo.ListRunsFilter) (repo.ListRunsResult, error)
	GetRunDetail(ctx context.Context, tenantID string, id uuid.UUID) (*repo.RunDetail, error)
	RecordTimeline(ctx context.Context, filter repo.RecordTimelineFilter) ([]repo.RecordTimelineEntry, error)
	ClaimBatch(ctx context.Context, n int, owner string) ([]repo.SyncRun, error)
	RefreshRunClaim(ctx context.Context, id uuid.UUID, owner string) (int64, error)
	PrepareRunCursor(ctx context.Context, runID uuid.UUID, owner, tenantID string, mappingID uuid.UUID, streamKey string) ([]byte, error)
	ApplyPullResult(ctx context.Context, in repo.ApplyPullInput) (repo.ApplyStats, error)
	PreparePushRecords(ctx context.Context, runID uuid.UUID, owner, tenantID string, mappingID uuid.UUID, provider string, limit int) ([]repo.PushRecord, error)
	ApplyPushResult(ctx context.Context, in repo.ApplyPushInput) (repo.ApplyStats, error)
	RecordAttempt(ctx context.Context, in repo.AttemptInput) error
	MarkRunSucceeded(ctx context.Context, id uuid.UUID, owner string) (int64, error)
	MarkRunFailed(ctx context.Context, id uuid.UUID, owner, kind, message string, nextDelay time.Duration, dead bool) (int64, error)
	QuarantineDegradedConnection(ctx context.Context, tenantID string, connectionID uuid.UUID, reason string) (int64, error)
	RetryRun(ctx context.Context, tenantID string, id uuid.UUID) (*repo.SyncRun, error)
	RetryFailure(ctx context.Context, tenantID string, id uuid.UUID, actor string) (*repo.RecordFailure, error)
	ResolveConflict(ctx context.Context, tenantID string, id uuid.UUID, resolution, actor string) (*repo.ConflictRow, error)
	ResolveConflicts(ctx context.Context, tenantID string, ids []uuid.UUID, resolution, actor string) (repo.BatchResolveConflictsResult, error)
	RecordEvent(ctx context.Context, in repo.SyncEvent) (*repo.SyncEvent, error)
	ListEvents(ctx context.Context, filter repo.ListEventsFilter) (repo.ListEventsResult, error)
	GetEvent(ctx context.Context, tenantID string, id uuid.UUID) (*repo.SyncEvent, error)
	ReplayEvent(ctx context.Context, tenantID string, id uuid.UUID, actor string, mappingID uuid.UUID, direction string) (*repo.SyncEvent, *repo.SyncRun, error)
	Health(ctx context.Context, tenantID string) (repo.Health, error)
	MetricSnapshot(ctx context.Context) (repo.MetricSnapshot, error)
}

type Service struct {
	repo  Repo
	store Store
	audit auditRecorder
}

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

type Actor struct {
	Type string
	ID   string
}

type CreateConnectionInput struct {
	TenantID           string
	Provider           string
	Name               string
	AuthType           string
	Credential         string
	WebhookSecret      string
	BaseURL            string
	ProviderConfigJSON string
	Scopes             []string
	Enabled            bool
	Actor              Actor
	AuditActor         auditlogsvc.Actor
}

type UpdateConnectionInput struct {
	TenantID           string
	ID                 uuid.UUID
	Name               *string
	Enabled            *bool
	Credential         *string
	WebhookSecret      *string
	BaseURL            *string
	ProviderConfigJSON *string
	Scopes             []string
	Actor              Actor
	AuditActor         auditlogsvc.Actor
}

type ResumeConnectionInput struct {
	TenantID   string
	ID         uuid.UUID
	Actor      Actor
	AuditActor auditlogsvc.Actor
}

const (
	QualificationStatusOK      = "ok"
	QualificationStatusWarning = "warning"
	QualificationStatusFailed  = "failed"
)

type QualificationCheck struct {
	Name       string
	Status     string
	Summary    string
	DetailJSON string
}

type QualificationResult struct {
	ConnectionID uuid.UUID
	Ready        bool
	Checks       []QualificationCheck
}

type UpdateMappingInput struct {
	TenantID          string
	ID                uuid.UUID
	Direction         string
	FieldMappingJSON  string
	StatusMappingJSON string
	ConflictPolicy    string
	TombstonePolicy   string
	Enabled           *bool
	Actor             Actor
	AuditActor        auditlogsvc.Actor
}

type ResetCursorInput struct {
	TenantID   string
	ID         uuid.UUID
	Actor      Actor
	AuditActor auditlogsvc.Actor
}

type PreviewMappingInput struct {
	TenantID          string
	ID                uuid.UUID
	FieldMappingJSON  *string
	StatusMappingJSON *string
}

type MappingPreview struct {
	Schema   externalsync.ObjectSchema
	Errors   []string
	Warnings []string
}

type BackfillInput struct {
	TenantID    string
	ID          uuid.UUID
	ResetCursor bool
	Actor       Actor
	AuditActor  auditlogsvc.Actor
}

type RequestRunInput struct {
	TenantID     string
	ConnectionID uuid.UUID
	MappingID    *uuid.UUID
	Direction    string
	Actor        Actor
	AuditActor   auditlogsvc.Actor
}

type ListRunsInput struct {
	TenantID     string
	ConnectionID *uuid.UUID
	MappingID    *uuid.UUID
	Status       string
	BeforeID     *uuid.UUID
	Limit        int
}

type RecordTimelineInput struct {
	TenantID      string
	MappingID     uuid.UUID
	LocalObjectID string
	ExternalKey   string
	Limit         int
}

type BatchResolveConflictsInput struct {
	TenantID   string
	IDs        []uuid.UUID
	Resolution string
	Actor      Actor
	AuditActor auditlogsvc.Actor
}

type RecordEventInput struct {
	TenantID              string
	ConnectionID          uuid.UUID
	MappingID             *uuid.UUID
	EventType             string
	ExternalEventID       string
	DedupeKey             string
	SignatureStatus       string
	PayloadDigest         string
	NormalizedPayloadJSON string
	FailureReason         string
	ReceivedAt            time.Time
}

type GitHubWebhookInput struct {
	TenantID        string
	ConnectionID    uuid.UUID
	EventType       string
	DeliveryID      string
	SignatureSHA256 string
	Body            []byte
	ReceivedAt      time.Time
}

type ListEventsInput struct {
	TenantID     string
	ConnectionID *uuid.UUID
	Status       string
	BeforeID     *uuid.UUID
	Limit        int
}

type ProcessResult struct {
	Provider           string
	ExternalObjectType string
	Status             string
	OperationStats     []ProcessOperationStats
}

type ProcessOperationStats struct {
	Operation string
	Stats     repo.ApplyStats
}

type processRunError struct {
	kind       string
	message    string
	retryable  bool
	retryAfter *time.Time
	cause      error
}

func (e processRunError) Error() string {
	if e.message == "" {
		return e.kind
	}
	return e.kind + ": " + e.message
}

func (e processRunError) Unwrap() error {
	return e.cause
}

func newProcessRunError(kind, message string, retryable bool, cause error) error {
	return newProcessRunErrorWithRetryAfter(kind, message, retryable, nil, cause)
}

func newProcessRunErrorWithRetryAfter(kind, message string, retryable bool, retryAfter *time.Time, cause error) error {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "other"
	}
	message = strings.TrimSpace(message)
	if message == "" && cause != nil {
		message = cause.Error()
	}
	return processRunError{kind: kind, message: message, retryable: retryable, retryAfter: retryAfter, cause: cause}
}

func processRunErrorInfo(err error) (string, bool, *time.Time, bool) {
	runErr := processRunError{}
	if errors.As(err, &runErr) { // ptrext:allow errors.As out-param
		return runErr.kind, runErr.retryable, runErr.retryAfter, true
	}
	return "", false, nil, false
}

func New(repo Repo, store Store) *Service {
	return ptrext.Of(Service{repo: repo, store: store})
}

func (s *Service) SetAuditLogger(audit auditRecorder) {
	s.audit = audit
}

func (s *Service) ListConnections(ctx context.Context, tenantID string) ([]repo.Connection, error) {
	return s.repo.ListConnections(ctx, tenantID)
}

func (s *Service) CreateConnection(ctx context.Context, in CreateConnectionInput) (*repo.Connection, error) {
	normalized, err := normalizeCreateConnection(in)
	if err != nil {
		return nil, err
	}
	id := uuid.New()
	encrypted, err := s.store.EncryptValue([]byte(normalized.Credential), connectionAAD(normalized.TenantID, id, normalized.Provider))
	if err != nil {
		return nil, fmt.Errorf("encrypt external credential: %w", err)
	}
	var webhookSecret secretstore.EncryptedValue
	if normalized.WebhookSecret != "" {
		webhookSecret, err = s.store.EncryptValue([]byte(normalized.WebhookSecret), connectionWebhookSecretAAD(normalized.TenantID, id, normalized.Provider))
		if err != nil {
			return nil, fmt.Errorf("encrypt external webhook secret: %w", err)
		}
	}
	row, err := s.repo.CreateConnection(ctx, repo.Connection{
		ID:                      id,
		TenantID:                normalized.TenantID,
		Provider:                normalized.Provider,
		Name:                    normalized.Name,
		Enabled:                 normalized.Enabled,
		Status:                  connectionStatus(normalized.Enabled),
		AuthType:                normalized.AuthType,
		BaseURL:                 normalized.BaseURL,
		ProviderConfig:          []byte(normalized.ProviderConfigJSON),
		Scopes:                  normalized.Scopes,
		CredentialKeyID:         encrypted.KeyID,
		CredentialCiphertext:    encrypted.Ciphertext,
		WebhookSecretKeyID:      webhookSecret.KeyID,
		WebhookSecretCiphertext: webhookSecret.Ciphertext,
		CreatedBy:               normalized.Actor.ID,
		UpdatedBy:               normalized.Actor.ID,
	})
	if err != nil {
		return nil, err
	}
	s.record(ctx, normalized.AuditActor, normalized.TenantID, "external_connection.create",
		"external_connection", row.ID.String(), "Created external sync connection", nil, connectionAudit(row))
	return row, nil
}

func (s *Service) UpdateConnection(ctx context.Context, in UpdateConnectionInput) (*repo.Connection, error) {
	current, err := s.repo.GetConnection(ctx, in.TenantID, in.ID)
	if err != nil {
		return nil, err
	}
	next := ptrext.Indirect(current)
	if in.Name != nil {
		next.Name = strings.TrimSpace(ptrext.Indirect(in.Name))
	}
	if in.Enabled != nil {
		next.Enabled = ptrext.Indirect(in.Enabled)
		next.Status = connectionStatus(next.Enabled)
	}
	if in.BaseURL != nil {
		next.BaseURL = strings.TrimSpace(ptrext.Indirect(in.BaseURL))
	}
	if in.ProviderConfigJSON != nil {
		cfg, err := normalizeJSONObject(ptrext.Indirect(in.ProviderConfigJSON), "provider_config_json")
		if err != nil {
			return nil, err
		}
		next.ProviderConfig = []byte(cfg)
	}
	if len(in.Scopes) > 0 {
		next.Scopes = normalizeStringList(in.Scopes)
	}
	next.UpdatedBy = in.Actor.ID
	next, updateCredential, updateWebhookSecret, err := s.applyConnectionSecretUpdates(next, in)
	if err != nil {
		return nil, err
	}
	if err := validateConnectionShape(next.Provider, next.Name, next.AuthType, string(next.ProviderConfig)); err != nil {
		return nil, err
	}
	updated, err := s.repo.UpdateConnection(ctx, next, updateCredential, updateWebhookSecret)
	if err != nil {
		return nil, err
	}
	s.record(ctx, in.AuditActor, in.TenantID, "external_connection.update",
		"external_connection", updated.ID.String(), "Updated external sync connection",
		connectionAudit(current), connectionAudit(updated))
	return updated, nil
}

func (s *Service) applyConnectionSecretUpdates(next repo.Connection, in UpdateConnectionInput) (repo.Connection, bool, bool, error) {
	updateCredential := in.Credential != nil
	if updateCredential {
		credential := strings.TrimSpace(ptrext.Indirect(in.Credential))
		if credential == "" {
			return next, false, false, fmt.Errorf("%w: credential is required", ErrValidation)
		}
		encrypted, err := s.store.EncryptValue([]byte(credential), connectionAAD(next.TenantID, next.ID, next.Provider))
		if err != nil {
			return next, false, false, fmt.Errorf("encrypt external credential: %w", err)
		}
		next.CredentialKeyID = encrypted.KeyID
		next.CredentialCiphertext = encrypted.Ciphertext
	}
	updateWebhookSecret := in.WebhookSecret != nil
	if updateWebhookSecret {
		row, err := s.applyWebhookSecretUpdate(next, ptrext.Indirect(in.WebhookSecret))
		if err != nil {
			return next, false, false, err
		}
		next = row
	}
	return next, updateCredential, updateWebhookSecret, nil
}

func (s *Service) applyWebhookSecretUpdate(next repo.Connection, raw string) (repo.Connection, error) {
	webhookSecret := strings.TrimSpace(raw)
	if webhookSecret == "" {
		next.WebhookSecretKeyID = ""
		next.WebhookSecretCiphertext = nil
		next.WebhookSecretSetAt = nil
		return next, nil
	}
	if err := validateWebhookSecret(webhookSecret); err != nil {
		return next, err
	}
	encrypted, err := s.store.EncryptValue([]byte(webhookSecret), connectionWebhookSecretAAD(next.TenantID, next.ID, next.Provider))
	if err != nil {
		return next, fmt.Errorf("encrypt external webhook secret: %w", err)
	}
	next.WebhookSecretKeyID = encrypted.KeyID
	next.WebhookSecretCiphertext = encrypted.Ciphertext
	return next, nil
}

func (s *Service) DeleteConnection(ctx context.Context, tenantID string, id uuid.UUID, actor Actor, auditActor auditlogsvc.Actor) error {
	before, err := s.repo.GetConnection(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteConnection(ctx, tenantID, id, actor.ID); err != nil {
		return err
	}
	s.record(ctx, auditActor, tenantID, "external_connection.delete",
		"external_connection", id.String(), "Deleted external sync connection", connectionAudit(before), nil)
	return nil
}

func (s *Service) ResumeConnection(ctx context.Context, in ResumeConnectionInput) (*repo.Connection, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Actor.ID = strings.TrimSpace(in.Actor.ID)
	if in.Actor.ID == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrValidation)
	}
	before, err := s.repo.GetConnection(ctx, in.TenantID, in.ID)
	if err != nil {
		return nil, err
	}
	row, err := s.repo.ResumeConnection(ctx, in.TenantID, in.ID, in.Actor.ID)
	if err != nil {
		return nil, err
	}
	s.record(ctx, in.AuditActor, in.TenantID, "external_connection.resume",
		"external_connection", in.ID.String(), "Resumed external sync connection",
		connectionAudit(before), connectionAudit(row))
	return row, nil
}

func (s *Service) TestConnection(ctx context.Context, tenantID string, id uuid.UUID, auditActor auditlogsvc.Actor) (externalsync.CheckResult, error) {
	conn, err := s.repo.GetConnection(ctx, tenantID, id)
	if err != nil {
		return externalsync.CheckResult{}, err
	}
	provider, ok := externalsync.Lookup(conn.Provider)
	if !ok {
		result := externalsync.CheckResult{OK: false, Error: externalsync.UnavailableError(conn.Provider).Error()}
		_, _ = s.repo.UpdateConnectionTestResult(ctx, tenantID, id, false, result.Error)
		s.record(ctx, auditActor, tenantID, "external_connection.test",
			"external_connection", id.String(), "Tested external sync connection", nil, testAudit(conn.Provider, result))
		return result, externalsync.UnavailableError(conn.Provider)
	}
	decrypted, err := s.decryptConnection(ptrext.Indirect(conn))
	if err != nil {
		return externalsync.CheckResult{}, err
	}
	start := time.Now()
	result, probeErr := provider.Check(ctx, decrypted)
	if result.Latency == 0 {
		result.Latency = time.Since(start)
	}
	if probeErr != nil && result.Error == "" {
		classified := provider.ClassifyError(probeErr)
		result.Error = classified.Message
	}
	result.Error = redact(result.Error)
	_, _ = s.repo.UpdateConnectionTestResult(ctx, tenantID, id, result.OK && probeErr == nil, result.Error)
	s.record(ctx, auditActor, tenantID, "external_connection.test",
		"external_connection", id.String(), "Tested external sync connection", nil, testAudit(conn.Provider, result))
	return result, probeErr
}

func (s *Service) QualifyConnection(ctx context.Context, tenantID string, id uuid.UUID, auditActor auditlogsvc.Actor) (QualificationResult, error) {
	tenantID = strings.TrimSpace(tenantID)
	conn, err := s.repo.GetConnection(ctx, tenantID, id)
	if err != nil {
		return QualificationResult{}, err
	}
	result := QualificationResult{ConnectionID: id, Ready: true}
	provider, ok := externalsync.Lookup(conn.Provider)
	if !ok {
		result.addCheck("provider_registered", QualificationStatusFailed, "Provider adapter is not registered",
			map[string]any{"provider": conn.Provider})
		result.Ready = false
		s.auditQualification(ctx, auditActor, tenantID, conn, result)
		return result, nil
	}
	result.addCheck("provider_registered", QualificationStatusOK, "Provider adapter is registered",
		map[string]any{"provider": conn.Provider})

	decrypted, err := s.decryptConnection(ptrext.Indirect(conn))
	if err != nil {
		result.addCheck("credential_decrypt", QualificationStatusFailed, "Credential could not be decrypted", nil)
		result.Ready = false
		s.auditQualification(ctx, auditActor, tenantID, conn, result)
		return result, nil
	}
	result.addCheck("credential_decrypt", QualificationStatusOK, "Credential decrypted with connection-scoped AAD", nil)

	checkStarted := time.Now()
	check, checkErr := provider.Check(ctx, decrypted)
	if check.Latency == 0 {
		check.Latency = time.Since(checkStarted)
	}
	if checkErr != nil || !check.OK {
		message := redact(check.Error)
		if message == "" && checkErr != nil {
			message = redact(provider.ClassifyError(checkErr).Message)
		}
		result.addCheck("provider_check", QualificationStatusFailed, nonEmpty(message, "Provider check failed"),
			map[string]any{"latency_ms": check.Latency.Milliseconds(), "request_id": check.RequestID})
		result.Ready = false
	} else {
		result.addCheck("provider_check", QualificationStatusOK, "Provider check succeeded",
			map[string]any{"latency_ms": check.Latency.Milliseconds(), "request_id": check.RequestID})
	}

	schemas, discoverErr := provider.Discover(ctx, decrypted)
	if discoverErr != nil {
		classified := provider.ClassifyError(discoverErr)
		result.addCheck("schema_discovery", QualificationStatusFailed,
			nonEmpty(redact(classified.Message), "Provider schema discovery failed"),
			map[string]any{"error_kind": classified.Kind, "http_status": classified.HTTPStatus})
		result.Ready = false
	} else {
		schemas = normalizeObjectSchemas(schemas)
		status := QualificationStatusOK
		summary := "Provider schema discovery returned object schemas"
		if len(schemas) == 0 {
			status = QualificationStatusWarning
			summary = "Provider schema discovery returned no object schemas"
		}
		result.addCheck("schema_discovery", status, summary, map[string]any{"object_count": len(schemas)})
		result.addSchemaMetadataCheck(schemas)
	}

	scopeStatus := QualificationStatusOK
	scopeSummary := "Connection exposes provider scopes"
	if len(conn.Scopes) == 0 {
		scopeStatus = QualificationStatusWarning
		scopeSummary = "Connection has no declared scopes"
	}
	result.addCheck("scope_visibility", scopeStatus, scopeSummary, map[string]any{"scopes": conn.Scopes})
	s.auditQualification(ctx, auditActor, tenantID, conn, result)
	return result, nil
}

func (s *Service) DiscoverConnectionSchema(ctx context.Context, tenantID string, id uuid.UUID) ([]externalsync.ObjectSchema, error) {
	conn, err := s.repo.GetConnection(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	provider, ok := externalsync.Lookup(conn.Provider)
	if !ok {
		return nil, externalsync.UnavailableError(conn.Provider)
	}
	decrypted, err := s.decryptConnection(ptrext.Indirect(conn))
	if err != nil {
		return nil, err
	}
	schemas, err := provider.Discover(ctx, decrypted)
	if err != nil {
		classified := provider.ClassifyError(err)
		message := redact(classified.Message)
		if message == "" {
			message = "provider schema discovery failed"
		}
		return nil, fmt.Errorf("%w: %s", ErrValidation, message)
	}
	return normalizeObjectSchemas(schemas), nil
}

func (s *Service) ListMappings(ctx context.Context, tenantID string, connectionID uuid.UUID) ([]repo.Mapping, error) {
	return s.repo.ListMappings(ctx, tenantID, connectionID)
}

func (s *Service) UpdateMapping(ctx context.Context, in UpdateMappingInput) (*repo.Mapping, error) {
	mapping, err := normalizeMapping(in)
	if err != nil {
		return nil, err
	}
	updated, err := s.repo.UpdateMapping(ctx, mapping)
	if err != nil {
		return nil, err
	}
	s.record(ctx, in.AuditActor, in.TenantID, "external_sync_mapping.update",
		"external_sync_mapping", updated.ID.String(), "Updated external sync mapping", nil, mappingAudit(updated))
	return updated, nil
}

func (s *Service) PreviewMapping(ctx context.Context, in PreviewMappingInput) (MappingPreview, error) {
	mapping, err := s.repo.GetMapping(ctx, strings.TrimSpace(in.TenantID), in.ID)
	if err != nil {
		return MappingPreview{}, err
	}
	schemas, err := s.DiscoverConnectionSchema(ctx, mapping.TenantID, mapping.ConnectionID)
	if err != nil {
		return MappingPreview{}, err
	}
	schema := schemaForObject(schemas, mapping.ExternalObjectType)
	fieldMapping := string(mapping.FieldMapping)
	if in.FieldMappingJSON != nil {
		fieldMapping = ptrext.Indirect(in.FieldMappingJSON)
	}
	statusMapping := string(mapping.StatusMapping)
	if in.StatusMappingJSON != nil {
		statusMapping = ptrext.Indirect(in.StatusMappingJSON)
	}
	errors, warnings := validateMappingPreview(fieldMapping, statusMapping, schema)
	return MappingPreview{Schema: schema, Errors: errors, Warnings: warnings}, nil
}

func (s *Service) ResetCursor(ctx context.Context, in ResetCursorInput) (*repo.ResetCursorResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Actor.ID = strings.TrimSpace(in.Actor.ID)
	if in.Actor.ID == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrValidation)
	}
	mapping, err := s.repo.GetMapping(ctx, in.TenantID, in.ID)
	if err != nil {
		return nil, err
	}
	if !mapping.Enabled {
		return nil, fmt.Errorf("%w: mapping must be enabled before resetting cursor", ErrValidation)
	}
	if !mappingAllowsRunDirection(mapping.Direction, repo.DirectionPull) {
		return nil, fmt.Errorf("%w: cursor reset requires a pull-capable mapping", ErrValidation)
	}
	result, err := s.repo.ResetCursor(ctx, in.TenantID, in.ID, in.Actor.ID)
	if err != nil {
		return nil, err
	}
	s.record(ctx, in.AuditActor, in.TenantID, "external_sync_cursor.reset",
		"external_sync_cursor", in.ID.String(), "Reset external sync cursor",
		mappingAudit(mapping), cursorResetAudit(result))
	return result, nil
}

func (s *Service) RequestBackfill(ctx context.Context, in BackfillInput) (*repo.BackfillResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Actor.ID = strings.TrimSpace(in.Actor.ID)
	if in.Actor.ID == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrValidation)
	}
	mapping, err := s.repo.GetMapping(ctx, in.TenantID, in.ID)
	if err != nil {
		return nil, err
	}
	if !mapping.Enabled {
		return nil, fmt.Errorf("%w: mapping must be enabled before backfill", ErrValidation)
	}
	if !mappingAllowsRunDirection(mapping.Direction, repo.DirectionPull) {
		return nil, fmt.Errorf("%w: backfill requires a pull-capable mapping", ErrValidation)
	}
	result, err := s.repo.EnqueueBackfill(ctx, in.TenantID, in.ID, in.Actor.ID, in.ResetCursor)
	if err != nil {
		return nil, err
	}
	s.record(ctx, in.AuditActor, in.TenantID, "external_sync_run.backfill",
		"external_sync_run", result.Run.ID.String(), "Requested external sync backfill",
		mappingAudit(mapping), backfillAudit(result, in.ResetCursor))
	return result, nil
}

func (s *Service) RequestRun(ctx context.Context, in RequestRunInput) (*repo.SyncRun, error) {
	mapping, err := s.repo.ResolveRunMapping(ctx, in.TenantID, in.ConnectionID, in.MappingID)
	if err != nil {
		return nil, err
	}
	direction := strings.TrimSpace(in.Direction)
	if direction == "" {
		direction = mapping.Direction
	}
	direction = normalizeDirection(direction)
	if direction == "" {
		return nil, fmt.Errorf("%w: invalid direction", ErrValidation)
	}
	if !mappingAllowsRunDirection(mapping.Direction, direction) {
		return nil, fmt.Errorf("%w: run direction %q is not allowed by mapping direction %q",
			ErrValidation, direction, mapping.Direction)
	}
	run, err := s.repo.InsertRun(ctx, repo.SyncRun{
		ID:           uuid.New(),
		TenantID:     in.TenantID,
		ConnectionID: in.ConnectionID,
		MappingID:    ptrext.Of(mapping.ID),
		Direction:    direction,
		Trigger:      repo.TriggerManual,
		ActorID:      in.Actor.ID,
	})
	if err != nil {
		return nil, err
	}
	s.record(ctx, in.AuditActor, in.TenantID, "external_sync_run.request",
		"external_sync_run", run.ID.String(), "Requested external sync run", nil, runAudit(run))
	return run, nil
}

func (s *Service) ListRuns(ctx context.Context, in ListRunsInput) (repo.ListRunsResult, error) {
	status := normalizeRunStatus(in.Status)
	if status == invalidStatus {
		return repo.ListRunsResult{}, fmt.Errorf("%w: invalid run status", ErrValidation)
	}
	return s.repo.ListRuns(ctx, repo.ListRunsFilter{
		TenantID:     strings.TrimSpace(in.TenantID),
		ConnectionID: in.ConnectionID,
		MappingID:    in.MappingID,
		Status:       status,
		BeforeID:     in.BeforeID,
		Limit:        in.Limit,
	})
}

func (s *Service) GetRunDetail(ctx context.Context, tenantID string, id uuid.UUID) (*repo.RunDetail, error) {
	return s.repo.GetRunDetail(ctx, tenantID, id)
}

func (s *Service) RecordTimeline(ctx context.Context, in RecordTimelineInput) ([]repo.RecordTimelineEntry, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.LocalObjectID = strings.TrimSpace(in.LocalObjectID)
	in.ExternalKey = strings.TrimSpace(in.ExternalKey)
	if in.MappingID == uuid.Nil {
		return nil, fmt.Errorf("%w: mapping_id is required", ErrValidation)
	}
	if in.LocalObjectID == "" && in.ExternalKey == "" {
		return nil, fmt.Errorf("%w: local_object_id or external_key is required", ErrValidation)
	}
	return s.repo.RecordTimeline(ctx, repo.RecordTimelineFilter{
		TenantID:      in.TenantID,
		MappingID:     in.MappingID,
		LocalObjectID: in.LocalObjectID,
		ExternalKey:   in.ExternalKey,
		Limit:         in.Limit,
	})
}

func (s *Service) RecordEvent(ctx context.Context, in RecordEventInput) (*repo.SyncEvent, error) {
	row, err := s.normalizeEvent(ctx, in)
	if err != nil {
		return nil, err
	}
	return s.repo.RecordEvent(ctx, row)
}

func (s *Service) RecordGitHubWebhook(ctx context.Context, in GitHubWebhookInput) (*repo.SyncEvent, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.EventType = strings.TrimSpace(in.EventType)
	in.DeliveryID = strings.TrimSpace(in.DeliveryID)
	if in.TenantID == "" {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	if in.ConnectionID == uuid.Nil {
		return nil, fmt.Errorf("%w: connection_id is required", ErrValidation)
	}
	if in.EventType == "" {
		return nil, fmt.Errorf("%w: X-GitHub-Event is required", ErrValidation)
	}
	if in.DeliveryID == "" {
		return nil, fmt.Errorf("%w: X-GitHub-Delivery is required", ErrValidation)
	}
	conn, err := s.repo.GetConnection(ctx, in.TenantID, in.ConnectionID)
	if err != nil {
		return nil, err
	}
	if conn.Provider != "github" {
		return nil, fmt.Errorf("%w: connection provider must be github", ErrValidation)
	}
	secret, err := s.decryptWebhookSecret(ptrext.Indirect(conn))
	if err != nil {
		return nil, err
	}
	signatureStatus := repo.EventSignatureVerified
	failureReason := ""
	if !verifyGitHubSignatureSHA256(in.SignatureSHA256, secret, in.Body) {
		signatureStatus = repo.EventSignatureFailed
		failureReason = "github webhook signature verification failed"
	}
	event, err := s.RecordEvent(ctx, RecordEventInput{
		TenantID:              in.TenantID,
		ConnectionID:          in.ConnectionID,
		EventType:             in.EventType,
		ExternalEventID:       in.DeliveryID,
		SignatureStatus:       signatureStatus,
		PayloadDigest:         eventPayloadDigest(in.Body),
		NormalizedPayloadJSON: normalizeGitHubWebhookPayload(in.EventType, in.DeliveryID, in.Body),
		FailureReason:         failureReason,
		ReceivedAt:            in.ReceivedAt,
	})
	if err != nil {
		return nil, err
	}
	if signatureStatus == repo.EventSignatureFailed {
		return event, ErrWebhookSignature
	}
	return event, nil
}

func (s *Service) ListEvents(ctx context.Context, in ListEventsInput) (repo.ListEventsResult, error) {
	status := normalizeEventStatus(in.Status)
	if status == invalidStatus {
		return repo.ListEventsResult{}, fmt.Errorf("%w: invalid event status", ErrValidation)
	}
	return s.repo.ListEvents(ctx, repo.ListEventsFilter{
		TenantID:     strings.TrimSpace(in.TenantID),
		ConnectionID: in.ConnectionID,
		Status:       status,
		BeforeID:     in.BeforeID,
		Limit:        in.Limit,
	})
}

func (s *Service) GetEvent(ctx context.Context, tenantID string, id uuid.UUID) (*repo.SyncEvent, error) {
	return s.repo.GetEvent(ctx, tenantID, id)
}

func (s *Service) ReplayEvent(ctx context.Context, tenantID string, id uuid.UUID, actor Actor, auditActor auditlogsvc.Actor) (*repo.SyncEvent, *repo.SyncRun, error) {
	event, err := s.repo.GetEvent(ctx, tenantID, id)
	if err != nil {
		return nil, nil, err
	}
	if event.SignatureStatus != repo.EventSignatureVerified && event.SignatureStatus != repo.EventSignatureNotRequired {
		return nil, nil, fmt.Errorf("%w: event signature is not verified", ErrValidation)
	}
	if event.Status != repo.EventStatusReceived {
		return nil, nil, fmt.Errorf("%w: event is not replayable", repo.ErrConflict)
	}
	mapping, err := s.repo.ResolveRunMapping(ctx, tenantID, event.ConnectionID, event.MappingID)
	if err != nil {
		return nil, nil, err
	}
	if !mappingAllowsRunDirection(mapping.Direction, repo.DirectionPull) {
		return nil, nil, fmt.Errorf("%w: webhook replay requires a pull-capable mapping", ErrValidation)
	}
	replayed, run, err := s.repo.ReplayEvent(ctx, tenantID, id, actor.ID, mapping.ID, repo.DirectionPull)
	if err != nil {
		return nil, nil, err
	}
	s.record(ctx, auditActor, tenantID, "external_sync_event.replay",
		"external_sync_event", id.String(), "Replayed external sync event", eventAudit(event), eventReplayAudit(replayed, run))
	return replayed, run, nil
}

func (s *Service) RetryRun(ctx context.Context, tenantID string, id uuid.UUID, actor Actor, auditActor auditlogsvc.Actor) (*repo.SyncRun, error) {
	run, err := s.repo.RetryRun(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	s.record(ctx, auditActor, tenantID, "external_sync_run.retry",
		"external_sync_run", id.String(), "Retried external sync run", nil, map[string]any{"actor_id": actor.ID})
	return run, nil
}

func (s *Service) RetryFailure(ctx context.Context, tenantID string, id uuid.UUID, actor Actor, auditActor auditlogsvc.Actor) (*repo.RecordFailure, error) {
	failure, err := s.repo.RetryFailure(ctx, tenantID, id, actor.ID)
	if err != nil {
		return nil, err
	}
	s.record(ctx, auditActor, tenantID, "external_sync_failure.retry",
		"external_sync_failure", id.String(), "Retried external sync record failure", nil, failureAudit(failure))
	return failure, nil
}

func (s *Service) ResolveConflict(ctx context.Context, tenantID string, id uuid.UUID, resolution string, actor Actor, auditActor auditlogsvc.Actor) (*repo.ConflictRow, error) {
	if !validResolution(resolution) {
		return nil, fmt.Errorf("%w: invalid conflict resolution", ErrValidation)
	}
	conflict, err := s.repo.ResolveConflict(ctx, tenantID, id, resolution, actor.ID)
	if err != nil {
		return nil, err
	}
	s.record(ctx, auditActor, tenantID, "external_sync_conflict.resolve",
		"external_sync_conflict", id.String(), "Resolved external sync conflict", nil, conflictAudit(conflict))
	s.recordConflictResolutionMetric(ctx, tenantID, ptrext.Indirect(conflict))
	return conflict, nil
}

func (s *Service) BatchResolveConflicts(ctx context.Context, in BatchResolveConflictsInput) (repo.BatchResolveConflictsResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Actor.ID = strings.TrimSpace(in.Actor.ID)
	if in.Actor.ID == "" {
		return repo.BatchResolveConflictsResult{}, fmt.Errorf("%w: actor is required", ErrValidation)
	}
	if !validResolution(in.Resolution) {
		return repo.BatchResolveConflictsResult{}, fmt.Errorf("%w: invalid conflict resolution", ErrValidation)
	}
	ids := uniqueUUIDs(in.IDs)
	if len(ids) == 0 {
		return repo.BatchResolveConflictsResult{}, fmt.Errorf("%w: conflict ids are required", ErrValidation)
	}
	if len(ids) > 50 {
		return repo.BatchResolveConflictsResult{}, fmt.Errorf("%w: at most 50 conflicts can be resolved at once", ErrValidation)
	}
	result, err := s.repo.ResolveConflicts(ctx, in.TenantID, ids, in.Resolution, in.Actor.ID)
	if err != nil {
		return repo.BatchResolveConflictsResult{}, err
	}
	s.record(ctx, in.AuditActor, in.TenantID, "external_sync_conflict.resolve",
		"external_sync_conflict", "batch", "Batch resolved external sync conflicts", nil,
		map[string]any{
			"requested_count": len(ids),
			"resolved_count":  len(result.Conflicts),
			"resolution":      in.Resolution,
		})
	for _, conflict := range result.Conflicts {
		s.recordConflictResolutionMetric(ctx, in.TenantID, conflict)
	}
	return result, nil
}

func (s *Service) Health(ctx context.Context, tenantID string) (repo.Health, error) {
	return s.repo.Health(ctx, tenantID)
}

func (s *Service) ProcessRun(ctx context.Context, run repo.SyncRun) (ProcessResult, error) {
	conn, err := s.repo.GetConnection(ctx, run.TenantID, run.ConnectionID)
	if err != nil {
		return ProcessResult{}, err
	}
	provider, ok := externalsync.Lookup(conn.Provider)
	if !ok {
		return ProcessResult{Provider: conn.Provider}, externalsync.UnavailableError(conn.Provider)
	}
	decrypted, err := s.decryptConnection(ptrext.Indirect(conn))
	if err != nil {
		return ProcessResult{Provider: conn.Provider}, err
	}
	mapping, err := s.repo.ResolveRunMapping(ctx, run.TenantID, run.ConnectionID, run.MappingID)
	if err != nil {
		return ProcessResult{Provider: conn.Provider}, err
	}
	result := ProcessResult{
		Provider:           conn.Provider,
		ExternalObjectType: mapping.ExternalObjectType,
		Status:             repo.RunStatusSucceeded,
	}
	if !mappingAllowsRunDirection(mapping.Direction, run.Direction) {
		result.Status = repo.RunStatusFailed
		return result, fmt.Errorf("%w: run direction %q is not allowed by mapping direction %q",
			ErrValidation, run.Direction, mapping.Direction)
	}
	switch run.Direction {
	case repo.DirectionPull:
		stats, err := s.processPull(ctx, run, ptrext.Indirect(mapping), decrypted, provider)
		if err != nil {
			return result, err
		}
		result.OperationStats = append(result.OperationStats, ProcessOperationStats{
			Operation: repo.DirectionPull,
			Stats:     stats,
		})
		result.Status = processStatus(stats)
	case repo.DirectionPush:
		stats, err := s.processPush(ctx, run, ptrext.Indirect(mapping), decrypted, provider)
		if err != nil {
			return result, err
		}
		result.OperationStats = append(result.OperationStats, ProcessOperationStats{
			Operation: repo.DirectionPush,
			Stats:     stats,
		})
		result.Status = processStatus(stats)
	default:
		stats, err := s.processPull(ctx, run, ptrext.Indirect(mapping), decrypted, provider)
		if err != nil {
			return result, err
		}
		pushStats, err := s.processPush(ctx, run, ptrext.Indirect(mapping), decrypted, provider)
		if err != nil {
			return result, err
		}
		result.OperationStats = append(result.OperationStats,
			ProcessOperationStats{Operation: repo.DirectionPull, Stats: stats},
			ProcessOperationStats{Operation: repo.DirectionPush, Stats: pushStats},
		)
		result.Status = processStatus(combineStats(stats, pushStats))
	}
	return result, nil
}

func (s *Service) processPull(ctx context.Context, run repo.SyncRun, mapping repo.Mapping, conn externalsync.Connection, provider externalsync.Provider) (repo.ApplyStats, error) {
	streamKey := repo.StreamDefault
	cursor, err := s.repo.PrepareRunCursor(ctx, run.ID, run.ClaimedBy, run.TenantID, mapping.ID, streamKey)
	if err != nil {
		return repo.ApplyStats{}, err
	}
	started := time.Now()
	result, err := provider.Pull(ctx, externalsync.PullRequest{
		Connection: conn,
		MappingID:  mapping.ID.String(),
		StreamKey:  streamKey,
		Cursor:     cursor,
	})
	if err != nil {
		return repo.ApplyStats{}, s.recordFailedProviderAttempt(ctx, provider, run, started, err)
	}
	if result.StreamKey != "" {
		streamKey = result.StreamKey
	}
	stats, err := s.repo.ApplyPullResult(ctx, repo.ApplyPullInput{
		TenantID:     run.TenantID,
		RunID:        run.ID,
		ConnectionID: run.ConnectionID,
		MappingID:    mapping.ID,
		Provider:     conn.Provider,
		StreamKey:    streamKey,
		CursorBefore: cursor,
		CursorAfter:  result.NextCursor,
		Records:      pullRecordsToRepo(result.Records),
	})
	if err != nil {
		message := redact(err.Error())
		_ = s.repo.RecordAttempt(ctx, repo.AttemptInput{
			RunID:         run.ID,
			AttemptNumber: attemptNumber(run),
			StartedAt:     started,
			Result:        "failed",
			ErrorKind:     "apply_failed",
			ErrorMessage:  message,
		})
		return repo.ApplyStats{}, newProcessRunError("apply_failed", message, true, err)
	}
	_ = s.repo.RecordAttempt(ctx, repo.AttemptInput{
		RunID:         run.ID,
		AttemptNumber: attemptNumber(run),
		StartedAt:     started,
		Result:        "succeeded",
	})
	return stats, nil
}

func (s *Service) processPush(ctx context.Context, run repo.SyncRun, mapping repo.Mapping, conn externalsync.Connection, provider externalsync.Provider) (repo.ApplyStats, error) {
	started := time.Now()
	records, err := s.repo.PreparePushRecords(ctx, run.ID, run.ClaimedBy, run.TenantID, mapping.ID, conn.Provider, 100)
	if err != nil {
		return repo.ApplyStats{}, err
	}
	result, err := provider.Push(ctx, externalsync.PushRequest{
		Connection: conn,
		MappingID:  mapping.ID.String(),
		Records:    pushRecordsToCore(records),
	})
	if err != nil {
		return repo.ApplyStats{}, s.recordFailedProviderAttempt(ctx, provider, run, started, err)
	}
	stats, err := s.repo.ApplyPushResult(ctx, repo.ApplyPushInput{
		TenantID:     run.TenantID,
		RunID:        run.ID,
		ConnectionID: run.ConnectionID,
		MappingID:    mapping.ID,
		Provider:     conn.Provider,
		Records:      records,
		Results:      pushResultsToRepo(result.Results),
	})
	if err != nil {
		message := redact(err.Error())
		_ = s.repo.RecordAttempt(ctx, repo.AttemptInput{
			RunID:         run.ID,
			AttemptNumber: attemptNumber(run),
			StartedAt:     started,
			Result:        "failed",
			ErrorKind:     "apply_failed",
			ErrorMessage:  message,
		})
		return repo.ApplyStats{}, newProcessRunError("apply_failed", message, true, err)
	}
	_ = s.repo.RecordAttempt(ctx, repo.AttemptInput{
		RunID:         run.ID,
		AttemptNumber: attemptNumber(run),
		StartedAt:     started,
		Result:        "succeeded",
	})
	return stats, nil
}

func (s *Service) recordFailedProviderAttempt(ctx context.Context, provider externalsync.Provider, run repo.SyncRun, started time.Time, err error) error {
	classified := provider.ClassifyError(err)
	kind := strings.TrimSpace(classified.Kind)
	if kind == "" {
		kind = "provider_error"
	}
	message := strings.TrimSpace(classified.Message)
	if message == "" {
		message = err.Error()
	}
	message = redact(message)
	_ = s.repo.RecordAttempt(ctx, repo.AttemptInput{
		RunID:             run.ID,
		AttemptNumber:     attemptNumber(run),
		StartedAt:         started,
		Result:            "failed",
		HTTPStatus:        classified.HTTPStatus,
		ProviderRequestID: classified.ProviderRequestID,
		RetryAfter:        classified.RetryAfter,
		ErrorKind:         kind,
		ErrorMessage:      message,
	})
	return newProcessRunErrorWithRetryAfter(kind, message, classified.Retryable, classified.RetryAfter, err)
}

func pullRecordsToRepo(records []externalsync.ExternalRecord) []repo.PullRecord {
	out := make([]repo.PullRecord, 0, len(records))
	for _, record := range records {
		var updatedAt *time.Time
		if !record.UpdatedAt.IsZero() {
			updatedAt = ptrext.Of(record.UpdatedAt)
		}
		out = append(out, repo.PullRecord{
			LocalObjectID:     record.LocalObjectID,
			ExternalKey:       record.Key,
			ExternalURL:       record.URL,
			ExternalVersion:   record.Version,
			ExternalUpdatedAt: updatedAt,
			Deleted:           record.Deleted,
			Payload:           append([]byte(nil), record.Payload...),
		})
	}
	return out
}

func pushRecordsToCore(records []repo.PushRecord) []externalsync.LocalRecord {
	out := make([]externalsync.LocalRecord, 0, len(records))
	for _, record := range records {
		out = append(out, externalsync.LocalRecord{
			ID:      record.LocalObjectID,
			Version: record.LocalVersion,
			Payload: append([]byte(nil), record.Payload...),
		})
	}
	return out
}

func pushResultsToRepo(results []externalsync.WriteResult) []repo.PushResult {
	out := make([]repo.PushResult, 0, len(results))
	for _, result := range results {
		row := repo.PushResult{
			LocalObjectID:   result.LocalID,
			ExternalKey:     result.Key,
			ExternalURL:     result.URL,
			ExternalVersion: result.Version,
			Retryable:       result.Retryable,
		}
		if result.Error != nil {
			row.ErrorKind = ptrext.Indirect(result.Error).Kind
			if strings.TrimSpace(row.ErrorKind) == "" {
				row.ErrorKind = "provider_error"
			}
			row.ErrorMessage = ptrext.Indirect(result.Error).Message
			row.Retryable = ptrext.Indirect(result.Error).Retryable
		}
		out = append(out, row)
	}
	return out
}

func processStatus(stats repo.ApplyStats) string {
	if stats.RecordsFailed > 0 || stats.ConflictsCreated > 0 {
		return repo.RunStatusPartial
	}
	return repo.RunStatusSucceeded
}

func combineStats(a, b repo.ApplyStats) repo.ApplyStats {
	return repo.ApplyStats{
		RecordsSeen:      a.RecordsSeen + b.RecordsSeen,
		RecordsChanged:   a.RecordsChanged + b.RecordsChanged,
		RecordsFailed:    a.RecordsFailed + b.RecordsFailed,
		ConflictsCreated: a.ConflictsCreated + b.ConflictsCreated,
	}
}

func attemptNumber(run repo.SyncRun) int {
	if run.Attempts <= 0 {
		return 1
	}
	return run.Attempts
}

func (s *Service) decryptConnection(row repo.Connection) (externalsync.Connection, error) {
	plaintext, err := s.store.DecryptValue(secretstore.EncryptedValue{
		KeyID:      row.CredentialKeyID,
		Ciphertext: row.CredentialCiphertext,
	}, connectionAAD(row.TenantID, row.ID, row.Provider))
	if err != nil {
		return externalsync.Connection{}, err
	}
	return externalsync.Connection{
		ID:             row.ID.String(),
		TenantID:       row.TenantID,
		Provider:       row.Provider,
		Name:           row.Name,
		AuthType:       row.AuthType,
		BaseURL:        row.BaseURL,
		ProviderConfig: row.ProviderConfig,
		Scopes:         row.Scopes,
		Credential:     plaintext,
	}, nil
}

func (s *Service) decryptWebhookSecret(row repo.Connection) ([]byte, error) {
	if row.WebhookSecretKeyID == "" || len(row.WebhookSecretCiphertext) == 0 {
		return nil, fmt.Errorf("%w: webhook secret is not configured", ErrValidation)
	}
	plaintext, err := s.store.DecryptValue(secretstore.EncryptedValue{
		KeyID:      row.WebhookSecretKeyID,
		Ciphertext: row.WebhookSecretCiphertext,
	}, connectionWebhookSecretAAD(row.TenantID, row.ID, row.Provider))
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func normalizeCreateConnection(in CreateConnectionInput) (CreateConnectionInput, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Provider = strings.TrimSpace(strings.ToLower(in.Provider))
	in.Name = strings.TrimSpace(in.Name)
	in.AuthType = strings.TrimSpace(strings.ToLower(in.AuthType))
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.Credential = strings.TrimSpace(in.Credential)
	in.WebhookSecret = strings.TrimSpace(in.WebhookSecret)
	in.Scopes = normalizeStringList(in.Scopes)
	if in.Actor.Type == "" {
		in.Actor.Type = "admin"
	}
	if in.Actor.ID == "" {
		return in, fmt.Errorf("%w: actor is required", ErrValidation)
	}
	cfg, err := normalizeJSONObject(in.ProviderConfigJSON, "provider_config_json")
	if err != nil {
		return in, err
	}
	in.ProviderConfigJSON = cfg
	if in.Credential == "" {
		return in, fmt.Errorf("%w: credential is required", ErrValidation)
	}
	if err := validateWebhookSecret(in.WebhookSecret); err != nil {
		return in, err
	}
	if err := validateConnectionShape(in.Provider, in.Name, in.AuthType, in.ProviderConfigJSON); err != nil {
		return in, err
	}
	return in, nil
}

func validateWebhookSecret(secret string) error {
	if secret != "" && len(secret) < 16 {
		return fmt.Errorf("%w: webhook_secret must be at least 16 characters", ErrValidation)
	}
	return nil
}

func validateConnectionShape(provider, name, authType, providerConfig string) error {
	if err := externalsync.ValidateProviderToken(provider); err != nil {
		return fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if name == "" || len(name) > 200 || !utf8.ValidString(name) {
		return fmt.Errorf("%w: name must be 1..200 valid UTF-8 bytes", ErrValidation)
	}
	switch authType {
	case "api_key", "token", "oauth", "basic":
	default:
		return fmt.Errorf("%w: invalid auth_type", ErrValidation)
	}
	if _, err := normalizeJSONObject(providerConfig, "provider_config_json"); err != nil {
		return err
	}
	return nil
}

func normalizeJSONObject(raw, field string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}", nil
	}
	if len(raw) > 32*1024 {
		return "", fmt.Errorf("%w: %s is too large", ErrValidation, field)
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", fmt.Errorf("%w: %s must be JSON", ErrValidation, field)
	}
	if _, ok := v.(map[string]any); !ok {
		return "", fmt.Errorf("%w: %s must be a JSON object", ErrValidation, field)
	}
	return raw, nil
}

func normalizeMapping(in UpdateMappingInput) (repo.Mapping, error) {
	direction := normalizeDirection(in.Direction)
	if direction == "" {
		return repo.Mapping{}, fmt.Errorf("%w: invalid direction", ErrValidation)
	}
	fieldMapping, err := normalizeJSONObject(in.FieldMappingJSON, "field_mapping_json")
	if err != nil {
		return repo.Mapping{}, err
	}
	statusMapping, err := normalizeJSONObject(in.StatusMappingJSON, "status_mapping_json")
	if err != nil {
		return repo.Mapping{}, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = ptrext.Indirect(in.Enabled)
	}
	conflictPolicy := strings.TrimSpace(in.ConflictPolicy)
	if conflictPolicy == "" {
		conflictPolicy = "manual"
	}
	tombstonePolicy := strings.TrimSpace(in.TombstonePolicy)
	if tombstonePolicy == "" {
		tombstonePolicy = "mark_stale"
	}
	return repo.Mapping{
		ID:              in.ID,
		TenantID:        in.TenantID,
		Direction:       direction,
		FieldMapping:    []byte(fieldMapping),
		StatusMapping:   []byte(statusMapping),
		ConflictPolicy:  conflictPolicy,
		TombstonePolicy: tombstonePolicy,
		Enabled:         enabled,
	}, nil
}

func (s *Service) normalizeEvent(ctx context.Context, in RecordEventInput) (repo.SyncEvent, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	conn, err := s.repo.GetConnection(ctx, in.TenantID, in.ConnectionID)
	if err != nil {
		return repo.SyncEvent{}, err
	}
	mappingID := in.MappingID
	if mappingID != nil {
		mapping, err := s.repo.ResolveRunMapping(ctx, in.TenantID, in.ConnectionID, mappingID)
		if err != nil {
			return repo.SyncEvent{}, err
		}
		mappingID = ptrext.Of(mapping.ID)
	}
	eventType := strings.TrimSpace(in.EventType)
	if eventType == "" || len(eventType) > 200 || !utf8.ValidString(eventType) {
		return repo.SyncEvent{}, fmt.Errorf("%w: event_type must be 1..200 valid UTF-8 bytes", ErrValidation)
	}
	externalEventID := truncateString(in.ExternalEventID, 512)
	payload, err := normalizeJSONObject(in.NormalizedPayloadJSON, "normalized_payload_json")
	if err != nil {
		return repo.SyncEvent{}, err
	}
	digest := strings.ToLower(strings.TrimSpace(in.PayloadDigest))
	if digest == "" {
		digest = eventPayloadDigest([]byte(payload))
	}
	if !validPayloadDigest(digest) {
		return repo.SyncEvent{}, fmt.Errorf("%w: payload_digest must be a 64-character hex SHA-256 digest", ErrValidation)
	}
	signatureStatus := normalizeEventSignatureStatus(in.SignatureStatus)
	if signatureStatus == "" {
		return repo.SyncEvent{}, fmt.Errorf("%w: invalid signature_status", ErrValidation)
	}
	status := repo.EventStatusReceived
	failureReason := ""
	if signatureStatus == repo.EventSignatureFailed {
		status = repo.EventStatusFailed
		failureReason = redact(truncateString(in.FailureReason, 2000))
	}
	dedupeKey := normalizeEventDedupeKey(in.DedupeKey, conn.Provider, eventType, externalEventID, digest)
	if dedupeKey == "" {
		return repo.SyncEvent{}, fmt.Errorf("%w: dedupe_key is required", ErrValidation)
	}
	receivedAt := in.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	return repo.SyncEvent{
		ID:                uuid.New(),
		TenantID:          in.TenantID,
		ConnectionID:      in.ConnectionID,
		MappingID:         mappingID,
		Provider:          conn.Provider,
		EventType:         eventType,
		ExternalEventID:   externalEventID,
		DedupeKey:         dedupeKey,
		SignatureStatus:   signatureStatus,
		Status:            status,
		PayloadDigest:     digest,
		NormalizedPayload: []byte(payload),
		ReceivedAt:        receivedAt,
		FailureReason:     failureReason,
	}, nil
}

func normalizeDirection(direction string) string {
	switch strings.TrimSpace(strings.ToLower(direction)) {
	case "", repo.DirectionPull:
		return repo.DirectionPull
	case repo.DirectionPush:
		return repo.DirectionPush
	case repo.DirectionBidirectional:
		return repo.DirectionBidirectional
	default:
		return ""
	}
}

func mappingAllowsRunDirection(mappingDirection, runDirection string) bool {
	mappingDirection = normalizeDirection(mappingDirection)
	runDirection = normalizeDirection(runDirection)
	if mappingDirection == "" || runDirection == "" {
		return false
	}
	return mappingDirection == repo.DirectionBidirectional || mappingDirection == runDirection
}

func normalizeEventSignatureStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "", repo.EventSignatureVerified:
		return repo.EventSignatureVerified
	case repo.EventSignatureFailed:
		return repo.EventSignatureFailed
	case repo.EventSignatureNotRequired:
		return repo.EventSignatureNotRequired
	default:
		return ""
	}
}

func normalizeEventStatus(status string) string {
	status = strings.TrimSpace(strings.ToLower(status))
	status = strings.TrimPrefix(status, "external_sync_event_status_")
	switch status {
	case "":
		return ""
	case repo.EventStatusReceived, repo.EventStatusReplayed, repo.EventStatusIgnored, repo.EventStatusFailed:
		return status
	default:
		return invalidStatus
	}
}

func normalizeEventDedupeKey(raw, provider, eventType, externalEventID, digest string) string {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		return truncateString(raw, 512)
	}
	if externalEventID != "" {
		return truncateString(provider+":"+eventType+":"+externalEventID, 512)
	}
	return truncateString(provider+":"+eventType+":"+digest, 512)
}

func eventPayloadDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func verifyGitHubSignatureSHA256(header string, secret, body []byte) bool {
	if len(secret) == 0 {
		return false
	}
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

func normalizeGitHubWebhookPayload(eventType, deliveryID string, body []byte) string {
	out := map[string]any{
		"provider":    "github",
		"event_type":  eventType,
		"delivery_id": deliveryID,
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		out["parse_error"] = "invalid_json"
		return mustMarshalJSONObject(out)
	}
	copyJSONField(out, payload, "action")
	copyJSONField(out, payload, "zen")
	if repository, ok := jsonObject(payload["repository"]); ok {
		out["repository"] = pickJSONFields(repository, "id", "node_id", "name", "full_name", "html_url", "private")
	}
	if issue, ok := jsonObject(payload["issue"]); ok {
		out["issue"] = pickJSONFields(issue, "id", "node_id", "number", "title", "state", "html_url", "created_at", "updated_at", "closed_at")
		if user, ok := jsonObject(issue["user"]); ok {
			out["issue_user"] = pickJSONFields(user, "id", "login", "html_url", "type")
		}
	}
	if sender, ok := jsonObject(payload["sender"]); ok {
		out["sender"] = pickJSONFields(sender, "id", "login", "html_url", "type")
	}
	return mustMarshalJSONObject(out)
}

func copyJSONField(dst map[string]any, src map[string]any, key string) {
	if value, ok := src[key]; ok {
		dst[key] = value
	}
}

func jsonObject(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok
}

func pickJSONFields(src map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, key := range keys {
		if value, ok := src[key]; ok {
			out[key] = value
		}
	}
	return out
}

func mustMarshalJSONObject(value map[string]any) string {
	out, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(out)
}

func validPayloadDigest(raw string) bool {
	decoded, err := hex.DecodeString(raw)
	return err == nil && len(decoded) == sha256.Size
}

func normalizeRunStatus(status string) string {
	status = strings.TrimSpace(strings.ToLower(status))
	status = strings.TrimPrefix(status, "external_sync_run_status_")
	switch status {
	case "":
		return ""
	case repo.RunStatusQueued, repo.RunStatusRunning, repo.RunStatusSucceeded,
		repo.RunStatusPartial, repo.RunStatusFailed, repo.RunStatusCancelled, repo.RunStatusDead:
		return status
	default:
		return invalidStatus
	}
}

func normalizeStringList(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		out = append(out, s)
		seen[s] = true
	}
	return out
}

func normalizeObjectSchemas(in []externalsync.ObjectSchema) []externalsync.ObjectSchema {
	out := make([]externalsync.ObjectSchema, 0, len(in))
	seen := map[string]bool{}
	for _, schema := range in {
		objectType := strings.TrimSpace(schema.Type)
		if objectType == "" || seen[objectType] {
			continue
		}
		out = append(out, externalsync.ObjectSchema{
			Type:           objectType,
			Fields:         normalizeStringList(schema.Fields),
			RequiredFields: normalizeStringList(schema.RequiredFields),
			WritableFields: normalizeStringList(schema.WritableFields),
		})
		seen[objectType] = true
	}
	return out
}

func (r *QualificationResult) addCheck(name, status, summary string, detail map[string]any) {
	if status == QualificationStatusFailed {
		r.Ready = false
	}
	r.Checks = append(r.Checks, QualificationCheck{
		Name:       name,
		Status:     status,
		Summary:    summary,
		DetailJSON: qualificationDetail(detail),
	})
}

func (r *QualificationResult) addSchemaMetadataCheck(schemas []externalsync.ObjectSchema) {
	missingFields := []string{}
	missingWritable := []string{}
	for _, schema := range schemas {
		if len(schema.Fields) == 0 {
			missingFields = append(missingFields, schema.Type)
		}
		if len(schema.WritableFields) == 0 {
			missingWritable = append(missingWritable, schema.Type)
		}
	}
	status := QualificationStatusOK
	summary := "Provider schema exposes field capability metadata"
	if len(missingFields) > 0 {
		status = QualificationStatusFailed
		summary = "Provider schema is missing fields"
	} else if len(missingWritable) > 0 {
		status = QualificationStatusWarning
		summary = "Provider schema is missing writable field metadata"
	}
	r.addCheck("schema_metadata", status, summary, map[string]any{
		"missing_fields":          missingFields,
		"missing_writable_fields": missingWritable,
	})
}

func qualificationDetail(detail map[string]any) string {
	if len(detail) == 0 {
		return "{}"
	}
	return mustMarshalJSONObject(detail)
}

func schemaForObject(schemas []externalsync.ObjectSchema, objectType string) externalsync.ObjectSchema {
	for _, schema := range schemas {
		if schema.Type == objectType {
			return schema
		}
	}
	return externalsync.ObjectSchema{Type: objectType}
}

func validateMappingPreview(fieldMappingJSON, statusMappingJSON string, schema externalsync.ObjectSchema) ([]string, []string) {
	var errorsOut []string
	var warnings []string
	fieldMapping, err := parseJSONObject(fieldMappingJSON, "field_mapping_json")
	if err != nil {
		errorsOut = append(errorsOut, err.Error())
	}
	if _, err := parseJSONObject(statusMappingJSON, "status_mapping_json"); err != nil {
		errorsOut = append(errorsOut, err.Error())
	}
	if len(errorsOut) > 0 || len(fieldMapping) == 0 {
		return errorsOut, warnings
	}
	targetFields := stringSet(schema.Fields)
	writableFields := stringSet(schema.WritableFields)
	for _, target := range sortedObjectKeys(fieldMapping) {
		if len(targetFields) > 0 && !targetFields[target] {
			warnings = append(warnings, "field_mapping_json references unknown provider field "+target)
		}
		if len(writableFields) > 0 && !writableFields[target] {
			warnings = append(warnings, "field_mapping_json references read-only provider field "+target)
		}
	}
	for _, required := range schema.RequiredFields {
		if !fieldMapping[required] {
			errorsOut = append(errorsOut, "field_mapping_json must map required provider field "+required)
		}
	}
	return errorsOut, warnings
}

func parseJSONObject(raw, field string) (map[string]bool, error) {
	normalized, err := normalizeJSONObject(raw, field)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(normalized), &object); err != nil { // ptrext:allow unmarshal-out-param
		return nil, fmt.Errorf("%w: %s must be JSON", ErrValidation, field)
	}
	out := map[string]bool{}
	for key := range object {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = true
		}
	}
	return out, nil
}

func stringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func sortedObjectKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func uniqueUUIDs(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		out = append(out, id)
		seen[id] = true
	}
	return out
}

func validResolution(resolution string) bool {
	switch resolution {
	case "local_wins", "external_wins", "manual_merge", "ignored":
		return true
	default:
		return false
	}
}

func nonEmpty(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func connectionStatus(enabled bool) string {
	if enabled {
		return repo.ConnectionStatusActive
	}
	return repo.ConnectionStatusDisabled
}

func connectionAAD(tenantID string, id uuid.UUID, provider string) []byte {
	return []byte("external_connections:" + tenantID + ":" + id.String() + ":" + provider)
}

func connectionWebhookSecretAAD(tenantID string, id uuid.UUID, provider string) []byte {
	return []byte("external_connections:" + tenantID + ":" + id.String() + ":" + provider + ":webhook_secret")
}

func truncateString(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func uuidToString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

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

func (s *Service) auditQualification(ctx context.Context, actor auditlogsvc.Actor, tenantID string, conn *repo.Connection, result QualificationResult) {
	counts := map[string]int{}
	checks := make([]map[string]string, 0, len(result.Checks))
	for _, check := range result.Checks {
		counts[check.Status]++
		checks = append(checks, map[string]string{
			"name":   check.Name,
			"status": check.Status,
		})
	}
	s.record(ctx, actor, tenantID, "external_connection.qualify",
		"external_connection", conn.ID.String(), "Qualified external sync connection", nil,
		map[string]any{
			"provider":      conn.Provider,
			"ready":         result.Ready,
			"check_counts":  counts,
			"check_results": checks,
		})
}

func (s *Service) recordConflictResolutionMetric(ctx context.Context, tenantID string, conflict repo.ConflictRow) {
	provider := "unknown"
	objectType := "unknown"
	if mapping, err := s.repo.GetMapping(ctx, tenantID, conflict.MappingID); err == nil && mapping != nil {
		objectType = mapping.ExternalObjectType
		if conn, err := s.repo.GetConnection(ctx, tenantID, mapping.ConnectionID); err == nil && conn != nil {
			provider = conn.Provider
		}
	}
	metrics.ExternalSyncConflictsTotal.WithLabelValues(
		metricLabel(provider),
		metricLabel(objectType),
		metricLabel(conflict.Resolution),
	).Inc()
}

func connectionAudit(c *repo.Connection) map[string]any {
	if c == nil {
		return nil
	}
	return map[string]any{
		"id":                        c.ID.String(),
		"provider":                  c.Provider,
		"name":                      c.Name,
		"enabled":                   c.Enabled,
		"status":                    c.Status,
		"auth_type":                 c.AuthType,
		"base_url":                  c.BaseURL,
		"provider_config_json":      string(c.ProviderConfig),
		"scopes":                    c.Scopes,
		"webhook_secret_configured": c.WebhookSecretKeyID != "" && len(c.WebhookSecretCiphertext) > 0,
		"last_test_status":          c.LastTestStatus,
	}
}

func testAudit(provider string, result externalsync.CheckResult) map[string]any {
	return map[string]any{
		"provider":   provider,
		"ok":         result.OK,
		"latency_ms": result.Latency.Milliseconds(),
		"error":      redact(result.Error),
		"request_id": result.RequestID,
	}
}

func mappingAudit(m *repo.Mapping) map[string]any {
	if m == nil {
		return nil
	}
	return map[string]any{
		"id":                   m.ID.String(),
		"connection_id":        m.ConnectionID.String(),
		"local_object_type":    m.LocalObjectType,
		"external_object_type": m.ExternalObjectType,
		"direction":            m.Direction,
		"conflict_policy":      m.ConflictPolicy,
		"tombstone_policy":     m.TombstonePolicy,
		"enabled":              m.Enabled,
	}
}

func runAudit(r *repo.SyncRun) map[string]any {
	if r == nil {
		return nil
	}
	return map[string]any{
		"id":            r.ID.String(),
		"connection_id": r.ConnectionID.String(),
		"mapping_id":    uuidToString(r.MappingID),
		"direction":     r.Direction,
		"trigger":       r.Trigger,
		"status":        r.Status,
	}
}

func cursorResetAudit(result *repo.ResetCursorResult) map[string]any {
	if result == nil {
		return nil
	}
	return map[string]any{
		"mapping_id":    result.Mapping.ID.String(),
		"connection_id": result.Mapping.ConnectionID.String(),
		"run_id":        result.Run.ID.String(),
		"run_direction": result.Run.Direction,
		"run_trigger":   result.Run.Trigger,
	}
}

func backfillAudit(result *repo.BackfillResult, resetCursor bool) map[string]any {
	if result == nil {
		return nil
	}
	return map[string]any{
		"mapping_id":    result.Mapping.ID.String(),
		"connection_id": result.Mapping.ConnectionID.String(),
		"run_id":        result.Run.ID.String(),
		"run_direction": result.Run.Direction,
		"run_trigger":   result.Run.Trigger,
		"reset_cursor":  resetCursor,
	}
}

func failureAudit(f *repo.RecordFailure) map[string]any {
	if f == nil {
		return nil
	}
	return map[string]any{
		"id":           f.ID.String(),
		"run_id":       f.RunID.String(),
		"mapping_id":   f.MappingID.String(),
		"operation":    f.Operation,
		"external_key": f.ExternalKey,
		"retry_mode":   f.RetryMode,
	}
}

func conflictAudit(c *repo.ConflictRow) map[string]any {
	if c == nil {
		return nil
	}
	return map[string]any{
		"id":           c.ID.String(),
		"mapping_id":   c.MappingID.String(),
		"external_key": c.ExternalKey,
		"status":       c.Status,
		"resolution":   c.Resolution,
	}
}

func eventAudit(e *repo.SyncEvent) map[string]any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"id":                e.ID.String(),
		"connection_id":     e.ConnectionID.String(),
		"mapping_id":        uuidToString(e.MappingID),
		"provider":          e.Provider,
		"event_type":        e.EventType,
		"external_event_id": e.ExternalEventID,
		"dedupe_key":        e.DedupeKey,
		"signature_status":  e.SignatureStatus,
		"status":            e.Status,
		"payload_digest":    e.PayloadDigest,
	}
}

func eventReplayAudit(e *repo.SyncEvent, r *repo.SyncRun) map[string]any {
	out := eventAudit(e)
	if out == nil {
		out = map[string]any{}
	}
	if r != nil {
		out["run_id"] = r.ID.String()
		out["run_trigger"] = r.Trigger
		out["run_direction"] = r.Direction
	}
	return out
}
