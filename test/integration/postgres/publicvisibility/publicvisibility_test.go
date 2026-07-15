//go:build integration

// SPDX-License-Identifier: Apache-2.0

package publicvisibility_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	crrepo "github.com/Phixsura/attune/internal/repo/customerrequest"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	"github.com/Phixsura/attune/internal/repo/tenant"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	pvsvc "github.com/Phixsura/attune/internal/service/publicvisibility"
	"github.com/Phixsura/attune/internal/testdb"
)

type env struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	publicRepo *pvrepo.Repo
	requests   *crrepo.Repo
	tenantID   string
	tenantSlug string
}

func setup(t *testing.T) env {
	t.Helper()
	ctx := context.Background()
	pool := testdb.NewPool(t)
	tenantSlug := "publicvisibility-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	tenantID, err := tenant.NewTenant(pool).Create(ctx, tenantSlug, "Public Visibility IO")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return env{
		ctx:        ctx,
		pool:       pool,
		publicRepo: pvrepo.New(pool),
		requests:   crrepo.New(pool),
		tenantID:   tenantID,
		tenantSlug: tenantSlug,
	}
}

func TestPGPublicVisibilityRequestPublicationLifecycle(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, "Pricing API")
	e.upsertPublicPolicy(t, pvrepo.ModerationStatePending)
	publication := e.upsertRequestPublication(t, request.ID, "pricing-api")
	assertPublishedRequestPublication(t, publication)

	loaded := mustGetRequestPublication(t, e, request.ID)
	assertLoadedRequestPublication(t, loaded, publication)

	e.addVote(t, request.ID, "portal:visitor-ada")
	e.approve(t, publication.Moderation.ID)
	candidate := mustGetPublicRequestCandidate(t, e, "pricing-api", "portal:visitor-ada")
	assertApprovedPublicRequestCandidate(t, candidate)

	assertRequestPublicationCrossTenantNotFound(t, e, request.ID)
}

func TestPGPublicVisibilityRejectsDuplicateSlug(t *testing.T) {
	e := setup(t)
	first := e.createRequest(t, "First request")
	second := e.createRequest(t, "Second request")
	e.upsertPublicPolicy(t, pvrepo.ModerationStateApproved)
	e.upsertRequestPublication(t, first.ID, "shared-slug")

	tx := e.begin(t)
	_, err := e.publicRepo.UpsertRequestPublicationTx(e.ctx, tx, pvrepo.RequestProfile{
		TenantID:         e.tenantID,
		RequestID:        second.ID,
		PublicSlug:       "shared-slug",
		PublicTitle:      "Second public title",
		IncludedInPortal: true,
		UpdatedBy:        "operator",
	}, pvrepo.ModerationStateApproved, "", "")
	rollback(t, e.ctx, tx)
	if !errors.Is(err, pvrepo.ErrInvalidInput) {
		t.Fatalf("duplicate slug error = %v, want ErrInvalidInput", err)
	}
}

func TestPGPublicVisibilityRejectsBlockedDefaultPolicyStates(t *testing.T) {
	e := setup(t)
	tests := []struct {
		name    string
		mutate  func(*pvrepo.Policy)
		wantErr string
	}{
		{
			name: "rejected request default",
			mutate: func(policy *pvrepo.Policy) {
				policy.DefaultRequestState = pvrepo.ModerationStateRejected
			},
			wantErr: "chk_public_visibility_policy_default_request_state",
		},
		{
			name: "hidden comment default",
			mutate: func(policy *pvrepo.Policy) {
				policy.DefaultCommentState = pvrepo.ModerationStateHidden
			},
			wantErr: "chk_public_visibility_policy_default_comment_state",
		},
		{
			name: "spam request default",
			mutate: func(policy *pvrepo.Policy) {
				policy.DefaultRequestState = pvrepo.ModerationStateSpam
			},
			wantErr: "chk_public_visibility_policy_default_request_state",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := e.begin(t)
			policy := e.publicPolicy(pvrepo.ModerationStatePending)
			tt.mutate(&policy)
			_, err := e.publicRepo.UpsertPolicyTx(e.ctx, tx, policy)
			rollback(t, e.ctx, tx)
			if !errors.Is(err, pvrepo.ErrInvalidInput) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("UpsertPolicyTx error = %v, want ErrInvalidInput containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestPGPublicVisibilityListsOnlyApprovedIncludedLiveRequests(t *testing.T) {
	e := setup(t)
	e.upsertPublicPolicy(t, pvrepo.ModerationStatePending)

	portalRoadmapRequest := e.createRequest(t, "Portal roadmap request")
	portalRoadmap := e.upsertRequestPublicationCustom(t, portalRoadmapRequest.ID,
		"portal-roadmap", "Portal roadmap", "Now", true, true)
	e.addVote(t, portalRoadmapRequest.ID, "portal:portal-roadmap")
	e.approve(t, portalRoadmap.Moderation.ID)
	e.setRequestPublicState(t, portalRoadmapRequest.ID, "shipped")
	e.setRequestStatus(t, portalRoadmapRequest.ID, crrepo.StatusShipped)

	portalOnlyRequest := e.createRequest(t, "Portal only request")
	portalOnly := e.upsertRequestPublicationCustom(t, portalOnlyRequest.ID,
		"portal-only", "Portal only", "Inbox", true, false)
	e.approve(t, portalOnly.Moderation.ID)

	roadmapOnlyRequest := e.createRequest(t, "Roadmap only request")
	roadmapOnly := e.upsertRequestPublicationCustom(t, roadmapOnlyRequest.ID,
		"roadmap-only", "Roadmap only", "under consideration", false, true)
	e.approve(t, roadmapOnly.Moderation.ID)

	pendingRequest := e.createRequest(t, "Pending public request")
	e.upsertRequestPublicationCustom(t, pendingRequest.ID, "pending-request", "Pending request", "Now", true, true)

	hiddenRequest := e.createRequest(t, "Hidden public request")
	hidden := e.upsertRequestPublicationCustom(t, hiddenRequest.ID, "hidden-request", "Hidden request", "Now", true, true)
	e.approve(t, hidden.Moderation.ID)
	e.setModerationState(t, hidden.Moderation.ID, pvrepo.ModerationStateHidden)

	excludedRequest := e.createRequest(t, "Excluded public request")
	excluded := e.upsertRequestPublicationCustom(t, excludedRequest.ID, "excluded-request", "Excluded request", "Now", false, false)
	e.approve(t, excluded.Moderation.ID)

	archivedRequest := e.createRequest(t, "Archived public request")
	archived := e.upsertRequestPublicationCustom(t, archivedRequest.ID, "archived-request", "Archived request", "Now", true, true)
	e.approve(t, archived.Moderation.ID)
	e.archiveRequest(t, archivedRequest.ID)

	portalList, err := e.publicRepo.ListPublicRequestCandidates(e.ctx, pvrepo.PublicRequestListFilter{
		TenantSlug: e.tenantSlug,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("ListPublicRequestCandidates(portal): %v", err)
	}
	assertPublicListSlugs(t, portalList.Items, []string{"portal-roadmap", "portal-only"})
	if portalList.Policy.TenantID != e.tenantID || publicListVoteCount(portalList.Items, "portal-roadmap") != 1 {
		t.Fatalf("portal list = %+v, want tenant policy and vote count", portalList)
	}

	roadmapList, err := e.publicRepo.ListPublicRequestCandidates(e.ctx, pvrepo.PublicRequestListFilter{
		TenantSlug:    e.tenantSlug,
		Roadmap:       true,
		RoadmapColumn: "under consideration",
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListPublicRequestCandidates(roadmap): %v", err)
	}
	assertPublicListSlugs(t, roadmapList.Items, []string{"roadmap-only"})

	stateList, err := e.publicRepo.ListPublicRequestCandidates(e.ctx, pvrepo.PublicRequestListFilter{
		TenantSlug: e.tenantSlug,
		State:      "ship",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("ListPublicRequestCandidates(state): %v", err)
	}
	assertPublicListSlugs(t, stateList.Items, []string{"portal-roadmap"})

	combinedList, err := e.publicRepo.ListPublicRequestCandidates(e.ctx, pvrepo.PublicRequestListFilter{
		TenantSlug:    e.tenantSlug,
		State:         "ship",
		RoadmapColumn: "shipped",
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListPublicRequestCandidates(combined): %v", err)
	}
	assertPublicListSlugs(t, combinedList.Items, []string{"portal-roadmap"})

	service := pvsvc.New(e.publicRepo, nil)
	publicRequests, err := service.ListPublicRequests(e.ctx, e.tenantSlug, 10, "", "", "", "ship", "shipped", false, false, "")
	if err != nil {
		t.Fatalf("ListPublicRequests: %v", err)
	}
	assertPublicRequestSlugs(t, publicRequests.Requests, []string{"portal-roadmap"})
	publicRoadmap, err := service.ListPublicRoadmap(e.ctx, e.tenantSlug, 10, "", "", "", "", "under consideration", false, false, "")
	if err != nil {
		t.Fatalf("ListPublicRoadmap: %v", err)
	}
	assertPublicRequestSlugs(t, publicRoadmap.Requests, []string{"roadmap-only"})
}

func TestPGPublicVisibilitySearchAndSortPublicRequests(t *testing.T) {
	e := setup(t)
	e.upsertPublicPolicy(t, pvrepo.ModerationStatePending)

	olderRequest := e.createRequest(t, "Older request")
	older := e.upsertRequestPublicationCustom(t, olderRequest.ID, "older-request", "Older request", "Now", true, true)
	e.approve(t, older.Moderation.ID)
	e.addVote(t, olderRequest.ID, "portal:older-1")
	e.addVote(t, olderRequest.ID, "portal:older-2")

	newerRequest := e.createRequest(t, "Newer request")
	newer := e.upsertRequestPublicationCustom(t, newerRequest.ID, "newer-request", "Newer request", "Now", true, true)
	e.approve(t, newer.Moderation.ID)

	searchRequest := e.createRequest(t, "Zebra request")
	searchable := e.upsertRequestPublicationCustom(t, searchRequest.ID, "zebra-request", "Zebra request", "Next", true, true)
	e.approve(t, searchable.Moderation.ID)
	e.touchRequestProfile(t, searchRequest.ID, time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC))
	e.touchRequestProfile(t, olderRequest.ID, time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC))
	e.touchRequestProfile(t, newerRequest.ID, time.Date(2026, 7, 13, 11, 0, 0, 0, time.UTC))

	searchList, err := e.publicRepo.ListPublicRequestCandidates(e.ctx, pvrepo.PublicRequestListFilter{
		TenantSlug: e.tenantSlug,
		Query:      "zebra",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("ListPublicRequestCandidates(search): %v", err)
	}
	assertPublicListSlugs(t, searchList.Items, []string{"zebra-request"})

	topList, err := e.publicRepo.ListPublicRequestCandidates(e.ctx, pvrepo.PublicRequestListFilter{
		TenantSlug: e.tenantSlug,
		Sort:       "top",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("ListPublicRequestCandidates(top): %v", err)
	}
	assertPublicListSlugs(t, topList.Items, []string{"older-request", "newer-request", "zebra-request"})
	if got := topList.Items[0].Profile.PublicSlug; got != "older-request" {
		t.Fatalf("top sort first slug = %q, want older-request", got)
	}

	recentList, err := e.publicRepo.ListPublicRequestCandidates(e.ctx, pvrepo.PublicRequestListFilter{
		TenantSlug: e.tenantSlug,
		Sort:       "recent",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("ListPublicRequestCandidates(recent): %v", err)
	}
	if got := recentList.Items[0].Profile.PublicSlug; got != "newer-request" {
		t.Fatalf("recent sort first slug = %q, want newer-request", got)
	}
}

func TestPGPublicVisibilityFiltersViewerVotesAndComments(t *testing.T) {
	e := setup(t)
	e.upsertPublicPolicy(t, pvrepo.ModerationStatePending)

	votedOnlyRequest := e.createRequest(t, "Voted only request")
	votedOnly := e.upsertRequestPublicationCustom(t, votedOnlyRequest.ID, "voted-only", "Voted only", "Now", true, true)
	e.approve(t, votedOnly.Moderation.ID)

	commentOnlyRequest := e.createRequest(t, "Comment only request")
	commentOnly := e.upsertRequestPublicationCustom(t, commentOnlyRequest.ID, "comment-only", "Comment only", "Next", true, true)
	e.approve(t, commentOnly.Moderation.ID)

	bothRequest := e.createRequest(t, "Both filters request")
	both := e.upsertRequestPublicationCustom(t, bothRequest.ID, "both-filters", "Both filters", "Later", true, true)
	e.approve(t, both.Moderation.ID)

	e.addVote(t, votedOnlyRequest.ID, "portal:visitor-1")
	e.addVote(t, bothRequest.ID, "portal:visitor-1")
	e.addVote(t, bothRequest.ID, "portal:visitor-2")

	auditRepo := auditlogrepo.New(e.pool)
	service := pvsvc.New(e.publicRepo, auditlogsvc.New(auditRepo))
	actor := auditlogsvc.Actor{Type: "portal", ID: "visitor-1", UserAgent: "integration-test"}

	createAndApproveComment := func(publicSlug, visitorID, body string) {
		t.Helper()
		detail, err := service.CreatePublicRequestComment(e.ctx, e.tenantSlug, publicSlug, visitorID, body, actor)
		if err != nil {
			t.Fatalf("CreatePublicRequestComment(%s): %v", publicSlug, err)
		}
		pending := mustListCommentModeration(t, service, e)
		assertPendingCommentModeration(t, pending, detail)
		approved := mustApproveCommentModeration(t, service, e, pending.Items[0].ID, actor)
		assertApprovedCommentModeration(t, approved)
	}

	createAndApproveComment("comment-only", "visitor-3", "Comment only body")
	createAndApproveComment("both-filters", "visitor-4", "Both filters body")

	votedList, err := e.publicRepo.ListPublicRequestCandidates(e.ctx, pvrepo.PublicRequestListFilter{
		TenantSlug:        e.tenantSlug,
		OnlyVotedByViewer: true,
		ViewerSubjectKey:  "portal:visitor-1",
		Sort:              "top",
		Limit:             10,
	})
	if err != nil {
		t.Fatalf("ListPublicRequestCandidates(voted): %v", err)
	}
	assertPublicListSlugs(t, votedList.Items, []string{"both-filters", "voted-only"})

	commentList, err := e.publicRepo.ListPublicRequestCandidates(e.ctx, pvrepo.PublicRequestListFilter{
		TenantSlug:       e.tenantSlug,
		OnlyWithComments: true,
		Sort:             "top",
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("ListPublicRequestCandidates(comments): %v", err)
	}
	assertPublicListSlugs(t, commentList.Items, []string{"both-filters", "comment-only"})

	combinedList, err := e.publicRepo.ListPublicRequestCandidates(e.ctx, pvrepo.PublicRequestListFilter{
		TenantSlug:        e.tenantSlug,
		OnlyVotedByViewer: true,
		OnlyWithComments:  true,
		ViewerSubjectKey:  "portal:visitor-1",
		Sort:              "top",
		Limit:             10,
	})
	if err != nil {
		t.Fatalf("ListPublicRequestCandidates(combined): %v", err)
	}
	assertPublicListSlugs(t, combinedList.Items, []string{"both-filters"})
}

func TestPGPublicVisibilitySimilarRequests(t *testing.T) {
	e := setup(t)
	e.upsertPublicPolicy(t, pvrepo.ModerationStatePending)

	primaryRequest := e.createRequest(t, "Pricing API request")
	primary := e.upsertRequestPublicationCustom(t, primaryRequest.ID, "pricing-api", "Pricing API", "Now", true, true)
	e.approve(t, primary.Moderation.ID)

	similarRequest := e.createRequest(t, "Pricing dashboard request")
	similar := e.upsertRequestPublicationCustom(t, similarRequest.ID, "pricing-dashboard", "Pricing Dashboard", "Next", true, true)
	e.approve(t, similar.Moderation.ID)

	list, err := e.publicRepo.ListPublicRequestCandidates(e.ctx, pvrepo.PublicRequestListFilter{
		TenantSlug:        e.tenantSlug,
		SimilarityText:    "Pricing API",
		ExcludePublicSlug: "pricing-api",
		Sort:              "top",
		Limit:             4,
	})
	if err != nil {
		t.Fatalf("ListPublicRequestCandidates(similar): %v", err)
	}
	assertPublicListSlugs(t, list.Items, []string{"pricing-dashboard"})

	service := pvsvc.New(e.publicRepo, nil)
	detail, err := service.GetPublicRequest(e.ctx, e.tenantSlug, "pricing-api", "visitor-1")
	if err != nil {
		t.Fatalf("GetPublicRequest(similar): %v", err)
	}
	if len(detail.SimilarRequests) != 1 || detail.SimilarRequests[0].Summary.PublicSlug != "pricing-dashboard" {
		t.Fatalf("GetPublicRequest(similar) = %+v, want dashboard suggestion", detail.SimilarRequests)
	}
}

func TestPGPublicVisibilityPortalVoteLifecycle(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, "Vote lifecycle request")
	e.upsertPublicPolicy(t, pvrepo.ModerationStatePending)
	publication := e.upsertRequestPublication(t, request.ID, "vote-lifecycle")
	e.approve(t, publication.Moderation.ID)

	service := pvsvc.New(e.publicRepo, nil)
	actor := auditlogsvc.Actor{Type: "portal", ID: "visitor-1", UserAgent: "integration-test"}

	voted, err := service.VotePublicRequest(e.ctx, e.tenantSlug, "vote-lifecycle", "visitor-1", actor)
	if err != nil {
		t.Fatalf("VotePublicRequest: %v", err)
	}
	if voted.Votes != 1 || !voted.ViewerHasVoted {
		t.Fatalf("voted request = %+v, want one portal vote", voted)
	}

	candidate, err := e.publicRepo.GetPublicRequestCandidate(e.ctx, e.tenantSlug, "vote-lifecycle", "portal:visitor-1")
	if err != nil {
		t.Fatalf("GetPublicRequestCandidate(after vote): %v", err)
	}
	if candidate.VoteCount != 1 || !candidate.ViewerHasVoted {
		t.Fatalf("candidate after vote = %+v, want voted state", candidate)
	}

	unvoted, err := service.UnvotePublicRequest(e.ctx, e.tenantSlug, "vote-lifecycle", "visitor-1", actor)
	if err != nil {
		t.Fatalf("UnvotePublicRequest: %v", err)
	}
	if unvoted.Votes != 0 || unvoted.ViewerHasVoted {
		t.Fatalf("unvoted request = %+v, want no portal vote", unvoted)
	}
}

func TestPGPublicVisibilityPortalVoteIsIdempotent(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, "Idempotent vote request")
	e.upsertPublicPolicy(t, pvrepo.ModerationStatePending)
	publication := e.upsertRequestPublication(t, request.ID, "vote-idempotent")
	e.approve(t, publication.Moderation.ID)

	service := pvsvc.New(e.publicRepo, nil)
	actor := auditlogsvc.Actor{Type: "portal", ID: "visitor-1", UserAgent: "integration-test"}

	first, err := service.VotePublicRequest(e.ctx, e.tenantSlug, "vote-idempotent", "visitor-1", actor)
	if err != nil {
		t.Fatalf("VotePublicRequest(first): %v", err)
	}
	if first.Votes != 1 || !first.ViewerHasVoted {
		t.Fatalf("first vote result = %+v, want one portal vote", first)
	}

	second, err := service.VotePublicRequest(e.ctx, e.tenantSlug, "vote-idempotent", "visitor-1", actor)
	if err != nil {
		t.Fatalf("VotePublicRequest(second): %v", err)
	}
	if second.Votes != 1 || !second.ViewerHasVoted {
		t.Fatalf("second vote result = %+v, want idempotent single portal vote", second)
	}

	var voteRows int
	if err := e.pool.QueryRow(e.ctx, `
		SELECT COUNT(*)
		FROM customer_request_votes
		WHERE tenant_id = $1
		  AND request_id = $2
		  AND subject_key = $3`,
		e.tenantID, request.ID, "portal:visitor-1",
	).Scan(&voteRows); err != nil {
		t.Fatalf("count vote rows: %v", err)
	}
	if voteRows != 1 {
		t.Fatalf("vote rows = %d, want 1", voteRows)
	}

	candidate := mustGetPublicRequestCandidate(t, e, "vote-idempotent", "portal:visitor-1")
	if candidate.VoteCount != 1 || !candidate.ViewerHasVoted {
		t.Fatalf("candidate after duplicate vote = %+v, want single counted vote", candidate)
	}
}

func TestPGPublicVisibilityPortalCommentLifecycle(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, "Comment lifecycle request")
	e.upsertPublicPolicy(t, pvrepo.ModerationStatePending)
	publication := e.upsertRequestPublication(t, request.ID, "comment-thread")
	e.approve(t, publication.Moderation.ID)

	auditRepo := auditlogrepo.New(e.pool)
	service := pvsvc.New(e.publicRepo, auditlogsvc.New(auditRepo))
	actor := auditlogsvc.Actor{Type: "portal", ID: "visitor-1", UserAgent: "integration-test"}

	detail := mustCreatePublicRequestComment(t, service, e, actor)
	assertPendingPublicRequestComment(t, detail)

	pending := mustListCommentModeration(t, service, e)
	assertPendingCommentModeration(t, pending, detail)

	approved := mustApproveCommentModeration(t, service, e, pending.Items[0].ID, actor)
	assertApprovedCommentModeration(t, approved)

	final := mustGetPublicRequest(t, service, e)
	assertFinalPublicRequestComment(t, final)

	assertPortalCommentCrossTenantNotFound(t, e)
}

func assertPublishedRequestPublication(t *testing.T, publication pvrepo.RequestPublication) {
	t.Helper()

	if publication.Profile.PublicSlug != "pricing-api" || publication.Profile.PublishedAt == nil {
		t.Fatalf("publication profile = %+v, want published pricing-api profile", publication.Profile)
	}
	if publication.Moderation.State != pvrepo.ModerationStatePending ||
		publication.Moderation.SubjectID != publication.Profile.ID.String() {
		t.Fatalf("moderation subject = %+v, want pending subject for profile", publication.Moderation)
	}
}

func assertLoadedRequestPublication(t *testing.T, loaded pvrepo.RequestPublication, publication pvrepo.RequestPublication) {
	t.Helper()

	if loaded.Profile.ID != publication.Profile.ID || loaded.Moderation.ID != publication.Moderation.ID {
		t.Fatalf("loaded publication = %+v, want %+v", loaded, publication)
	}
}

func mustGetRequestPublication(t *testing.T, e env, requestID uuid.UUID) pvrepo.RequestPublication {
	t.Helper()

	loaded, err := e.publicRepo.GetRequestPublication(e.ctx, e.tenantID, requestID)
	if err != nil {
		t.Fatalf("GetRequestPublication: %v", err)
	}
	return ptrext.Indirect(loaded)
}

func mustGetPublicRequestCandidate(t *testing.T, e env, slug, viewerSubjectKey string) pvrepo.PublicRequestCandidate {
	t.Helper()

	candidate, err := e.publicRepo.GetPublicRequestCandidate(e.ctx, e.tenantSlug, slug, viewerSubjectKey)
	if err != nil {
		t.Fatalf("GetPublicRequestCandidate: %v", err)
	}
	return ptrext.Indirect(candidate)
}

func assertApprovedPublicRequestCandidate(t *testing.T, candidate pvrepo.PublicRequestCandidate) {
	t.Helper()

	if candidate.Policy.PortalAccessMode != pvrepo.AccessModePublic ||
		candidate.Policy.RequestsEnabled != true ||
		candidate.Moderation.State != pvrepo.ModerationStateApproved ||
		candidate.VoteCount != 1 ||
		!candidate.ViewerHasVoted ||
		candidate.SubmitterDisplay != "Ada" {
		t.Fatalf("candidate = %+v, want approved public request with one vote", candidate)
	}
}

func assertRequestPublicationCrossTenantNotFound(t *testing.T, e env, requestID uuid.UUID) {
	t.Helper()

	otherTenantID := e.createTenant(t, "publicvisibility-other")
	if _, err := e.publicRepo.GetRequestPublication(e.ctx, otherTenantID, requestID); !errors.Is(err, pvrepo.ErrNotFound) {
		t.Fatalf("cross-tenant GetRequestPublication error = %v, want ErrNotFound", err)
	}
}

func mustCreatePublicRequestComment(t *testing.T, service *pvsvc.Service, e env, actor auditlogsvc.Actor) pvsvc.PublicRequest {
	t.Helper()

	detail, err := service.CreatePublicRequestComment(e.ctx, e.tenantSlug, "comment-thread", "visitor-1", " Great idea ", actor)
	if err != nil {
		t.Fatalf("CreatePublicRequestComment: %v", err)
	}
	return detail
}

func assertPendingPublicRequestComment(t *testing.T, detail pvsvc.PublicRequest) {
	t.Helper()

	if detail.Comments != 0 || !detail.CanComment {
		t.Fatalf("comment detail = %+v, want pending comment not counted yet", detail)
	}
	if len(detail.CommentItems) != 1 || detail.CommentItems[0].Body != "Great idea" ||
		detail.CommentItems[0].State != pvrepo.ModerationStatePending ||
		detail.CommentItems[0].SubmittedByDisplay != "Portal visitor" {
		t.Fatalf("comment detail = %+v, want trimmed pending comment", detail)
	}
}

func mustListCommentModeration(t *testing.T, service *pvsvc.Service, e env) pvrepo.ListResult {
	t.Helper()

	pending, err := service.ListModeration(e.ctx, pvsvc.ListModerationInput{
		TenantID: e.tenantID,
		Surfaces: []pvrepo.Surface{pvrepo.SurfaceRequestComment},
		States:   []pvrepo.ModerationState{pvrepo.ModerationStatePending},
	})
	if err != nil {
		t.Fatalf("ListModeration(comment pending): %v", err)
	}
	return pending
}

func assertPendingCommentModeration(t *testing.T, pending pvrepo.ListResult, detail pvsvc.PublicRequest) {
	t.Helper()

	if len(pending.Items) != 1 || pending.Items[0].SubjectID != detail.CommentItems[0].ID.String() {
		t.Fatalf("pending moderation = %+v, want created request_comment subject", pending)
	}
}

func mustApproveCommentModeration(t *testing.T, service *pvsvc.Service, e env, moderationID uuid.UUID, actor auditlogsvc.Actor) pvrepo.ModerationSubject {
	t.Helper()

	approved, err := service.Moderate(e.ctx, pvsvc.ModerateInput{
		TenantID:   e.tenantID,
		ID:         moderationID,
		Action:     pvsvc.ActionApprove,
		ReasonCode: "safe",
		ReasonNote: "reviewed for public portal",
		Actor:      actor,
	})
	if err != nil {
		t.Fatalf("Moderate approve comment: %v", err)
	}
	return approved
}

func assertApprovedCommentModeration(t *testing.T, approved pvrepo.ModerationSubject) {
	t.Helper()

	if approved.Surface != pvrepo.SurfaceRequestComment || approved.State != pvrepo.ModerationStateApproved {
		t.Fatalf("approved comment = %+v, want approved request_comment", approved)
	}
}

func mustGetPublicRequest(t *testing.T, service *pvsvc.Service, e env) pvsvc.PublicRequest {
	t.Helper()

	final, err := service.GetPublicRequest(e.ctx, e.tenantSlug, "comment-thread", "visitor-1")
	if err != nil {
		t.Fatalf("GetPublicRequest(after comment approve): %v", err)
	}
	return final
}

func assertFinalPublicRequestComment(t *testing.T, final pvsvc.PublicRequest) {
	t.Helper()

	if final.Comments != 1 || len(final.CommentItems) != 1 || final.CommentItems[0].State != pvrepo.ModerationStateApproved {
		t.Fatalf("final public request = %+v, want approved comment counted", final)
	}
}

func assertPortalCommentCrossTenantNotFound(t *testing.T, e env) {
	t.Helper()

	otherTenantSlug := "publicvisibility-other"
	e.createTenant(t, otherTenantSlug)
	if _, err := e.publicRepo.GetPublicRequestCandidate(e.ctx, otherTenantSlug, "comment-thread", "portal:visitor-1"); !errors.Is(err, pvrepo.ErrNotFound) {
		t.Fatalf("cross-tenant GetPublicRequestCandidate error = %v, want ErrNotFound", err)
	}
}

func TestPGPublicVisibilityModeratesCommentAndSubmissionSubjects(t *testing.T) {
	e := setup(t)
	e.upsertPublicPolicy(t, pvrepo.ModerationStatePending)
	auditRepo := auditlogrepo.New(e.pool)
	service := pvsvc.New(e.publicRepo, auditlogsvc.New(auditRepo))
	actor := auditlogsvc.Actor{Type: "admin", ID: "operator", Email: "operator@example.com"}

	comment := e.createModerationSubject(t,
		pvrepo.SurfaceRequestComment, "request-comment-1", pvrepo.ModerationStatePending)
	submission := e.createModerationSubject(t,
		pvrepo.SurfacePortalSubmission, "portal-submission-1", pvrepo.ModerationStatePending)

	pending, err := service.ListModeration(e.ctx, pvsvc.ListModerationInput{
		TenantID: e.tenantID,
		Surfaces: []pvrepo.Surface{
			pvrepo.SurfaceRequestComment,
			pvrepo.SurfacePortalSubmission,
		},
		States: []pvrepo.ModerationState{pvrepo.ModerationStatePending},
	})
	if err != nil {
		t.Fatalf("ListModeration(pending comment/submission): %v", err)
	}
	assertModerationSubjectSet(t, pending.Items, []string{
		"request_comment:request-comment-1",
		"portal_submission:portal-submission-1",
	})

	approvedComment, err := service.Moderate(e.ctx, pvsvc.ModerateInput{
		TenantID: e.tenantID,
		ID:       comment.ID,
		Action:   pvsvc.ActionApprove,
		Actor:    actor,
	})
	if err != nil {
		t.Fatalf("Moderate approve comment: %v", err)
	}
	if approvedComment.Surface != pvrepo.SurfaceRequestComment ||
		approvedComment.State != pvrepo.ModerationStateApproved {
		t.Fatalf("approved comment = %+v, want approved request_comment", approvedComment)
	}

	hiddenComment, err := service.Moderate(e.ctx, pvsvc.ModerateInput{
		TenantID:   e.tenantID,
		ID:         comment.ID,
		Action:     pvsvc.ActionHide,
		ReasonCode: "privacy",
		ReasonNote: "contains private customer detail",
		Actor:      actor,
	})
	if err != nil {
		t.Fatalf("Moderate hide comment: %v", err)
	}
	if hiddenComment.Surface != pvrepo.SurfaceRequestComment ||
		hiddenComment.State != pvrepo.ModerationStateHidden ||
		hiddenComment.ReasonCode != "privacy" {
		t.Fatalf("hidden comment = %+v, want hidden request_comment with reason", hiddenComment)
	}

	spamSubmission, err := service.Moderate(e.ctx, pvsvc.ModerateInput{
		TenantID:   e.tenantID,
		ID:         submission.ID,
		Action:     pvsvc.ActionMarkSpam,
		ReasonCode: "abuse.spam",
		ReasonNote: "automated spam submission",
		Actor:      actor,
	})
	if err != nil {
		t.Fatalf("Moderate spam submission: %v", err)
	}
	if spamSubmission.Surface != pvrepo.SurfacePortalSubmission ||
		spamSubmission.State != pvrepo.ModerationStateSpam {
		t.Fatalf("spam submission = %+v, want spam portal_submission", spamSubmission)
	}

	blocked, err := service.ListModeration(e.ctx, pvsvc.ListModerationInput{
		TenantID: e.tenantID,
		Surfaces: []pvrepo.Surface{
			pvrepo.SurfaceRequestComment,
			pvrepo.SurfacePortalSubmission,
		},
		States: []pvrepo.ModerationState{
			pvrepo.ModerationStateHidden,
			pvrepo.ModerationStateSpam,
		},
	})
	if err != nil {
		t.Fatalf("ListModeration(blocked comment/submission): %v", err)
	}
	assertModerationSubjectSet(t, blocked.Items, []string{
		"request_comment:request-comment-1",
		"portal_submission:portal-submission-1",
	})
	e.assertAuditActions(t, auditRepo, []string{
		"moderation.approve",
		"moderation.hide",
		"moderation.mark_spam",
	})
}

func TestPGPublicVisibilityRejectsInvalidGenericSubjectID(t *testing.T) {
	e := setup(t)
	e.upsertPublicPolicy(t, pvrepo.ModerationStatePending)

	for _, tc := range []struct {
		name      string
		subjectID string
	}{
		{name: "empty", subjectID: ""},
		{name: "too long", subjectID: strings.Repeat("s", 257)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := e.begin(t)
			_, err := e.publicRepo.CreateModerationSubjectTx(e.ctx, tx, pvrepo.ModerationSubject{
				TenantID:           e.tenantID,
				Surface:            pvrepo.SurfacePortalSubmission,
				SubjectID:          tc.subjectID,
				State:              pvrepo.ModerationStatePending,
				SubmittedByDisplay: "Ada",
			})
			rollback(t, e.ctx, tx)
			if !errors.Is(err, pvrepo.ErrInvalidInput) {
				t.Fatalf("CreateModerationSubjectTx(%q) error = %v, want ErrInvalidInput", tc.name, err)
			}
		})
	}
}

func TestPGPublicVisibilityServiceWritesAuditRows(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, "Audited request")
	auditRepo := auditlogrepo.New(e.pool)
	service := pvsvc.New(e.publicRepo, auditlogsvc.New(auditRepo))
	actor := auditlogsvc.Actor{Type: "admin", ID: "operator", Email: "operator@example.com"}

	if _, err := service.UpdatePolicy(e.ctx, pvsvc.UpdatePolicyInput{
		TenantID:              e.tenantID,
		PortalAccessMode:      pvrepo.AccessModePublic,
		SearchIndexingEnabled: true,
		RequestsEnabled:       true,
		CommentsEnabled:       true,
		RoadmapEnabled:        true,
		ChangelogEnabled:      true,
		SubmissionWriteMode:   pvrepo.WriteModeIdentified,
		CommentWriteMode:      pvrepo.WriteModeDisabled,
		VoteWriteMode:         pvrepo.WriteModeAnonymous,
		DefaultRequestState:   pvrepo.ModerationStatePending,
		DefaultCommentState:   pvrepo.ModerationStatePending,
		SubmitterIdentityMode: pvrepo.IdentityModeDisplayName,
		ShowVoteCount:         true,
		ShowCommentCount:      true,
		ShowSubmitterDisplay:  true,
		Actor:                 actor,
	}); err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}
	publication, err := service.UpsertRequestProfile(e.ctx, pvsvc.UpsertRequestProfileInput{
		TenantID:               e.tenantID,
		RequestID:              request.ID,
		PublicSlug:             "audited-request",
		PublicTitle:            "Audited request",
		PublicSummary:          "Public-safe summary",
		PublicState:            "planned",
		RoadmapColumn:          "next",
		IncludedInPortal:       true,
		IncludedInRoadmap:      true,
		SubmittedByDisplay:     "Ada",
		SubmittedByFingerprint: "tenant-local-digest",
		Actor:                  actor,
	})
	if err != nil {
		t.Fatalf("UpsertRequestProfile: %v", err)
	}
	if _, err := service.Moderate(e.ctx, pvsvc.ModerateInput{
		TenantID:   e.tenantID,
		ID:         publication.Moderation.ID,
		Action:     pvsvc.ActionApprove,
		ReasonCode: "safe",
		ReasonNote: "public-safe",
		Actor:      actor,
	}); err != nil {
		t.Fatalf("Moderate approve: %v", err)
	}

	e.assertAuditActions(t, auditRepo, []string{
		"public_policy.update",
		"public_request_profile.upsert",
		"moderation.approve",
	})
}

func (e env) upsertPublicPolicy(t *testing.T, defaultState pvrepo.ModerationState) {
	t.Helper()
	tx := e.begin(t)
	_, err := e.publicRepo.UpsertPolicyTx(e.ctx, tx, e.publicPolicy(defaultState))
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("UpsertPolicyTx: %v", err)
	}
	commit(t, e.ctx, tx)
}

func (e env) publicPolicy(defaultState pvrepo.ModerationState) pvrepo.Policy {
	return pvrepo.Policy{
		TenantID:              e.tenantID,
		PortalAccessMode:      pvrepo.AccessModePublic,
		SearchIndexingEnabled: true,
		RequestsEnabled:       true,
		CommentsEnabled:       true,
		RoadmapEnabled:        true,
		ChangelogEnabled:      true,
		SubmissionWriteMode:   pvrepo.WriteModeIdentified,
		CommentWriteMode:      pvrepo.WriteModeIdentified,
		VoteWriteMode:         pvrepo.WriteModeAnonymous,
		DefaultRequestState:   defaultState,
		DefaultCommentState:   pvrepo.ModerationStatePending,
		SubmitterIdentityMode: pvrepo.IdentityModeDisplayName,
		ShowVoteCount:         true,
		ShowCommentCount:      true,
		ShowSubmitterDisplay:  true,
		UpdatedBy:             "operator",
	}
}

func (e env) upsertRequestPublication(t *testing.T, requestID uuid.UUID, slug string) pvrepo.RequestPublication {
	t.Helper()
	return e.upsertRequestPublicationCustom(t, requestID, slug, "Pricing API", "next", true, true)
}

func (e env) upsertRequestPublicationCustom(
	t *testing.T,
	requestID uuid.UUID,
	slug string,
	title string,
	roadmapColumn string,
	includedInPortal bool,
	includedInRoadmap bool,
) pvrepo.RequestPublication {
	t.Helper()
	tx := e.begin(t)
	publication, err := e.publicRepo.UpsertRequestPublicationTx(e.ctx, tx, pvrepo.RequestProfile{
		TenantID:          e.tenantID,
		RequestID:         requestID,
		PublicSlug:        slug,
		PublicTitle:       title,
		PublicSummary:     "Public-safe summary",
		PublicState:       "planned",
		RoadmapColumn:     roadmapColumn,
		IncludedInPortal:  includedInPortal,
		IncludedInRoadmap: includedInRoadmap,
		UpdatedBy:         "operator",
	}, pvrepo.ModerationStatePending, "Ada", "tenant-local-digest")
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("UpsertRequestPublicationTx: %v", err)
	}
	commit(t, e.ctx, tx)
	return ptrext.Indirect(publication)
}

func (e env) approve(t *testing.T, subjectID uuid.UUID) {
	t.Helper()
	e.setModerationState(t, subjectID, pvrepo.ModerationStateApproved)
}

func (e env) setModerationState(t *testing.T, subjectID uuid.UUID, state pvrepo.ModerationState) {
	t.Helper()
	tx := e.begin(t)
	reviewedAt := time.Now().UTC()
	subject, err := e.publicRepo.UpdateSubjectStateTx(e.ctx, tx, e.tenantID, subjectID,
		state, "safe", "reviewed for public portal", "reviewer", reviewedAt)
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("UpdateSubjectStateTx: %v", err)
	}
	if subject.ReviewedAt == nil || subject.ReviewedBy != "reviewer" {
		rollback(t, e.ctx, tx)
		t.Fatalf("approved subject = %+v, want reviewer fields", subject)
	}
	commit(t, e.ctx, tx)
}

func (e env) createModerationSubject(
	t *testing.T,
	surface pvrepo.Surface,
	subjectID string,
	state pvrepo.ModerationState,
) pvrepo.ModerationSubject {
	t.Helper()
	tx := e.begin(t)
	subject, err := e.publicRepo.CreateModerationSubjectTx(e.ctx, tx, pvrepo.ModerationSubject{
		TenantID:               e.tenantID,
		Surface:                surface,
		SubjectID:              subjectID,
		State:                  state,
		SubmittedByDisplay:     "Ada",
		SubmittedByFingerprint: "tenant-local-digest",
	})
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("CreateModerationSubjectTx: %v", err)
	}
	commit(t, e.ctx, tx)
	return ptrext.Indirect(subject)
}

func (e env) archiveRequest(t *testing.T, requestID uuid.UUID) {
	t.Helper()
	if _, err := e.pool.Exec(e.ctx, `
		UPDATE customer_requests
		SET archived_at = NOW()
		WHERE tenant_id = $1 AND id = $2`, e.tenantID, requestID); err != nil {
		t.Fatalf("archive request: %v", err)
	}
}

func (e env) touchRequestProfile(t *testing.T, requestID uuid.UUID, updatedAt time.Time) {
	t.Helper()
	if _, err := e.pool.Exec(e.ctx, `
		UPDATE public_request_profiles
		SET updated_at = $1
		WHERE tenant_id = $2 AND request_id = $3`,
		updatedAt, e.tenantID, requestID); err != nil {
		t.Fatalf("touch request profile: %v", err)
	}
}

func (e env) setRequestPublicState(t *testing.T, requestID uuid.UUID, state string) {
	t.Helper()
	if _, err := e.pool.Exec(e.ctx, `
		UPDATE public_request_profiles
		SET public_state = $1
		WHERE tenant_id = $2 AND request_id = $3`,
		state, e.tenantID, requestID); err != nil {
		t.Fatalf("set request public state: %v", err)
	}
}

func (e env) setRequestStatus(t *testing.T, requestID uuid.UUID, status crrepo.Status) {
	t.Helper()
	tx := e.begin(t)
	_, _, err := e.requests.UpdateTx(e.ctx, tx, crrepo.UpdateInput{
		TenantID: e.tenantID,
		ID:       requestID,
		Status:   ptrext.Of(status),
		ActorID:  "operator",
	})
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("set request status: %v", err)
	}
	commit(t, e.ctx, tx)
}

func (e env) createRequest(t *testing.T, title string) crrepo.Summary {
	t.Helper()
	tx := e.begin(t)
	created, err := e.requests.CreateTx(e.ctx, tx, crrepo.CreateInput{
		TenantID:    e.tenantID,
		Title:       title,
		Description: "internal description",
		Status:      crrepo.StatusOpen,
		Priority:    crrepo.PriorityNone,
		ActorID:     "operator",
	})
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("CreateTx: %v", err)
	}
	commit(t, e.ctx, tx)
	return ptrext.Indirect(created)
}

func (e env) addVote(t *testing.T, requestID uuid.UUID, subjectKey string) {
	t.Helper()
	_, err := e.pool.Exec(e.ctx, `
		INSERT INTO customer_request_votes (
			tenant_id, request_id, subject_key, subject_display, weight, created_by
		) VALUES ($1, $2, $3, $3, 1, 'operator')`,
		e.tenantID, requestID, subjectKey)
	if err != nil {
		t.Fatalf("insert vote: %v", err)
	}
}

func (e env) assertAuditActions(t *testing.T, auditRepo *auditlogrepo.Repo, actions []string) {
	t.Helper()
	for _, action := range actions {
		result, err := auditRepo.List(e.ctx, auditlogrepo.ListFilter{
			TenantID:  e.tenantID,
			Actions:   []string{action},
			Unbounded: true,
		})
		if err != nil {
			t.Fatalf("list audit action %q: %v", action, err)
		}
		if len(result.Items) != 1 {
			t.Fatalf("audit action %q rows = %d, want 1", action, len(result.Items))
		}
		if result.Items[0].ActorType != "admin" || result.Items[0].ActorID != "operator" {
			t.Fatalf("audit action %q actor = %+v, want admin operator", action, result.Items[0])
		}
	}
}

func (e env) createTenant(t *testing.T, slug string) string {
	t.Helper()
	tenantID, err := tenant.NewTenant(e.pool).Create(e.ctx, slug+"-"+strings.ReplaceAll(uuid.NewString(), "-", ""), "Other")
	if err != nil {
		t.Fatalf("create tenant %q: %v", slug, err)
	}
	return tenantID
}

func (e env) begin(t *testing.T) pgx.Tx {
	t.Helper()
	tx, err := e.pool.Begin(e.ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	return tx
}

func commit(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
}

func rollback(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	_ = tx.Rollback(ctx)
}

func assertPublicListSlugs(t *testing.T, items []pvrepo.PublicRequestListCandidate, want []string) {
	t.Helper()
	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.Profile.PublicSlug)
	}
	assertSlugSet(t, got, want)
}

func publicListVoteCount(items []pvrepo.PublicRequestListCandidate, slug string) int {
	for _, item := range items {
		if item.Profile.PublicSlug == slug {
			return item.VoteCount
		}
	}
	return -1
}

func assertPublicRequestSlugs(t *testing.T, items []pvsvc.PublicRequest, want []string) {
	t.Helper()
	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.Summary.PublicSlug)
	}
	assertSlugSet(t, got, want)
}

func assertModerationSubjectSet(t *testing.T, items []pvrepo.ModerationSubject, want []string) {
	t.Helper()
	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, string(item.Surface)+":"+item.SubjectID)
	}
	assertSlugSet(t, got, want)
}

func assertSlugSet(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slugs = %#v, want %#v", got, want)
	}
	seen := make(map[string]int, len(got))
	for _, slug := range got {
		seen[slug]++
	}
	for _, slug := range want {
		if seen[slug] != 1 {
			t.Fatalf("slugs = %#v, want %#v", got, want)
		}
	}
}
