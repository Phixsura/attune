// SPDX-License-Identifier: Apache-2.0

package publicvisibility

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
	repo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/publicvisibility"
)

type fakeService struct {
	getPolicy             func(context.Context, string) (repo.Policy, error)
	updatePolicy          func(context.Context, svc.UpdatePolicyInput) (repo.Policy, error)
	listModeration        func(context.Context, svc.ListModerationInput) (repo.ListResult, error)
	getRequestPublication func(context.Context, string, uuid.UUID) (repo.RequestPublication, error)
	upsertRequestProfile  func(context.Context, svc.UpsertRequestProfileInput) (repo.RequestPublication, error)
	moderate              func(context.Context, svc.ModerateInput) (repo.ModerationSubject, error)
}

func (f *fakeService) GetPolicy(ctx context.Context, tenantID string) (repo.Policy, error) {
	if f.getPolicy == nil {
		return repo.Policy{}, nil
	}
	return f.getPolicy(ctx, tenantID)
}

func (f *fakeService) UpdatePolicy(ctx context.Context, in svc.UpdatePolicyInput) (repo.Policy, error) {
	if f.updatePolicy == nil {
		return repo.Policy{}, nil
	}
	return f.updatePolicy(ctx, in)
}

func (f *fakeService) ListModeration(ctx context.Context, in svc.ListModerationInput) (repo.ListResult, error) {
	if f.listModeration == nil {
		return repo.ListResult{}, nil
	}
	return f.listModeration(ctx, in)
}

func (f *fakeService) GetRequestPublication(ctx context.Context, tenantID string, requestID uuid.UUID) (repo.RequestPublication, error) {
	if f.getRequestPublication == nil {
		return repo.RequestPublication{}, nil
	}
	return f.getRequestPublication(ctx, tenantID, requestID)
}

func (f *fakeService) UpsertRequestProfile(ctx context.Context, in svc.UpsertRequestProfileInput) (repo.RequestPublication, error) {
	if f.upsertRequestProfile == nil {
		return repo.RequestPublication{}, nil
	}
	return f.upsertRequestProfile(ctx, in)
}

func (f *fakeService) Moderate(ctx context.Context, in svc.ModerateInput) (repo.ModerationSubject, error) {
	if f.moderate == nil {
		return repo.ModerationSubject{}, nil
	}
	return f.moderate(ctx, in)
}

func TestGetPolicyMapsServicePolicy(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 9, 30, 0, 123, time.UTC)
	handler := NewHandler(ptrext.Of(fakeService{
		getPolicy: func(_ context.Context, tenantID string) (repo.Policy, error) {
			require.Equal(t, "tenant-1", tenantID)
			return repo.Policy{
				TenantID:              tenantID,
				PortalAccessMode:      repo.AccessModeInviteOnly,
				SearchIndexingEnabled: true,
				RequestsEnabled:       true,
				CommentsEnabled:       true,
				RoadmapEnabled:        true,
				ChangelogEnabled:      true,
				SubmissionWriteMode:   repo.WriteModeAnonymous,
				CommentWriteMode:      repo.WriteModeIdentified,
				VoteWriteMode:         repo.WriteModeDisabled,
				DefaultRequestState:   repo.ModerationStateApproved,
				DefaultCommentState:   repo.ModerationStatePending,
				SubmitterIdentityMode: repo.IdentityModeOrganization,
				ShowVoteCount:         true,
				ShowCommentCount:      false,
				ShowSubmitterDisplay:  true,
				HidePublicTimestamps:  true,
				UpdatedBy:             "admin-1",
				CreatedAt:             now,
				UpdatedAt:             now.Add(time.Minute),
			}, nil
		},
	}))

	result, err := handler.GetPolicy(testCtx(), ptrext.Of(attunev1.GetPublicVisibilityPolicyRequest{}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, "tenant-1", result.Body.GetTenantId())
	require.Equal(t, attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_INVITE_ONLY, result.Body.GetPortalAccessMode())
	require.Equal(t, attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_ANONYMOUS, result.Body.GetSubmissionWriteMode())
	require.Equal(t, attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_IDENTIFIED, result.Body.GetCommentWriteMode())
	require.Equal(t, attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_DISABLED, result.Body.GetVoteWriteMode())
	require.Equal(t, attunev1.ModerationState_MODERATION_STATE_APPROVED, result.Body.GetDefaultRequestState())
	require.Equal(t, attunev1.PublicIdentityMode_PUBLIC_IDENTITY_MODE_ORGANIZATION, result.Body.GetSubmitterIdentityMode())
	require.True(t, result.Body.GetSearchIndexingEnabled())
	require.True(t, result.Body.GetHidePublicTimestamps())
	require.Equal(t, "2026-07-10T09:30:00.000000123Z", result.Body.GetCreatedAt())
}

func TestUpdatePolicyMapsRequestToService(t *testing.T) {
	t.Parallel()

	var got svc.UpdatePolicyInput
	handler := NewHandler(ptrext.Of(fakeService{
		updatePolicy: func(_ context.Context, in svc.UpdatePolicyInput) (repo.Policy, error) {
			got = in
			return repo.Policy{TenantID: in.TenantID, PortalAccessMode: in.PortalAccessMode}, nil
		},
	}))

	req := ptrext.Of(attunev1.UpdatePublicVisibilityPolicyRequest{
		PortalAccessMode:      attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_PUBLIC,
		SearchIndexingEnabled: true,
		RequestsEnabled:       true,
		CommentsEnabled:       true,
		RoadmapEnabled:        true,
		ChangelogEnabled:      false,
		SubmissionWriteMode:   attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_IDENTIFIED,
		CommentWriteMode:      attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_DISABLED,
		VoteWriteMode:         attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_ANONYMOUS,
		DefaultRequestState:   attunev1.ModerationState_MODERATION_STATE_PENDING,
		DefaultCommentState:   attunev1.ModerationState_MODERATION_STATE_APPROVED,
		SubmitterIdentityMode: attunev1.PublicIdentityMode_PUBLIC_IDENTITY_MODE_DISPLAY_NAME,
		ShowVoteCount:         true,
		ShowCommentCount:      true,
		ShowSubmitterDisplay:  true,
		HidePublicTimestamps:  true,
	})
	result, err := handler.UpdatePolicy(testCtx(), req)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, "tenant-1", got.TenantID)
	require.Equal(t, repo.AccessModePublic, got.PortalAccessMode)
	require.Equal(t, repo.WriteModeIdentified, got.SubmissionWriteMode)
	require.Equal(t, repo.WriteModeDisabled, got.CommentWriteMode)
	require.Equal(t, repo.WriteModeAnonymous, got.VoteWriteMode)
	require.Equal(t, repo.ModerationStatePending, got.DefaultRequestState)
	require.Equal(t, repo.ModerationStateApproved, got.DefaultCommentState)
	require.Equal(t, repo.IdentityModeDisplayName, got.SubmitterIdentityMode)
	requireActor(t, got.Actor)
}

func TestListModerationMapsFiltersAndCursor(t *testing.T) {
	t.Parallel()

	subjectID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	var got svc.ListModerationInput
	handler := NewHandler(ptrext.Of(fakeService{
		listModeration: func(_ context.Context, in svc.ListModerationInput) (repo.ListResult, error) {
			got = in
			return repo.ListResult{
				Items: []repo.ModerationSubject{{
					ID:                 subjectID,
					TenantID:           in.TenantID,
					Surface:            repo.SurfaceRequestComment,
					SubjectID:          "comment-1",
					State:              repo.ModerationStateHidden,
					ReasonCode:         "policy",
					ReasonNote:         "contains sensitive material",
					SubmittedByDisplay: "Ada",
					ReviewedBy:         "admin-1",
					ReviewedAt:         ptrext.Of(time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)),
					CreatedAt:          time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC),
					UpdatedAt:          time.Date(2026, 7, 10, 10, 1, 0, 0, time.UTC),
				}},
				NextCursor: "cursor-2",
			}, nil
		},
	}))

	result, err := handler.ListModeration(testCtx(), ptrext.Of(attunev1.ListModerationSubjectsRequest{
		Surface: []attunev1.PublicSurface{
			attunev1.PublicSurface_PUBLIC_SURFACE_UNSPECIFIED,
			attunev1.PublicSurface_PUBLIC_SURFACE_REQUEST_COMMENT,
		},
		State: []attunev1.ModerationState{
			attunev1.ModerationState_MODERATION_STATE_UNSPECIFIED,
			attunev1.ModerationState_MODERATION_STATE_HIDDEN,
		},
		Limit:  25,
		Cursor: "cursor-1",
	}))

	require.NoError(t, err)
	require.Equal(t, svc.ListModerationInput{
		TenantID: "tenant-1",
		Surfaces: []repo.Surface{
			repo.SurfaceRequestComment,
		},
		States: []repo.ModerationState{
			repo.ModerationStateHidden,
		},
		Limit:  25,
		Cursor: "cursor-1",
	}, got)
	require.Equal(t, "cursor-2", result.Body.GetNextCursor())
	require.Len(t, result.Body.GetSubjects(), 1)
	require.Equal(t, subjectID.String(), result.Body.GetSubjects()[0].GetId())
	require.Equal(t, attunev1.PublicSurface_PUBLIC_SURFACE_REQUEST_COMMENT, result.Body.GetSubjects()[0].GetSurface())
	require.Equal(t, "2026-07-10T10:00:00Z", result.Body.GetSubjects()[0].GetReviewedAt())
}

func TestRequestProfileHandlersMapIDsAndPayloads(t *testing.T) {
	t.Parallel()

	requestID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	var upserted svc.UpsertRequestProfileInput
	publication := samplePublication(requestID)
	handler := NewHandler(ptrext.Of(fakeService{
		getRequestPublication: func(_ context.Context, tenantID string, got uuid.UUID) (repo.RequestPublication, error) {
			require.Equal(t, "tenant-1", tenantID)
			require.Equal(t, requestID, got)
			return publication, nil
		},
		upsertRequestProfile: func(_ context.Context, in svc.UpsertRequestProfileInput) (repo.RequestPublication, error) {
			upserted = in
			return publication, nil
		},
	}))

	getResult, err := handler.GetRequestProfile(testCtx(), ptrext.Of(attunev1.GetPublicRequestProfileRequest{
		RequestId: requestID.String(),
	}))
	require.NoError(t, err)
	require.Equal(t, "public-slug", getResult.Body.GetProfile().GetPublicSlug())
	require.Equal(t, attunev1.ModerationState_MODERATION_STATE_APPROVED, getResult.Body.GetModeration().GetState())

	upsertResult, err := handler.UpsertRequestProfile(testCtx(), ptrext.Of(attunev1.UpsertPublicRequestProfileRequest{
		RequestId:          requestID.String(),
		PublicSlug:         "new-slug",
		PublicTitle:        "Public title",
		PublicSummary:      "A concise public summary",
		PublicState:        "planned",
		RoadmapColumn:      "next",
		IncludedInPortal:   true,
		IncludedInRoadmap:  true,
		SubmittedByDisplay: "Ada",
	}))
	require.NoError(t, err)
	require.Equal(t, "tenant-1", upserted.TenantID)
	require.Equal(t, requestID, upserted.RequestID)
	require.Equal(t, "new-slug", upserted.PublicSlug)
	require.Equal(t, "Ada", upserted.SubmittedByDisplay)
	require.True(t, upserted.IncludedInPortal)
	requireActor(t, upserted.Actor)
	require.Equal(t, "public-title", upsertResult.Body.GetProfile().GetPublicTitle())
}

func TestModerationActionsMapServiceInput(t *testing.T) {
	t.Parallel()

	subjectID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	cases := []struct {
		name string
		run  func(*Handler) (dispatcher.Result[*attunev1.ModerationSubject], error)
		want svc.ModerationAction
	}{
		{name: "approve", want: svc.ActionApprove, run: func(h *Handler) (dispatcher.Result[*attunev1.ModerationSubject], error) {
			return h.Approve(testCtx(), ptrext.Of(attunev1.ApproveModerationSubjectRequest{
				Id: subjectID.String(), ReasonCode: "ok", ReasonNote: "safe to show",
			}))
		}},
		{name: "reject", want: svc.ActionReject, run: func(h *Handler) (dispatcher.Result[*attunev1.ModerationSubject], error) {
			return h.Reject(testCtx(), ptrext.Of(attunev1.RejectModerationSubjectRequest{
				Id: subjectID.String(), ReasonCode: "policy", ReasonNote: "not public",
			}))
		}},
		{name: "hide", want: svc.ActionHide, run: func(h *Handler) (dispatcher.Result[*attunev1.ModerationSubject], error) {
			return h.Hide(testCtx(), ptrext.Of(attunev1.HideModerationSubjectRequest{
				Id: subjectID.String(), ReasonCode: "review", ReasonNote: "needs review",
			}))
		}},
		{name: "mark spam", want: svc.ActionMarkSpam, run: func(h *Handler) (dispatcher.Result[*attunev1.ModerationSubject], error) {
			return h.MarkSpam(testCtx(), ptrext.Of(attunev1.MarkModerationSubjectSpamRequest{
				Id: subjectID.String(), ReasonCode: "spam", ReasonNote: "automated spam",
			}))
		}},
		{name: "restore", want: svc.ActionRestore, run: func(h *Handler) (dispatcher.Result[*attunev1.ModerationSubject], error) {
			return h.Restore(testCtx(), ptrext.Of(attunev1.RestoreModerationSubjectRequest{
				Id: subjectID.String(), ReasonCode: "appeal", ReasonNote: "appeal accepted",
			}))
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got svc.ModerateInput
			handler := NewHandler(ptrext.Of(fakeService{
				moderate: func(_ context.Context, in svc.ModerateInput) (repo.ModerationSubject, error) {
					got = in
					return sampleSubject(in.ID, in.Action), nil
				},
			}))

			result, err := tc.run(handler)

			require.NoError(t, err)
			require.Equal(t, tc.want, got.Action)
			require.Equal(t, "tenant-1", got.TenantID)
			require.Equal(t, subjectID, got.ID)
			require.NotEmpty(t, got.ReasonCode)
			require.NotEmpty(t, got.ReasonNote)
			requireActor(t, got.Actor)
			require.Equal(t, subjectID.String(), result.Body.GetId())
		})
	}
}

func TestHandlerErrors(t *testing.T) {
	t.Parallel()

	t.Run("service not configured", func(t *testing.T) {
		t.Parallel()
		_, err := NewHandler(nil).GetPolicy(testCtx(), ptrext.Of(attunev1.GetPublicVisibilityPolicyRequest{}))
		requireDispatcherError(t, err, http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL)
	})

	t.Run("bad request id", func(t *testing.T) {
		t.Parallel()
		handler := NewHandler(ptrext.Of(fakeService{}))
		_, err := handler.GetRequestProfile(testCtx(), ptrext.Of(attunev1.GetPublicRequestProfileRequest{RequestId: "bad"}))
		requireDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)
	})

	t.Run("bad moderation id", func(t *testing.T) {
		t.Parallel()
		handler := NewHandler(ptrext.Of(fakeService{}))
		_, err := handler.Approve(testCtx(), ptrext.Of(attunev1.ApproveModerationSubjectRequest{Id: "bad"}))
		requireDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)
	})

	t.Run("validation", func(t *testing.T) {
		t.Parallel()
		handler := NewHandler(ptrext.Of(fakeService{
			getPolicy: func(context.Context, string) (repo.Policy, error) {
				return repo.Policy{}, svc.ErrValidation
			},
		}))
		_, err := handler.GetPolicy(testCtx(), ptrext.Of(attunev1.GetPublicVisibilityPolicyRequest{}))
		requireDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		handler := NewHandler(ptrext.Of(fakeService{
			getPolicy: func(context.Context, string) (repo.Policy, error) {
				return repo.Policy{}, repo.ErrNotFound
			},
		}))
		_, err := handler.GetPolicy(testCtx(), ptrext.Of(attunev1.GetPublicVisibilityPolicyRequest{}))
		requireDispatcherError(t, err, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND)
	})

	t.Run("internal", func(t *testing.T) {
		t.Parallel()
		handler := NewHandler(ptrext.Of(fakeService{
			getPolicy: func(context.Context, string) (repo.Policy, error) {
				return repo.Policy{}, errors.New("database down")
			},
		}))
		_, err := handler.GetPolicy(testCtx(), ptrext.Of(attunev1.GetPublicVisibilityPolicyRequest{}))
		requireDispatcherError(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})
}

func TestHandlerNotConfiguredErrors(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil)
	ctx := testCtx()
	id := uuid.NewString()

	_, err := handler.UpdatePolicy(ctx, ptrext.Of(attunev1.UpdatePublicVisibilityPolicyRequest{}))
	requireDispatcherError(t, err, http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL)
	_, err = handler.ListModeration(ctx, ptrext.Of(attunev1.ListModerationSubjectsRequest{}))
	requireDispatcherError(t, err, http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL)
	_, err = handler.GetRequestProfile(ctx, ptrext.Of(attunev1.GetPublicRequestProfileRequest{RequestId: id}))
	requireDispatcherError(t, err, http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL)
	_, err = handler.UpsertRequestProfile(ctx, ptrext.Of(attunev1.UpsertPublicRequestProfileRequest{RequestId: id}))
	requireDispatcherError(t, err, http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL)
	_, err = handler.Approve(ctx, ptrext.Of(attunev1.ApproveModerationSubjectRequest{Id: id}))
	requireDispatcherError(t, err, http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL)
}

func TestHandlerMapsMutationServiceErrors(t *testing.T) {
	t.Parallel()

	id := uuid.NewString()
	handler := NewHandler(ptrext.Of(fakeService{
		updatePolicy: func(context.Context, svc.UpdatePolicyInput) (repo.Policy, error) {
			return repo.Policy{}, svc.ErrValidation
		},
		listModeration: func(context.Context, svc.ListModerationInput) (repo.ListResult, error) {
			return repo.ListResult{}, repo.ErrInvalidInput
		},
		getRequestPublication: func(context.Context, string, uuid.UUID) (repo.RequestPublication, error) {
			return repo.RequestPublication{}, svc.ErrNotFound
		},
		upsertRequestProfile: func(context.Context, svc.UpsertRequestProfileInput) (repo.RequestPublication, error) {
			return repo.RequestPublication{}, repo.ErrNotFound
		},
		moderate: func(context.Context, svc.ModerateInput) (repo.ModerationSubject, error) {
			return repo.ModerationSubject{}, svc.ErrInvalidTransition
		},
	}))

	_, err := handler.UpdatePolicy(testCtx(), ptrext.Of(attunev1.UpdatePublicVisibilityPolicyRequest{}))
	requireDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
	_, err = handler.ListModeration(testCtx(), ptrext.Of(attunev1.ListModerationSubjectsRequest{}))
	requireDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
	_, err = handler.GetRequestProfile(testCtx(), ptrext.Of(attunev1.GetPublicRequestProfileRequest{RequestId: id}))
	requireDispatcherError(t, err, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND)
	_, err = handler.UpsertRequestProfile(testCtx(), ptrext.Of(attunev1.UpsertPublicRequestProfileRequest{RequestId: id}))
	requireDispatcherError(t, err, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND)
	_, err = handler.Approve(testCtx(), ptrext.Of(attunev1.ApproveModerationSubjectRequest{Id: id}))
	requireDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
}

func TestBindListRequest(t *testing.T) {
	t.Parallel()

	req := ptrext.Of(attunev1.ListModerationSubjectsRequest{})
	err := BindListRequest(httptest.NewRequest(http.MethodGet,
		"/?surface=PUBLIC_SURFACE_REQUEST,PUBLIC_SURFACE_CHANGELOG_POST&surface=bad&state=MODERATION_STATE_PENDING&state=MODERATION_STATE_SPAM&limit=42&cursor=next",
		http.NoBody), req)

	require.NoError(t, err)
	require.Equal(t, []attunev1.PublicSurface{
		attunev1.PublicSurface_PUBLIC_SURFACE_REQUEST,
		attunev1.PublicSurface_PUBLIC_SURFACE_CHANGELOG_POST,
	}, req.GetSurface())
	require.Equal(t, []attunev1.ModerationState{
		attunev1.ModerationState_MODERATION_STATE_PENDING,
		attunev1.ModerationState_MODERATION_STATE_SPAM,
	}, req.GetState())
	require.Equal(t, uint32(42), req.GetLimit())
	require.Equal(t, "next", req.GetCursor())
}

func TestBindListRequestRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"-1", "nope", "4294967296"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			req := ptrext.Of(attunev1.ListModerationSubjectsRequest{})
			err := BindListRequest(httptest.NewRequest(http.MethodGet, "/?limit="+raw, http.NoBody), req)
			requireDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST)
		})
	}
}

func TestConversionDefaults(t *testing.T) {
	t.Parallel()

	require.Equal(t, repo.AccessModeDisabled, accessModeFromProto(attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_UNSPECIFIED))
	require.Equal(t, repo.WriteModeDisabled, writeModeFromProto(attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_UNSPECIFIED))
	require.Equal(t, repo.IdentityModeAnonymous, identityModeFromProto(attunev1.PublicIdentityMode_PUBLIC_IDENTITY_MODE_UNSPECIFIED))
	require.Equal(t, repo.ModerationStatePending, stateFromProto(attunev1.ModerationState_MODERATION_STATE_UNSPECIFIED))
	require.Equal(t, repo.SurfaceRequest, surfaceFromProto(attunev1.PublicSurface_PUBLIC_SURFACE_UNSPECIFIED))
	require.Equal(t, "", formatTime(nil))
	require.Nil(t, optionalTime(ptrext.Of(time.Time{})))
}

func TestConversionHelpersCoverAllKnownValues(t *testing.T) {
	t.Parallel()

	require.Equal(t, repo.AccessModeAuthenticated,
		accessModeFromProto(attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_AUTHENTICATED))
	require.Equal(t, attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_PUBLIC, accessModeToProto(repo.AccessModePublic))
	require.Equal(t, attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_AUTHENTICATED,
		accessModeToProto(repo.AccessModeAuthenticated))
	require.Equal(t, attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_INVITE_ONLY,
		accessModeToProto(repo.AccessModeInviteOnly))
	require.Equal(t, attunev1.PublicIdentityMode_PUBLIC_IDENTITY_MODE_ORGANIZATION,
		identityModeToProto(repo.IdentityModeOrganization))
	require.Equal(t, attunev1.ModerationState_MODERATION_STATE_REJECTED,
		stateToProto(repo.ModerationStateRejected))
	require.Equal(t, attunev1.ModerationState_MODERATION_STATE_SPAM, stateToProto(repo.ModerationStateSpam))
	require.Equal(t, repo.ModerationStateRejected,
		stateFromProto(attunev1.ModerationState_MODERATION_STATE_REJECTED))
	require.Equal(t, repo.ModerationStateSpam, stateFromProto(attunev1.ModerationState_MODERATION_STATE_SPAM))
	require.Equal(t, repo.SurfaceRoadmapItem,
		surfaceFromProto(attunev1.PublicSurface_PUBLIC_SURFACE_ROADMAP_ITEM))
	require.Equal(t, repo.SurfaceChangelogPost,
		surfaceFromProto(attunev1.PublicSurface_PUBLIC_SURFACE_CHANGELOG_POST))
	require.Equal(t, repo.SurfacePortalSubmission,
		surfaceFromProto(attunev1.PublicSurface_PUBLIC_SURFACE_PORTAL_SUBMISSION))
	require.Equal(t, attunev1.PublicSurface_PUBLIC_SURFACE_ROADMAP_ITEM, surfaceToProto(repo.SurfaceRoadmapItem))
	require.Equal(t, attunev1.PublicSurface_PUBLIC_SURFACE_CHANGELOG_POST, surfaceToProto(repo.SurfaceChangelogPost))
	require.Equal(t, attunev1.PublicSurface_PUBLIC_SURFACE_PORTAL_SUBMISSION, surfaceToProto(repo.SurfacePortalSubmission))
}

func testCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth: ptrext.Of(session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "user-1",
			UserType: "oidc",
		}),
	})
}

func samplePublication(requestID uuid.UUID) repo.RequestPublication {
	now := time.Date(2026, 7, 10, 11, 0, 0, 0, time.UTC)
	profileID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	return repo.RequestPublication{
		Profile: repo.RequestProfile{
			ID:                profileID,
			TenantID:          "tenant-1",
			RequestID:         requestID,
			PublicSlug:        "public-slug",
			PublicTitle:       "public-title",
			PublicSummary:     "public summary",
			PublicState:       "open",
			RoadmapColumn:     "now",
			IncludedInPortal:  true,
			IncludedInRoadmap: true,
			PublishedAt:       ptrext.Of(now),
			UpdatedBy:         "admin-1",
			CreatedAt:         now,
			UpdatedAt:         now.Add(time.Minute),
		},
		Moderation: repo.ModerationSubject{
			ID:        uuid.MustParse("55555555-5555-5555-5555-555555555555"),
			TenantID:  "tenant-1",
			Surface:   repo.SurfaceRequest,
			SubjectID: profileID.String(),
			State:     repo.ModerationStateApproved,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

func sampleSubject(id uuid.UUID, action svc.ModerationAction) repo.ModerationSubject {
	state := repo.ModerationStateApproved
	if action == svc.ActionReject {
		state = repo.ModerationStateRejected
	}
	return repo.ModerationSubject{
		ID:        id,
		TenantID:  "tenant-1",
		Surface:   repo.SurfaceRequest,
		SubjectID: "request-profile-1",
		State:     state,
		CreatedAt: time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 7, 10, 12, 1, 0, 0, time.UTC),
	}
}

func requireActor(t *testing.T, actor auditlogsvc.Actor) {
	t.Helper()
	require.Equal(t, "oidc", actor.Type)
	require.Equal(t, "user-1", actor.ID)
}

func requireDispatcherError(t *testing.T, err error, status int, code attunev1.ErrorCode) {
	t.Helper()
	var de *dispatcher.Error
	require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-param
	require.Equal(t, status, de.Status)
	require.Equal(t, code, de.Code)
}
