package apikey

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/service/apikey"
)

type apiKeysService interface {
	List(ctx context.Context, tenantID string) ([]apikeyrepo.APIKeyListRow, error)
	Issue(ctx context.Context, tenantID, label string) (raw string, keyID uuid.UUID, err error)
	Revoke(ctx context.Context, tenantID string, id uuid.UUID) error
}

// APIKeysHandler serves /fb/v1/console/api-keys. All three operations
// scope to the session's tenant — see auth.RequireSession middleware
// which writes TenantID to context before this handler runs.
type APIKeysHandler struct {
	svc apiKeysService
}

func NewAPIKeysHandler(svc *apikey.APIKeys) *APIKeysHandler {
	return ptrext.Of(APIKeysHandler{svc: svc})
}

func toProtoAPIKey(row apikeyrepo.APIKeyListRow) *attunev1.ApiKey {
	k := ptrext.Of(attunev1.ApiKey{
		Id:        row.ID.String(),
		KeyPrefix: row.KeyPrefix,
		Label:     row.Label,
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
	})
	if row.LastUsedAt != nil {
		k.LastUsedAt = ptrext.Of(row.LastUsedAt.UTC().Format(time.RFC3339))
	}
	if row.RevokedAt != nil {
		k.RevokedAt = ptrext.Of(row.RevokedAt.UTC().Format(time.RFC3339))
	}
	return k
}
