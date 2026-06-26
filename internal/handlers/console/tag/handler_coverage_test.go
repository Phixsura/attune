package tag

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// configurableTagRepo is a fully configurable fake that lets each test control
// return values independently. It lives here (not in audit_test.go) so it
// does not collide with the simpler fakeTagRepo defined there.
type configurableTagRepo struct {
	listTags   []feedbacktag.Tag
	listErr    error
	createTag  *feedbacktag.Tag
	createErr  error
	updateTag  *feedbacktag.Tag
	updateErr  error
	archiveErr error
	getTag     *feedbacktag.Tag
	getErr     error
}

func (r *configurableTagRepo) List(context.Context, string, bool) ([]feedbacktag.Tag, error) {
	return r.listTags, r.listErr
}

func (r *configurableTagRepo) Create(_ context.Context, t feedbacktag.Tag) (*feedbacktag.Tag, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	if r.createTag != nil {
		return r.createTag, nil
	}
	out := t
	out.ID = uuid.New()
	out.CreatedAt = time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	out.UpdatedAt = out.CreatedAt
	return ptrext.Of(out), nil
}

func (r *configurableTagRepo) Update(_ context.Context, t feedbacktag.Tag) (*feedbacktag.Tag, error) {
	if r.updateErr != nil {
		return nil, r.updateErr
	}
	if r.updateTag != nil {
		return r.updateTag, nil
	}
	out := t
	out.UpdatedAt = time.Date(2026, 6, 26, 11, 0, 0, 0, time.UTC)
	return ptrext.Of(out), nil
}

func (r *configurableTagRepo) Archive(context.Context, string, uuid.UUID) error {
	return r.archiveErr
}

func (r *configurableTagRepo) GetByID(context.Context, string, uuid.UUID) (*feedbacktag.Tag, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if r.getTag != nil {
		cp := ptrext.Indirect(r.getTag)
		return ptrext.Of(cp), nil
	}
	return nil, feedbacktag.ErrNotFound
}

// configurableAuditRecorder records events and can optionally return an error.
type configurableAuditRecorder struct {
	events []auditlogsvc.Event
	err    error
}

func (a *configurableAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	a.events = append(a.events, event)
	return a.err
}

// ---------------------------------------------------------------------------
// ToProto
// ---------------------------------------------------------------------------

func TestToProto(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	t.Run("full tag with scope", func(t *testing.T) {
		t.Parallel()
		tag := feedbacktag.Tag{
			ID:             uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"),
			Name:           "urgent",
			Color:          "#ef4444",
			Description:    "high priority",
			ExclusiveScope: ptrext.Of("severity"),
			UsageCount:     42,
			CreatedBy:      "user-1",
			CreatedAt:      now,
			UpdatedAt:      now.Add(time.Hour),
		}
		p := ToProto(tag)
		require.Equal(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", p.Id)
		require.Equal(t, "urgent", p.Name)
		require.Equal(t, "#ef4444", p.Color)
		require.Equal(t, "high priority", p.Description)
		require.NotNil(t, p.ExclusiveScope)
		require.Equal(t, "severity", ptrext.Indirect(p.ExclusiveScope))
		require.Equal(t, int32(42), p.UsageCount)
		require.False(t, p.Archived)
		require.Equal(t, "user-1", p.CreatedBy)
		require.Equal(t, now.Format(time.RFC3339), p.CreatedAt)
		require.Equal(t, now.Add(time.Hour).Format(time.RFC3339), p.UpdatedAt)
	})

	t.Run("tag without scope", func(t *testing.T) {
		t.Parallel()
		tag := feedbacktag.Tag{
			Name:      "bug",
			CreatedAt: now,
			UpdatedAt: now,
		}
		p := ToProto(tag)
		require.Nil(t, p.ExclusiveScope)
		require.False(t, p.Archived)
	})

	t.Run("archived tag", func(t *testing.T) {
		t.Parallel()
		tag := feedbacktag.Tag{
			Name:       "old",
			ArchivedAt: ptrext.Of(now),
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		p := ToProto(tag)
		require.True(t, p.Archived)
	})
}

// ---------------------------------------------------------------------------
// normalizeExclusiveScope
// ---------------------------------------------------------------------------

func TestNormalizeExclusiveScope(t *testing.T) {
	t.Parallel()

	t.Run("nil field", func(t *testing.T) {
		t.Parallel()
		got, err := normalizeExclusiveScope(nil, "")
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("empty after trim", func(t *testing.T) {
		t.Parallel()
		got, err := normalizeExclusiveScope(ptrext.Of("   "), "   ")
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("valid value", func(t *testing.T) {
		t.Parallel()
		got, err := normalizeExclusiveScope(ptrext.Of("severity"), "severity")
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, "severity", ptrext.Indirect(got))
	})

	t.Run("too long", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("x", 33)
		_, err := normalizeExclusiveScope(ptrext.Of(long), long)
		require.Error(t, err)
		require.Contains(t, err.Error(), "scope exceeds 32 characters")
	})
}

// ---------------------------------------------------------------------------
// buildTagUpdateInput
// ---------------------------------------------------------------------------

func TestBuildTagUpdateInput(t *testing.T) {
	t.Parallel()

	t.Run("empty name is valid", func(t *testing.T) {
		t.Parallel()
		inp, err := buildTagUpdateInput(ptrext.Of(attunev1.UpdateTagRequest{}))
		require.NoError(t, err)
		require.Equal(t, "", inp.name)
	})

	t.Run("name too long", func(t *testing.T) {
		t.Parallel()
		_, err := buildTagUpdateInput(ptrext.Of(attunev1.UpdateTagRequest{
			Name: ptrext.Of(strings.Repeat("a", 49)),
		}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("name with control chars", func(t *testing.T) {
		t.Parallel()
		_, err := buildTagUpdateInput(ptrext.Of(attunev1.UpdateTagRequest{
			Name: ptrext.Of("bad\x00name"),
		}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("description too long", func(t *testing.T) {
		t.Parallel()
		_, err := buildTagUpdateInput(ptrext.Of(attunev1.UpdateTagRequest{
			Description: ptrext.Of(strings.Repeat("d", 201)),
		}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("scope too long", func(t *testing.T) {
		t.Parallel()
		_, err := buildTagUpdateInput(ptrext.Of(attunev1.UpdateTagRequest{
			ExclusiveScope: ptrext.Of(strings.Repeat("s", 33)),
		}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("valid scope", func(t *testing.T) {
		t.Parallel()
		inp, err := buildTagUpdateInput(ptrext.Of(attunev1.UpdateTagRequest{
			ExclusiveScope: ptrext.Of("severity"),
		}))
		require.NoError(t, err)
		require.NotNil(t, inp.scope)
		require.Equal(t, "severity", ptrext.Indirect(inp.scope))
	})

	t.Run("all valid fields", func(t *testing.T) {
		t.Parallel()
		inp, err := buildTagUpdateInput(ptrext.Of(attunev1.UpdateTagRequest{
			Name:           ptrext.Of("Escalated"),
			Color:          ptrext.Of("#22C55E"),
			Description:    ptrext.Of("escalated items"),
			ExclusiveScope: ptrext.Of("priority"),
		}))
		require.NoError(t, err)
		require.Equal(t, "Escalated", inp.name)
		require.Equal(t, "#22c55e", inp.color)
		require.Equal(t, "escalated items", inp.description)
		require.NotNil(t, inp.scope)
		require.Equal(t, "priority", ptrext.Indirect(inp.scope))
	})
}

// ---------------------------------------------------------------------------
// tagWriteError
// ---------------------------------------------------------------------------

func TestTagWriteError(t *testing.T) {
	t.Parallel()
	ctx := tagCtx()

	t.Run("nil error returns nil", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, tagWriteError(ctx, "w", "t", "id", nil, "msg"))
	})

	t.Run("ErrNotFound maps to 404", func(t *testing.T) {
		t.Parallel()
		err := tagWriteError(ctx, "w", "t", "id", feedbacktag.ErrNotFound, "msg")
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusNotFound, de.Status)
	})

	t.Run("ErrNameConflict maps to 409", func(t *testing.T) {
		t.Parallel()
		err := tagWriteError(ctx, "w", "t", "id", feedbacktag.ErrNameConflict, "msg")
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusConflict, de.Status)
	})

	t.Run("ErrInvalidInput maps to 400", func(t *testing.T) {
		t.Parallel()
		err := tagWriteError(ctx, "w", "t", "id", feedbacktag.ErrInvalidInput, "msg")
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("generic error maps to 500", func(t *testing.T) {
		t.Parallel()
		err := tagWriteError(ctx, "w", "t", "id", errors.New("boom"), "custom msg")
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusInternalServerError, de.Status)
		require.Equal(t, "custom msg", de.Message)
	})
}

// ---------------------------------------------------------------------------
// tagAuditSnapshot
// ---------------------------------------------------------------------------

func TestTagAuditSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("full tag", func(t *testing.T) {
		t.Parallel()
		tag := feedbacktag.Tag{
			ID:             uuid.MustParse("11111111-2222-3333-4444-555555555555"),
			Name:           "urgent",
			Color:          "#ef4444",
			Description:    "high priority",
			ExclusiveScope: ptrext.Of("severity"),
			UsageCount:     5,
			CreatedBy:      "user-1",
		}
		snap := tagAuditSnapshot(tag)
		require.Equal(t, "11111111-2222-3333-4444-555555555555", snap["id"])
		require.Equal(t, "urgent", snap["name"])
		require.Equal(t, "#ef4444", snap["color"])
		require.Equal(t, "high priority", snap["description"])
		require.Equal(t, ptrext.Of("severity"), snap["exclusive_scope"])
		require.Equal(t, 5, snap["usage_count"])
		require.Equal(t, false, snap["archived"])
		require.Equal(t, "user-1", snap["created_by"])
	})

	t.Run("zero-value tag", func(t *testing.T) {
		t.Parallel()
		snap := tagAuditSnapshot(feedbacktag.Tag{})
		require.Equal(t, uuid.UUID{}.String(), snap["id"])
		require.Equal(t, "", snap["name"])
		require.Nil(t, snap["exclusive_scope"])
		require.Equal(t, false, snap["archived"])
	})

	t.Run("archived tag", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		tag := feedbacktag.Tag{ArchivedAt: ptrext.Of(now)}
		snap := tagAuditSnapshot(tag)
		require.Equal(t, true, snap["archived"])
	})
}

// ---------------------------------------------------------------------------
// tagAuditSummary
// ---------------------------------------------------------------------------

func TestTagAuditSummary(t *testing.T) {
	t.Parallel()

	t.Run("tag with name", func(t *testing.T) {
		t.Parallel()
		got := tagAuditSummary("Created tag", feedbacktag.Tag{Name: "urgent"})
		require.Equal(t, "Created tag (urgent)", got)
	})

	t.Run("tag without name", func(t *testing.T) {
		t.Parallel()
		got := tagAuditSummary("Archived tag", feedbacktag.Tag{})
		require.Equal(t, "Archived tag", got)
	})
}

// ---------------------------------------------------------------------------
// List handler
// ---------------------------------------------------------------------------

func TestListHandler(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
		repo := ptrext.Of(configurableTagRepo{
			listTags: []feedbacktag.Tag{
				{ID: uuid.New(), Name: "bug", Color: "#ef4444", CreatedAt: now, UpdatedAt: now},
				{ID: uuid.New(), Name: "feature", Color: "#22c55e", CreatedAt: now, UpdatedAt: now},
			},
		})
		h := ptrext.Of(Handler{repo: repo})
		result, err := h.List(tagCtx(), ptrext.Of(attunev1.ListTagsRequest{}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, result.Status)
		require.Len(t, result.Body.Tags, 2)
		require.Equal(t, "bug", result.Body.Tags[0].Name)
		require.Equal(t, "feature", result.Body.Tags[1].Name)
	})

	t.Run("repo error returns 500", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{listErr: errors.New("db down")})
		h := ptrext.Of(Handler{repo: repo})
		_, err := h.List(tagCtx(), ptrext.Of(attunev1.ListTagsRequest{}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})
}

// ---------------------------------------------------------------------------
// Create handler
// ---------------------------------------------------------------------------

func TestCreateHandler(t *testing.T) {
	t.Parallel()

	t.Run("empty name returns 400", func(t *testing.T) {
		t.Parallel()
		h := ptrext.Of(Handler{repo: ptrext.Of(configurableTagRepo{})})
		_, err := h.Create(tagCtx(), ptrext.Of(attunev1.CreateTagRequest{Name: ""}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("description too long returns 400", func(t *testing.T) {
		t.Parallel()
		h := ptrext.Of(Handler{repo: ptrext.Of(configurableTagRepo{})})
		_, err := h.Create(tagCtx(), ptrext.Of(attunev1.CreateTagRequest{
			Name:        "ok",
			Description: ptrext.Of(strings.Repeat("d", 201)),
		}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("scope too long returns 400", func(t *testing.T) {
		t.Parallel()
		h := ptrext.Of(Handler{repo: ptrext.Of(configurableTagRepo{})})
		_, err := h.Create(tagCtx(), ptrext.Of(attunev1.CreateTagRequest{
			Name:           "ok",
			ExclusiveScope: ptrext.Of(strings.Repeat("s", 33)),
		}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("name conflict returns 409", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{createErr: feedbacktag.ErrNameConflict})
		h := ptrext.Of(Handler{repo: repo})
		_, err := h.Create(tagCtx(), ptrext.Of(attunev1.CreateTagRequest{Name: "dup"}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusConflict, de.Status)
	})

	t.Run("invalid input returns 400", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{createErr: feedbacktag.ErrInvalidInput})
		h := ptrext.Of(Handler{repo: repo})
		_, err := h.Create(tagCtx(), ptrext.Of(attunev1.CreateTagRequest{Name: "bad"}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("internal error returns 500", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{createErr: errors.New("boom")})
		h := ptrext.Of(Handler{repo: repo})
		_, err := h.Create(tagCtx(), ptrext.Of(attunev1.CreateTagRequest{Name: "ok"}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})

	t.Run("happy path with auto-palette color", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{
			listTags: []feedbacktag.Tag{{}, {}, {}}, // 3 existing tags
		})
		audit := ptrext.Of(configurableAuditRecorder{})
		h := ptrext.Of(Handler{repo: repo, audit: audit})

		result, err := h.Create(tagCtx(), ptrext.Of(attunev1.CreateTagRequest{Name: "new-tag"}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, result.Status)
		require.Equal(t, "new-tag", result.Body.Name)
		// 3 existing => palette index 3 => "#22c55e"
		require.Equal(t, defaultPalette[3], result.Body.Color)
		require.Len(t, audit.events, 1)
		require.Equal(t, "tag.create", audit.events[0].Action)
	})

	t.Run("happy path with explicit color", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{})
		audit := ptrext.Of(configurableAuditRecorder{})
		h := ptrext.Of(Handler{repo: repo, audit: audit})

		result, err := h.Create(tagCtx(), ptrext.Of(attunev1.CreateTagRequest{
			Name:  "colored",
			Color: ptrext.Of("#AABBCC"),
		}))
		require.NoError(t, err)
		require.Equal(t, "#aabbcc", result.Body.Color)
	})
}

// ---------------------------------------------------------------------------
// Update handler
// ---------------------------------------------------------------------------

func TestUpdateHandler(t *testing.T) {
	t.Parallel()

	t.Run("invalid UUID returns 400", func(t *testing.T) {
		t.Parallel()
		h := ptrext.Of(Handler{repo: ptrext.Of(configurableTagRepo{})})
		_, err := h.Update(tagCtx(), ptrext.Of(attunev1.UpdateTagRequest{Id: "not-a-uuid"}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("name with control chars returns 400", func(t *testing.T) {
		t.Parallel()
		h := ptrext.Of(Handler{repo: ptrext.Of(configurableTagRepo{})})
		_, err := h.Update(tagCtx(), ptrext.Of(attunev1.UpdateTagRequest{
			Id:   uuid.New().String(),
			Name: ptrext.Of("bad\x00name"),
		}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("not found returns 404", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{updateErr: feedbacktag.ErrNotFound})
		h := ptrext.Of(Handler{repo: repo})
		_, err := h.Update(tagCtx(), ptrext.Of(attunev1.UpdateTagRequest{
			Id:   uuid.New().String(),
			Name: ptrext.Of("renamed"),
		}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusNotFound, de.Status)
	})

	t.Run("name conflict returns 409", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{updateErr: feedbacktag.ErrNameConflict})
		h := ptrext.Of(Handler{repo: repo})
		_, err := h.Update(tagCtx(), ptrext.Of(attunev1.UpdateTagRequest{
			Id:   uuid.New().String(),
			Name: ptrext.Of("dup"),
		}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusConflict, de.Status)
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		tagID := uuid.New()
		now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
		existing := feedbacktag.Tag{
			ID:        tagID,
			TenantID:  "tenant-1",
			Name:      "old",
			Color:     "#ef4444",
			CreatedAt: now,
			UpdatedAt: now,
		}
		repo := ptrext.Of(configurableTagRepo{
			getTag: ptrext.Of(existing),
			updateTag: ptrext.Of(feedbacktag.Tag{
				ID:        tagID,
				TenantID:  "tenant-1",
				Name:      "new",
				Color:     "#22c55e",
				CreatedAt: now,
				UpdatedAt: now.Add(time.Hour),
			}),
		})
		audit := ptrext.Of(configurableAuditRecorder{})
		h := ptrext.Of(Handler{repo: repo, audit: audit})
		result, err := h.Update(tagCtx(), ptrext.Of(attunev1.UpdateTagRequest{
			Id:   tagID.String(),
			Name: ptrext.Of("new"),
		}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, result.Status)
		require.Equal(t, "new", result.Body.Name)
		require.Len(t, audit.events, 1)
		require.Equal(t, "tag.update", audit.events[0].Action)
	})
}

// ---------------------------------------------------------------------------
// Archive handler
// ---------------------------------------------------------------------------

func TestArchiveHandler(t *testing.T) {
	t.Parallel()

	t.Run("invalid UUID returns 400", func(t *testing.T) {
		t.Parallel()
		h := ptrext.Of(Handler{repo: ptrext.Of(configurableTagRepo{})})
		_, err := h.Archive(tagCtx(), ptrext.Of(attunev1.ArchiveTagRequest{Id: "not-a-uuid"}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusBadRequest, de.Status)
	})

	t.Run("not found returns 404", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{archiveErr: feedbacktag.ErrNotFound})
		h := ptrext.Of(Handler{repo: repo})
		_, err := h.Archive(tagCtx(), ptrext.Of(attunev1.ArchiveTagRequest{Id: uuid.New().String()}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusNotFound, de.Status)
	})

	t.Run("internal error returns 500", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{archiveErr: errors.New("db down")})
		h := ptrext.Of(Handler{repo: repo})
		_, err := h.Archive(tagCtx(), ptrext.Of(attunev1.ArchiveTagRequest{Id: uuid.New().String()}))
		var de *dispatcher.Error
		require.ErrorAs(t, err, &de) // ptrext:allow errors.As out-parameter
		require.Equal(t, http.StatusInternalServerError, de.Status)
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		tagID := uuid.New()
		now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
		existing := feedbacktag.Tag{
			ID:        tagID,
			TenantID:  "tenant-1",
			Name:      "old-tag",
			Color:     "#ef4444",
			CreatedAt: now,
			UpdatedAt: now,
		}
		archivedTag := existing
		archivedTag.ArchivedAt = ptrext.Of(now.Add(time.Hour))
		repo := ptrext.Of(configurableTagRepo{
			getTag: ptrext.Of(archivedTag),
		})
		audit := ptrext.Of(configurableAuditRecorder{})
		h := ptrext.Of(Handler{repo: repo, audit: audit})
		result, err := h.Archive(tagCtx(), ptrext.Of(attunev1.ArchiveTagRequest{Id: tagID.String()}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, result.Status)
		require.Len(t, audit.events, 1)
		require.Equal(t, "tag.archive", audit.events[0].Action)
	})
}

// ---------------------------------------------------------------------------
// loadBeforeSnapshot
// ---------------------------------------------------------------------------

func TestLoadBeforeSnapshot(t *testing.T) {
	t.Parallel()
	tagID := uuid.New()
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	t.Run("tag found returns snapshot", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{
			getTag: ptrext.Of(feedbacktag.Tag{
				ID:        tagID,
				TenantID:  "tenant-1",
				Name:      "found",
				CreatedAt: now,
				UpdatedAt: now,
			}),
		})
		h := ptrext.Of(Handler{repo: repo})
		snap := h.loadBeforeSnapshot(tagCtx(), "test", "tenant-1", tagID)
		require.NotNil(t, snap)
		require.Equal(t, "found", snap["name"])
	})

	t.Run("ErrNotFound returns nil", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{getErr: feedbacktag.ErrNotFound})
		h := ptrext.Of(Handler{repo: repo})
		snap := h.loadBeforeSnapshot(tagCtx(), "test", "tenant-1", tagID)
		require.Nil(t, snap)
	})

	t.Run("other error returns nil", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(configurableTagRepo{getErr: errors.New("db error")})
		h := ptrext.Of(Handler{repo: repo})
		snap := h.loadBeforeSnapshot(tagCtx(), "test", "tenant-1", tagID)
		require.Nil(t, snap)
	})
}

// ---------------------------------------------------------------------------
// recordAudit
// ---------------------------------------------------------------------------

func TestRecordAudit(t *testing.T) {
	t.Parallel()

	t.Run("nil audit is no-op", func(t *testing.T) {
		t.Parallel()
		h := ptrext.Of(Handler{repo: ptrext.Of(configurableTagRepo{})})
		// audit field is nil
		err := h.recordAudit(tagCtx(), "tag.create", feedbacktag.Tag{}, "summary", nil, nil)
		require.NoError(t, err)
	})

	t.Run("empty UserType defaults to admin", func(t *testing.T) {
		t.Parallel()
		audit := ptrext.Of(configurableAuditRecorder{})
		h := ptrext.Of(Handler{repo: ptrext.Of(configurableTagRepo{}), audit: audit})
		ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
			Context: context.Background(),
			Auth:    ptrext.Of(session.AuthCtx{TenantID: "t1", UserID: "u1", UserType: ""}),
		})
		err := h.recordAudit(ctx, "tag.create", feedbacktag.Tag{}, "summary", nil, nil)
		require.NoError(t, err)
		require.Len(t, audit.events, 1)
		require.Equal(t, "admin", audit.events[0].Actor.Type)
	})

	t.Run("explicit UserType preserved", func(t *testing.T) {
		t.Parallel()
		audit := ptrext.Of(configurableAuditRecorder{})
		h := ptrext.Of(Handler{repo: ptrext.Of(configurableTagRepo{}), audit: audit})
		ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
			Context: context.Background(),
			Auth:    ptrext.Of(session.AuthCtx{TenantID: "t1", UserID: "u1", UserType: "oidc"}),
		})
		err := h.recordAudit(ctx, "tag.update", feedbacktag.Tag{}, "summary", nil, nil)
		require.NoError(t, err)
		require.Len(t, audit.events, 1)
		require.Equal(t, "oidc", audit.events[0].Actor.Type)
	})
}
