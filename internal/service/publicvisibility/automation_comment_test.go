package publicvisibility

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/publicvisibility"
)

func automationCommentFixture() (*Service, *fakePublicRepo, uuid.UUID) {
	requestID := uuid.MustParse("aaaaaaaa-1000-4000-8000-0000000000aa")
	fake := ptrext.Of(fakePublicRepo{
		policy: ptrext.Of(repo.Policy{
			TenantID:            "tenant-a",
			PortalAccessMode:    repo.AccessModePublic,
			CommentsEnabled:     true,
			CommentWriteMode:    repo.WriteModeAnonymous,
			DefaultCommentState: repo.ModerationStatePending,
		}),
		beginTx: ptrext.Of(fakePublicTx{}),
		addCommentResult: ptrext.Of(repo.PublicRequestComment{
			ID:                 uuid.MustParse("bbbbbbbb-1000-4000-8000-0000000000bb"),
			Body:               "shipping next week",
			SubmittedByDisplay: "Team update",
		}),
		createModerationSubjectResult: ptrext.Of(repo.ModerationSubject{
			ID:    uuid.MustParse("cccccccc-1000-4000-8000-0000000000cc"),
			State: repo.ModerationStatePending,
		}),
	})
	return ptrext.Of(Service{repo: fake}), fake, requestID
}

func TestCreateAutomationRequestComment_ModeratedFlow(t *testing.T) {
	svc, fake, requestID := automationCommentFixture()

	err := svc.CreateAutomationRequestComment(
		context.Background(), "tenant-a", requestID, "shipping next week", "apikey:k1")
	if err != nil {
		t.Fatalf("CreateAutomationRequestComment: %v", err)
	}
	if !fake.addCommentCalled {
		t.Fatal("comment insert not called")
	}
	if fake.addCommentTenantID != "tenant-a" || fake.addCommentRequestID != requestID {
		t.Fatalf("comment target: %s/%s", fake.addCommentTenantID, fake.addCommentRequestID)
	}
	if fake.addCommentSubjectKey != "automation:apikey:k1" {
		t.Fatalf("subject key: %q", fake.addCommentSubjectKey)
	}
	if !fake.createModerationSubjectCalled {
		t.Fatal("moderation subject not created — automation must never bypass review")
	}
	if fake.createModerationSubjectInput.State != repo.ModerationStatePending {
		t.Fatalf("moderation state: %s", fake.createModerationSubjectInput.State)
	}
}

func TestCreateAutomationRequestComment_PolicyGate(t *testing.T) {
	svc, fake, requestID := automationCommentFixture()
	fake.policy.CommentWriteMode = repo.WriteModeDisabled

	err := svc.CreateAutomationRequestComment(
		context.Background(), "tenant-a", requestID, "x", "apikey:k1")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled when comment writes are off, got %v", err)
	}
	if fake.addCommentCalled {
		t.Fatal("comment must not be inserted when policy blocks writes")
	}
}

func TestCreateAutomationRequestComment_Validation(t *testing.T) {
	svc, _, requestID := automationCommentFixture()
	if err := svc.CreateAutomationRequestComment(
		context.Background(), "tenant-a", requestID, "  ", "apikey:k1"); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank body: want ErrValidation, got %v", err)
	}
}

func TestCreateAutomationRequestComment_MissingRequest(t *testing.T) {
	svc, fake, requestID := automationCommentFixture()
	fake.addCommentErr = repo.ErrNotFound
	if err := svc.CreateAutomationRequestComment(
		context.Background(), "tenant-a", requestID, "x", "apikey:k1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing request: want ErrNotFound, got %v", err)
	}
}
