// SPDX-License-Identifier: Apache-2.0

package replydraft

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

var (
	ErrWorkflowNotFound         = errors.New("reply draft workflow not found")
	ErrWorkflowInvalidState     = errors.New("reply draft workflow invalid state")
	ErrWorkflowHookNotFound     = errors.New("reply send hook not configured")
	ErrWorkflowAlreadySent      = errors.New("reply draft already sent")
	ErrWorkflowInProgress       = errors.New("reply send already in progress")
	ErrWorkflowStale            = errors.New("reply draft source changed")
	ErrWorkflowRevisionConflict = errors.New("reply draft revision conflict")
	ErrDeliveryNotFound         = errors.New("reply delivery attempt not found")
	ErrInvalidSendHook          = errors.New("invalid reply send hook")
	ErrInvalidIdempotencyKey    = errors.New("invalid idempotency key")
	ErrIdempotencyConflict      = errors.New("idempotency key used with different request parameters")
	replySendIdempotencyKeyRE   = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
	replySendEgressPolicy       = nethardening.Policy{}
)

type SecretStore interface {
	EncryptValue(plaintext, aad []byte) (secretstore.EncryptedValue, error)
	DecryptValue(value secretstore.EncryptedValue, aad []byte) ([]byte, error)
}

type WorkflowRepo interface {
	GetActiveDraft(ctx context.Context, tenantID string, feedbackID int64) (replydraftrepo.Draft, error)
	EditDraft(ctx context.Context, tenantID string, feedbackID int64, content string, expectedRevision int64, actor replydraftrepo.Actor) (replydraftrepo.Draft, error)
	ApproveDraft(ctx context.Context, tenantID string, feedbackID int64, expectedRevision int64, actor replydraftrepo.Actor) (replydraftrepo.Draft, error)
	RejectDraft(ctx context.Context, tenantID string, feedbackID int64, expectedRevision int64, actor replydraftrepo.Actor) (replydraftrepo.Draft, error)
	ListRevisions(ctx context.Context, tenantID string, feedbackID int64) ([]replydraftrepo.Revision, error)
	ListEvents(ctx context.Context, tenantID string, feedbackID int64) ([]replydraftrepo.Event, error)
	UpsertHook(ctx context.Context, in replydraftrepo.HookUpsert) (replydraftrepo.Hook, error)
	GetActiveHook(ctx context.Context, tenantID string) (replydraftrepo.Hook, error)
	GetLatestHook(ctx context.Context, tenantID string) (replydraftrepo.Hook, error)
	DisableHook(ctx context.Context, tenantID, actorID string) (replydraftrepo.Hook, error)
	ListDeliveryAttempts(ctx context.Context, tenantID string, limit int) ([]replydraftrepo.DeliveryAttempt, error)
	GetDeliveryHealth(ctx context.Context, tenantID string) (replydraftrepo.DeliveryHealth, error)
	GetDeliveryAttempt(ctx context.Context, tenantID string, attemptID string) (replydraftrepo.DeliveryAttempt, error)
	PrepareHookTest(ctx context.Context, tenantID string, idempotencyKey string, actor replydraftrepo.Actor) (replydraftrepo.DeliveryPrepare, error)
	ClaimDueDeliveries(ctx context.Context, limit int, actor replydraftrepo.Actor) ([]replydraftrepo.DeliveryPrepare, error)
	ResetStalePendingDeliveries(ctx context.Context, olderThan time.Duration) (int64, error)
	PrepareRedelivery(ctx context.Context, tenantID string, attemptID string, actor replydraftrepo.Actor) (replydraftrepo.DeliveryPrepare, error)
	PrepareDelivery(ctx context.Context, tenantID string, feedbackID int64, idempotencyKey string, expectedRevision int64, actor replydraftrepo.Actor) (replydraftrepo.DeliveryPrepare, error)
	MarkDeliveryAccepted(ctx context.Context, attemptID string, httpStatus int, externalMessageID string) (replydraftrepo.Draft, error)
	MarkDeliveryFailed(ctx context.Context, attemptID string, httpStatus int, cause error) error
}

type Workflow struct {
	repo    WorkflowRepo
	secrets SecretStore
	sender  ReplySender
	surveys SurveySink
}

type SurveyReplySentEvent struct {
	TenantID          string
	FeedbackID        int64
	DraftID           string
	AttemptID         string
	RevisionID        string
	ExternalMessageID string
	ActorID           string
}

type SurveySink interface {
	RecordReplySent(ctx context.Context, event SurveyReplySentEvent) (int, error)
}

type Snapshot struct {
	Draft          *replydraftrepo.Draft
	Revisions      []replydraftrepo.Revision
	Events         []replydraftrepo.Event
	HookConfigured bool
	AllowedActions []string
	Blockers       []string
}

type HookConfig struct {
	Hook       replydraftrepo.Hook
	SecretOnce string
}

type SendResult struct {
	Snapshot  Snapshot
	FromCache bool
}

type HookTestResult struct {
	Attempt replydraftrepo.DeliveryAttempt
}

func NewWorkflow(repo WorkflowRepo, secrets SecretStore, sender ReplySender) *Workflow {
	if sender == nil {
		sender = NewWebhookReplySender(nil)
	}
	return ptrext.Of(Workflow{repo: repo, secrets: secrets, sender: sender})
}

func (s *Workflow) SetSurveySink(sink SurveySink) {
	s.surveys = sink
}

// SetEgressPolicy installs the reply-send-hook SSRF policy used for config-time
// validation. The sender also uses notify.Transport, which enforces the same
// policy at dial time after DNS resolution.
func SetEgressPolicy(p nethardening.Policy) { replySendEgressPolicy = p }

func (s *Workflow) Snapshot(ctx context.Context, tenantID string, feedbackID int64) (Snapshot, error) {
	draft, err := s.repo.GetActiveDraft(ctx, tenantID, feedbackID)
	if errors.Is(err, replydraftrepo.ErrDraftNotFound) {
		return Snapshot{}, nil
	}
	if err != nil {
		return Snapshot{}, mapRepoErr(err)
	}
	revisions, err := s.repo.ListRevisions(ctx, tenantID, feedbackID)
	if err != nil {
		return Snapshot{}, err
	}
	events, err := s.repo.ListEvents(ctx, tenantID, feedbackID)
	if err != nil {
		return Snapshot{}, err
	}
	hookConfigured := s.hookConfigured(ctx, tenantID)
	return buildSnapshot(draft, revisions, events, hookConfigured), nil
}

func (s *Workflow) Edit(
	ctx context.Context, tenantID string, feedbackID int64, content string, expectedRevision int64, actor replydraftrepo.Actor,
) (Snapshot, error) {
	if strings.TrimSpace(content) == "" {
		return Snapshot{}, ErrWorkflowInvalidState
	}
	if _, err := s.repo.EditDraft(ctx, tenantID, feedbackID, content, expectedRevision, actor); err != nil {
		return Snapshot{}, mapRepoErr(err)
	}
	return s.Snapshot(ctx, tenantID, feedbackID)
}

func (s *Workflow) Approve(
	ctx context.Context, tenantID string, feedbackID int64, expectedRevision int64, actor replydraftrepo.Actor,
) (Snapshot, error) {
	if _, err := s.repo.ApproveDraft(ctx, tenantID, feedbackID, expectedRevision, actor); err != nil {
		return Snapshot{}, mapRepoErr(err)
	}
	return s.Snapshot(ctx, tenantID, feedbackID)
}

func (s *Workflow) Reject(
	ctx context.Context, tenantID string, feedbackID int64, expectedRevision int64, actor replydraftrepo.Actor,
) (Snapshot, error) {
	if _, err := s.repo.RejectDraft(ctx, tenantID, feedbackID, expectedRevision, actor); err != nil {
		return Snapshot{}, mapRepoErr(err)
	}
	return s.Snapshot(ctx, tenantID, feedbackID)
}

func (s *Workflow) Send(
	ctx context.Context, tenantID string, feedbackID int64, idempotencyKey string, expectedRevision int64, actor replydraftrepo.Actor,
) (SendResult, error) {
	if !replySendIdempotencyKeyRE.MatchString(idempotencyKey) {
		return SendResult{}, ErrInvalidIdempotencyKey
	}
	prep, err := s.repo.PrepareDelivery(ctx, tenantID, feedbackID, idempotencyKey, expectedRevision, actor)
	if err != nil {
		return SendResult{}, mapRepoErr(err)
	}
	if prep.FromCache {
		snap, err := s.Snapshot(ctx, tenantID, feedbackID)
		return SendResult{Snapshot: snap, FromCache: true}, err
	}
	result, err := s.sender.Send(ctx, prep, s.decryptHook)
	if err != nil {
		if markErr := s.repo.MarkDeliveryFailed(ctx, prep.AttemptID, result.HTTPStatus, err); markErr != nil {
			return SendResult{}, mapRepoErr(markErr)
		}
		return SendResult{}, err
	}
	accepted, err := s.repo.MarkDeliveryAccepted(ctx, prep.AttemptID, result.HTTPStatus, result.ExternalMessageID)
	if err != nil {
		return SendResult{}, err
	}
	s.recordSurveyReplySent(ctx, prep, accepted, result.ExternalMessageID)
	snap, err := s.Snapshot(ctx, tenantID, feedbackID)
	return SendResult{Snapshot: snap}, err
}

func (s *Workflow) UpsertHook(
	ctx context.Context, tenantID string, name string, rawURL string, rawSecret string, enabled bool, actorID string,
) (HookConfig, error) {
	hookName, err := normalizeHookName(name)
	if err != nil {
		return HookConfig{}, err
	}
	parsed, err := validateHookURL(rawURL)
	if err != nil {
		return HookConfig{}, err
	}
	encURL, err := s.secrets.EncryptValue([]byte(parsed.String()), hookURLAAD(tenantID))
	if err != nil {
		return HookConfig{}, fmt.Errorf("encrypt reply send hook url: %w", err)
	}
	encSecret, secretOnce, err := s.encryptHookSecret(ctx, tenantID, rawSecret)
	if err != nil {
		return HookConfig{}, err
	}
	hook, err := s.repo.UpsertHook(ctx, replydraftrepo.HookUpsert{
		TenantID: tenantID, Name: hookName, URLCiphertext: encURL.Ciphertext,
		URLKeyID: encURL.KeyID, URLFingerprint: hookFingerprint(parsed.String()),
		URLHost: parsed.Hostname(), SecretCiphertext: encSecret.Ciphertext,
		SecretKeyID: encSecret.KeyID, Enabled: enabled, ActorID: actorID,
	})
	if err != nil {
		return HookConfig{}, err
	}
	return HookConfig{Hook: hook, SecretOnce: secretOnce}, nil
}

func (s *Workflow) GetHook(ctx context.Context, tenantID string) (HookConfig, error) {
	hook, err := s.repo.GetLatestHook(ctx, tenantID)
	if errors.Is(err, replydraftrepo.ErrHookNotFound) {
		return HookConfig{}, ErrWorkflowHookNotFound
	}
	if err != nil {
		return HookConfig{}, err
	}
	return HookConfig{Hook: hook}, nil
}

func (s *Workflow) DisableHook(ctx context.Context, tenantID, actorID string) (HookConfig, error) {
	hook, err := s.repo.DisableHook(ctx, tenantID, actorID)
	if err != nil {
		return HookConfig{}, mapRepoErr(err)
	}
	return HookConfig{Hook: hook}, nil
}

func (s *Workflow) ListDeliveries(ctx context.Context, tenantID string, limit int) ([]replydraftrepo.DeliveryAttempt, error) {
	attempts, err := s.repo.ListDeliveryAttempts(ctx, tenantID, limit)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return attempts, nil
}

func (s *Workflow) DeliveryHealth(ctx context.Context, tenantID string) (replydraftrepo.DeliveryHealth, error) {
	health, err := s.repo.GetDeliveryHealth(ctx, tenantID)
	if err != nil {
		return replydraftrepo.DeliveryHealth{}, mapRepoErr(err)
	}
	return health, nil
}

func (s *Workflow) TestHook(
	ctx context.Context, tenantID string, idempotencyKey string, actor replydraftrepo.Actor,
) (HookTestResult, error) {
	key, err := normalizeDeliveryKey(idempotencyKey, "reply_test")
	if err != nil {
		return HookTestResult{}, err
	}
	prep, err := s.repo.PrepareHookTest(ctx, tenantID, key, actor)
	if err != nil {
		return HookTestResult{}, mapRepoErr(err)
	}
	if prep.FromCache {
		attempt, err := s.repo.GetDeliveryAttempt(ctx, tenantID, prep.AttemptID)
		return HookTestResult{Attempt: attempt}, mapRepoErr(err)
	}
	attempt, err := s.executeObservableDelivery(ctx, tenantID, prep)
	return HookTestResult{Attempt: attempt}, err
}

func (s *Workflow) Redeliver(
	ctx context.Context, tenantID string, attemptID string, actor replydraftrepo.Actor,
) (replydraftrepo.DeliveryAttempt, error) {
	if strings.TrimSpace(attemptID) == "" {
		return replydraftrepo.DeliveryAttempt{}, ErrDeliveryNotFound
	}
	prep, err := s.repo.PrepareRedelivery(ctx, tenantID, attemptID, actor)
	if err != nil {
		return replydraftrepo.DeliveryAttempt{}, mapRepoErr(err)
	}
	return s.executeObservableDelivery(ctx, tenantID, prep)
}

func (s *Workflow) ClaimDueDeliveries(
	ctx context.Context, limit int, actor replydraftrepo.Actor,
) ([]replydraftrepo.DeliveryPrepare, error) {
	return s.repo.ClaimDueDeliveries(ctx, limit, actor)
}

func (s *Workflow) ResetStalePendingDeliveries(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.repo.ResetStalePendingDeliveries(ctx, olderThan)
}

func (s *Workflow) executeObservableDelivery(
	ctx context.Context, tenantID string, prep replydraftrepo.DeliveryPrepare,
) (replydraftrepo.DeliveryAttempt, error) {
	result, sendErr := s.sender.Send(ctx, prep, s.decryptHook)
	if sendErr != nil {
		if err := s.repo.MarkDeliveryFailed(ctx, prep.AttemptID, result.HTTPStatus, sendErr); err != nil {
			return replydraftrepo.DeliveryAttempt{}, mapRepoErr(err)
		}
		attempt, err := s.repo.GetDeliveryAttempt(ctx, tenantID, prep.AttemptID)
		return attempt, mapRepoErr(err)
	}
	accepted, err := s.repo.MarkDeliveryAccepted(ctx, prep.AttemptID, result.HTTPStatus, result.ExternalMessageID)
	if err != nil {
		return replydraftrepo.DeliveryAttempt{}, mapRepoErr(err)
	}
	s.recordSurveyReplySent(ctx, prep, accepted, result.ExternalMessageID)
	attempt, err := s.repo.GetDeliveryAttempt(ctx, tenantID, prep.AttemptID)
	return attempt, mapRepoErr(err)
}

func (s *Workflow) recordSurveyReplySent(
	ctx context.Context,
	prep replydraftrepo.DeliveryPrepare,
	accepted replydraftrepo.Draft,
	externalMessageID string,
) {
	const where = "service.replydraft.recordSurveyReplySent"
	if s.surveys == nil {
		return
	}
	revisionID := prep.Revision.ID
	if accepted.SentRevisionID != "" {
		revisionID = accepted.SentRevisionID
	}
	_, err := s.surveys.RecordReplySent(ctx, SurveyReplySentEvent{
		TenantID:          accepted.TenantID,
		FeedbackID:        accepted.FeedbackID,
		DraftID:           accepted.ID,
		AttemptID:         prep.AttemptID,
		RevisionID:        revisionID,
		ExternalMessageID: externalMessageID,
		ActorID:           prep.Actor.ID,
	})
	if err != nil {
		logext.Warnf(ctx, "[%s] survey trigger failed,tenant_id:%s,feedback_id:%d,attempt_id:%s,err:%+v",
			where, accepted.TenantID, accepted.FeedbackID, prep.AttemptID, err.Error())
	}
}

func (s *Workflow) hookConfigured(ctx context.Context, tenantID string) bool {
	_, err := s.repo.GetActiveHook(ctx, tenantID)
	return err == nil
}

func buildSnapshot(
	draft replydraftrepo.Draft,
	revisions []replydraftrepo.Revision,
	events []replydraftrepo.Event,
	hookConfigured bool,
) Snapshot {
	allowed, blockers := actionsForDraft(draft, hookConfigured)
	return Snapshot{
		Draft:          ptrext.Of(draft),
		Revisions:      revisions,
		Events:         events,
		HookConfigured: hookConfigured,
		AllowedActions: allowed,
		Blockers:       blockers,
	}
}

func actionsForDraft(draft replydraftrepo.Draft, hookConfigured bool) ([]string, []string) {
	switch draft.Status {
	case replydraftrepo.StatusSuggested, replydraftrepo.StatusEdited:
		if hookConfigured {
			return []string{"edit", "approve", "reject", "regenerate"}, nil
		}
		return []string{"edit", "reject", "regenerate"}, []string{"reply_send_hook_missing"}
	case replydraftrepo.StatusApproved:
		if hookConfigured {
			return []string{"edit", "reject", "send", "regenerate"}, nil
		}
		return []string{"edit", "reject", "regenerate"}, []string{"reply_send_hook_missing"}
	case replydraftrepo.StatusSendPending:
		return nil, []string{"send_in_progress"}
	case replydraftrepo.StatusSendFailed:
		if hookConfigured {
			return []string{"edit", "reject", "send", "regenerate"}, []string{"send_failed"}
		}
		return []string{"edit", "reject", "regenerate"}, []string{"reply_send_hook_missing", "send_failed"}
	case replydraftrepo.StatusStale:
		blocker := draft.LastBlocker
		if blocker == "" {
			blocker = "stale_source"
		}
		return []string{"edit", "regenerate"}, []string{blocker}
	case replydraftrepo.StatusRejected, replydraftrepo.StatusSent:
		return []string{"regenerate"}, nil
	default:
		return nil, []string{"unknown_status"}
	}
}

func mapRepoErr(err error) error {
	switch {
	case errors.Is(err, replydraftrepo.ErrDraftNotFound), errors.Is(err, replydraftrepo.ErrNotFound):
		return ErrWorkflowNotFound
	case errors.Is(err, replydraftrepo.ErrInvalidDraftState):
		return ErrWorkflowInvalidState
	case errors.Is(err, replydraftrepo.ErrHookNotFound):
		return ErrWorkflowHookNotFound
	case errors.Is(err, replydraftrepo.ErrAlreadySent):
		return ErrWorkflowAlreadySent
	case errors.Is(err, replydraftrepo.ErrRequestInProgress):
		return ErrWorkflowInProgress
	case errors.Is(err, replydraftrepo.ErrIdempotencyConflict):
		return ErrIdempotencyConflict
	case errors.Is(err, replydraftrepo.ErrStaleDraft):
		return ErrWorkflowStale
	case errors.Is(err, replydraftrepo.ErrRevisionConflict):
		return ErrWorkflowRevisionConflict
	case errors.Is(err, replydraftrepo.ErrDeliveryNotFound):
		return ErrDeliveryNotFound
	default:
		return err
	}
}

func normalizeDeliveryKey(raw string, prefix string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		generated, err := randomDeliveryKey(prefix)
		if err != nil {
			return "", err
		}
		key = generated
	}
	if !replySendIdempotencyKeyRE.MatchString(key) {
		return "", ErrInvalidIdempotencyKey
	}
	return key, nil
}

func randomDeliveryKey(prefix string) (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate delivery idempotency key: %w", err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func validateHookURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: malformed url", ErrInvalidSendHook)
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("%w: hook url must include a host", ErrInvalidSendHook)
	}
	loopbackHTTP := parsed.Scheme == "http" && nethardening.IsLoopbackHost(parsed.Hostname())
	if parsed.Scheme != "https" && !loopbackHTTP {
		return nil, fmt.Errorf("%w: hook url must be https or loopback http", ErrInvalidSendHook)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("%w: hook url must not include credentials", ErrInvalidSendHook)
	}
	parsed.Fragment = ""
	if err := replySendEgressPolicy.ValidateURL(parsed.String()); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSendHook, err)
	}
	return parsed, nil
}

func normalizeHookName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "Default reply send hook", nil
	}
	if utf8.RuneCountInString(name) > 120 {
		return "", fmt.Errorf("%w: hook name must be at most 120 characters", ErrInvalidSendHook)
	}
	if strings.ContainsFunc(name, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	}) {
		return "", fmt.Errorf("%w: hook name must not include control characters", ErrInvalidSendHook)
	}
	return name, nil
}

func normalizeHookSecret(raw string) (string, bool, error) {
	secret := strings.TrimSpace(raw)
	if secret == "" {
		generated, err := randomSecret()
		return generated, true, err
	}
	if len(secret) < 16 {
		return "", false, fmt.Errorf("%w: secret must be at least 16 characters", ErrInvalidSendHook)
	}
	return secret, false, nil
}

func (s *Workflow) encryptHookSecret(
	ctx context.Context, tenantID string, rawSecret string,
) (secretstore.EncryptedValue, string, error) {
	secret := strings.TrimSpace(rawSecret)
	if secret == "" {
		existing, err := s.repo.GetLatestHook(ctx, tenantID)
		if err == nil && len(existing.SecretCiphertext) > 0 {
			return secretstore.EncryptedValue{KeyID: existing.SecretKeyID, Ciphertext: existing.SecretCiphertext}, "", nil
		}
		if err != nil && !errors.Is(err, replydraftrepo.ErrHookNotFound) {
			return secretstore.EncryptedValue{}, "", err
		}
	}
	secret, generated, err := normalizeHookSecret(secret)
	if err != nil {
		return secretstore.EncryptedValue{}, "", err
	}
	encSecret, err := s.secrets.EncryptValue([]byte(secret), hookSecretAAD(tenantID))
	if err != nil {
		return secretstore.EncryptedValue{}, "", fmt.Errorf("encrypt reply send hook secret: %w", err)
	}
	if generated {
		return encSecret, secret, nil
	}
	return encSecret, "", nil
}

func randomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate reply hook secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hookFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func hookURLAAD(tenantID string) []byte {
	return []byte("reply_send_hooks:url:" + tenantID)
}

func hookSecretAAD(tenantID string) []byte {
	return []byte("reply_send_hooks:secret:" + tenantID)
}

type HookTarget struct {
	URL    string
	Secret string
}

type ReplySender interface {
	Send(ctx context.Context, prep replydraftrepo.DeliveryPrepare, decrypt func(context.Context, replydraftrepo.Hook) (HookTarget, error)) (DeliverySendResult, error)
}

type DeliverySendResult struct {
	HTTPStatus        int
	ExternalMessageID string
}

type WebhookReplySender struct {
	transport *notify.Transport
}

func NewWebhookReplySender(transport *notify.Transport) *WebhookReplySender {
	if transport == nil {
		transport = notify.NewTransport(nil, notify.DefaultRetry())
	}
	return ptrext.Of(WebhookReplySender{transport: transport})
}

func (s *WebhookReplySender) Send(
	ctx context.Context,
	prep replydraftrepo.DeliveryPrepare,
	decrypt func(context.Context, replydraftrepo.Hook) (HookTarget, error),
) (DeliverySendResult, error) {
	target, err := decrypt(ctx, prep.Hook)
	if err != nil {
		return DeliverySendResult{}, err
	}
	body, err := json.Marshal(replySendPayloadFromPrep(prep))
	if err != nil {
		return DeliverySendResult{}, fmt.Errorf("marshal reply send payload: %w", err)
	}
	var status int
	var externalMessageID string
	check := func(ctx context.Context, code int, body []byte) error {
		status = code
		if err := outbound.CheckWebhook("reply-send-hook")(ctx, code, body); err != nil {
			return err
		}
		externalMessageID = externalMessageIDFromHookResponse(body)
		return nil
	}
	build := buildReplySendRequest(target, body, prep)
	err = s.transport.Send(ctx, "reply-send-hook", func(ctx context.Context) (*http.Request, error) {
		status = 0
		externalMessageID = ""
		return build(ctx)
	}, check)
	return DeliverySendResult{HTTPStatus: status, ExternalMessageID: externalMessageID}, err
}

func externalMessageIDFromHookResponse(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	for _, key := range []string{"external_message_id", "externalMessageId", "message_id", "messageId", "id"} {
		raw, ok := payload[key].(string)
		if !ok {
			continue
		}
		value := strings.TrimSpace(raw)
		if value == "" || len(value) > 256 {
			continue
		}
		return value
	}
	return ""
}

func (s *Workflow) decryptHook(ctx context.Context, hook replydraftrepo.Hook) (HookTarget, error) {
	rawURL, err := s.secrets.DecryptValue(secretstore.EncryptedValue{KeyID: hook.URLKeyID, Ciphertext: hook.URLCiphertext}, hookURLAAD(hook.TenantID))
	if err != nil {
		return HookTarget{}, fmt.Errorf("decrypt reply send hook url: %w", err)
	}
	rawSecret, err := s.secrets.DecryptValue(secretstore.EncryptedValue{KeyID: hook.SecretKeyID, Ciphertext: hook.SecretCiphertext}, hookSecretAAD(hook.TenantID))
	if err != nil {
		return HookTarget{}, fmt.Errorf("decrypt reply send hook secret: %w", err)
	}
	return HookTarget{URL: string(rawURL), Secret: string(rawSecret)}, nil
}

type replySendPayload struct {
	Version        string         `json:"version"`
	EventType      string         `json:"event_type"`
	TenantID       string         `json:"tenant_id"`
	FeedbackID     string         `json:"feedback_id,omitempty"`
	DraftID        string         `json:"draft_id,omitempty"`
	RevisionID     string         `json:"revision_id,omitempty"`
	CycleNo        int            `json:"cycle_no,omitempty"`
	RevisionNo     int            `json:"revision_no,omitempty"`
	Text           string         `json:"text,omitempty"`
	IdempotencyKey string         `json:"idempotency_key"`
	SentAt         string         `json:"sent_at"`
	Test           bool           `json:"test,omitempty"`
	Message        string         `json:"message,omitempty"`
	Metadata       map[string]any `json:"metadata"`
}

func replySendPayloadFromPrep(prep replydraftrepo.DeliveryPrepare) replySendPayload {
	eventType := prep.EventType
	if eventType == "" {
		eventType = replydraftrepo.DeliveryEventReplySend
	}
	payload := replySendPayload{
		Version:        "1",
		EventType:      eventType,
		TenantID:       prep.Hook.TenantID,
		IdempotencyKey: prep.IdempotencyKey,
		SentAt:         time.Now().UTC().Format(time.RFC3339),
		Metadata: map[string]any{
			"hook_id":    prep.Hook.ID,
			"attempt_id": prep.AttemptID,
		},
	}
	if eventType == replydraftrepo.DeliveryEventReplyTest {
		payload.Test = true
		payload.Message = "Attune reply send hook test event"
		return payload
	}
	return replySendPayload{
		Version:        "1",
		EventType:      eventType,
		TenantID:       prep.Draft.TenantID,
		FeedbackID:     strconv.FormatInt(prep.Draft.FeedbackID, 10),
		DraftID:        prep.Draft.ID,
		RevisionID:     prep.Revision.ID,
		CycleNo:        prep.Draft.CycleNo,
		RevisionNo:     prep.Revision.RevisionNo,
		Text:           prep.Revision.Content,
		IdempotencyKey: prep.IdempotencyKey,
		SentAt:         time.Now().UTC().Format(time.RFC3339),
		Metadata: map[string]any{
			"hook_id":    prep.Hook.ID,
			"attempt_id": prep.AttemptID,
		},
	}
}

func buildReplySendRequest(target HookTarget, body []byte, prep replydraftrepo.DeliveryPrepare) notify.RequestBuilder {
	return func(ctx context.Context) (*http.Request, error) {
		timestamp := time.Now().UTC().Format(time.RFC3339)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("User-Agent", "attune/1.0")
		req.Header.Set("X-Attune-Timestamp", timestamp)
		req.Header.Set("X-Attune-Signature", versionedReplySignature(body, target.Secret, timestamp))
		req.Header.Set("X-Attune-Delivery-Id", prep.AttemptID)
		req.Header.Set("X-Attune-Idempotency-Key", prep.IdempotencyKey)
		return req, nil
	}
}

func versionedReplySignature(body []byte, secret string, timestamp string) string {
	signedBody := append([]byte(timestamp+"."), body...)
	return "v1=" + strings.TrimPrefix(outbound.BytesSign(signedBody, secret), "sha256=")
}
