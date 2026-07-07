// SPDX-License-Identifier: Apache-2.0

package customerrequest

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
)

func TestNormalizePromoteDedupeAndDefaults(t *testing.T) {
	got, err := normalizePromote(PromoteInput{
		TenantID:       "tenant-a",
		FeedbackIDs:    []int64{42, 0, 42, 99, -7},
		Title:          "  Request title  ",
		Description:    "  Evidence from customers  ",
		IdempotencyKey: "promote_123",
	})
	if err != nil {
		t.Fatalf("normalizePromote() error = %v", err)
	}

	if got.Title != "Request title" {
		t.Fatalf("Title = %q, want trimmed title", got.Title)
	}
	if got.Description != "Evidence from customers" {
		t.Fatalf("Description = %q, want trimmed description", got.Description)
	}
	if got.Status != repo.StatusOpen {
		t.Fatalf("Status = %q, want %q", got.Status, repo.StatusOpen)
	}
	if got.Priority != repo.PriorityNone {
		t.Fatalf("Priority = %q, want %q", got.Priority, repo.PriorityNone)
	}
	if want := []int64{42, 99}; !reflect.DeepEqual(got.FeedbackIDs, want) {
		t.Fatalf("FeedbackIDs = %#v, want %#v", got.FeedbackIDs, want)
	}
}

func TestNormalizePromoteRejectsEmptyEvidence(t *testing.T) {
	_, err := normalizePromote(PromoteInput{
		TenantID:       "tenant-a",
		FeedbackIDs:    []int64{0, -2},
		Title:          "Request title",
		IdempotencyKey: "promote_123",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizePromote() error = %v, want ErrValidation", err)
	}
}

func TestNormalizeIssueInputDerivesExternalKey(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	got, err := normalizeIssueInput(LinkIssueInput{
		TenantID:    "tenant-a",
		RequestID:   requestID,
		Provider:    " GitHub ",
		ExternalURL: " https://github.com/Phixsura/attune/issues/212 ",
		Title:       " Customer Request object ",
		Status:      " open ",
	})
	if err != nil {
		t.Fatalf("normalizeIssueInput() error = %v", err)
	}

	if got.Provider != "github" {
		t.Fatalf("Provider = %q, want github", got.Provider)
	}
	if got.ExternalKey != "Phixsura/attune#212" {
		t.Fatalf("ExternalKey = %q, want derived GitHub issue key", got.ExternalKey)
	}
	if got.Title != "Customer Request object" {
		t.Fatalf("Title = %q, want trimmed title", got.Title)
	}
	if got.Status != "open" {
		t.Fatalf("Status = %q, want trimmed status", got.Status)
	}
}

func TestNormalizeCustomerLinkTrimsIdentityFields(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	got, err := normalizeCustomerLink(LinkCustomerInput{
		TenantID:       "tenant-a",
		RequestID:      requestID,
		SubjectKey:     " user:42 ",
		SubjectHash:    " hash:42 ",
		SubjectDisplay: " Ada Lovelace ",
		AccountKey:     " account:acme ",
		AccountDisplay: " Acme Inc ",
		Note:           " Enterprise buyer ",
	})
	if err != nil {
		t.Fatalf("normalizeCustomerLink() error = %v", err)
	}

	if got.SubjectKey != "user:42" || got.SubjectHash != "hash:42" || got.SubjectDisplay != "Ada Lovelace" {
		t.Fatalf("subject fields not normalized: %+v", got)
	}
	if got.AccountKey != "account:acme" || got.AccountDisplay != "Acme Inc" {
		t.Fatalf("account fields not normalized: %+v", got)
	}
	if got.Note != "Enterprise buyer" {
		t.Fatalf("Note = %q, want trimmed note", got.Note)
	}
}

func TestNormalizeCustomerLinkRejectsMissingIdentity(t *testing.T) {
	_, err := normalizeCustomerLink(LinkCustomerInput{
		TenantID:  "tenant-a",
		RequestID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeCustomerLink() error = %v, want ErrValidation", err)
	}
}

func TestNormalizeVoteDefaultsAndValidatesWeight(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	got, err := normalizeVote(VoteInput{
		TenantID:   "tenant-a",
		RequestID:  requestID,
		AccountKey: " account:acme ",
	})
	if err != nil {
		t.Fatalf("normalizeVote() error = %v", err)
	}
	if got.AccountKey != "account:acme" {
		t.Fatalf("AccountKey = %q, want trimmed account key", got.AccountKey)
	}
	if got.Weight != 1 {
		t.Fatalf("Weight = %d, want default weight 1", got.Weight)
	}

	_, err = normalizeVote(VoteInput{
		TenantID:   "tenant-a",
		RequestID:  requestID,
		AccountKey: "account:acme",
		Weight:     101,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeVote(weight=101) error = %v, want ErrValidation", err)
	}
}

func TestNormalizeSupporterRejectsOversizedFields(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	_, err := normalizeCustomerLink(LinkCustomerInput{
		TenantID:   "tenant-a",
		RequestID:  requestID,
		SubjectKey: strings.Repeat("x", 513),
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeCustomerLink(oversized subject) error = %v, want ErrValidation", err)
	}
}

func TestNormalizeIssueInputRejectsUnsupportedProvider(t *testing.T) {
	_, err := normalizeIssueInput(LinkIssueInput{
		TenantID:    "tenant-a",
		RequestID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Provider:    "not-a-tracker",
		ExternalURL: "https://example.com/issues/1",
	})
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("normalizeIssueInput() error = %v, want ErrUnsupportedProvider", err)
	}
}

func TestNormalizeUpdateTrimsPointerFields(t *testing.T) {
	status := repo.StatusInProgress
	priority := repo.PriorityHigh
	got, err := normalizeUpdate(UpdateInput{
		TenantID:    "tenant-a",
		ID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Title:       ptrext.Of("  Better exports  "),
		Description: ptrext.Of("  Batch export requests  "),
		Status:      ptrext.Of(status),
		Priority:    ptrext.Of(priority),
	})
	if err != nil {
		t.Fatalf("normalizeUpdate() error = %v", err)
	}

	if got.Title == nil || ptrext.Indirect(got.Title) != "Better exports" {
		t.Fatalf("Title = %#v, want trimmed pointer", got.Title)
	}
	if got.Description == nil || ptrext.Indirect(got.Description) != "Batch export requests" {
		t.Fatalf("Description = %#v, want trimmed pointer", got.Description)
	}
}
