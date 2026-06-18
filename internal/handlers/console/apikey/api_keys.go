package apikey

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/service/apikey"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type apiKeysService interface {
	List(ctx context.Context, tenantID string) ([]apikeyrepo.APIKeyListRow, error)
	Issue(ctx context.Context, tenantID, label string) (raw string, keyID uuid.UUID, err error)
	IssueWithScopes(ctx context.Context, tenantID, label string, scopes []domain.Scope) (raw string, keyID uuid.UUID, err error)
	GetScopes(ctx context.Context, keyID uuid.UUID) ([]domain.Scope, error)
	Revoke(ctx context.Context, tenantID string, id uuid.UUID) error
}

// APIKeysHandler serves /fb/v1/console/api-keys. All three operations
// scope to the session's tenant — see auth.RequireSession middleware
// which writes TenantID to context before this handler runs.
type APIKeysHandler struct {
	svc   apiKeysService
	audit auditRecorder
}

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

func NewAPIKeysHandler(svc *apikey.APIKeys) *APIKeysHandler {
	return ptrext.Of(APIKeysHandler{svc: svc})
}

func (h *APIKeysHandler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}

func toProtoAPIKey(row apikeyrepo.APIKeyListRow, scopes []domain.Scope) *attunev1.ApiKey {
	scopeStrs := make([]string, len(scopes))
	for i, s := range scopes {
		scopeStrs[i] = string(s)
	}
	k := ptrext.Of(attunev1.ApiKey{
		Id:        row.ID.String(),
		KeyPrefix: row.KeyPrefix,
		Label:     row.Label,
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		Scopes:    scopeStrs,
	})
	if row.LastUsedAt != nil {
		k.LastUsedAt = ptrext.Of(row.LastUsedAt.UTC().Format(time.RFC3339))
	}
	if row.RevokedAt != nil {
		k.RevokedAt = ptrext.Of(row.RevokedAt.UTC().Format(time.RFC3339))
	}
	return k
}

func auditActor(auth *session.AuthCtx, req *http.Request) auditlogsvc.Actor {
	actorType := auth.UserType
	if actorType == "" {
		actorType = "admin"
	}
	return auditlogsvc.ActorFromRequest(actorType, auth.UserID, req)
}
