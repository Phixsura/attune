// SPDX-License-Identifier: Apache-2.0

package clusters

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/embedding"
)

// --- fakes ---------------------------------------------------------------

type fakeClusterRepo struct {
	listResult *embedding.ClusterListResult
	listErr    error
	membersRes *embedding.ClusterMembersResult
	membersErr error
	enabled    bool
	enabledErr error
}

func (f *fakeClusterRepo) ListClusters(_ context.Context, _ string, _ embedding.ClusterListOpts) (*embedding.ClusterListResult, error) {
	return f.listResult, f.listErr
}

func (f *fakeClusterRepo) GetClusterMembers(_ context.Context, _ string, _ uuid.UUID, _ embedding.ClusterMembersOpts) (*embedding.ClusterMembersResult, error) {
	return f.membersRes, f.membersErr
}

func (f *fakeClusterRepo) IsClusteringEnabled(_ context.Context, _ string) (bool, error) {
	return f.enabled, f.enabledErr
}

func clusterCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	})
}

// --- BindListRequest -----------------------------------------------------

func TestBindListRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		check func(t *testing.T, req *attunev1.ListClustersRequest)
	}{
		{
			name:  "empty query",
			query: "",
			check: func(t *testing.T, req *attunev1.ListClustersRequest) {
				require.Nil(t, req.RecencyDays)
				require.Nil(t, req.MinCount)
				require.Nil(t, req.Limit)
				require.Nil(t, req.Cursor)
				require.Nil(t, req.Sort)
				require.Nil(t, req.Q)
			},
		},
		{
			name:  "all params",
			query: "?recency_days=14&min_count=5&limit=20&cursor=abc&sort=count&q=billing",
			check: func(t *testing.T, req *attunev1.ListClustersRequest) {
				require.Equal(t, int32(14), req.GetRecencyDays())
				require.Equal(t, int32(5), req.GetMinCount())
				require.Equal(t, int32(20), req.GetLimit())
				require.Equal(t, "abc", req.GetCursor())
				require.Equal(t, "count", req.GetSort())
				require.Equal(t, "billing", req.GetQ())
			},
		},
		{
			name:  "invalid recency_days ignored",
			query: "?recency_days=notanumber",
			check: func(t *testing.T, req *attunev1.ListClustersRequest) {
				require.Nil(t, req.RecencyDays)
			},
		},
		{
			name:  "invalid min_count ignored",
			query: "?min_count=xyz",
			check: func(t *testing.T, req *attunev1.ListClustersRequest) {
				require.Nil(t, req.MinCount)
			},
		},
		{
			name:  "invalid limit ignored",
			query: "?limit=bad",
			check: func(t *testing.T, req *attunev1.ListClustersRequest) {
				require.Nil(t, req.Limit)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/clusters"+tt.query, nil)
			req := ptrext.Of(attunev1.ListClustersRequest{})
			err := BindListRequest(r, req)
			require.NoError(t, err)
			tt.check(t, req)
		})
	}
}

// --- BindGetMembersQuery -------------------------------------------------

func TestBindGetMembersQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		check func(t *testing.T, req *attunev1.GetClusterMembersRequest)
	}{
		{
			name:  "empty query",
			query: "",
			check: func(t *testing.T, req *attunev1.GetClusterMembersRequest) {
				require.Nil(t, req.Limit)
				require.Nil(t, req.Cursor)
			},
		},
		{
			name:  "all params",
			query: "?limit=50&cursor=abc123",
			check: func(t *testing.T, req *attunev1.GetClusterMembersRequest) {
				require.Equal(t, int32(50), req.GetLimit())
				require.Equal(t, "abc123", req.GetCursor())
			},
		},
		{
			name:  "invalid limit ignored",
			query: "?limit=bad",
			check: func(t *testing.T, req *attunev1.GetClusterMembersRequest) {
				require.Nil(t, req.Limit)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/clusters/x/members"+tt.query, nil)
			req := ptrext.Of(attunev1.GetClusterMembersRequest{})
			err := BindGetMembersQuery(r, req)
			require.NoError(t, err)
			tt.check(t, req)
		})
	}
}

// --- List handler --------------------------------------------------------

func TestList_ClusteringDisabled(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeClusterRepo{enabled: false})
	h := ptrext.Of(ClustersHandler{repo: repo})

	res, err := h.List(clusterCtx(), ptrext.Of(attunev1.ListClustersRequest{}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.Status)
	require.False(t, res.Body.GetClusteringEnabled())
	require.Empty(t, res.Body.GetItems())
}

func TestList_EnabledCheckError(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeClusterRepo{enabledErr: errors.New("db down")})
	h := ptrext.Of(ClustersHandler{repo: repo})

	_, err := h.List(clusterCtx(), ptrext.Of(attunev1.ListClustersRequest{}))
	var de *dispatcher.Error
	require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

func TestList_HappyPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	cID := uuid.New()
	repo := ptrext.Of(fakeClusterRepo{
		enabled: true,
		listResult: ptrext.Of(embedding.ClusterListResult{
			Items: []embedding.ClusterSummary{
				{ClusterID: cID, Count: 5, LatestAt: now, Label: "billing", SampleTitle: "billing issue"},
			},
			TotalCount: 1,
		}),
	})
	h := ptrext.Of(ClustersHandler{repo: repo})

	res, err := h.List(clusterCtx(), ptrext.Of(attunev1.ListClustersRequest{}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.Status)
	require.True(t, res.Body.GetClusteringEnabled())
	require.Len(t, res.Body.GetItems(), 1)
	require.Equal(t, cID.String(), res.Body.GetItems()[0].GetClusterId())
	require.Equal(t, int32(5), res.Body.GetItems()[0].GetCount())
	require.Equal(t, "billing", res.Body.GetItems()[0].GetLabel())
	require.Equal(t, "billing issue", res.Body.GetItems()[0].GetSampleTitle())
	require.Equal(t, int32(1), res.Body.GetTotalCount())
}

func TestList_WithNextCursor(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeClusterRepo{
		enabled: true,
		listResult: ptrext.Of(embedding.ClusterListResult{
			Items:      []embedding.ClusterSummary{},
			NextCursor: "12345:abc",
			TotalCount: 100,
		}),
	})
	h := ptrext.Of(ClustersHandler{repo: repo})

	res, err := h.List(clusterCtx(), ptrext.Of(attunev1.ListClustersRequest{}))
	require.NoError(t, err)
	require.Equal(t, "12345:abc", res.Body.GetNextCursor())
}

func TestList_EmptyCursor(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeClusterRepo{
		enabled: true,
		listResult: ptrext.Of(embedding.ClusterListResult{
			Items:      []embedding.ClusterSummary{},
			NextCursor: "",
		}),
	})
	h := ptrext.Of(ClustersHandler{repo: repo})

	res, err := h.List(clusterCtx(), ptrext.Of(attunev1.ListClustersRequest{}))
	require.NoError(t, err)
	require.Nil(t, res.Body.NextCursor)
}

func TestList_ListError(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeClusterRepo{
		enabled: true,
		listErr: errors.New("query failed"),
	})
	h := ptrext.Of(ClustersHandler{repo: repo})

	_, err := h.List(clusterCtx(), ptrext.Of(attunev1.ListClustersRequest{}))
	var de *dispatcher.Error
	require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
	require.Equal(t, http.StatusInternalServerError, de.Status)
}

// --- GetMembers handler --------------------------------------------------

func TestGetMembers_InvalidClusterID(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeClusterRepo{})
	h := ptrext.Of(ClustersHandler{repo: repo})

	_, err := h.GetMembers(clusterCtx(), ptrext.Of(attunev1.GetClusterMembersRequest{
		ClusterId: "not-a-uuid",
	}))
	var de *dispatcher.Error
	require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
	require.Equal(t, http.StatusBadRequest, de.Status)
}

func TestGetMembers_HappyPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	cID := uuid.New()
	repo := ptrext.Of(fakeClusterRepo{
		membersRes: ptrext.Of(embedding.ClusterMembersResult{
			Items: []embedding.ClusterMember{
				{ID: 42, Content: "content", EnrichedTitle: "title", Source: "api", CreatedAt: now, Similarity: 0.95},
			},
			Label:      "billing",
			TotalCount: 1,
		}),
	})
	h := ptrext.Of(ClustersHandler{repo: repo})

	res, err := h.GetMembers(clusterCtx(), ptrext.Of(attunev1.GetClusterMembersRequest{
		ClusterId: cID.String(),
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.Status)
	require.Len(t, res.Body.GetItems(), 1)
	require.Equal(t, int64(42), res.Body.GetItems()[0].GetId())
	require.Equal(t, "content", res.Body.GetItems()[0].GetContent())
	require.Equal(t, "title", res.Body.GetItems()[0].GetEnrichedTitle())
	require.Equal(t, "api", res.Body.GetItems()[0].GetSource())
	require.InDelta(t, 0.95, res.Body.GetItems()[0].GetSimilarity(), 0.001)
	require.Equal(t, "billing", res.Body.GetClusterLabel())
	require.Equal(t, int32(1), res.Body.GetTotalCount())
}

func TestGetMembers_WithNextCursor(t *testing.T) {
	t.Parallel()

	cID := uuid.New()
	repo := ptrext.Of(fakeClusterRepo{
		membersRes: ptrext.Of(embedding.ClusterMembersResult{
			Items:      []embedding.ClusterMember{},
			NextCursor: "cursor-val",
			TotalCount: 50,
		}),
	})
	h := ptrext.Of(ClustersHandler{repo: repo})

	res, err := h.GetMembers(clusterCtx(), ptrext.Of(attunev1.GetClusterMembersRequest{
		ClusterId: cID.String(),
	}))
	require.NoError(t, err)
	require.Equal(t, "cursor-val", res.Body.GetNextCursor())
}

func TestGetMembers_EmptyCursor(t *testing.T) {
	t.Parallel()

	cID := uuid.New()
	repo := ptrext.Of(fakeClusterRepo{
		membersRes: ptrext.Of(embedding.ClusterMembersResult{
			Items:      []embedding.ClusterMember{},
			NextCursor: "",
		}),
	})
	h := ptrext.Of(ClustersHandler{repo: repo})

	res, err := h.GetMembers(clusterCtx(), ptrext.Of(attunev1.GetClusterMembersRequest{
		ClusterId: cID.String(),
	}))
	require.NoError(t, err)
	require.Nil(t, res.Body.NextCursor)
}

func TestGetMembers_RepoError(t *testing.T) {
	t.Parallel()

	cID := uuid.New()
	repo := ptrext.Of(fakeClusterRepo{
		membersErr: errors.New("db error"),
	})
	h := ptrext.Of(ClustersHandler{repo: repo})

	_, err := h.GetMembers(clusterCtx(), ptrext.Of(attunev1.GetClusterMembersRequest{
		ClusterId: cID.String(),
	}))
	var de *dispatcher.Error
	require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
	require.Equal(t, http.StatusInternalServerError, de.Status)
}
