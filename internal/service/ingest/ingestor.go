package ingest

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// Ingestor is the business-layer entry point for "a new feedback row
// just arrived". It validates, persists via repo, and triggers async
// enrichment. Handlers depend on this concrete type (not an interface)
// because there is one ingest pipeline today.
type Ingestor struct {
	repo      feedbackInserter
	submitter enrich.Submitter
}

type feedbackInserter interface {
	Insert(
		ctx context.Context,
		tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
		in domain.IngestInput,
	) (int64, error)
}

func NewIngestor(r feedbackInserter, submitter enrich.Submitter) *Ingestor {
	return ptrext.Of(Ingestor{repo: r, submitter: submitter})
}

// IngestRow validates input, persists it, and fires off best-effort
// enrichment. Returns the new row id. tenantID is the TEXT tenants.id;
// keyID is the UUID of the api key used to authenticate (uuid.Nil for
// non-API-key sources like the #66 inbound webhook / email adapters).
func (i *Ingestor) IngestRow(ctx context.Context, tenantID string, keyID uuid.UUID, in domain.IngestInput) (int64, error) {
	const where = "service.Ingestor.IngestRow"
	logext.Infof(ctx, "[%s] start,tenant_id:%s,key_id:%s,source:%s,content_len:%d",
		where, tenantID, keyID, in.Source, len(in.Content))
	if err := in.Validate(); err != nil {
		logext.Warnf(ctx, "[%s] reject: validation,tenant_id:%s,source:%s,err:%s",
			where, tenantID, in.Source, err.Error())
		return 0, err
	}
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
