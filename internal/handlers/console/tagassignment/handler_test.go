// ptrext:file-allow test fixtures use handler/auth pointers and fake repos.
package tagassignment

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
)

// ---------------------------------------------------------------------------
// Fake repos
// ---------------------------------------------------------------------------

type fakeTagRepo struct {
	getByIDTag *feedbacktag.Tag
	getByIDErr error

	getByNameTag *feedbacktag.Tag
	getByNameErr error

	createTag *feedbacktag.Tag
	createErr error

	incrementCalls []uuid.UUID
	incrementErr   error

	decrementCalls []uuid.UUID
	decrementErr   error
}

func (f *fakeTagRepo) GetByID(_ context.Context, _ string, _ uuid.UUID) (*feedbacktag.Tag, error) {
	return f.getByIDTag, f.getByIDErr
}

func (f *fakeTagRepo) GetByName(_ context.Context, _, _ string) (*feedbacktag.Tag, error) {
	return f.getByNameTag, f.getByNameErr
}

func (f *fakeTagRepo) Create(_ context.Context, _ feedbacktag.Tag) (*feedbacktag.Tag, error) {
	return f.createTag, f.createErr
}

func (f *fakeTagRepo) IncrementUsage(_ context.Context, _ string, tagID uuid.UUID) error {
	f.incrementCalls = append(f.incrementCalls, tagID)
	return f.incrementErr
}

func (f *fakeTagRepo) DecrementUsage(_ context.Context, _ string, tagID uuid.UUID) error {
	f.decrementCalls = append(f.decrementCalls, tagID)
	return f.decrementErr
}

type fakeAssignmentRepo struct {
	addInserted bool
	addErr      error

	removeRemoved bool
	removeErr     error

	removeByScopeIDs []uuid.UUID
	removeByScopeErr error
}

func (f *fakeAssignmentRepo) Add(_ context.Context, _ string, _ int64, _ uuid.UUID, _ string) (bool, error) {
	return f.addInserted, f.addErr
}

func (f *fakeAssignmentRepo) Remove(_ context.Context, _ string, _ int64, _ uuid.UUID) (bool, error) {
	return f.removeRemoved, f.removeErr
}

func (f *fakeAssignmentRepo) RemoveByScopeExcluding(_ context.Context, _ string, _ int64, _ string, _ uuid.UUID) ([]uuid.UUID, error) {
	return f.removeByScopeIDs, f.removeByScopeErr
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var (
	fixedTagID  = uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	fixedTagID2 = uuid.MustParse("11111111-2222-3333-4444-555555555555")
)

func testCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return &dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    &session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"},
	}
}

func fixedTag() feedbacktag.Tag {
	return feedbacktag.Tag{
		ID:       fixedTagID,
		TenantID: "tenant-1",
		Name:     "bug",
		Color:    "#ff0000",
	}
}

func assertDispatcherErr(t *testing.T, err error, wantStatus int, wantCode attunev1.ErrorCode) {
	t.Helper()
	var de *dispatcher.Error
	require.ErrorAs(t, err, &de)
	require.Equal(t, wantStatus, de.Status)
	require.Equal(t, wantCode, de.Code)
}

// ---------------------------------------------------------------------------
// resolveTag
// ---------------------------------------------------------------------------

func TestResolveTag(t *testing.T) {
	t.Parallel()

	t.Run("tag_id valid", func(t *testing.T) {
		t.Parallel()
		tag := fixedTag()
		tr := &fakeTagRepo{getByIDTag: ptrext.Of(tag)}
		h := NewHandler(tr, &fakeAssignmentRepo{})
		ctx := testCtx()
		idStr := fixedTagID.String()
		got, err := h.resolveTag(ctx, ctx.Auth, &attunev1.AddFeedbackTagRequest{
			TagId: &idStr,
		})
		require.NoError(t, err)
		require.Equal(t, fixedTagID, got.ID)
	})

	t.Run("tag_id invalid UUID", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(&fakeTagRepo{}, &fakeAssignmentRepo{})
		ctx := testCtx()
		bad := "not-a-uuid"
		_, err := h.resolveTag(ctx, ctx.Auth, &attunev1.AddFeedbackTagRequest{
			TagId: &bad,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid tag id")
	})

	t.Run("tag_id GetByID fails", func(t *testing.T) {
		t.Parallel()
		tr := &fakeTagRepo{getByIDErr: errors.New("db down")}
		h := NewHandler(tr, &fakeAssignmentRepo{})
		ctx := testCtx()
		idStr := fixedTagID.String()
		_, err := h.resolveTag(ctx, ctx.Auth, &attunev1.AddFeedbackTagRequest{
			TagId: &idStr,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "db down")
	})

	t.Run("tag_name found", func(t *testing.T) {
		t.Parallel()
		tag := fixedTag()
		tr := &fakeTagRepo{getByNameTag: ptrext.Of(tag)}
		h := NewHandler(tr, &fakeAssignmentRepo{})
		ctx := testCtx()
		name := "bug"
		got, err := h.resolveTag(ctx, ctx.Auth, &attunev1.AddFeedbackTagRequest{
			TagName: &name,
		})
		require.NoError(t, err)
		require.Equal(t, "bug", got.Name)
	})

	t.Run("tag_name not found creates with default color", func(t *testing.T) {
		t.Parallel()
		created := fixedTag()
		created.Color = "#6b7280"
		tr := &fakeTagRepo{
			getByNameErr: feedbacktag.ErrNotFound,
			createTag:    ptrext.Of(created),
		}
		h := NewHandler(tr, &fakeAssignmentRepo{})
		ctx := testCtx()
		name := "new-tag"
		got, err := h.resolveTag(ctx, ctx.Auth, &attunev1.AddFeedbackTagRequest{
			TagName: &name,
		})
		require.NoError(t, err)
		require.Equal(t, "#6b7280", got.Color)
	})

	t.Run("tag_name not found creates with custom color", func(t *testing.T) {
		t.Parallel()
		created := fixedTag()
		created.Color = "#00ff00"
		tr := &fakeTagRepo{
			getByNameErr: feedbacktag.ErrNotFound,
			createTag:    ptrext.Of(created),
		}
		h := NewHandler(tr, &fakeAssignmentRepo{})
		ctx := testCtx()
		name := "new-tag"
		color := "#00FF00"
		got, err := h.resolveTag(ctx, ctx.Auth, &attunev1.AddFeedbackTagRequest{
			TagName:  &name,
			TagColor: &color,
		})
		require.NoError(t, err)
		require.Equal(t, "#00ff00", got.Color)
	})

	t.Run("neither tag_id nor tag_name", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(&fakeTagRepo{}, &fakeAssignmentRepo{})
		ctx := testCtx()
		_, err := h.resolveTag(ctx, ctx.Auth, &attunev1.AddFeedbackTagRequest{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "either tag_id or tag_name is required")
	})

	t.Run("empty tag_name", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(&fakeTagRepo{}, &fakeAssignmentRepo{})
		ctx := testCtx()
		empty := "   "
		_, err := h.resolveTag(ctx, ctx.Auth, &attunev1.AddFeedbackTagRequest{
			TagName: &empty,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "either tag_id or tag_name is required")
	})

	t.Run("tag_name lookup returns non-ErrNotFound error", func(t *testing.T) {
		t.Parallel()
		tr := &fakeTagRepo{getByNameErr: errors.New("connection refused")}
		h := NewHandler(tr, &fakeAssignmentRepo{})
		ctx := testCtx()
		name := "bug"
		_, err := h.resolveTag(ctx, ctx.Auth, &attunev1.AddFeedbackTagRequest{
			TagName: &name,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "connection refused")
	})

	t.Run("tag_name create fails", func(t *testing.T) {
		t.Parallel()
		tr := &fakeTagRepo{
			getByNameErr: feedbacktag.ErrNotFound,
			createErr:    errors.New("create failed"),
		}
		h := NewHandler(tr, &fakeAssignmentRepo{})
		ctx := testCtx()
		name := "new-tag"
		_, err := h.resolveTag(ctx, ctx.Auth, &attunev1.AddFeedbackTagRequest{
			TagName: &name,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "create failed")
	})
}

// ---------------------------------------------------------------------------
// Add handler
// ---------------------------------------------------------------------------

func TestAdd(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		tag := fixedTag()
		tr := &fakeTagRepo{getByIDTag: ptrext.Of(tag)}
		ar := &fakeAssignmentRepo{addInserted: true}
		h := NewHandler(tr, ar)
		ctx := testCtx()
		idStr := fixedTagID.String()
		res, err := h.Add(ctx, &attunev1.AddFeedbackTagRequest{
			FeedbackId: 42,
			TagId:      &idStr,
		})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
		require.NotNil(t, res.Body.GetTag())
		require.Equal(t, fixedTagID.String(), res.Body.GetTag().GetId())
	})

	t.Run("resolve fails returns 400", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(&fakeTagRepo{}, &fakeAssignmentRepo{})
		ctx := testCtx()
		// neither tag_id nor tag_name -> resolveTag error
		_, err := h.Add(ctx, &attunev1.AddFeedbackTagRequest{
			FeedbackId: 42,
		})
		assertDispatcherErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
	})

	t.Run("addWithScope fails returns 500", func(t *testing.T) {
		t.Parallel()
		tag := fixedTag()
		tr := &fakeTagRepo{getByIDTag: ptrext.Of(tag)}
		ar := &fakeAssignmentRepo{addErr: errors.New("db write failed")}
		h := NewHandler(tr, ar)
		ctx := testCtx()
		idStr := fixedTagID.String()
		_, err := h.Add(ctx, &attunev1.AddFeedbackTagRequest{
			FeedbackId: 42,
			TagId:      &idStr,
		})
		assertDispatcherErr(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})
}

// ---------------------------------------------------------------------------
// Remove handler
// ---------------------------------------------------------------------------

func TestRemove(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		tr := &fakeTagRepo{}
		ar := &fakeAssignmentRepo{removeRemoved: true}
		h := NewHandler(tr, ar)
		ctx := testCtx()
		res, err := h.Remove(ctx, &attunev1.RemoveFeedbackTagRequest{
			FeedbackId: 42,
			TagId:      fixedTagID.String(),
		})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
	})

	t.Run("invalid tag ID returns 400", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(&fakeTagRepo{}, &fakeAssignmentRepo{})
		ctx := testCtx()
		_, err := h.Remove(ctx, &attunev1.RemoveFeedbackTagRequest{
			FeedbackId: 42,
			TagId:      "not-a-uuid",
		})
		assertDispatcherErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_BAD_ID)
	})

	t.Run("remove fails returns 500", func(t *testing.T) {
		t.Parallel()
		ar := &fakeAssignmentRepo{removeErr: errors.New("db error")}
		h := NewHandler(&fakeTagRepo{}, ar)
		ctx := testCtx()
		_, err := h.Remove(ctx, &attunev1.RemoveFeedbackTagRequest{
			FeedbackId: 42,
			TagId:      fixedTagID.String(),
		})
		assertDispatcherErr(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})

	t.Run("removed=true decrements usage", func(t *testing.T) {
		t.Parallel()
		tr := &fakeTagRepo{}
		ar := &fakeAssignmentRepo{removeRemoved: true}
		h := NewHandler(tr, ar)
		ctx := testCtx()
		_, err := h.Remove(ctx, &attunev1.RemoveFeedbackTagRequest{
			FeedbackId: 42,
			TagId:      fixedTagID.String(),
		})
		require.NoError(t, err)
		require.Len(t, tr.decrementCalls, 1)
		require.Equal(t, fixedTagID, tr.decrementCalls[0])
	})

	t.Run("removed=false skips decrement", func(t *testing.T) {
		t.Parallel()
		tr := &fakeTagRepo{}
		ar := &fakeAssignmentRepo{removeRemoved: false}
		h := NewHandler(tr, ar)
		ctx := testCtx()
		_, err := h.Remove(ctx, &attunev1.RemoveFeedbackTagRequest{
			FeedbackId: 42,
			TagId:      fixedTagID.String(),
		})
		require.NoError(t, err)
		require.Empty(t, tr.decrementCalls)
	})
}

// ---------------------------------------------------------------------------
// BatchUpdate handler
// ---------------------------------------------------------------------------

func TestBatchUpdate(t *testing.T) {
	t.Parallel()

	t.Run("too many feedback IDs", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(&fakeTagRepo{}, &fakeAssignmentRepo{})
		ctx := testCtx()
		ids := make([]int64, 101)
		for i := range ids {
			ids[i] = int64(i)
		}
		_, err := h.BatchUpdate(ctx, &attunev1.BatchUpdateFeedbackTagsRequest{
			FeedbackIds: ids,
		})
		assertDispatcherErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
	})

	t.Run("too many tag operations", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(&fakeTagRepo{}, &fakeAssignmentRepo{})
		ctx := testCtx()
		addIDs := make([]string, 21)
		for i := range addIDs {
			addIDs[i] = uuid.New().String()
		}
		_, err := h.BatchUpdate(ctx, &attunev1.BatchUpdateFeedbackTagsRequest{
			FeedbackIds: []int64{1},
			AddTagIds:   addIDs,
		})
		assertDispatcherErr(t, err, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
	})

	t.Run("invalid add tag ID early return", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(&fakeTagRepo{}, &fakeAssignmentRepo{})
		ctx := testCtx()
		res, err := h.BatchUpdate(ctx, &attunev1.BatchUpdateFeedbackTagsRequest{
			FeedbackIds: []int64{1},
			AddTagIds:   []string{"not-a-uuid"},
		})
		// resolveAddTags returns an early result (not via error path).
		require.NoError(t, err)
		require.Equal(t, 0, res.Status)
	})

	t.Run("add tag not found early return", func(t *testing.T) {
		t.Parallel()
		tr := &fakeTagRepo{getByIDErr: feedbacktag.ErrNotFound}
		h := NewHandler(tr, &fakeAssignmentRepo{})
		ctx := testCtx()
		res, err := h.BatchUpdate(ctx, &attunev1.BatchUpdateFeedbackTagsRequest{
			FeedbackIds: []int64{1},
			AddTagIds:   []string{fixedTagID.String()},
		})
		require.NoError(t, err)
		require.Equal(t, 0, res.Status)
	})

	t.Run("invalid remove tag ID early return", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(&fakeTagRepo{}, &fakeAssignmentRepo{})
		ctx := testCtx()
		res, err := h.BatchUpdate(ctx, &attunev1.BatchUpdateFeedbackTagsRequest{
			FeedbackIds:  []int64{1},
			RemoveTagIds: []string{"bad-id"},
		})
		require.NoError(t, err)
		require.Equal(t, 0, res.Status)
	})

	t.Run("remove tag not found early return", func(t *testing.T) {
		t.Parallel()
		tr := &fakeTagRepo{getByIDErr: feedbacktag.ErrNotFound}
		h := NewHandler(tr, &fakeAssignmentRepo{})
		ctx := testCtx()
		res, err := h.BatchUpdate(ctx, &attunev1.BatchUpdateFeedbackTagsRequest{
			FeedbackIds:  []int64{1},
			RemoveTagIds: []string{fixedTagID.String()},
		})
		require.NoError(t, err)
		require.Equal(t, 0, res.Status)
	})

	t.Run("happy path adds and removes", func(t *testing.T) {
		t.Parallel()
		tag := fixedTag()
		tr := &fakeTagRepo{getByIDTag: ptrext.Of(tag)}
		ar := &fakeAssignmentRepo{addInserted: true, removeRemoved: true}
		h := NewHandler(tr, ar)
		ctx := testCtx()
		res, err := h.BatchUpdate(ctx, &attunev1.BatchUpdateFeedbackTagsRequest{
			FeedbackIds:  []int64{1, 2},
			AddTagIds:    []string{fixedTagID.String()},
			RemoveTagIds: []string{fixedTagID.String()},
		})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
		// 2 feedback IDs x (1 add + 1 remove) = 4 affected
		require.Equal(t, int32(4), res.Body.GetAffected())
	})

	t.Run("partial add failure logged but not fatal", func(t *testing.T) {
		t.Parallel()
		tag := fixedTag()
		tr := &fakeTagRepo{getByIDTag: ptrext.Of(tag)}
		ar := &fakeAssignmentRepo{addErr: errors.New("transient")}
		h := NewHandler(tr, ar)
		ctx := testCtx()
		res, err := h.BatchUpdate(ctx, &attunev1.BatchUpdateFeedbackTagsRequest{
			FeedbackIds: []int64{1},
			AddTagIds:   []string{fixedTagID.String()},
		})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
		require.Equal(t, int32(0), res.Body.GetAffected())
	})

	t.Run("partial remove failure logged but not fatal", func(t *testing.T) {
		t.Parallel()
		tag := fixedTag()
		tr := &fakeTagRepo{getByIDTag: ptrext.Of(tag)}
		ar := &fakeAssignmentRepo{removeErr: errors.New("transient")}
		h := NewHandler(tr, ar)
		ctx := testCtx()
		res, err := h.BatchUpdate(ctx, &attunev1.BatchUpdateFeedbackTagsRequest{
			FeedbackIds:  []int64{1},
			RemoveTagIds: []string{fixedTagID.String()},
		})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.Status)
		require.Equal(t, int32(0), res.Body.GetAffected())
	})
}

// ---------------------------------------------------------------------------
// addWithScope
// ---------------------------------------------------------------------------

func TestAddWithScope(t *testing.T) {
	t.Parallel()

	t.Run("no exclusive scope direct add", func(t *testing.T) {
		t.Parallel()
		tr := &fakeTagRepo{}
		ar := &fakeAssignmentRepo{addInserted: true}
		h := NewHandler(tr, ar)
		tag := fixedTag() // ExclusiveScope is nil
		inserted, err := h.addWithScope(context.Background(), "tenant-1", 42, ptrext.Of(tag), "user-1")
		require.NoError(t, err)
		require.True(t, inserted)
		require.Len(t, tr.incrementCalls, 1)
		require.Equal(t, fixedTagID, tr.incrementCalls[0])
	})

	t.Run("with exclusive scope removes others first", func(t *testing.T) {
		t.Parallel()
		removedID := fixedTagID2
		tr := &fakeTagRepo{}
		ar := &fakeAssignmentRepo{
			removeByScopeIDs: []uuid.UUID{removedID},
			addInserted:      true,
		}
		h := NewHandler(tr, ar)
		tag := fixedTag()
		tag.ExclusiveScope = ptrext.Of("sentiment")
		inserted, err := h.addWithScope(context.Background(), "tenant-1", 42, ptrext.Of(tag), "user-1")
		require.NoError(t, err)
		require.True(t, inserted)
		// Decrement called for the removed scoped tag, plus increment for the added tag.
		require.Len(t, tr.decrementCalls, 1)
		require.Equal(t, removedID, tr.decrementCalls[0])
		require.Len(t, tr.incrementCalls, 1)
		require.Equal(t, fixedTagID, tr.incrementCalls[0])
	})

	t.Run("scope removal error returns error", func(t *testing.T) {
		t.Parallel()
		ar := &fakeAssignmentRepo{removeByScopeErr: errors.New("scope remove failed")}
		h := NewHandler(&fakeTagRepo{}, ar)
		tag := fixedTag()
		tag.ExclusiveScope = ptrext.Of("sentiment")
		_, err := h.addWithScope(context.Background(), "tenant-1", 42, ptrext.Of(tag), "user-1")
		require.Error(t, err)
		require.Contains(t, err.Error(), "scope remove failed")
	})

	t.Run("add error returns error", func(t *testing.T) {
		t.Parallel()
		ar := &fakeAssignmentRepo{addErr: errors.New("insert failed")}
		h := NewHandler(&fakeTagRepo{}, ar)
		tag := fixedTag()
		_, err := h.addWithScope(context.Background(), "tenant-1", 42, ptrext.Of(tag), "user-1")
		require.Error(t, err)
		require.Contains(t, err.Error(), "insert failed")
	})

	t.Run("add not inserted skips increment", func(t *testing.T) {
		t.Parallel()
		tr := &fakeTagRepo{}
		ar := &fakeAssignmentRepo{addInserted: false}
		h := NewHandler(tr, ar)
		tag := fixedTag()
		inserted, err := h.addWithScope(context.Background(), "tenant-1", 42, ptrext.Of(tag), "user-1")
		require.NoError(t, err)
		require.False(t, inserted)
		require.Empty(t, tr.incrementCalls)
	})
}
