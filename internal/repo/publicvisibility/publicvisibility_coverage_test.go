// SPDX-License-Identifier: Apache-2.0

package publicvisibility

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachablePublicVisibilityRepo(t)
	requestID := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")

	expectPublicVisibilityErr(t, "Begin", func() error {
		tx, err := r.Begin(ctx)
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
		return err
	})
	expectPublicVisibilityErr(t, "GetPolicy", func() error {
		_, err := r.GetPolicy(ctx, "tenant-1")
		return err
	})
	expectPublicVisibilityErr(t, "ResolveTenantIDBySlug", func() error {
		_, err := r.ResolveTenantIDBySlug(ctx, "acme")
		return err
	})
	expectPublicVisibilityErr(t, "ListSubjects", func() error {
		_, err := r.ListSubjects(ctx, ListFilter{
			TenantID: "tenant-1",
			Surfaces: []Surface{SurfaceRequest},
			States:   []ModerationState{ModerationStatePending},
			Limit:    250,
		})
		return err
	})
	expectPublicVisibilityErr(t, "GetSubject", func() error {
		_, err := r.GetSubject(ctx, "tenant-1", requestID)
		return err
	})
	expectPublicVisibilityErr(t, "GetRequestPublication", func() error {
		_, err := r.GetRequestPublication(ctx, "tenant-1", requestID)
		return err
	})
	expectPublicVisibilityErr(t, "GetPublicRequestCandidate", func() error {
		_, err := r.GetPublicRequestCandidate(ctx, "acme", "request-1", "portal:user-1")
		return err
	})
	expectPublicVisibilityErr(t, "ListPublicRequestCandidates", func() error {
		_, err := r.ListPublicRequestCandidates(ctx, PublicRequestListFilter{
			TenantSlug:        "acme",
			Query:             "billing",
			ExcludePublicSlug: "old-request",
			Sort:              "recent",
			State:             "open",
			RoadmapColumn:     "planned",
			OnlyVotedByViewer: true,
			OnlyWithComments:  true,
			ViewerSubjectKey:  "portal:user-1",
			Limit:             250,
			SimilarityText:    "billing export request",
		})
		return err
	})
	expectPublicVisibilityErr(t, "ListPublicRequestComments", func() error {
		_, err := r.ListPublicRequestComments(ctx, "acme", "request-1", "portal:user-1")
		return err
	})
	if _, err := r.ListSubjects(ctx, ListFilter{TenantID: "tenant-1", Cursor: "nan"}); err == nil {
		t.Fatalf("ListSubjects(invalid cursor) error = nil")
	}
	if _, err := r.ListPublicRequestCandidates(ctx, PublicRequestListFilter{TenantSlug: "acme", Cursor: "nan"}); err == nil {
		t.Fatalf("ListPublicRequestCandidates(invalid cursor) error = nil")
	}
}

func newUnreachablePublicVisibilityRepo(t *testing.T) *Repo {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return New(pool)
}

func expectPublicVisibilityErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
