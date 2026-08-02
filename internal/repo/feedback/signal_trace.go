package feedback

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	SignalTraceStageSource       = "source_event"
	SignalTraceStageEnrichment   = "enrichment"
	SignalTraceStageRequest      = "request"
	SignalTraceStageNotification = "notification"
	SignalTraceStageSurvey       = "survey"

	signalTraceStatusCompleted = "completed"
	signalTraceStatusFailed    = "failed"
	signalTraceStatusMissing   = "missing"
	signalTraceStatusObserved  = "observed"
	signalTraceStatusPending   = "pending"
)

type SignalTrace struct {
	FeedbackID     int64
	TenantID       string
	SignalTraceID  string
	Source         string
	Stages         []SignalTraceStage
	Events         []SignalTraceEvent
	GeneratedAt    time.Time
	Complete       bool
	MissingStages  []string
	TerminalStatus string
}

type SignalTraceStage struct {
	Key         string
	Label       string
	Status      string
	EventCount  int
	LastEventAt *time.Time
}

type SignalTraceEvent struct {
	Stage      string
	Kind       string
	Status     string
	TraceID    string
	Summary    string
	OccurredAt time.Time
	Metadata   map[string]any
}

type signalTraceRoot struct {
	ID                 int64
	TenantID           string
	SignalTraceID      string
	Source             string
	Type               string
	UserID             string
	EnrichmentStatus   string
	EnrichmentError    string
	EnrichmentAttempts int
	CreatedAt          time.Time
	EnrichedAt         sql.NullTime
	SourceMeta         []byte
}

type traceStageDefinition struct {
	key   string
	label string
}

var signalTraceStages = []traceStageDefinition{
	{SignalTraceStageSource, "Source event"},
	{SignalTraceStageEnrichment, "AI enrichment"},
	{SignalTraceStageRequest, "Customer request"},
	{SignalTraceStageNotification, "Customer notification"},
	{SignalTraceStageSurvey, "Survey follow-up"},
}

func (r *FeedbackRepo) FeedbackSignalTrace(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) (SignalTrace, error) {
	root, err := r.loadSignalTraceRoot(ctx, tenantID, feedbackID)
	if err != nil {
		return SignalTrace{}, err
	}
	events := []SignalTraceEvent{
		sourceSignalTraceEvent(root),
		enrichmentSignalTraceEvent(root),
	}
	limit = normalizeSignalTraceLimit(limit)
	loaders := []func(context.Context, string, int64, int) ([]SignalTraceEvent, error){
		r.semanticRunTraceEvents,
		r.llmAuditTraceEvents,
		r.qualityFailureTraceEvents,
		r.legacyOutboxTraceEvents,
		r.customerRequestTraceEvents,
		r.requestNotificationTraceEvents,
		r.surveyTraceEvents,
	}
	for _, load := range loaders {
		next, err := load(ctx, tenantID, feedbackID, limit)
		if err != nil {
			return SignalTrace{}, err
		}
		events = append(events, next...)
	}
	return buildSignalTrace(root, events, limit), nil
}

func normalizeSignalTraceLimit(limit int) int {
	if limit <= 0 {
		return 80
	}
	if limit > 150 {
		return 150
	}
	return limit
}

func (r *FeedbackRepo) loadSignalTraceRoot(ctx context.Context, tenantID string, feedbackID int64) (signalTraceRoot, error) {
	var root signalTraceRoot
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, signal_trace_id, source, type, user_id,
		       enrichment_status, COALESCE(enrichment_error, ''),
		       enrichment_attempts, created_at, enriched_at, source_meta
		FROM user_feedback
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, feedbackID,
	).Scan(
		&root.ID, &root.TenantID, &root.SignalTraceID, &root.Source, &root.Type, &root.UserID, // ptrext:allow scan-target
		&root.EnrichmentStatus, &root.EnrichmentError, &root.EnrichmentAttempts, // ptrext:allow scan-target
		&root.CreatedAt, &root.EnrichedAt, &root.SourceMeta, // ptrext:allow scan-target
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return signalTraceRoot{}, ErrFeedbackNotFound
	}
	if err != nil {
		return signalTraceRoot{}, fmt.Errorf("load feedback signal trace root: %w", err)
	}
	return root, nil
}

func sourceSignalTraceEvent(root signalTraceRoot) SignalTraceEvent {
	meta := map[string]any{
		"source":           root.Source,
		"type":             root.Type,
		"user_id":          root.UserID,
		"source_meta_keys": sourceMetaKeys(root.SourceMeta),
	}
	return SignalTraceEvent{
		Stage:      SignalTraceStageSource,
		Kind:       "source_captured",
		Status:     signalTraceStatusCompleted,
		TraceID:    root.SignalTraceID,
		Summary:    "Feedback source event captured",
		OccurredAt: root.CreatedAt,
		Metadata:   meta,
	}
}

func enrichmentSignalTraceEvent(root signalTraceRoot) SignalTraceEvent {
	occurredAt := root.CreatedAt
	if root.EnrichedAt.Valid {
		occurredAt = root.EnrichedAt.Time
	}
	meta := map[string]any{
		"attempts": root.EnrichmentAttempts,
	}
	if root.EnrichmentError != "" {
		meta["error"] = root.EnrichmentError
	}
	return SignalTraceEvent{
		Stage:      SignalTraceStageEnrichment,
		Kind:       "enrichment_state",
		Status:     normalizeTraceStatus(root.EnrichmentStatus),
		TraceID:    root.SignalTraceID,
		Summary:    "Current enrichment status: " + root.EnrichmentStatus,
		OccurredAt: occurredAt,
		Metadata:   meta,
	}
}

func sourceMetaKeys(raw []byte) []string {
	var meta map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &meta) != nil {
		return nil
	}
	keys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (r *FeedbackRepo) semanticRunTraceEvents(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) ([]SignalTraceEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, created_at, prompt_version, model, source, logical_model,
		       provider_model, channel_id, channel_name, confidence
		FROM semantic_extraction_runs
		WHERE tenant_id = $1 AND subject_type = $2 AND subject_id = $3
		ORDER BY created_at DESC, id DESC
		LIMIT $4`,
		tenantID, SemanticSubjectFeedback, feedbackID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("signal trace semantic runs: %w", err)
	}
	defer rows.Close()
	var events []SignalTraceEvent
	for rows.Next() {
		event, err := scanSemanticRunTraceEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanSemanticRunTraceEvent(rows pgx.Rows) (SignalTraceEvent, error) {
	var id int64
	var createdAt time.Time
	var promptVersion, model, source, logicalModel, providerModel, channelID, channelName string
	var confidence []byte
	if err := rows.Scan(
		&id, &createdAt, &promptVersion, &model, &source, &logicalModel, // ptrext:allow scan-target
		&providerModel, &channelID, &channelName, &confidence, // ptrext:allow scan-target
	); err != nil {
		return SignalTraceEvent{}, fmt.Errorf("scan semantic run trace event: %w", err)
	}
	return SignalTraceEvent{
		Stage:      SignalTraceStageEnrichment,
		Kind:       "semantic_extraction",
		Status:     signalTraceStatusCompleted,
		Summary:    "Semantic extraction evidence recorded",
		OccurredAt: createdAt,
		Metadata: map[string]any{
			"semantic_run_id": id,
			"prompt_version":  promptVersion,
			"model":           model,
			"source":          source,
			"logical_model":   logicalModel,
			"provider_model":  providerModel,
			"channel_id":      channelID,
			"channel_name":    channelName,
			"confidence":      jsonMap(confidence),
		},
	}, nil
}

func (r *FeedbackRepo) llmAuditTraceEvents(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) ([]SignalTraceEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, created_at, inbound_trace_id, otel_trace_id, model_id, purpose,
		       prompt_tokens, completion_tokens, cost_usd::text, status, error, latency_ms
		FROM llm_audit
		WHERE tenant_id = $1 AND feedback_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT $3`,
		tenantID, feedbackID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("signal trace llm audit: %w", err)
	}
	defer rows.Close()
	var events []SignalTraceEvent
	for rows.Next() {
		event, err := scanLLMAuditTraceEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanLLMAuditTraceEvent(rows pgx.Rows) (SignalTraceEvent, error) {
	var id int64
	var createdAt time.Time
	var inboundTraceID, otelTraceID, modelID, purpose, costUSD, status, llmErr string
	var promptTokens, completionTokens, latencyMS int
	if err := rows.Scan(
		&id, &createdAt, &inboundTraceID, &otelTraceID, &modelID, &purpose, // ptrext:allow scan-target
		&promptTokens, &completionTokens, &costUSD, &status, &llmErr, &latencyMS, // ptrext:allow scan-target
	); err != nil {
		return SignalTraceEvent{}, fmt.Errorf("scan llm audit trace event: %w", err)
	}
	return SignalTraceEvent{
		Stage:      SignalTraceStageEnrichment,
		Kind:       "llm_call",
		Status:     normalizeTraceStatus(status),
		TraceID:    inboundTraceID,
		Summary:    "LLM call recorded for " + purpose,
		OccurredAt: createdAt,
		Metadata: map[string]any{
			"llm_audit_id":      id,
			"otel_trace_id":     otelTraceID,
			"model_id":          modelID,
			"purpose":           purpose,
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"cost_usd":          costUSD,
			"error":             llmErr,
			"latency_ms":        latencyMS,
		},
	}, nil
}

func (r *FeedbackRepo) qualityFailureTraceEvents(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) ([]SignalTraceEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, event_at, reason_class, logical_model, provider_model,
		       channel_id, channel_name, source, attempts, terminal
		FROM classification_quality_failure_events
		WHERE tenant_id = $1 AND feedback_id = $2
		ORDER BY event_at DESC, id DESC
		LIMIT $3`,
		tenantID, feedbackID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("signal trace quality failures: %w", err)
	}
	defer rows.Close()
	var events []SignalTraceEvent
	for rows.Next() {
		event, err := scanQualityFailureTraceEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanQualityFailureTraceEvent(rows pgx.Rows) (SignalTraceEvent, error) {
	var id int64
	var eventAt time.Time
	var reasonClass, logicalModel, providerModel, channelID, channelName, source string
	var attempts int
	var terminal bool
	if err := rows.Scan(
		&id, &eventAt, &reasonClass, &logicalModel, &providerModel, // ptrext:allow scan-target
		&channelID, &channelName, &source, &attempts, &terminal, // ptrext:allow scan-target
	); err != nil {
		return SignalTraceEvent{}, fmt.Errorf("scan quality failure trace event: %w", err)
	}
	return SignalTraceEvent{
		Stage:      SignalTraceStageEnrichment,
		Kind:       "classification_failure",
		Status:     signalTraceStatusFailed,
		Summary:    "Classification failed: " + reasonClass,
		OccurredAt: eventAt,
		Metadata: map[string]any{
			"failure_event_id": id,
			"reason_class":     reasonClass,
			"logical_model":    logicalModel,
			"provider_model":   providerModel,
			"channel_id":       channelID,
			"channel_name":     channelName,
			"source":           source,
			"attempts":         attempts,
			"terminal":         terminal,
		},
	}, nil
}

func (r *FeedbackRepo) legacyOutboxTraceEvents(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) ([]SignalTraceEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, destination_type, audience, status, attempts, trace_id,
		       COALESCE(last_error, ''), COALESCE(dead_reason, ''),
		       created_at, delivered_at
		FROM notify_outbox
		WHERE tenant_id = $1 AND feedback_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT $3`,
		tenantID, feedbackID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("signal trace legacy outbox: %w", err)
	}
	defer rows.Close()
	var events []SignalTraceEvent
	for rows.Next() {
		event, err := scanLegacyOutboxTraceEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanLegacyOutboxTraceEvent(rows pgx.Rows) (SignalTraceEvent, error) {
	var id int64
	var destinationType, audience, status, traceID, lastError, deadReason string
	var attempts int
	var createdAt time.Time
	var deliveredAt sql.NullTime
	if err := rows.Scan(
		&id, &destinationType, &audience, &status, &attempts, &traceID, // ptrext:allow scan-target
		&lastError, &deadReason, &createdAt, &deliveredAt, // ptrext:allow scan-target
	); err != nil {
		return SignalTraceEvent{}, fmt.Errorf("scan legacy outbox trace event: %w", err)
	}
	occurredAt := createdAt
	if deliveredAt.Valid {
		occurredAt = deliveredAt.Time
	}
	return SignalTraceEvent{
		Stage:      SignalTraceStageNotification,
		Kind:       "feedback_outbox_delivery",
		Status:     normalizeTraceStatus(status),
		TraceID:    traceID,
		Summary:    "Feedback outbox delivery " + status,
		OccurredAt: occurredAt,
		Metadata: map[string]any{
			"outbox_id":        id,
			"destination_type": destinationType,
			"audience":         audience,
			"attempts":         attempts,
			"last_error":       lastError,
			"dead_reason":      deadReason,
		},
	}, nil
}

func (r *FeedbackRepo) customerRequestTraceEvents(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) ([]SignalTraceEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT cr.id::text, cr.display_id, cr.title, cr.status, cr.priority,
		       l.importance, l.created_by, l.created_at
		FROM customer_request_feedback_links l
		JOIN customer_requests cr
		  ON cr.tenant_id = l.tenant_id AND cr.id = l.request_id
		WHERE l.tenant_id = $1 AND l.feedback_id = $2
		ORDER BY l.created_at DESC, cr.updated_at DESC
		LIMIT $3`,
		tenantID, feedbackID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("signal trace customer requests: %w", err)
	}
	defer rows.Close()
	var events []SignalTraceEvent
	for rows.Next() {
		event, err := scanCustomerRequestTraceEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanCustomerRequestTraceEvent(rows pgx.Rows) (SignalTraceEvent, error) {
	var requestID, displayID, title, status, priority, importance, createdBy string
	var createdAt time.Time
	if err := rows.Scan(
		&requestID, &displayID, &title, &status, &priority, // ptrext:allow scan-target
		&importance, &createdBy, &createdAt, // ptrext:allow scan-target
	); err != nil {
		return SignalTraceEvent{}, fmt.Errorf("scan customer request trace event: %w", err)
	}
	return SignalTraceEvent{
		Stage:      SignalTraceStageRequest,
		Kind:       "request_linked",
		Status:     normalizeTraceStatus(status),
		Summary:    "Feedback linked to request " + displayID,
		OccurredAt: createdAt,
		Metadata: map[string]any{
			"request_id": requestID,
			"display_id": displayID,
			"title":      title,
			"status":     status,
			"priority":   priority,
			"importance": importance,
			"created_by": createdBy,
		},
	}, nil
}

func (r *FeedbackRepo) requestNotificationTraceEvents(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) ([]SignalTraceEvent, error) {
	events, err := r.requestNotificationEventTraceEvents(ctx, tenantID, feedbackID, limit)
	if err != nil {
		return nil, err
	}
	deliveries, err := r.requestNotificationDeliveryTraceEvents(ctx, tenantID, feedbackID, limit)
	if err != nil {
		return nil, err
	}
	return append(events, deliveries...), nil
}

func (r *FeedbackRepo) requestNotificationEventTraceEvents(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) ([]SignalTraceEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT e.id::text, e.primary_request_id::text, e.event_type,
		       e.audience_scope, e.status, e.attempts, e.last_error,
		       e.created_at, e.resolved_at
		FROM customer_request_feedback_links l
		JOIN customer_request_notification_events e
		  ON e.tenant_id = l.tenant_id AND e.primary_request_id = l.request_id
		WHERE l.tenant_id = $1 AND l.feedback_id = $2
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT $3`,
		tenantID, feedbackID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("signal trace notification events: %w", err)
	}
	defer rows.Close()
	var events []SignalTraceEvent
	for rows.Next() {
		event, err := scanRequestNotificationEventTraceEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanRequestNotificationEventTraceEvent(rows pgx.Rows) (SignalTraceEvent, error) {
	var eventID, requestID, eventType, audienceScope, status, lastError string
	var attempts int
	var createdAt time.Time
	var resolvedAt sql.NullTime
	if err := rows.Scan(
		&eventID, &requestID, &eventType, &audienceScope, &status, // ptrext:allow scan-target
		&attempts, &lastError, &createdAt, &resolvedAt, // ptrext:allow scan-target
	); err != nil {
		return SignalTraceEvent{}, fmt.Errorf("scan notification event trace event: %w", err)
	}
	occurredAt := createdAt
	if resolvedAt.Valid {
		occurredAt = resolvedAt.Time
	}
	return SignalTraceEvent{
		Stage:      SignalTraceStageNotification,
		Kind:       "request_notification_event",
		Status:     normalizeTraceStatus(status),
		Summary:    "Customer notification event " + status,
		OccurredAt: occurredAt,
		Metadata: map[string]any{
			"event_id":       eventID,
			"request_id":     requestID,
			"event_type":     eventType,
			"audience_scope": audienceScope,
			"attempts":       attempts,
			"last_error":     lastError,
		},
	}, nil
}

func (r *FeedbackRepo) requestNotificationDeliveryTraceEvents(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) ([]SignalTraceEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id, d.event_id::text, e.primary_request_id::text, d.channel,
		       d.status, d.attempts, d.failure_kind, COALESCE(d.http_status, 0),
		       d.last_error, d.dead_reason, d.trace_id, d.created_at, d.delivered_at
		FROM customer_request_feedback_links l
		JOIN customer_request_notification_events e
		  ON e.tenant_id = l.tenant_id AND e.primary_request_id = l.request_id
		JOIN customer_request_notification_deliveries d
		  ON d.tenant_id = e.tenant_id AND d.event_id = e.id
		WHERE l.tenant_id = $1 AND l.feedback_id = $2
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT $3`,
		tenantID, feedbackID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("signal trace notification deliveries: %w", err)
	}
	defer rows.Close()
	var events []SignalTraceEvent
	for rows.Next() {
		event, err := scanRequestNotificationDeliveryTraceEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanRequestNotificationDeliveryTraceEvent(rows pgx.Rows) (SignalTraceEvent, error) {
	var deliveryID int64
	var eventID, requestID, channel, status, failureKind, lastError, deadReason, traceID string
	var attempts, httpStatus int
	var createdAt time.Time
	var deliveredAt sql.NullTime
	if err := rows.Scan(
		&deliveryID, &eventID, &requestID, &channel, &status, &attempts, // ptrext:allow scan-target
		&failureKind, &httpStatus, &lastError, &deadReason, &traceID, // ptrext:allow scan-target
		&createdAt, &deliveredAt, // ptrext:allow scan-target
	); err != nil {
		return SignalTraceEvent{}, fmt.Errorf("scan notification delivery trace event: %w", err)
	}
	occurredAt := createdAt
	if deliveredAt.Valid {
		occurredAt = deliveredAt.Time
	}
	return SignalTraceEvent{
		Stage:      SignalTraceStageNotification,
		Kind:       "request_notification_delivery",
		Status:     normalizeTraceStatus(status),
		TraceID:    traceID,
		Summary:    "Customer notification delivery " + status,
		OccurredAt: occurredAt,
		Metadata: map[string]any{
			"delivery_id":  deliveryID,
			"event_id":     eventID,
			"request_id":   requestID,
			"channel":      channel,
			"attempts":     attempts,
			"failure_kind": failureKind,
			"http_status":  httpStatus,
			"last_error":   lastError,
			"dead_reason":  deadReason,
		},
	}, nil
}

func (r *FeedbackRepo) surveyTraceEvents(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) ([]SignalTraceEvent, error) {
	invitations, err := r.surveyInvitationTraceEvents(ctx, tenantID, feedbackID, limit)
	if err != nil {
		return nil, err
	}
	responses, err := r.surveyResponseTraceEvents(ctx, tenantID, feedbackID, limit)
	if err != nil {
		return nil, err
	}
	reviews, err := r.surveyLowScoreReviewTraceEvents(ctx, tenantID, feedbackID, limit)
	if err != nil {
		return nil, err
	}
	events := append(invitations, responses...)
	return append(events, reviews...), nil
}

func (r *FeedbackRepo) surveyInvitationTraceEvents(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) ([]SignalTraceEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT si.id::text, si.campaign_id::text, si.request_id::text,
		       si.source_type, si.source_id, si.distribution_mode,
		       si.delivery_status, si.response_status, si.suppression_status,
		       si.provider, si.provider_message_id, si.attempts,
		       si.failure_kind, COALESCE(si.http_status, 0),
		       si.created_at, si.delivered_at, si.responded_at
		FROM customer_request_feedback_links l
		JOIN survey_invitations si
		  ON si.tenant_id = l.tenant_id AND si.request_id = l.request_id
		WHERE l.tenant_id = $1 AND l.feedback_id = $2
		ORDER BY si.created_at DESC, si.id DESC
		LIMIT $3`,
		tenantID, feedbackID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("signal trace survey invitations: %w", err)
	}
	defer rows.Close()
	var events []SignalTraceEvent
	for rows.Next() {
		event, err := scanSurveyInvitationTraceEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanSurveyInvitationTraceEvent(rows pgx.Rows) (SignalTraceEvent, error) {
	var invitationID, campaignID, requestID, sourceType, sourceID, distributionMode string
	var deliveryStatus, responseStatus, suppressionStatus, provider, providerMessageID string
	var failureKind string
	var attempts, httpStatus int
	var createdAt time.Time
	var deliveredAt, respondedAt sql.NullTime
	if err := rows.Scan(
		&invitationID, &campaignID, &requestID, &sourceType, &sourceID, // ptrext:allow scan-target
		&distributionMode, &deliveryStatus, &responseStatus, &suppressionStatus, // ptrext:allow scan-target
		&provider, &providerMessageID, &attempts, &failureKind, &httpStatus, // ptrext:allow scan-target
		&createdAt, &deliveredAt, &respondedAt, // ptrext:allow scan-target
	); err != nil {
		return SignalTraceEvent{}, fmt.Errorf("scan survey invitation trace event: %w", err)
	}
	occurredAt := coalesceTraceTime(createdAt, deliveredAt, respondedAt)
	return SignalTraceEvent{
		Stage:      SignalTraceStageSurvey,
		Kind:       "survey_invitation",
		Status:     normalizeSurveyInvitationStatus(deliveryStatus, responseStatus, suppressionStatus),
		Summary:    "Survey invitation " + deliveryStatus,
		OccurredAt: occurredAt,
		Metadata: map[string]any{
			"invitation_id":       invitationID,
			"campaign_id":         campaignID,
			"request_id":          requestID,
			"source_type":         sourceType,
			"source_id":           sourceID,
			"distribution_mode":   distributionMode,
			"delivery_status":     deliveryStatus,
			"response_status":     responseStatus,
			"suppression_status":  suppressionStatus,
			"provider":            provider,
			"provider_message_id": providerMessageID,
			"attempts":            attempts,
			"failure_kind":        failureKind,
			"http_status":         httpStatus,
		},
	}, nil
}

func (r *FeedbackRepo) surveyResponseTraceEvents(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) ([]SignalTraceEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT sr.id::text, sr.invitation_id::text, sr.campaign_id::text,
		       sr.request_id::text, sr.score, sr.locale, sr.submitted_at
		FROM customer_request_feedback_links l
		JOIN survey_responses sr
		  ON sr.tenant_id = l.tenant_id AND sr.request_id = l.request_id
		WHERE l.tenant_id = $1 AND l.feedback_id = $2
		ORDER BY sr.submitted_at DESC, sr.id DESC
		LIMIT $3`,
		tenantID, feedbackID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("signal trace survey responses: %w", err)
	}
	defer rows.Close()
	var events []SignalTraceEvent
	for rows.Next() {
		event, err := scanSurveyResponseTraceEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanSurveyResponseTraceEvent(rows pgx.Rows) (SignalTraceEvent, error) {
	var responseID, invitationID, campaignID, requestID, locale string
	var score int
	var submittedAt time.Time
	if err := rows.Scan(
		&responseID, &invitationID, &campaignID, &requestID, // ptrext:allow scan-target
		&score, &locale, &submittedAt, // ptrext:allow scan-target
	); err != nil {
		return SignalTraceEvent{}, fmt.Errorf("scan survey response trace event: %w", err)
	}
	return SignalTraceEvent{
		Stage:      SignalTraceStageSurvey,
		Kind:       "survey_response",
		Status:     signalTraceStatusCompleted,
		Summary:    "Survey response received",
		OccurredAt: submittedAt,
		Metadata: map[string]any{
			"response_id":   responseID,
			"invitation_id": invitationID,
			"campaign_id":   campaignID,
			"request_id":    requestID,
			"score":         score,
			"locale":        locale,
		},
	}, nil
}

func (r *FeedbackRepo) surveyLowScoreReviewTraceEvents(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) ([]SignalTraceEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT lsr.response_id::text, sr.request_id::text, lsr.status, lsr.severity,
		       lsr.root_cause, lsr.customer_contacted, lsr.due_at,
		       lsr.reviewed_at, lsr.created_at, lsr.updated_at
		FROM customer_request_feedback_links l
		JOIN survey_responses sr
		  ON sr.tenant_id = l.tenant_id AND sr.request_id = l.request_id
		JOIN survey_low_score_reviews lsr
		  ON lsr.tenant_id = sr.tenant_id AND lsr.response_id = sr.id
		WHERE l.tenant_id = $1 AND l.feedback_id = $2
		ORDER BY lsr.updated_at DESC, lsr.response_id DESC
		LIMIT $3`,
		tenantID, feedbackID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("signal trace low score reviews: %w", err)
	}
	defer rows.Close()
	var events []SignalTraceEvent
	for rows.Next() {
		event, err := scanSurveyLowScoreReviewTraceEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanSurveyLowScoreReviewTraceEvent(rows pgx.Rows) (SignalTraceEvent, error) {
	var responseID, requestID, status, severity, rootCause string
	var customerContacted bool
	var dueAt, reviewedAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := rows.Scan(
		&responseID, &requestID, &status, &severity, &rootCause, // ptrext:allow scan-target
		&customerContacted, &dueAt, &reviewedAt, &createdAt, &updatedAt, // ptrext:allow scan-target
	); err != nil {
		return SignalTraceEvent{}, fmt.Errorf("scan low score review trace event: %w", err)
	}
	occurredAt := updatedAt
	if reviewedAt.Valid {
		occurredAt = reviewedAt.Time
	}
	return SignalTraceEvent{
		Stage:      SignalTraceStageSurvey,
		Kind:       "survey_low_score_review",
		Status:     normalizeTraceStatus(status),
		Summary:    "Low score review " + status,
		OccurredAt: occurredAt,
		Metadata: map[string]any{
			"response_id":        responseID,
			"request_id":         requestID,
			"severity":           severity,
			"root_cause":         rootCause,
			"customer_contacted": customerContacted,
			"due_at":             nullableTraceTime(dueAt),
			"created_at":         createdAt.UTC().Format(time.RFC3339),
		},
	}, nil
}

func buildSignalTrace(root signalTraceRoot, events []SignalTraceEvent, limit int) SignalTrace {
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].OccurredAt.Equal(events[j].OccurredAt) {
			return events[i].Stage < events[j].Stage
		}
		return events[i].OccurredAt.Before(events[j].OccurredAt)
	})
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	stages := buildSignalTraceStages(events)
	missing := missingTraceStages(stages)
	return SignalTrace{
		FeedbackID:     root.ID,
		TenantID:       root.TenantID,
		SignalTraceID:  root.SignalTraceID,
		Source:         root.Source,
		Stages:         stages,
		Events:         events,
		GeneratedAt:    time.Now().UTC(),
		Complete:       len(missing) == 0,
		MissingStages:  missing,
		TerminalStatus: terminalTraceStatus(stages),
	}
}

func buildSignalTraceStages(events []SignalTraceEvent) []SignalTraceStage {
	out := make([]SignalTraceStage, 0, len(signalTraceStages))
	for _, def := range signalTraceStages {
		stageEvents := traceEventsForStage(events, def.key)
		stage := SignalTraceStage{
			Key:        def.key,
			Label:      def.label,
			Status:     statusForTraceEvents(stageEvents),
			EventCount: len(stageEvents),
		}
		if len(stageEvents) > 0 {
			last := latestTraceEventTime(stageEvents)
			stage.LastEventAt = ptrext.Of(last)
		}
		out = append(out, stage)
	}
	return out
}

func traceEventsForStage(events []SignalTraceEvent, stage string) []SignalTraceEvent {
	var out []SignalTraceEvent
	for _, event := range events {
		if event.Stage == stage {
			out = append(out, event)
		}
	}
	return out
}

func statusForTraceEvents(events []SignalTraceEvent) string {
	if len(events) == 0 {
		return signalTraceStatusMissing
	}
	status := signalTraceStatusCompleted
	for _, event := range events {
		switch normalizeTraceStatus(event.Status) {
		case signalTraceStatusFailed:
			return signalTraceStatusFailed
		case signalTraceStatusPending:
			status = signalTraceStatusPending
		case signalTraceStatusObserved:
			if status != signalTraceStatusPending {
				status = signalTraceStatusObserved
			}
		}
	}
	return status
}

func latestTraceEventTime(events []SignalTraceEvent) time.Time {
	latest := events[0].OccurredAt
	for _, event := range events[1:] {
		if event.OccurredAt.After(latest) {
			latest = event.OccurredAt
		}
	}
	return latest.UTC()
}

func missingTraceStages(stages []SignalTraceStage) []string {
	var missing []string
	for _, stage := range stages {
		if stage.Status == signalTraceStatusMissing {
			missing = append(missing, stage.Key)
		}
	}
	return missing
}

func terminalTraceStatus(stages []SignalTraceStage) string {
	status := signalTraceStatusCompleted
	for _, stage := range stages {
		switch stage.Status {
		case signalTraceStatusFailed:
			return signalTraceStatusFailed
		case signalTraceStatusMissing:
			status = signalTraceStatusMissing
		case signalTraceStatusPending:
			if status != signalTraceStatusMissing {
				status = signalTraceStatusPending
			}
		case signalTraceStatusObserved:
			if status == signalTraceStatusCompleted {
				status = signalTraceStatusObserved
			}
		}
	}
	return status
}

func normalizeTraceStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "", "ok", "done", "delivered", "resolved", "completed", "shipped", "cancelled", "dismissed", "not_applicable":
		return signalTraceStatusCompleted
	case "pending", "enriching", "resolving", "open", "planned", "in_progress", "in_review", "delayed", "started", "not_started":
		return signalTraceStatusPending
	case "failed", "dead", "error", "rejected", "bounced", "complained", "suppressed", "expired":
		return signalTraceStatusFailed
	default:
		return signalTraceStatusObserved
	}
}

func normalizeSurveyInvitationStatus(deliveryStatus, responseStatus, suppressionStatus string) string {
	if strings.TrimSpace(suppressionStatus) == "suppressed" {
		return signalTraceStatusFailed
	}
	if normalizeTraceStatus(responseStatus) == signalTraceStatusCompleted {
		return signalTraceStatusCompleted
	}
	return normalizeTraceStatus(deliveryStatus)
}

func jsonMap(raw []byte) map[string]any {
	var out map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &out) != nil {
		return map[string]any{}
	}
	return out
}

func coalesceTraceTime(fallback time.Time, candidates ...sql.NullTime) time.Time {
	for i := len(candidates) - 1; i >= 0; i-- {
		if candidates[i].Valid {
			return candidates[i].Time
		}
	}
	return fallback
}

func nullableTraceTime(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}
