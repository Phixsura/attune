// Package clusters handles /fb/v1/console/clusters endpoints (#25).
package clusters

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/embedding"
)

// ClustersHandler serves /fb/v1/console/clusters.
type ClustersHandler struct {
	repo clusterRepo
}

type clusterRepo interface {
	ListClusters(ctx context.Context, tenantID string, opts embedding.ClusterListOpts) (*embedding.ClusterListResult, error)
	GetClusterMembers(ctx context.Context, tenantID string, clusterID uuid.UUID, opts embedding.ClusterMembersOpts) (*embedding.ClusterMembersResult, error)
	IsClusteringEnabled(ctx context.Context, tenantID string) (bool, error)
}

func NewClustersHandler(r *embedding.TaskRepo) *ClustersHandler {
	return ptrext.Of(ClustersHandler{repo: r})
}

// BindListRequest extracts query params into the proto request.
func BindListRequest(r *http.Request, req *attunev1.ListClustersRequest) error {
	q := r.URL.Query()
	if v := q.Get("recency_days"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			req.RecencyDays = ptrext.Of(int32(n))
		}
	}
	if v := q.Get("min_count"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			req.MinCount = ptrext.Of(int32(n))
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			req.Limit = ptrext.Of(int32(n))
		}
	}
	if v := q.Get("cursor"); v != "" {
		req.Cursor = ptrext.Of(v)
	}
	if v := q.Get("sort"); v != "" {
		req.Sort = ptrext.Of(v)
	}
	if v := q.Get("q"); v != "" {
		req.Q = ptrext.Of(v)
	}
	return nil
}

// List handles GET /fb/v1/console/clusters.
func (h *ClustersHandler) List(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListClustersRequest,
) (dispatcher.Result[*attunev1.ListClustersResponse], error) {
	const where = "console.ClustersHandler.List"
	auth := ctx.Auth

	enabled, err := h.repo.IsClusteringEnabled(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] check clustering enabled failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListClustersResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to check clustering status",
		)
	}

	if !enabled {
		return dispatcher.OK(ptrext.Of(attunev1.ListClustersResponse{
			Items:             nil,
			ClusteringEnabled: false,
		}))
	}

	// Defaults applied by repo layer; only set explicitly provided values
	opts := embedding.ClusterListOpts{
		RecencyDays: int(req.GetRecencyDays()),
		MinCount:    int(req.GetMinCount()),
		Limit:       int(req.GetLimit()),
		Cursor:      req.GetCursor(),
		Sort:        req.GetSort(),
		Query:       req.GetQ(),
	}

	result, err := h.repo.ListClusters(ctx, auth.TenantID, opts)
	if err != nil {
		logext.Errorf(ctx, "[%s] list clusters failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListClustersResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to list clusters",
		)
	}

	items := make([]*attunev1.ClusterSummary, 0, len(result.Items))
	for _, c := range result.Items {
		items = append(items, ptrext.Of(attunev1.ClusterSummary{
			ClusterId:   c.ClusterID.String(),
			Count:       int32(c.Count),
			LatestAt:    c.LatestAt.UTC().Format(time.RFC3339),
			Label:       c.Label,
			SampleTitle: c.SampleTitle,
		}))
	}

	resp := ptrext.Of(attunev1.ListClustersResponse{
		Items:             items,
		ClusteringEnabled: true,
		TotalCount:        int32(result.TotalCount),
	})
	if result.NextCursor != "" {
		resp.NextCursor = ptrext.Of(result.NextCursor)
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d,total:%d",
		where, auth.TenantID, len(items), result.TotalCount)
	return dispatcher.OK(resp)
}

// BindGetMembersQuery extracts query params for GetClusterMembers.
func BindGetMembersQuery(r *http.Request, req *attunev1.GetClusterMembersRequest) error {
	q := r.URL.Query()
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			req.Limit = ptrext.Of(int32(n))
		}
	}
	if v := q.Get("cursor"); v != "" {
		req.Cursor = ptrext.Of(v)
	}
	return nil
}

// GetMembers handles GET /fb/v1/console/clusters/{cluster_id}/members.
func (h *ClustersHandler) GetMembers(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetClusterMembersRequest,
) (dispatcher.Result[*attunev1.GetClusterMembersResponse], error) {
	const where = "console.ClustersHandler.GetMembers"
	auth := ctx.Auth

	clusterID, err := uuid.Parse(req.GetClusterId())
	if err != nil {
		return dispatcher.Fail[*attunev1.GetClusterMembersResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_BAD_REQUEST,
			"invalid cluster_id",
		)
	}

	// Defaults applied by repo layer; only set explicitly provided values
	opts := embedding.ClusterMembersOpts{
		Limit:  int(req.GetLimit()),
		Cursor: req.GetCursor(),
	}

	result, err := h.repo.GetClusterMembers(ctx, auth.TenantID, clusterID, opts)
	if err != nil {
		logext.Errorf(ctx, "[%s] get cluster members failed,tenant_id:%s,cluster_id:%s,err:%+v",
			where, auth.TenantID, clusterID, err.Error())
		return dispatcher.Fail[*attunev1.GetClusterMembersResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to get cluster members",
		)
	}

	items := make([]*attunev1.ClusterMember, 0, len(result.Items))
	for _, m := range result.Items {
		items = append(items, ptrext.Of(attunev1.ClusterMember{
			Id:            m.ID,
			Content:       m.Content,
			EnrichedTitle: m.EnrichedTitle,
			Source:        m.Source,
			CreatedAt:     m.CreatedAt.UTC().Format(time.RFC3339),
			Similarity:    m.Similarity,
		}))
	}

	resp := ptrext.Of(attunev1.GetClusterMembersResponse{
		Items:        items,
		ClusterLabel: result.Label,
		TotalCount:   int32(result.TotalCount),
	})
	if result.NextCursor != "" {
		resp.NextCursor = ptrext.Of(result.NextCursor)
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,cluster_id:%s,count:%d",
		where, auth.TenantID, clusterID, len(items))
	return dispatcher.OK(resp)
}
