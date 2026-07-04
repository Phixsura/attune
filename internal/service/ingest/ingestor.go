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
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// Idempotency sentinel errors, mapped by the handler to 409 / 400.
var (
	// ErrIdempotencyConflict: same key, different request body.
	ErrIdempotencyConflict = errors.New("idempotency key used with different request")
	// ErrInvalidIdempotencyKey: the supplied key is malformed.
	ErrInvalidIdempotencyKey = errors.New("invalid idempotency key (8-64 chars, [A-Za-z0-9_-])")
)

// idempotencyKeyRe bounds the key (length 8-64, chars [A-Za-z0-9_-]) so a
// malformed value is a fast 400 rather than a wide/garbage index entry.
var idempotencyKeyRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,64}$`)

// Ingestor is the business-layer entry point for "a new feedback row
// just arrived". It validates, persists via repo, and triggers async
// enrichment. Handlers depend on this concrete type (not an interface)
// because there is one ingest pipeline today.
type Ingestor struct {
	repo      feedbackInserter
	submitter enrich.Submitter
	sources   domain.SourceSet // injected union; validates in.Source
}

type feedbackInserter interface {
	Insert(
		ctx context.Context,
		tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
		in domain.IngestInput,
	) (int64, error)
	// InsertIdempotent dedups under the partial unique index; returns
	// (id, deduped, err). deduped=true → an existing row was returned.
	InsertIdempotent(
		ctx context.Context,
		tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
		in domain.IngestInput,
		idemHash []byte,
	) (int64, bool, error)
}

// NewIngestor wires the ingest pipeline. sources is the injected SourceSet used
// to validate in.Source; a nil set falls back to domain.DefaultSourceSet.
func NewIngestor(r feedbackInserter, submitter enrich.Submitter, sources domain.SourceSet) *Ingestor {
	if sources == nil {
		sources = domain.DefaultSourceSet()
	}
	return ptrext.Of(Ingestor{repo: r, submitter: submitter, sources: sources})
}

// IngestRow validates input, persists it, and fires off best-effort
// enrichment. Returns the new row id. tenantID is the TEXT tenants.id;
// keyID is the UUID of the api key used to authenticate (uuid.Nil for
// non-API-key sources like the #66 inbound webhook / email adapters).
//
// When in.IdempotencyKey is set, persistence dedups via the partial unique
// index: a replay (same key + body) returns the original id without inserting
// or re-enriching; the same key with a different body is ErrIdempotencyConflict.
func (i *Ingestor) IngestRow(ctx context.Context, tenantID string, keyID uuid.UUID, in domain.IngestInput) (int64, error) {
	const where = "service.Ingestor.IngestRow"
	logext.Infof(ctx, "[%s] start,tenant_id:%s,key_id:%s,source:%s,content_len:%d,idempotent:%t",
		where, tenantID, keyID, in.Source, len(in.Content), in.IdempotencyKey != "")
	if err := in.Validate(i.sources); err != nil {
		logext.Warnf(ctx, "[%s] reject: validation,tenant_id:%s,source:%s,err:%s",
			where, tenantID, in.Source, err.Error())
		return 0, err
	}
	if in.IdempotencyKey != "" && !idempotencyKeyRe.MatchString(in.IdempotencyKey) {
		// Don't log the raw key (attacker-controlled); length is enough to debug.
		logext.Warnf(ctx, "[%s] reject: malformed idempotency key,tenant_id:%s,key_len:%d",
			where, tenantID, len(in.IdempotencyKey))
		return 0, ErrInvalidIdempotencyKey
	}

	in = scrubUntrustedSourceMeta(keyID, in)
	userID := composeUserID(keyID, in.SourceUser)
	subjectKey, subjectDisplay := subjectkey.Normalize(in.SourceUser, userID)
	subjectHash := ""
	if subjectKey != "" {
		subjectHash = subjectkey.Hash(tenantID, subjectKey)
	}

	id, deduped, err := i.persist(ctx, tenantID, keyID, userID, subjectKey, subjectDisplay, subjectHash, in)
	if err != nil {
		if errors.Is(err, feedbackrepo.ErrIdempotencyConflict) {
			metrics.IdempotencyKeyUsage.WithLabelValues(tenantID, "conflict").Inc()
			return 0, ErrIdempotencyConflict
		}
		logext.Errorf(ctx, "[%s] repo.Insert failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
		return 0, fmt.Errorf("repo insert: %w", err)
	}

	// Feed the same idempotency-usage metric the batch path does, so the
	// Operations "Idempotency" dashboard panel covers single-row ingest too.
	if in.IdempotencyKey != "" {
		outcome := "new"
		if deduped {
			outcome = "cache_hit"
		}
		metrics.IdempotencyKeyUsage.WithLabelValues(tenantID, outcome).Inc()
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,feedback_id:%d,deduped:%t", where, tenantID, id, deduped)
	// A replay that already enqueued enrichment on its first insert should not
	// submit twice.
	if !deduped && i.submitter != nil {
		if err := i.submitter.Submit(ctx, enrich.Job{ID: id, TraceID: trace.FromContext(ctx)}); err != nil {
			logext.Warnf(ctx, "[%s] enrich submit failed,feedback_id:%d,inbound_trace_id:%s,err:%+v",
				where, id, trace.FromContext(ctx), err.Error())
		}
	}
	return id, nil
}

// persist routes to the idempotent insert when a key is present, else the plain
// insert. Returns (id, deduped, err).
func (i *Ingestor) persist(
	ctx context.Context, tenantID string, keyID uuid.UUID, userID, subjectKey, subjectDisplay, subjectHash string, in domain.IngestInput,
) (int64, bool, error) {
	if in.IdempotencyKey != "" {
		return i.repo.InsertIdempotent(ctx, tenantID, userID, subjectKey, subjectDisplay, subjectHash, in, hashIngest(tenantID, keyID, in))
	}
	id, err := i.repo.Insert(ctx, tenantID, userID, subjectKey, subjectDisplay, subjectHash, in)
	return id, false, err
}

// hashIngest is the canonical request fingerprint stored alongside the row, so
// the same idempotency key reused with a different payload — or by a different
// API key within the same tenant (keyID) — is a conflict rather than a silent
// dedup to another caller's row.
func hashIngest(tenantID string, keyID uuid.UUID, in domain.IngestInput) []byte {
	canonical := struct {
		TenantID   string         `json:"t"`
		KeyID      string         `json:"k"`
		Content    string         `json:"c"`
		Source     string         `json:"s"`
		SourceUser string         `json:"u"`
		PageURL    string         `json:"p"`
		SourceMeta map[string]any `json:"m"`
	}{tenantID, keyID.String(), in.Content, in.Source, in.SourceUser, in.PageURL, in.SourceMeta}
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
