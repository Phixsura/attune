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

	if publication.Profile.PublicSlug != "pricing-api" || publication.Profile.PublishedAt == nil {
		t.Fatalf("publication profile = %+v, want published pricing-api profile", publication.Profile)
	}
	if publication.Moderation.State != pvrepo.ModerationStatePending ||
		publication.Moderation.SubjectID != publication.Profile.ID.String() {
		t.Fatalf("moderation subject = %+v, want pending subject for profile", publication.Moderation)
	}

	loaded, err := e.publicRepo.GetRequestPublication(e.ctx, e.tenantID, request.ID)
	if err != nil {
		t.Fatalf("GetRequestPublication: %v", err)
	}
	if loaded.Profile.ID != publication.Profile.ID || loaded.Moderation.ID != publication.Moderation.ID {
		t.Fatalf("loaded publication = %+v, want %+v", loaded, publication)
	}

	e.addVote(t, request.ID, "user:ada")
	e.approve(t, publication.Moderation.ID)
	candidate, err := e.publicRepo.GetPublicRequestCandidate(e.ctx, e.tenantSlug, "pricing-api")
	if err != nil {
		t.Fatalf("GetPublicRequestCandidate: %v", err)
	}
	if candidate.Policy.PortalAccessMode != pvrepo.AccessModePublic ||
		candidate.Policy.RequestsEnabled != true ||
		candidate.Moderation.State != pvrepo.ModerationStateApproved ||
		candidate.VoteCount != 1 ||
		candidate.SubmitterDisplay != "Ada" {
		t.Fatalf("candidate = %+v, want approved public request with one vote", candidate)
	}

	otherTenantID := e.createTenant(t, "publicvisibility-other")
	if _, err := e.publicRepo.GetRequestPublication(e.ctx, otherTenantID, request.ID); !errors.Is(err, pvrepo.ErrNotFound) {
		t.Fatalf("cross-tenant GetRequestPublication error = %v, want ErrNotFound", err)
	}
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
	tx := e.begin(t)
	publication, err := e.publicRepo.UpsertRequestPublicationTx(e.ctx, tx, pvrepo.RequestProfile{
		TenantID:          e.tenantID,
		RequestID:         requestID,
		PublicSlug:        slug,
		PublicTitle:       "Pricing API",
		PublicSummary:     "Public-safe summary",
		PublicState:       "planned",
		RoadmapColumn:     "next",
		IncludedInPortal:  true,
		IncludedInRoadmap: true,
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
	tx := e.begin(t)
	reviewedAt := time.Now().UTC()
	subject, err := e.publicRepo.UpdateSubjectStateTx(e.ctx, tx, e.tenantID, subjectID,
		pvrepo.ModerationStateApproved, "safe", "approved for public portal", "reviewer", reviewedAt)
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
