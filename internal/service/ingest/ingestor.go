package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
	"github.com/Phixsura/attune/internal/repo/idempotency"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// Idempotency sentinel errors, mapped by the handler to 409 / 400.
var (
	// ErrIdempotencyConflict: same key, different request body.
	ErrIdempotencyConflict = errors.New("idempotency key used with different request")
	// ErrRequestInProgress: same key, a concurrent request still pending.
	ErrRequestInProgress = errors.New("request with this idempotency key is in progress")
	// ErrInvalidIdempotencyKey: the supplied key is malformed.
	ErrInvalidIdempotencyKey = errors.New("invalid idempotency key (8-64 chars, [A-Za-z0-9_-])")
)

// idempotencyKeyRe matches the idempotency_keys CHECK constraint (length 8-64,
// chars [A-Za-z0-9_-]) so a malformed key fails fast with 400 rather than a DB
// constraint 500.
var idempotencyKeyRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,64}$`)

// Ingestor is the business-layer entry point for "a new feedback row
// just arrived". It validates, persists via repo, and triggers async
// enrichment. Handlers depend on this concrete type (not an interface)
// because there is one ingest pipeline today.
type Ingestor struct {
	repo      feedbackInserter
	submitter enrich.Submitter
	// idem dedups retried ingests when a request carries an Idempotency-Key.
	// nil disables idempotency (the path simply inserts).
	idem idempotency.Store
}

type feedbackInserter interface {
	Insert(
		ctx context.Context,
		tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
		in domain.IngestInput,
	) (int64, error)
}

func NewIngestor(r feedbackInserter, submitter enrich.Submitter, idem idempotency.Store) *Ingestor {
	return ptrext.Of(Ingestor{repo: r, submitter: submitter, idem: idem})
}

// IngestRow validates input, persists it, and fires off best-effort
// enrichment. Returns the new row id. tenantID is the TEXT tenants.id;
// keyID is the UUID of the api key used to authenticate (uuid.Nil for
// non-API-key sources like the #66 inbound webhook / email adapters).
//
// When in.IdempotencyKey is set and a store is configured, a repeated ingest
// with the same key returns the original row id instead of inserting again.
func (i *Ingestor) IngestRow(ctx context.Context, tenantID string, keyID uuid.UUID, in domain.IngestInput) (int64, error) {
	const where = "service.Ingestor.IngestRow"
	logext.Infof(ctx, "[%s] start,tenant_id:%s,key_id:%s,source:%s,content_len:%d,idempotent:%t",
		where, tenantID, keyID, in.Source, len(in.Content), in.IdempotencyKey != "")
	if err := in.Validate(); err != nil {
		logext.Warnf(ctx, "[%s] reject: validation,tenant_id:%s,source:%s,err:%s",
			where, tenantID, in.Source, err.Error())
		return 0, err
	}
	if i.idem != nil && in.IdempotencyKey != "" {
		return i.ingestIdempotent(ctx, tenantID, keyID, in)
	}
	return i.insertAndEnrich(ctx, tenantID, keyID, in)
}

// ingestIdempotent wraps the insert with Acquire/Complete so a retried request
// with the same key never duplicates a row. Mirrors the batch pipeline pattern.
func (i *Ingestor) ingestIdempotent(ctx context.Context, tenantID string, keyID uuid.UUID, in domain.IngestInput) (int64, error) {
	const where = "service.Ingestor.ingestIdempotent"
	if !idempotencyKeyRe.MatchString(in.IdempotencyKey) {
		return 0, ErrInvalidIdempotencyKey
	}

	key, acquired, err := i.idem.Acquire(ctx, tenantID, in.IdempotencyKey, hashIngest(tenantID, in), 0)
	switch {
	case errors.Is(err, idempotency.ErrHashMismatch):
		return 0, ErrIdempotencyConflict
	case errors.Is(err, idempotency.ErrExpired):
		// Expired key behaves like a fresh request (no dedup guarantee).
		return i.insertAndEnrich(ctx, tenantID, keyID, in)
	case err != nil:
		return 0, fmt.Errorf("idempotency acquire: %w", err)
	}

	if !acquired {
		switch {
		case key.Status == idempotency.StatusPending:
			return 0, ErrRequestInProgress
		case key.Status == idempotency.StatusCompleted && key.ResponseBody != nil:
			var cached struct {
				ID int64 `json:"id"`
			}
			if jErr := json.Unmarshal(key.ResponseBody, &cached); jErr == nil && cached.ID != 0 {
				logext.Infof(ctx, "[%s] cache hit,tenant_id:%s,feedback_id:%d", where, tenantID, cached.ID)
				return cached.ID, nil
			}
		}
		// failed / nil / corrupt cached body — fall through to a fresh insert.
		return i.insertAndEnrich(ctx, tenantID, keyID, in)
	}

	id, err := i.insertAndEnrich(ctx, tenantID, keyID, in)
	if err != nil {
		if fErr := i.idem.Fail(ctx, tenantID, in.IdempotencyKey); fErr != nil {
			logext.Warnf(ctx, "[%s] mark key failed,tenant_id:%s,err:%+v", where, tenantID, fErr.Error())
		}
		return 0, err
	}
	body, _ := json.Marshal(struct {
		ID int64 `json:"id"`
	}{ID: id})
	if cErr := i.idem.Complete(ctx, tenantID, in.IdempotencyKey, 200, body); cErr != nil {
		// The row is already persisted; a Complete failure only weakens dedup
		// for a subsequent retry. Log and return success.
		logext.Warnf(ctx, "[%s] mark key completed,tenant_id:%s,feedback_id:%d,err:%+v",
			where, tenantID, id, cErr.Error())
	}
	return id, nil
}

// insertAndEnrich persists the row and fires best-effort enrichment.
func (i *Ingestor) insertAndEnrich(ctx context.Context, tenantID string, keyID uuid.UUID, in domain.IngestInput) (int64, error) {
	const where = "service.Ingestor.insertAndEnrich"
	in = scrubUntrustedSourceMeta(keyID, in)
	userID := composeUserID(keyID, in.SourceUser)
	subjectKey, subjectDisplay := subjectkey.Normalize(in.SourceUser, userID)
	subjectHash := ""
	if subjectKey != "" {
		subjectHash = subjectkey.Hash(tenantID, subjectKey)
	}
	id, err := i.repo.Insert(ctx, tenantID, userID, subjectKey, subjectDisplay, subjectHash, in)
	if err != nil {
		logext.Errorf(ctx, "[%s] repo.Insert failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return 0, fmt.Errorf("repo insert: %w", err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,feedback_id:%d", where, tenantID, id)
	if i.submitter != nil {
		if err := i.submitter.Submit(ctx, enrich.Job{ID: id, TraceID: trace.FromContext(ctx)}); err != nil {
			logext.Warnf(ctx, "[%s] enrich submit deferred,feedback_id:%d,inbound_trace_id:%s,err:%+v",
				where, id, trace.FromContext(ctx), err.Error())
		}
	}
	return id, nil
}

// hashIngest is the canonical request fingerprint for idempotency-conflict
// detection: the same key with a different payload is a 409, not a cache hit.
func hashIngest(tenantID string, in domain.IngestInput) []byte {
	canonical := struct {
		TenantID   string         `json:"t"`
		Content    string         `json:"c"`
		Source     string         `json:"s"`
		SourceUser string         `json:"u"`
		PageURL    string         `json:"p"`
		SourceMeta map[string]any `json:"m"`
	}{tenantID, in.Content, in.Source, in.SourceUser, in.PageURL, in.SourceMeta}
	data, _ := json.Marshal(canonical)
	sum := sha256.Sum256(data)
	return sum[:]
}

func scrubUntrustedSourceMeta(keyID uuid.UUID, in domain.IngestInput) domain.IngestInput {
	if keyID == uuid.Nil || in.SourceMeta == nil {
		return in
	}
	if _, ok := in.SourceMeta[domain.SourceMetaInboundSourceID]; !ok {
		if _, ok := in.SourceMeta[domain.SourceMetaInboundSourceName]; !ok {
			return in
		}
	}
	meta := make(map[string]any, len(in.SourceMeta))
	for k, v := range in.SourceMeta {
		switch k {
		case domain.SourceMetaInboundSourceID, domain.SourceMetaInboundSourceName:
			continue
		default:
			meta[k] = v
		}
	}
	in.SourceMeta = meta
	return in
}

// composeUserID prefixes every external-source row with the api key
// uuid so we can trace which key submitted what, while preserving any
// upstream user id (e.g. email From: / webhook source_user) for later
// support lookups.
func composeUserID(keyID uuid.UUID, sourceUser string) string {
	uid := "ext_" + keyID.String()
	if sourceUser != "" {
		uid = uid + ":" + sourceUser
	}
	return uid
}
