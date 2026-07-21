// ptrext:file-allow customer request service tests use fake stores and pointer-valued fixtures.
// SPDX-License-Identifier: Apache-2.0

package customerrequest

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
	externalsyncrepo "github.com/Phixsura/attune/internal/repo/externalsync"
	"github.com/Phixsura/attune/internal/repo/idempotency"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
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

func TestNormalizePromoteRejectsTooManyFeedbackIDs(t *testing.T) {
	ids := make([]int64, 101)
	for i := range ids {
		ids[i] = int64(i + 1)
	}

	_, err := normalizePromote(PromoteInput{
		TenantID:       "tenant-a",
		FeedbackIDs:    ids,
		Title:          "Request title",
		IdempotencyKey: "promote_123",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizePromote(101 ids) error = %v, want ErrValidation", err)
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

func TestResolveManagedIssueLinkTargetRejectsAmbiguousLocatorInput(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	connectionID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	service := &Service{}

	tests := []struct {
		name string
		in   LinkIssueInput
		want error
	}{
		{
			name: "url and issue number",
			in: LinkIssueInput{
				TenantID:     "tenant-a",
				RequestID:    requestID,
				Provider:     "github",
				ExternalURL:  "https://github.com/Phixsura/attune/issues/212",
				ConnectionID: ptrext.Of(connectionID),
				IssueNumber:  "212",
			},
			want: ErrValidation,
		},
		{
			name: "issue number without connection",
			in: LinkIssueInput{
				TenantID:    "tenant-a",
				RequestID:   requestID,
				Provider:    "github",
				IssueNumber: "212",
			},
			want: ErrValidation,
		},
		{
			name: "non github provider with issue number",
			in: LinkIssueInput{
				TenantID:     "tenant-a",
				RequestID:    requestID,
				Provider:     "jira",
				ConnectionID: ptrext.Of(connectionID),
				IssueNumber:  "212",
			},
			want: ErrUnsupportedProvider,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.resolveManagedIssueLinkTarget(context.Background(), tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("resolveManagedIssueLinkTarget() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestResolveManagedIssueLinkTargetAllowsDirectURLInput(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := &Service{}
	in := LinkIssueInput{
		TenantID:    "tenant-a",
		RequestID:   requestID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/212",
		IssueNumber: "   ",
	}

	got, err := service.resolveManagedIssueLinkTarget(context.Background(), in)
	if err != nil {
		t.Fatalf("resolveManagedIssueLinkTarget() error = %v", err)
	}
	if got.ExternalURL != in.ExternalURL || got.IssueNumber != "" {
		t.Fatalf("resolveManagedIssueLinkTarget() = %+v, want direct URL unchanged with blank issue number", got)
	}
}

func TestEnqueueManagedIssuePullBranches(t *testing.T) {
	ctx := context.Background()
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	connectionID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	mappingID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	input := LinkIssueInput{
		TenantID:  "tenant-a",
		RequestID: requestID,
		Provider:  "github",
		Actor:     auditlogsvc.Actor{ID: "user-1"},
	}
	target := &repo.ManagedIssueSyncTarget{
		ConnectionID: connectionID,
		MappingID:    mappingID,
		ExternalKey:  "Phixsura/attune#228",
	}

	(&Service{}).enqueueManagedIssuePull(ctx, input, target)

	store := &recordingIssueCreateRunStore{}
	service := &Service{issueCreates: store}
	service.enqueueManagedIssuePull(ctx, input, nil)
	service.enqueueManagedIssuePull(ctx, LinkIssueInput{Provider: "jira"}, target)
	service.enqueueManagedIssuePull(ctx, input, &repo.ManagedIssueSyncTarget{ConnectionID: connectionID, MappingID: mappingID})
	if len(store.pullInputs) != 0 {
		t.Fatalf("skip branches enqueued %d pull runs, want 0", len(store.pullInputs))
	}

	service.enqueueManagedIssuePull(ctx, input, target)
	if len(store.pullInputs) != 1 {
		t.Fatalf("pull run enqueues = %d, want 1", len(store.pullInputs))
	}
	enqueued := store.pullInputs[0]
	if enqueued.TenantID != input.TenantID ||
		enqueued.RequestID != requestID ||
		enqueued.ConnectionID != connectionID ||
		enqueued.MappingID != mappingID ||
		enqueued.ExternalKey != "Phixsura/attune#228" ||
		enqueued.ActorID != "user-1" {
		t.Fatalf("pull input = %+v", enqueued)
	}

	failingStore := &recordingIssueCreateRunStore{pullErr: errors.New("queue failed")}
	(&Service{issueCreates: failingStore}).enqueueManagedIssuePull(ctx, input, target)
	if len(failingStore.pullInputs) != 1 {
		t.Fatalf("failing store pull inputs = %d, want 1", len(failingStore.pullInputs))
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

func TestNormalizeSupporterProfileAndValidationBranches(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	revenue := int64(125000)

	gotLink, err := normalizeCustomerLink(LinkCustomerInput{
		TenantID:   "tenant-a",
		RequestID:  requestID,
		AccountKey: " account:acme ",
		AccountProfile: AccountProfileInput{
			RevenueCents:    ptrext.Of(revenue),
			RevenueCurrency: " ",
			Tier:            " enterprise ",
		},
	})
	if err != nil {
		t.Fatalf("normalizeCustomerLink(profile) error = %v", err)
	}
	if gotLink.AccountProfile.RevenueCurrency != "USD" ||
		gotLink.AccountProfile.Tier != "enterprise" ||
		accountRevenueCents(gotLink.AccountProfile) != revenue {
		t.Fatalf("normalizeCustomerLink(profile) = %+v, want normalized account profile", gotLink.AccountProfile)
	}

	gotVote, err := normalizeVote(VoteInput{
		TenantID:       "tenant-a",
		RequestID:      requestID,
		AccountKey:     " account:acme ",
		AccountProfile: AccountProfileInput{LifecycleStatus: " active "},
	})
	if err != nil {
		t.Fatalf("normalizeVote(profile) error = %v", err)
	}
	if gotVote.Weight != 1 ||
		gotVote.AccountProfile.RevenueCurrency != "USD" ||
		gotVote.AccountProfile.LifecycleStatus != "active" {
		t.Fatalf("normalizeVote(profile) = %+v, want default weight and normalized profile", gotVote)
	}

	cases := []struct {
		name string
		link LinkCustomerInput
	}{
		{
			name: "missing tenant",
			link: LinkCustomerInput{
				RequestID:  requestID,
				AccountKey: "account:acme",
			},
		},
		{
			name: "missing request id",
			link: LinkCustomerInput{
				TenantID:   "tenant-a",
				AccountKey: "account:acme",
			},
		},
		{
			name: "profile without account key",
			link: LinkCustomerInput{
				TenantID:       "tenant-a",
				RequestID:      requestID,
				SubjectKey:     "user:42",
				AccountProfile: AccountProfileInput{Tier: "enterprise"},
			},
		},
		{
			name: "invalid profile currency",
			link: LinkCustomerInput{
				TenantID:       "tenant-a",
				RequestID:      requestID,
				AccountKey:     "account:acme",
				AccountProfile: AccountProfileInput{RevenueCurrency: "US"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizeCustomerLink(tc.link); !errors.Is(err, ErrValidation) {
				t.Fatalf("normalizeCustomerLink() error = %v, want ErrValidation", err)
			}
		})
	}

	if _, err := normalizeVote(VoteInput{
		TenantID:   "tenant-a",
		RequestID:  requestID,
		AccountKey: "account:acme",
		Weight:     -1,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeVote(weight=-1) error = %v, want ErrValidation", err)
	}
	if _, err := normalizeVote(VoteInput{
		TenantID:       "tenant-a",
		RequestID:      requestID,
		SubjectKey:     "user:42",
		AccountProfile: AccountProfileInput{CRMProvider: "salesforce"},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeVote(profile without account) error = %v, want ErrValidation", err)
	}
	if _, err := normalizeVote(VoteInput{
		TenantID:       "tenant-a",
		RequestID:      requestID,
		AccountKey:     "account:acme",
		AccountProfile: AccountProfileInput{RevenueCents: ptrext.Of(int64(-1))},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeVote(negative revenue) error = %v, want ErrValidation", err)
	}
}

func TestValidSupporterFieldsEachLimit(t *testing.T) {
	valid := []string{"subject-key", "subject-hash", "Subject Display", "account-key", "Account Display", "note"}
	tests := []struct {
		name  string
		field int
		value string
	}{
		{name: "subject key", field: 0, value: strings.Repeat("x", 513)},
		{name: "subject hash", field: 1, value: strings.Repeat("x", 129)},
		{name: "subject display", field: 2, value: strings.Repeat("x", 501)},
		{name: "account key", field: 3, value: strings.Repeat("x", 513)},
		{name: "account display", field: 4, value: strings.Repeat("x", 501)},
		{name: "note", field: 5, value: strings.Repeat("x", 5001)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := append([]string(nil), valid...)
			fields[tt.field] = tt.value
			if validSupporterFields(fields[0], fields[1], fields[2], fields[3], fields[4], fields[5]) {
				t.Fatal("validSupporterFields() = true, want false")
			}
		})
	}
	if !validSupporterFields(valid[0], valid[1], valid[2], valid[3], valid[4], valid[5]) {
		t.Fatal("validSupporterFields(valid) = false, want true")
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

func TestNormalizeIssueInputValidationBranches(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	got, err := normalizeIssueInput(LinkIssueInput{
		TenantID:    "tenant-a",
		RequestID:   requestID,
		Provider:    " GitHub ",
		ExternalURL: " http://github.com/Phixsura/attune/issues/228 ",
		ExternalKey: " Phixsura/attune#228 ",
		Title:       " Bidirectional sync ",
		Status:      " open ",
	})
	if err != nil {
		t.Fatalf("normalizeIssueInput(http) error = %v", err)
	}
	if got.Provider != "github" ||
		got.ExternalURL != "http://github.com/Phixsura/attune/issues/228" ||
		got.ExternalKey != "Phixsura/attune#228" ||
		got.Title != "Bidirectional sync" ||
		got.Status != "open" {
		t.Fatalf("normalizeIssueInput(http) = %+v, want trimmed issue fields", got)
	}

	cases := []struct {
		name string
		in   LinkIssueInput
		want error
	}{
		{
			name: "missing tenant",
			in: LinkIssueInput{
				RequestID:   requestID,
				Provider:    "github",
				ExternalURL: "https://github.com/Phixsura/attune/issues/228",
			},
			want: ErrValidation,
		},
		{
			name: "missing request id",
			in: LinkIssueInput{
				TenantID:    "tenant-a",
				Provider:    "github",
				ExternalURL: "https://github.com/Phixsura/attune/issues/228",
			},
			want: ErrValidation,
		},
		{
			name: "missing url host",
			in: LinkIssueInput{
				TenantID:    "tenant-a",
				RequestID:   requestID,
				Provider:    "github",
				ExternalURL: "https:///issues/228",
			},
			want: ErrInvalidIssueURL,
		},
		{
			name: "long url",
			in: LinkIssueInput{
				TenantID:    "tenant-a",
				RequestID:   requestID,
				Provider:    "other",
				ExternalURL: "https://tracker.example.com/" + strings.Repeat("x", 2049),
				ExternalKey: "tracker-1",
			},
			want: ErrValidation,
		},
		{
			name: "long title",
			in: LinkIssueInput{
				TenantID:    "tenant-a",
				RequestID:   requestID,
				Provider:    "other",
				ExternalURL: "https://tracker.example.com/items/1",
				ExternalKey: "tracker-1",
				Title:       strings.Repeat("x", 501),
			},
			want: ErrValidation,
		},
		{
			name: "long status",
			in: LinkIssueInput{
				TenantID:    "tenant-a",
				RequestID:   requestID,
				Provider:    "other",
				ExternalURL: "https://tracker.example.com/items/1",
				ExternalKey: "tracker-1",
				Status:      strings.Repeat("x", 121),
			},
			want: ErrValidation,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizeIssueInput(tc.in); !errors.Is(err, tc.want) {
				t.Fatalf("normalizeIssueInput() error = %v, want %v", err, tc.want)
			}
		})
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

func TestNormalizeUpdateRejectsInvalidPatchFields(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	for name, in := range map[string]UpdateInput{
		"empty title": {
			TenantID: "tenant-a",
			ID:       requestID,
			Title:    ptrext.Of("  "),
		},
		"long title": {
			TenantID: "tenant-a",
			ID:       requestID,
			Title:    ptrext.Of(strings.Repeat("x", 201)),
		},
		"long description": {
			TenantID:    "tenant-a",
			ID:          requestID,
			Description: ptrext.Of(strings.Repeat("x", 10001)),
		},
		"bad status": {
			TenantID: "tenant-a",
			ID:       requestID,
			Status:   ptrext.Of(repo.Status("bad")),
		},
		"bad priority": {
			TenantID: "tenant-a",
			ID:       requestID,
			Priority: ptrext.Of(repo.Priority("bad")),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeUpdate(in); !errors.Is(err, ErrValidation) {
				t.Fatalf("normalizeUpdate() error = %v, want ErrValidation", err)
			}
		})
	}
}

func TestNormalizeCreateDefaultsAndValidation(t *testing.T) {
	got, err := normalizeCreate(CreateInput{
		TenantID:       "tenant-a",
		Title:          "  Export bundles  ",
		Description:    "  CSV exports  ",
		IdempotencyKey: " create_key ",
	})
	if err != nil {
		t.Fatalf("normalizeCreate() error = %v", err)
	}
	if got.Title != "Export bundles" || got.Description != "CSV exports" {
		t.Fatalf("normalizeCreate() = %+v, want trimmed text", got)
	}
	if got.Status != repo.StatusOpen || got.Priority != repo.PriorityNone {
		t.Fatalf("normalizeCreate() defaults = (%q, %q), want open/none", got.Status, got.Priority)
	}

	for name, in := range map[string]CreateInput{
		"missing tenant": {Title: "title", IdempotencyKey: "valid_key"},
		"missing title":  {TenantID: "tenant-a", IdempotencyKey: "valid_key"},
		"long title":     {TenantID: "tenant-a", Title: strings.Repeat("x", 201), IdempotencyKey: "valid_key"},
		"bad key":        {TenantID: "tenant-a", Title: "title", IdempotencyKey: "short"},
		"bad status":     {TenantID: "tenant-a", Title: "title", Status: repo.Status("bad"), IdempotencyKey: "valid_key"},
		"bad priority":   {TenantID: "tenant-a", Title: "title", Priority: repo.Priority("bad"), IdempotencyKey: "valid_key"},
		"long desc":      {TenantID: "tenant-a", Title: "title", Description: strings.Repeat("x", 10001), IdempotencyKey: "valid_key"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeCreate(in); !errors.Is(err, ErrValidation) {
				t.Fatalf("normalizeCreate() error = %v, want ErrValidation", err)
			}
		})
	}
}

func TestNormalizeLinkFeedbackDefaultsAndValidation(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	link, err := normalizeLinkFeedback(LinkFeedbackInput{
		TenantID:   "tenant-a",
		RequestID:  requestID,
		FeedbackID: 42,
		Note:       "  renewal blocker  ",
	})
	if err != nil {
		t.Fatalf("normalizeLinkFeedback() error = %v", err)
	}
	if link.Importance != repo.ImportanceNormal || link.Note != "renewal blocker" {
		t.Fatalf("normalizeLinkFeedback() = %+v, want default importance and trimmed note", link)
	}
	if _, err := normalizeLinkFeedback(LinkFeedbackInput{TenantID: "tenant-a", RequestID: requestID, FeedbackID: 42, Importance: repo.Importance("bad")}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeLinkFeedback(bad importance) error = %v, want ErrValidation", err)
	}
}

func TestNormalizeNoteAndListDefaults(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	note, err := normalizeNote(NoteInput{TenantID: "tenant-a", RequestID: requestID, Body: "  Coordinate rollout  "})
	if err != nil {
		t.Fatalf("normalizeNote() error = %v", err)
	}
	if note.Body != "Coordinate rollout" {
		t.Fatalf("normalizeNote().Body = %q, want trimmed body", note.Body)
	}
	if _, err := normalizeNote(NoteInput{TenantID: "tenant-a", RequestID: requestID}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeNote(empty) error = %v, want ErrValidation", err)
	}
	if _, err := normalizeNote(NoteInput{RequestID: requestID, Body: "note"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeNote(missing tenant) error = %v, want ErrValidation", err)
	}
	if _, err := normalizeNote(NoteInput{TenantID: "tenant-a", Body: "note"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeNote(missing request) error = %v, want ErrValidation", err)
	}
	if _, err := normalizeNote(NoteInput{
		TenantID:  "tenant-a",
		RequestID: requestID,
		Body:      strings.Repeat("x", 5001),
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeNote(long body) error = %v, want ErrValidation", err)
	}

	if defaultVisibility("") != repo.VisibilityActive || defaultSort("") != repo.SortUpdatedAt || defaultDirection("") != repo.DirectionDesc {
		t.Fatal("default list helpers did not return active/updated_at/desc")
	}
	if defaultVisibility(repo.VisibilityAll) != repo.VisibilityAll || defaultSort(repo.SortDecisionScore) != repo.SortDecisionScore || defaultDirection(repo.DirectionAsc) != repo.DirectionAsc {
		t.Fatal("default list helpers did not preserve explicit values")
	}
	if validImportance(repo.Importance("bad")) || validIssueSyncState(repo.IssueSyncState("bad")) {
		t.Fatal("validators accepted invalid enum values")
	}
}

func TestNormalizeScoringSettingsPatchAndValidation(t *testing.T) {
	current := repo.DefaultScoringSettings("tenant-a")
	feedbackWeight := 7
	revenueCentsPerPoint := int64(250000)
	normalized, err := normalizeScoringSettings(ScoringSettingsInput{
		TenantID:             " tenant-a ",
		FeedbackWeight:       ptrext.Of(feedbackWeight),
		RevenueCentsPerPoint: ptrext.Of(revenueCentsPerPoint),
	}, current)
	if err != nil {
		t.Fatalf("normalizeScoringSettings() error = %v", err)
	}
	if normalized.TenantID != "tenant-a" ||
		normalized.FeedbackWeight != feedbackWeight ||
		normalized.PriorityUrgentWeight != current.PriorityUrgentWeight ||
		normalized.RevenueCentsPerPoint != revenueCentsPerPoint ||
		normalized.ActorID != "system" {
		t.Fatalf("normalizeScoringSettings() = %+v, want patch over defaults", normalized)
	}

	negative := -1
	if _, err := normalizeScoringSettings(ScoringSettingsInput{
		TenantID:       "tenant-a",
		FeedbackWeight: ptrext.Of(negative),
	}, current); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeScoringSettings(negative) error = %v, want ErrValidation", err)
	}
	zeroRevenue := int64(0)
	if _, err := normalizeScoringSettings(ScoringSettingsInput{
		TenantID:             "tenant-a",
		RevenueCentsPerPoint: ptrext.Of(zeroRevenue),
	}, current); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeScoringSettings(zero revenue divisor) error = %v, want ErrValidation", err)
	}
	if _, err := normalizeScoringSettings(ScoringSettingsInput{}, current); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeScoringSettings(empty tenant) error = %v, want ErrValidation", err)
	}

	audit := scoringSettingsAuditFields(repo.ScoringSettings{FeedbackWeight: 7, RevenueCentsPerPoint: 250000})
	if audit["feedback_weight"] != 7 || audit["revenue_cents_per_point"] != int64(250000) {
		t.Fatalf("scoringSettingsAuditFields() = %+v, want scoring fields", audit)
	}
}

func TestNormalizeAccountProfiles(t *testing.T) {
	revenue := int64(12345)
	profile, err := normalizeAccountProfile(" acme ", AccountProfileInput{
		RevenueCents:    ptrext.Of(revenue),
		RevenueCurrency: " usd ",
		Tier:            " enterprise ",
		SizeSegment:     " mid_market ",
		LifecycleStatus: " active ",
		CRMProvider:     " salesforce ",
		CRMExternalID:   " 001 ",
	})
	if err != nil {
		t.Fatalf("normalizeAccountProfile() error = %v", err)
	}
	if profile.RevenueCurrency != "USD" || profile.Tier != "enterprise" || accountRevenueCents(profile) != revenue {
		t.Fatalf("normalizeAccountProfile() = %+v, want normalized profile", profile)
	}
	if accountRevenueCents(AccountProfileInput{}) != 0 {
		t.Fatal("accountRevenueCents(nil) != 0")
	}
	if !hasAccountProfileInput(profile) {
		t.Fatal("hasAccountProfileInput() = false, want true")
	}
	if _, err := normalizeAccountProfile("", AccountProfileInput{Tier: "enterprise"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeAccountProfile(no account) error = %v, want ErrValidation", err)
	}
	if _, err := normalizeAccountProfile("acme", AccountProfileInput{RevenueCents: ptrext.Of(int64(-1))}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeAccountProfile(negative revenue) error = %v, want ErrValidation", err)
	}
	if _, err := normalizeAccountProfile("acme", AccountProfileInput{RevenueCurrency: "US"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeAccountProfile(bad currency) error = %v, want ErrValidation", err)
	}
	if _, err := normalizeAccountProfile("acme", AccountProfileInput{CRMExternalID: strings.Repeat("x", 513)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeAccountProfile(long crm id) error = %v, want ErrValidation", err)
	}
}

func TestNormalizeIssueSync(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	linkID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	got, err := normalizeIssueSync(IssueSyncInput{
		TenantID:               "tenant-a",
		RequestID:              requestID,
		IssueLinkID:            linkID,
		Status:                 " open ",
		ExternalStatusCategory: " in_progress ",
		ExternalAssignee:       " ops@example.com ",
		ExternalUpdatedAt:      "2026-07-07T00:00:00Z",
		SyncError:              " rate limited ",
	})
	if err != nil {
		t.Fatalf("normalizeIssueSync() error = %v", err)
	}
	if got.SyncState != repo.IssueSyncStateSynced || got.Status != "open" || got.SyncError != "rate limited" {
		t.Fatalf("normalizeIssueSync() = %+v, want defaults and trimmed fields", got)
	}
	if _, err := normalizeIssueSync(IssueSyncInput{TenantID: "tenant-a", RequestID: requestID, IssueLinkID: linkID, SyncState: repo.IssueSyncState("bad")}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeIssueSync(bad state) error = %v, want ErrValidation", err)
	}
	if _, err := normalizeIssueSync(IssueSyncInput{TenantID: "tenant-a", RequestID: requestID, IssueLinkID: linkID, ExternalUpdatedAt: "yesterday"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeIssueSync(bad time) error = %v, want ErrValidation", err)
	}
	if _, err := normalizeIssueSync(IssueSyncInput{TenantID: "tenant-a", RequestID: requestID, IssueLinkID: linkID, SyncError: strings.Repeat("x", 2001)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeIssueSync(long sync error) error = %v, want ErrValidation", err)
	}

	for _, tc := range []struct {
		name string
		in   IssueSyncInput
	}{
		{
			name: "missing tenant",
			in: IssueSyncInput{
				RequestID:   requestID,
				IssueLinkID: linkID,
			},
		},
		{
			name: "missing request",
			in: IssueSyncInput{
				TenantID:    "tenant-a",
				IssueLinkID: linkID,
			},
		},
		{
			name: "missing issue link",
			in: IssueSyncInput{
				TenantID:  "tenant-a",
				RequestID: requestID,
			},
		},
		{
			name: "long status",
			in: IssueSyncInput{
				TenantID:    "tenant-a",
				RequestID:   requestID,
				IssueLinkID: linkID,
				Status:      strings.Repeat("x", 121),
			},
		},
		{
			name: "long status category",
			in: IssueSyncInput{
				TenantID:               "tenant-a",
				RequestID:              requestID,
				IssueLinkID:            linkID,
				ExternalStatusCategory: strings.Repeat("x", 121),
			},
		},
		{
			name: "long assignee",
			in: IssueSyncInput{
				TenantID:         "tenant-a",
				RequestID:        requestID,
				IssueLinkID:      linkID,
				ExternalAssignee: strings.Repeat("x", 501),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizeIssueSync(tc.in); !errors.Is(err, ErrValidation) {
				t.Fatalf("normalizeIssueSync() error = %v, want ErrValidation", err)
			}
		})
	}
}

func TestIssueKeyDerivation(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	for _, tc := range []LinkIssueInput{
		{TenantID: "tenant-a", RequestID: requestID, Provider: "jira", ExternalURL: "https://jira.example.com/browse/ENG-123"},
		{TenantID: "tenant-a", RequestID: requestID, Provider: "linear", ExternalURL: "https://linear.app/team/issue/ENG-456/title"},
		{TenantID: "tenant-a", RequestID: requestID, Provider: "other", ExternalURL: "https://tracker.example.com/items/1", ExternalKey: "custom-1"},
	} {
		if got, err := normalizeIssueInput(tc); err != nil || got.ExternalKey == "" {
			t.Fatalf("normalizeIssueInput(%s) = %+v, %v; want external key", tc.Provider, got, err)
		}
	}
	if _, err := normalizeIssueInput(LinkIssueInput{TenantID: "tenant-a", RequestID: requestID, Provider: "github", ExternalURL: "ftp://github.com/x/y/issues/1"}); !errors.Is(err, ErrInvalidIssueURL) {
		t.Fatalf("normalizeIssueInput(bad url) error = %v, want ErrInvalidIssueURL", err)
	}
	if _, err := normalizeIssueInput(LinkIssueInput{TenantID: "tenant-a", RequestID: requestID, Provider: "other", ExternalURL: "https://tracker.example.com/items/1", ExternalKey: strings.Repeat("x", 513)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeIssueInput(long key) error = %v, want ErrValidation", err)
	}
	if got := deriveExternalKey("github", mustParseCustomerRequestIssueURL(t, "https://github.com/Phixsura/attune/pull/1")); got != "https://github.com/Phixsura/attune/pull/1" {
		t.Fatalf("deriveExternalKey(non-issue github) = %q", got)
	}
}

func TestGitHubIssueCreateHelpers(t *testing.T) {
	if hasGitHubIssueLink(nil) {
		t.Fatal("hasGitHubIssueLink(nil) = true, want false")
	}
	detail := &Detail{Request: repo.Detail{
		IssueLinks: []repo.IssueLink{
			{Provider: "jira"},
			{Provider: "GitHub"},
		},
	}}
	if !hasGitHubIssueLink(detail) {
		t.Fatal("hasGitHubIssueLink() = false, want true for GitHub provider")
	}
	if hasGitHubIssueLink(&Detail{Request: repo.Detail{IssueLinks: []repo.IssueLink{{Provider: "linear"}}}}) {
		t.Fatal("hasGitHubIssueLink(linear) = true, want false")
	}

	for _, err := range []error{externalsyncrepo.ErrMappingNotFound, externalsyncrepo.ErrConflict} {
		if got := mapIssueCreateRunError(err); !errors.Is(got, repo.ErrConflict) {
			t.Fatalf("mapIssueCreateRunError(%v) = %v, want repo.ErrConflict", err, got)
		}
	}
	if got := mapIssueCreateRunError(externalsyncrepo.ErrLocalObjectNotFound); !errors.Is(got, repo.ErrNotFound) {
		t.Fatalf("mapIssueCreateRunError(local object not found) = %v, want repo.ErrNotFound", got)
	}
	other := errors.New("boom")
	if got := mapIssueCreateRunError(other); !errors.Is(got, other) {
		t.Fatalf("mapIssueCreateRunError(other) = %v, want original error", got)
	}
}

func TestAuditMetadataHelpers(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ownerID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	linkID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	summary := repo.Summary{
		ID:                      requestID,
		TenantID:                "tenant-a",
		DisplayID:               "CR-7",
		Title:                   "Export bundles",
		Status:                  repo.StatusOpen,
		Priority:                repo.PriorityHigh,
		OwnerMemberID:           ptrext.Of(ownerID),
		SupportingFeedbackCount: 2,
		CustomerCount:           1,
		AccountCount:            1,
		VoteCount:               3,
		RevenueImpactCents:      12345,
		DeliveryHealth:          repo.DeliveryHealthSynced,
	}
	if got := createAuditSummary("customer_request.create", summary); got != "Created customer request CR-7" {
		t.Fatalf("createAuditSummary() = %q, want display id summary", got)
	}
	if got := createAuditSummary("customer_request.promote_feedback", repo.Summary{}); got != "Promoted feedback to customer request" {
		t.Fatalf("createAuditSummary(promote) = %q", got)
	}
	if got := createAuditSummary("customer_request.create", repo.Summary{}); got != "Created customer request" {
		t.Fatalf("createAuditSummary(create without display id) = %q", got)
	}
	if createAuditMetadata(summary, "idempotency-key")["owner_member_id"] != ownerID.String() {
		t.Fatalf("createAuditMetadata() = %+v, want owner member id", createAuditMetadata(summary, "idempotency-key"))
	}
	if updateAuditBeforeAfter(summary, repo.Summary{ID: requestID, Title: "Renamed", Status: repo.StatusPlanned, Priority: repo.PriorityUrgent})["title_changed"] != true {
		t.Fatal("updateAuditBeforeAfter() did not detect title change")
	}
	if issueAuditMetadata(requestID, repo.IssueLink{ID: linkID, Provider: "github", ExternalKey: "key", ExternalURL: "https://github.com/o/r/issues/1"})["url_host"] != "github.com" {
		t.Fatal("issueAuditMetadata() did not parse host")
	}
	if customerAuditMetadata(requestID, repo.CustomerLink{ID: linkID, SubjectKey: "subject", AccountKey: "acme", Note: "note"})["account_key_set"] != true {
		t.Fatal("customerAuditMetadata() did not mark account key")
	}
	if voteAuditMetadata(requestID, repo.Vote{ID: linkID, Weight: 3, Note: "note"})["weight"] != 3 {
		t.Fatal("voteAuditMetadata() did not include weight")
	}
	if noteAuditMetadata(requestID, repo.Note{ID: linkID, Body: "body"})["body_length"] != 4 {
		t.Fatal("noteAuditMetadata() did not include body length")
	}
	if uuidPtrString(ptrext.Of(ownerID)) != ownerID.String() || uuidPtrString(nil) != "" {
		t.Fatal("uuidPtrString() returned unexpected values")
	}
}

func TestIdempotencyPayloadAndTimeHelpers(t *testing.T) {
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ownerID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	linkID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	if _, err := hashPayload("create", createIdempotencyPayload(CreateInput{TenantID: "tenant-a", Title: "Export", OwnerMemberID: ptrext.Of(ownerID)})); err != nil {
		t.Fatalf("hashPayload(create) error = %v", err)
	}
	if _, err := hashPayload("promote", promoteIdempotencyPayload(PromoteInput{TenantID: "tenant-a", FeedbackIDs: []int64{42}, OwnerMemberID: ptrext.Of(ownerID)})); err != nil {
		t.Fatalf("hashPayload(promote) error = %v", err)
	}
	if _, err := hashPayload("merge", mergeIdempotencyPayload(MergeInput{TenantID: "tenant-a", SourceID: requestID, TargetID: linkID})); err != nil {
		t.Fatalf("hashPayload(merge) error = %v", err)
	}
	if parseOptionalTime(" ") != nil {
		t.Fatal("parseOptionalTime(blank) != nil")
	}
	if parseOptionalTime("bad") != nil {
		t.Fatal("parseOptionalTime(bad) != nil")
	}
	if parseOptionalTime(now.Format(time.RFC3339)) == nil {
		t.Fatal("parseOptionalTime(valid) = nil")
	}
	if _, err := hashPayload("bad", map[string]any{"fn": func() {}}); err == nil {
		t.Fatal("hashPayload(unmarshalable) error = nil, want error")
	}
}

func TestAcquireIdempotencyCompleted(t *testing.T) {
	ctx := context.Background()
	detail := ptrext.Of(Detail{Request: repo.Detail{Summary: repo.Summary{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")}}})
	body, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("Marshal detail: %v", err)
	}

	store := &fakeIdempotencyStore{record: &idempotency.Key{Status: idempotency.StatusCompleted, ResponseBody: body}}
	service := New(nil, store, nil)
	cached, acquired, err := service.acquireIdempotency(ctx, "tenant-a", "key", "create", map[string]string{"title": "Export"})
	if err != nil {
		t.Fatalf("acquireIdempotency(completed) error = %v", err)
	}
	if acquired || cached == nil || cached.Request.Summary.ID != detail.Request.Summary.ID {
		t.Fatalf("acquireIdempotency(completed) = cached:%+v acquired:%v, want cached detail", cached, acquired)
	}
}

func TestAcquireIdempotencyStates(t *testing.T) {
	ctx := context.Background()
	service := New(nil, nil, nil)
	if cached, acquired, err := service.acquireIdempotency(ctx, "tenant-a", "key", "create", nil); cached != nil || acquired || err != nil {
		t.Fatalf("acquireIdempotency(no store) = cached:%+v acquired:%v err:%v", cached, acquired, err)
	}

	store := &fakeIdempotencyStore{acquired: true}
	service = New(nil, store, nil)
	cached, acquired, err := service.acquireIdempotency(ctx, "tenant-a", "key", "create", map[string]string{"title": "Export"})
	if err != nil || cached != nil || !acquired {
		t.Fatalf("acquireIdempotency(acquired) = cached:%+v acquired:%v err:%v", cached, acquired, err)
	}

	store = &fakeIdempotencyStore{record: &idempotency.Key{Status: idempotency.StatusPending}}
	service = New(nil, store, nil)
	if _, _, err := service.acquireIdempotency(ctx, "tenant-a", "key", "create", map[string]string{"title": "Export"}); !errors.Is(err, ErrRequestInProgress) {
		t.Fatalf("acquireIdempotency(pending) error = %v, want ErrRequestInProgress", err)
	}

	store = &fakeIdempotencyStore{acquireErr: idempotency.ErrHashMismatch}
	service = New(nil, store, nil)
	if _, _, err := service.acquireIdempotency(ctx, "tenant-a", "key", "create", map[string]string{"title": "Export"}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("acquireIdempotency(hash mismatch) error = %v, want ErrIdempotencyConflict", err)
	}

	store = &fakeIdempotencyStore{acquireErr: idempotency.ErrExpired, reacquire: true}
	service = New(nil, store, nil)
	if _, acquired, err := service.acquireIdempotency(ctx, "tenant-a", "key", "create", map[string]string{"title": "Export"}); err != nil || !acquired || !store.deleted {
		t.Fatalf("acquireIdempotency(expired) acquired:%v deleted:%v err:%v", acquired, store.deleted, err)
	}

	store = &fakeIdempotencyStore{acquireErr: idempotency.ErrExpired, deleteErr: errors.New("delete failed")}
	service = New(nil, store, nil)
	if _, _, err := service.acquireIdempotency(ctx, "tenant-a", "key", "create", nil); err == nil || !strings.Contains(err.Error(), "delete failed") {
		t.Fatalf("acquireIdempotency(expired delete error) error = %v", err)
	}

	store = &fakeIdempotencyStore{record: &idempotency.Key{Status: idempotency.StatusCompleted, ResponseBody: []byte("{")}}
	service = New(nil, store, nil)
	if _, _, err := service.acquireIdempotency(ctx, "tenant-a", "key", "create", nil); err == nil {
		t.Fatal("acquireIdempotency(bad cached json) error = nil, want error")
	}
}

func TestCompleteIdempotencyBranches(t *testing.T) {
	ctx := context.Background()
	detail := ptrext.Of(Detail{Request: repo.Detail{Summary: repo.Summary{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")}}})
	service := New(nil, nil, nil)
	if got, err := service.completeIdempotency(ctx, "tenant-a", "key", false, detail, nil); err != nil || got != detail {
		t.Fatalf("completeIdempotency(no store) = %+v, %v", got, err)
	}

	store := &fakeIdempotencyStore{}
	service = New(nil, store, nil)
	if _, err := service.completeIdempotency(ctx, "tenant-a", "key", true, detail, nil); err != nil || !store.completed {
		t.Fatalf("completeIdempotency(success) completed:%v err:%v", store.completed, err)
	}
	store = &fakeIdempotencyStore{}
	service = New(nil, store, nil)
	if _, err := service.completeIdempotency(ctx, "tenant-a", "key", true, detail, errors.New("operation failed")); err == nil || !store.failed {
		t.Fatalf("completeIdempotency(error) failed:%v err:%v", store.failed, err)
	}
	store = &fakeIdempotencyStore{completeErr: errors.New("complete failed")}
	service = New(nil, store, nil)
	if _, err := service.completeIdempotency(ctx, "tenant-a", "key", true, detail, nil); err == nil || !strings.Contains(err.Error(), "complete failed") {
		t.Fatalf("completeIdempotency(complete error) error = %v", err)
	}
}

func TestServiceMethodsRejectInvalidInputsBeforeRepoUse(t *testing.T) {
	ctx := context.Background()
	service := New(nil, nil, nil)
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	linkID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	cases := map[string]func() error{
		"list": func() error {
			_, err := service.List(ctx, ListInput{})
			return err
		},
		"get scoring settings": func() error {
			_, err := service.GetScoringSettings(ctx, " ")
			return err
		},
		"update scoring settings": func() error {
			_, err := service.UpdateScoringSettings(ctx, ScoringSettingsInput{})
			return err
		},
		"create": func() error {
			_, err := service.Create(ctx, CreateInput{})
			return err
		},
		"update": func() error {
			_, err := service.Update(ctx, UpdateInput{})
			return err
		},
		"promote feedback": func() error {
			_, err := service.PromoteFeedback(ctx, PromoteInput{})
			return err
		},
		"link feedback": func() error {
			_, err := service.LinkFeedback(ctx, LinkFeedbackInput{})
			return err
		},
		"unlink feedback": func() error {
			_, err := service.UnlinkFeedback(ctx, "", requestID, 1, repoActor())
			return err
		},
		"link customer": func() error {
			_, err := service.LinkCustomer(ctx, LinkCustomerInput{TenantID: "tenant-a", RequestID: requestID})
			return err
		},
		"unlink customer": func() error {
			_, err := service.UnlinkCustomer(ctx, "tenant-a", uuid.Nil, linkID, repoActor())
			return err
		},
		"add vote": func() error {
			_, err := service.AddVote(ctx, VoteInput{TenantID: "tenant-a", RequestID: requestID, Weight: 101})
			return err
		},
		"remove vote": func() error {
			_, err := service.RemoveVote(ctx, "tenant-a", requestID, uuid.Nil, repoActor())
			return err
		},
		"add note": func() error {
			_, err := service.AddNote(ctx, NoteInput{TenantID: "tenant-a", RequestID: requestID})
			return err
		},
		"delete note": func() error {
			_, err := service.DeleteNote(ctx, "tenant-a", requestID, uuid.Nil, repoActor())
			return err
		},
		"merge": func() error {
			_, err := service.Merge(ctx, MergeInput{TenantID: "tenant-a", SourceID: requestID, TargetID: requestID, IdempotencyKey: "merge_key"})
			return err
		},
		"link issue": func() error {
			_, err := service.LinkIssue(ctx, LinkIssueInput{TenantID: "tenant-a", RequestID: requestID, Provider: "github", ExternalURL: "ftp://example.test/repo/issues/1"})
			return err
		},
		"unlink issue": func() error {
			_, err := service.UnlinkIssue(ctx, "tenant-a", requestID, uuid.Nil, repoActor())
			return err
		},
		"record issue sync": func() error {
			_, err := service.RecordIssueSync(ctx, IssueSyncInput{TenantID: "tenant-a", RequestID: requestID, IssueLinkID: linkID, SyncState: repo.IssueSyncState("bad")})
			return err
		},
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if !errors.Is(err, ErrValidation) && !errors.Is(err, ErrInvalidIssueURL) {
				t.Fatalf("%s error = %v, want validation error", name, err)
			}
		})
	}
}

func mustParseCustomerRequestIssueURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	return parsed
}

func repoActor() auditlogsvc.Actor {
	return auditlogsvc.Actor{ID: "actor-1"}
}

type recordingIssueCreateRunStore struct {
	createInputs []externalsyncrepo.CustomerRequestIssueCreateRunInput
	createResult *externalsyncrepo.CustomerRequestIssueCreateRunResult
	createErr    error
	pullInputs   []externalsyncrepo.CustomerRequestIssuePullRunInput
	pullResult   *externalsyncrepo.CustomerRequestIssuePullRunResult
	pullErr      error
}

func (s *recordingIssueCreateRunStore) CreateCustomerRequestIssueRun(
	_ context.Context,
	in externalsyncrepo.CustomerRequestIssueCreateRunInput,
) (*externalsyncrepo.CustomerRequestIssueCreateRunResult, error) {
	s.createInputs = append(s.createInputs, in)
	return s.createResult, s.createErr
}

func (s *recordingIssueCreateRunStore) CreateCustomerRequestIssuePullRun(
	_ context.Context,
	in externalsyncrepo.CustomerRequestIssuePullRunInput,
) (*externalsyncrepo.CustomerRequestIssuePullRunResult, error) {
	s.pullInputs = append(s.pullInputs, in)
	return s.pullResult, s.pullErr
}

type fakeIdempotencyStore struct {
	record      *idempotency.Key
	acquired    bool
	acquireErr  error
	reacquire   bool
	deleteErr   error
	completeErr error
	deleted     bool
	completed   bool
	failed      bool
}

func (f *fakeIdempotencyStore) Acquire(_ context.Context, tenantID, key string, requestHash []byte, ttl time.Duration) (*idempotency.Key, bool, error) {
	if f.acquireErr != nil {
		err := f.acquireErr
		f.acquireErr = nil
		if errors.Is(err, idempotency.ErrExpired) && f.reacquire {
			return nil, false, err
		}
		return nil, false, err
	}
	if f.reacquire && f.deleted {
		return &idempotency.Key{TenantID: tenantID, Key: key, RequestHash: requestHash, Status: idempotency.StatusPending}, true, nil
	}
	return f.record, f.acquired, nil
}

func (f *fakeIdempotencyStore) Complete(_ context.Context, _ string, _ string, _ int, _ []byte) error {
	f.completed = true
	return f.completeErr
}

func (f *fakeIdempotencyStore) Fail(_ context.Context, _ string, _ string) error {
	f.failed = true
	return nil
}

func (f *fakeIdempotencyStore) Get(_ context.Context, _ string, _ string) (*idempotency.Key, error) {
	return nil, idempotency.ErrNotFound
}

func (f *fakeIdempotencyStore) Delete(_ context.Context, _ string, _ string) error {
	f.deleted = true
	return f.deleteErr
}

func (f *fakeIdempotencyStore) CleanupExpired(context.Context) (int64, error) {
	return 0, nil
}
