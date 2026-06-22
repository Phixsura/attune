package enrichconfig

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/service/enrich"
)

func BindListPromptVersionsRequest(r *http.Request, req *attunev1.ListEnrichPromptVersionsRequest) error {
	q := r.URL.Query()
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		limit, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "limit must be an integer")
		}
		req.Limit = ptrext.Of(int32(limit))
	}
	if cursor := strings.TrimSpace(q.Get("cursor")); cursor != "" {
		if _, err := uuid.Parse(cursor); err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "cursor must be a version id")
		}
		req.Cursor = ptrext.Of(cursor)
	}
	if query := strings.TrimSpace(q.Get("q")); query != "" {
		req.Q = ptrext.Of(query)
	}
	return nil
}

// ListPromptVersions handles GET /fb/v1/console/enrich-config/versions.
func (h *Handler) ListPromptVersions(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListEnrichPromptVersionsRequest) (dispatcher.Result[*attunev1.ListEnrichPromptVersionsResponse], error) {
	limit := 12
	if req.Limit != nil {
		limit = int(req.GetLimit())
	}
	if limit < 1 || limit > 50 {
		return dispatcher.Fail[*attunev1.ListEnrichPromptVersionsResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "limit must be between 1 and 50")
	}
	result, err := h.svc.ListPromptVersions(ctx, ctx.Auth.TenantID, enrich.PromptVersionListFilter{
		Limit:    limit,
		CursorID: strings.TrimSpace(req.GetCursor()),
		Query:    strings.TrimSpace(req.GetQ()),
	})
	if err != nil {
		return dispatcher.Fail[*attunev1.ListEnrichPromptVersionsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list prompt versions")
	}
	resp := ptrext.Of(attunev1.ListEnrichPromptVersionsResponse{
		Versions: promptVersionsToProto(result.Items),
	})
	if result.NextCursor != "" {
		resp.NextCursor = ptrext.Of(result.NextCursor)
	}
	return dispatcher.OK(resp)
}
