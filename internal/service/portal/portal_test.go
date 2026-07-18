// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	publicvisibilityrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	tenantrepo "github.com/Phixsura/attune/internal/repo/tenant"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

func TestGetSubmissionConfigReturnsEffectiveDefaultsAndTenantName(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakePortalRepo{
		tenantID: "tenant-1",
		policy: publicvisibilityrepo.Policy{
			PortalAccessMode:      publicvisibilityrepo.AccessModePublic,
			SubmissionWriteMode:   publicvisibilityrepo.WriteModeIdentified,
			SubmitterIdentityMode: publicvisibilityrepo.IdentityModeDisplayName,
		},
	})
	svc := New(repo, repo, nil, ptrext.Of(fakeTenantReader{name: "Acme Co"}), nil)

	cfg, err := svc.GetSubmissionConfig(context.Background(), " acme ")
	if err != nil {
		t.Fatalf("GetSubmissionConfig() error = %v", err)
	}
	if cfg.TenantID != "tenant-1" || cfg.TenantSlug != "acme" || cfg.TenantName != "Acme Co" {
		t.Fatalf("config = %#v, want tenant metadata normalized", cfg)
	}
	if cfg.Form.Headline != "Send feedback" || cfg.Form.Description != "Share bugs, ideas, or anything blocking your work." {
		t.Fatalf("config form = %#v, want default portal form copy", cfg.Form)
	}
	if !cfg.CanSubmit {
		t.Fatal("CanSubmit = false, want true for enabled write mode")
	}
	if cfg.PortalAccessMode != publicvisibilityrepo.AccessModePublic || cfg.SubmissionWriteMode != publicvisibilityrepo.WriteModeIdentified {
		t.Fatalf("config policy modes = %#v, want public/identified", cfg)
	}

	hidden := ptrext.Of(fakePortalRepo{
		tenantID: "tenant-1",
		policy: publicvisibilityrepo.Policy{
			PortalAccessMode: publicvisibilityrepo.AccessModeAuthenticated,
		},
	})
	hiddenSvc := New(hidden, hidden, nil, nil, nil)
	if _, err := hiddenSvc.GetSubmissionConfig(context.Background(), "acme"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSubmissionConfig() hidden portal error = %v, want %v", err, ErrNotFound)
	}
}

func TestSubmitUsesIdempotentInsertAndCreatesAuditTrail(t *testing.T) {
	t.Parallel()

	tx := ptrext.Of(fakePortalTx{})
	repo := ptrext.Of(fakePortalRepo{
		tenantID: "tenant-1",
		tx:       tx,
		policy: publicvisibilityrepo.Policy{
			PortalAccessMode:      publicvisibilityrepo.AccessModePublic,
			SubmissionWriteMode:   publicvisibilityrepo.WriteModeIdentified,
			SubmitterIdentityMode: publicvisibilityrepo.IdentityModeDisplayName,
			DefaultRequestState:   publicvisibilityrepo.ModerationStateApproved,
			PortalSubmissionForm: publicvisibilityrepo.PortalSubmissionForm{
				Fields: []publicvisibilityrepo.PortalSubmissionField{
					{
						Key:      "severity",
						Label:    "Severity",
						Kind:     publicvisibilityrepo.PortalSubmissionFieldKindSelect,
						Required: true,
						Options:  []string{"low", "high"},
					},
					{
						Key:     "components",
						Label:   "Components",
						Kind:    publicvisibilityrepo.PortalSubmissionFieldKindMultiSelect,
						Options: []string{"ui", "api"},
					},
					{
						Key:   "consent",
						Label: "Consent",
						Kind:  publicvisibilityrepo.PortalSubmissionFieldKindBoolean,
					},
				},
			},
		},
	})
	feedback := ptrext.Of(fakeFeedbackInserter{idemID: 101})
	audit := ptrext.Of(fakeAuditRecorder{})
	svc := New(repo, repo, feedback, nil, audit)

	input := SubmitInput{
		TenantSlug:   " acme ",
		Kind:         " BUG ",
		Title:        "  Login fails  ",
		Details:      "  After SSO redirect  ",
		PageURL:      " https://app.example.com/login ",
		DisplayName:  " Ada Lovelace ",
		Organization: " Acme ",
		CustomFields: map[string]any{
			"severity":   " high ",
			"components": []any{"ui", " api "},
			"consent":    true,
		},
		IdempotencyKey: "retry-safe_123",
		UserAgent:      "PortalTest/1.0",
	}
	result, err := svc.Submit(context.Background(), input)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	assertIdempotentPortalSubmission(t, input, result, repo, feedback, audit, tx)
}

func assertIdempotentPortalSubmission(
	t *testing.T,
	input SubmitInput,
	result SubmitResult,
	repo *fakePortalRepo,
	feedback *fakeFeedbackInserter,
	audit *fakeAuditRecorder,
	tx *fakePortalTx,
) {
	t.Helper()
	if result.SubmissionID != "101" || result.Kind != "bug" || result.ModerationState != publicvisibilityrepo.ModerationStateApproved {
		t.Fatalf("result = %#v, want idempotent portal submission response", result)
	}
	if result.Acknowledgement != "Thanks. We will review your submission." {
		t.Fatalf("result acknowledgement = %q, want default acknowledgement", result.Acknowledgement)
	}
	if feedback.insertIdemCalls != 1 || feedback.insertCalls != 0 {
		t.Fatalf("feedback calls = insert:%d idempotent:%d, want idempotent insert only", feedback.insertCalls, feedback.insertIdemCalls)
	}
	if feedback.gotTenantID != "tenant-1" || feedback.gotUserID == "" || feedback.gotSubjectKey != "Ada Lovelace" || feedback.gotSubjectDisplay != "Ada Lovelace" {
		t.Fatalf("feedback identity fields = %#v, want normalized display name identity", feedback)
	}
	if feedback.gotSubjectHash != subjectkey.Hash("tenant-1", "Ada Lovelace") {
		t.Fatalf("subject hash = %q, want tenant-scoped subject hash", feedback.gotSubjectHash)
	}
	assertPortalSubmissionPayload(t, input, feedback)
	assertPortalSubmissionAudit(t, audit)
	assertPortalSubmissionTx(t, tx)
	if repo.gotSubject.TenantID != "tenant-1" || repo.gotSubject.Surface != publicvisibilityrepo.SurfacePortalSubmission || repo.gotSubject.SubjectID != "101" {
		t.Fatalf("moderation subject = %#v, want portal submission subject", repo.gotSubject)
	}
}

func assertPortalSubmissionPayload(t *testing.T, input SubmitInput, feedback *fakeFeedbackInserter) {
	t.Helper()
	if feedback.gotInput.IdempotencyKey != "retry-safe_123" || feedback.gotInput.Type != "bug" || feedback.gotInput.Source != "portal" {
		t.Fatalf("feedback input = %#v, want portal ingest shape", feedback.gotInput)
	}
	if got, want := feedback.gotInput.Content, "Login fails\n\nAfter SSO redirect"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if got, want := feedback.gotInput.SourceMeta, wantPortalSubmissionSourceMeta(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("source meta = %#v, want %#v", got, want)
	}
	if got, want := feedback.gotIdemHash, wantPortalSubmissionIdempotencyHash("tenant-1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("idempotency hash = %x, want %x", got, want)
	}
}

func assertPortalSubmissionAudit(t *testing.T, audit *fakeAuditRecorder) {
	t.Helper()
	if audit.calls != 1 {
		t.Fatalf("audit calls = %d, want 1", audit.calls)
	}
	if audit.event.Action != "portal_submission.create" || audit.event.Actor.Type != "portal" || audit.event.Actor.UserAgent != "PortalTest/1.0" {
		t.Fatalf("audit event = %#v, want portal submission audit", audit.event)
	}
	if got, want := audit.event.After, map[string]any{
		"kind":                 "bug",
		"title":                "Login fails",
		"state":                publicvisibilityrepo.ModerationStateApproved,
		"page_url":             "https://app.example.com/login",
		"private_contact_keys": []string{"display_name", "organization"},
		"custom_field_count":   3,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("audit after = %#v, want %#v", got, want)
	}
}

func assertPortalSubmissionTx(t *testing.T, tx *fakePortalTx) {
	t.Helper()
	if tx.commitCalls != 1 || tx.rollbackCalls != 1 {
		t.Fatalf("tx commit/rollback = %d/%d, want 1/1", tx.commitCalls, tx.rollbackCalls)
	}
}

func wantPortalSubmissionSourceMeta(input SubmitInput) map[string]any {
	return map[string]any{
		"portal_submission": map[string]any{
			"kind":     "bug",
			"title":    "Login fails",
			"details":  "After SSO redirect",
			"page_url": "https://app.example.com/login",
			"private_contact": map[string]any{
				"display_name": "Ada Lovelace",
				"organization": "Acme",
			},
			"custom_fields": map[string]any{
				"severity":   "high",
				"components": []string{"ui", "api"},
				"consent":    true,
			},
			"user_agent": input.UserAgent,
		},
	}
}

func wantPortalSubmissionIdempotencyHash(tenantID string) []byte {
	return hashPortalSubmission(
		tenantID,
		"bug",
		"Login fails",
		"After SSO redirect",
		"https://app.example.com/login",
		"Ada Lovelace",
		map[string]any{
			"display_name": "Ada Lovelace",
			"organization": "Acme",
		},
		map[string]any{
			"severity":   "high",
			"components": []string{"ui", "api"},
			"consent":    true,
		},
	)
}

func TestSubmitDedupedIdempotentReplaySkipsModerationSubject(t *testing.T) {
	t.Parallel()

	tx := ptrext.Of(fakePortalTx{})
	repo := ptrext.Of(fakePortalRepo{
		tenantID: "tenant-1",
		tx:       tx,
		policy: publicvisibilityrepo.Policy{
			PortalAccessMode:      publicvisibilityrepo.AccessModePublic,
			SubmissionWriteMode:   publicvisibilityrepo.WriteModeAnonymous,
			SubmitterIdentityMode: publicvisibilityrepo.IdentityModeAnonymous,
			DefaultRequestState:   publicvisibilityrepo.ModerationStatePending,
		},
	})
	feedback := ptrext.Of(fakeFeedbackInserter{
		idemID:      303,
		idemDeduped: true,
	})
	audit := ptrext.Of(fakeAuditRecorder{})
	svc := New(repo, repo, feedback, nil, audit)

	result, err := svc.Submit(context.Background(), SubmitInput{
		TenantSlug:     "acme",
		Kind:           "request",
		Title:          "Need help",
		Details:        "Please make retries safe",
		IdempotencyKey: "retry-safe_789",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if result.SubmissionID != "303" || result.ModerationState != publicvisibilityrepo.ModerationStatePending {
		t.Fatalf("result = %#v, want deduped replay response", result)
	}
	if feedback.insertIdemCalls != 1 || repo.createSubjectCalls != 0 || audit.calls != 0 {
		t.Fatalf("deduped replay should not create moderation subjects or audit rows, got feedback:%d subjects:%d audits:%d", feedback.insertIdemCalls, repo.createSubjectCalls, audit.calls)
	}
	if tx.commitCalls != 1 || tx.rollbackCalls != 1 {
		t.Fatalf("tx commit/rollback = %d/%d, want 1/1", tx.commitCalls, tx.rollbackCalls)
	}
}

func TestSubmitUsesPlainInsertWithoutIdempotencyKey(t *testing.T) {
	t.Parallel()

	tx := ptrext.Of(fakePortalTx{})
	repo := ptrext.Of(fakePortalRepo{
		tenantID: "tenant-1",
		tx:       tx,
		policy: publicvisibilityrepo.Policy{
			PortalAccessMode:      publicvisibilityrepo.AccessModePublic,
			SubmissionWriteMode:   publicvisibilityrepo.WriteModeAnonymous,
			SubmitterIdentityMode: publicvisibilityrepo.IdentityModeAnonymous,
			DefaultRequestState:   publicvisibilityrepo.ModerationStatePending,
		},
	})
	feedback := ptrext.Of(fakeFeedbackInserter{insertID: 202})
	svc := New(repo, repo, feedback, nil, nil)

	result, err := svc.Submit(context.Background(), SubmitInput{
		TenantSlug: "acme",
		Kind:       "general",
		Title:      "Portal idea",
		Details:    "Add keyboard shortcuts",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if result.SubmissionID != "202" || result.Kind != "general" || result.ModerationState != publicvisibilityrepo.ModerationStatePending {
		t.Fatalf("result = %#v, want plain insert response", result)
	}
	if feedback.insertCalls != 1 || feedback.insertIdemCalls != 0 {
		t.Fatalf("feedback calls = insert:%d idempotent:%d, want plain insert only", feedback.insertCalls, feedback.insertIdemCalls)
	}
	if feedback.gotInput.IdempotencyKey != "" {
		t.Fatalf("idempotency key = %q, want empty", feedback.gotInput.IdempotencyKey)
	}
	if tx.commitCalls != 1 || tx.rollbackCalls != 1 {
		t.Fatalf("tx commit/rollback = %d/%d, want 1/1", tx.commitCalls, tx.rollbackCalls)
	}
}

func TestSubmitRejectsInvalidIdempotencyKey(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakePortalRepo{
		tenantID: "tenant-1",
		policy: publicvisibilityrepo.Policy{
			PortalAccessMode:      publicvisibilityrepo.AccessModePublic,
			SubmissionWriteMode:   publicvisibilityrepo.WriteModeAnonymous,
			SubmitterIdentityMode: publicvisibilityrepo.IdentityModeAnonymous,
		},
	})
	svc := New(repo, repo, ptrext.Of(fakeFeedbackInserter{}), nil, nil)

	_, err := svc.Submit(context.Background(), SubmitInput{
		TenantSlug:     "acme",
		Kind:           "request",
		Title:          "Need help",
		Details:        "Portal should reject malformed keys",
		IdempotencyKey: "bad key",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Submit() error = %v, want %v", err, ErrValidation)
	}
	if repo.beginCalls != 0 {
		t.Fatalf("begin calls = %d, want 0 for early validation failure", repo.beginCalls)
	}
}

func TestSubmitRejectsAdversarialInputWithoutStartingTx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input SubmitInput
	}{
		{
			name: "missing title",
			input: SubmitInput{
				TenantSlug: "acme",
				Kind:       "request",
				Details:    "Missing title should fail",
			},
		},
		{
			name: "bad kind",
			input: SubmitInput{
				TenantSlug: "acme",
				Kind:       "PORTAL_SUBMISSION_KIND_HACK",
				Title:      "Bad kind",
				Details:    "Bad kind should fail",
			},
		},
		{
			name: "javascript url",
			input: SubmitInput{
				TenantSlug: "acme",
				Kind:       "request",
				Title:      "URL test",
				Details:    "Should reject javascript URLs",
				PageURL:    "javascript:alert(1)",
			},
		},
		{
			name: "honeypot filled",
			input: SubmitInput{
				TenantSlug: "acme",
				Kind:       "request",
				Title:      "Bot trap",
				Details:    "Should reject honeypot",
				Honeypot:   "I am a bot",
			},
		},
		{
			name: "unknown custom fields",
			input: SubmitInput{
				TenantSlug:   "acme",
				Kind:         "request",
				Title:        "Custom fields",
				Details:      "Should reject unexpected custom fields",
				CustomFields: map[string]any{"ghost": "value"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := ptrext.Of(fakePortalRepo{
				tenantID: "tenant-1",
				policy: publicvisibilityrepo.Policy{
					PortalAccessMode:      publicvisibilityrepo.AccessModePublic,
					SubmissionWriteMode:   publicvisibilityrepo.WriteModeAnonymous,
					SubmitterIdentityMode: publicvisibilityrepo.IdentityModeAnonymous,
				},
			})
			feedback := ptrext.Of(fakeFeedbackInserter{})
			svc := New(repo, repo, feedback, nil, nil)

			_, err := svc.Submit(context.Background(), tt.input)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("Submit() error = %v, want %v", err, ErrValidation)
			}
			if repo.beginCalls != 0 || feedback.insertCalls != 0 || feedback.insertIdemCalls != 0 {
				t.Fatalf(
					"validation should stop before tx start, got begin:%d insert:%d idempotent:%d",
					repo.beginCalls,
					feedback.insertCalls,
					feedback.insertIdemCalls,
				)
			}
		})
	}
}

func TestNormalizeCustomFieldsTextAndOptionalValues(t *testing.T) {
	t.Parallel()

	got, err := normalizeCustomFields(
		[]publicvisibilityrepo.PortalSubmissionField{
			{
				Key:      "summary",
				Kind:     publicvisibilityrepo.PortalSubmissionFieldKindText,
				Required: true,
			},
			{
				Key:  "notes",
				Kind: publicvisibilityrepo.PortalSubmissionFieldKindTextarea,
			},
		},
		map[string]any{
			"summary": "  hello portal  ",
			"notes":   "   ",
		},
	)
	if err != nil {
		t.Fatalf("normalizeCustomFields() error = %v", err)
	}
	if got, want := got, map[string]any{"summary": "hello portal"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeCustomFields() = %#v, want %#v", got, want)
	}
}

func TestNormalizeCustomFieldValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		field publicvisibilityrepo.PortalSubmissionField
		name  string
		value any
	}{
		{
			field: publicvisibilityrepo.PortalSubmissionField{
				Kind:     publicvisibilityrepo.PortalSubmissionFieldKindText,
				Required: true,
			},
			name:  "required text blank",
			value: "   ",
		},
		{
			field: publicvisibilityrepo.PortalSubmissionField{
				Kind: publicvisibilityrepo.PortalSubmissionFieldKindText,
			},
			name:  "text non string",
			value: 123,
		},
		{
			field: publicvisibilityrepo.PortalSubmissionField{
				Kind:     publicvisibilityrepo.PortalSubmissionFieldKindSelect,
				Options:  []string{"low", "high"},
				Required: true,
			},
			name:  "required select blank",
			value: " ",
		},
		{
			field: publicvisibilityrepo.PortalSubmissionField{
				Kind:    publicvisibilityrepo.PortalSubmissionFieldKindSelect,
				Options: []string{"low", "high"},
			},
			name:  "select outside options",
			value: "medium",
		},
		{
			field: publicvisibilityrepo.PortalSubmissionField{
				Kind:     publicvisibilityrepo.PortalSubmissionFieldKindMultiSelect,
				Options:  []string{"ui", "api"},
				Required: true,
			},
			name:  "required multiselect empty",
			value: []string{},
		},
		{
			field: publicvisibilityrepo.PortalSubmissionField{
				Kind:    publicvisibilityrepo.PortalSubmissionFieldKindMultiSelect,
				Options: []string{"ui", "api"},
			},
			name:  "multiselect outside options",
			value: []any{"ui", "docs"},
		},
		{
			field: publicvisibilityrepo.PortalSubmissionField{
				Kind: publicvisibilityrepo.PortalSubmissionFieldKindMultiSelect,
			},
			name:  "multiselect blank item",
			value: []any{"ui", " "},
		},
		{
			field: publicvisibilityrepo.PortalSubmissionField{
				Kind: publicvisibilityrepo.PortalSubmissionFieldKindBoolean,
			},
			name:  "boolean non bool",
			value: "true",
		},
		{
			field: publicvisibilityrepo.PortalSubmissionField{
				Kind: publicvisibilityrepo.PortalSubmissionFieldKind("unsupported"),
			},
			name:  "unknown kind",
			value: "value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := normalizeCustomFieldValue(tt.field, tt.value)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("normalizeCustomFieldValue() error = %v, want %v", err, ErrValidation)
			}
		})
	}
}

func TestNormalizeStringArrayVariants(t *testing.T) {
	t.Parallel()

	fromStrings, err := normalizeStringArray([]string{" ui ", "api"})
	if err != nil {
		t.Fatalf("normalizeStringArray([]string) error = %v", err)
	}
	if got, want := fromStrings, []string{"ui", "api"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeStringArray([]string) = %#v, want %#v", got, want)
	}

	fromAny, err := normalizeStringArray([]any{" support "})
	if err != nil {
		t.Fatalf("normalizeStringArray([]any) error = %v", err)
	}
	if got, want := fromAny, []string{"support"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeStringArray([]any) = %#v, want %#v", got, want)
	}

	_, err = normalizeStringArray("ui")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeStringArray(string) error = %v, want %v", err, ErrValidation)
	}
}

func TestSubmitPreservesAdversarialPortalTextForRendering(t *testing.T) {
	t.Parallel()

	tx := ptrext.Of(fakePortalTx{})
	repo := ptrext.Of(fakePortalRepo{
		tenantID: "tenant-1",
		tx:       tx,
		policy: publicvisibilityrepo.Policy{
			PortalAccessMode:      publicvisibilityrepo.AccessModePublic,
			SubmissionWriteMode:   publicvisibilityrepo.WriteModeAnonymous,
			SubmitterIdentityMode: publicvisibilityrepo.IdentityModeAnonymous,
			DefaultRequestState:   publicvisibilityrepo.ModerationStatePending,
		},
	})
	feedback := ptrext.Of(fakeFeedbackInserter{insertID: 404})
	audit := ptrext.Of(fakeAuditRecorder{})
	svc := New(repo, repo, feedback, nil, audit)

	title := `<img src=x onerror="window.__portalXssTitle=1">`
	details := "line1\n<svg onload=\"window.__portalXssDetails=1\"></svg>"

	result, err := svc.Submit(context.Background(), SubmitInput{
		TenantSlug: "acme",
		Kind:       "request",
		Title:      title,
		Details:    details,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if result.SubmissionID != "404" || result.Kind != "request" {
		t.Fatalf("result = %#v, want accepted portal submission", result)
	}
	if got, want := feedback.gotInput.Content, title+"\n\n"+details; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if got, want := feedback.gotInput.SourceMeta, map[string]any{
		"portal_submission": map[string]any{
			"kind":    "request",
			"title":   title,
			"details": details,
		},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source meta = %#v, want %#v", got, want)
	}
	after, ok := audit.event.After.(map[string]any)
	if !ok {
		t.Fatalf("audit after = %#v, want map[string]any", audit.event.After)
	}
	if got := after["title"]; got != title {
		t.Fatalf("audit title = %v, want raw title text", got)
	}
}

func TestSubmitMapsIdempotencyConflict(t *testing.T) {
	t.Parallel()

	tx := ptrext.Of(fakePortalTx{})
	repo := ptrext.Of(fakePortalRepo{
		tenantID: "tenant-1",
		tx:       tx,
		policy: publicvisibilityrepo.Policy{
			PortalAccessMode:      publicvisibilityrepo.AccessModePublic,
			SubmissionWriteMode:   publicvisibilityrepo.WriteModeAnonymous,
			SubmitterIdentityMode: publicvisibilityrepo.IdentityModeAnonymous,
		},
	})
	feedback := ptrext.Of(fakeFeedbackInserter{
		idemErr: feedbackrepo.ErrIdempotencyConflict,
	})
	svc := New(repo, repo, feedback, nil, nil)

	_, err := svc.Submit(context.Background(), SubmitInput{
		TenantSlug:     "acme",
		Kind:           "request",
		Title:          "Need help",
		Details:        "The portal should reject reused keys",
		IdempotencyKey: "retry-safe_456",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Submit() error = %v, want %v", err, ErrConflict)
	}
	if feedback.insertIdemCalls != 1 || repo.createSubjectCalls != 0 || tx.commitCalls != 0 {
		t.Fatalf("idempotency conflict should stop after insert, got feedback:%d subjects:%d commits:%d", feedback.insertIdemCalls, repo.createSubjectCalls, tx.commitCalls)
	}
	if tx.rollbackCalls != 1 {
		t.Fatalf("rollback calls = %d, want 1 after conflict", tx.rollbackCalls)
	}
}

type fakePortalRepo struct {
	tenantID           string
	policy             publicvisibilityrepo.Policy
	beginErr           error
	resolveErr         error
	policyErr          error
	subjectErr         error
	tx                 *fakePortalTx
	beginCalls         int
	resolveCalls       int
	policyCalls        int
	createSubjectCalls int
	gotSubject         publicvisibilityrepo.ModerationSubject
}

func (f *fakePortalRepo) Begin(context.Context) (pgx.Tx, error) {
	f.beginCalls++
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	if f.tx == nil {
		f.tx = ptrext.Of(fakePortalTx{})
	}
	return f.tx, nil
}

func (f *fakePortalRepo) ResolveTenantIDBySlug(context.Context, string) (string, error) {
	f.resolveCalls++
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return f.tenantID, nil
}

func (f *fakePortalRepo) GetPolicy(context.Context, string) (publicvisibilityrepo.Policy, error) {
	f.policyCalls++
	if f.policyErr != nil {
		return publicvisibilityrepo.Policy{}, f.policyErr
	}
	return f.policy, nil
}

func (f *fakePortalRepo) CreateModerationSubjectTx(_ context.Context, _ pgx.Tx, subject publicvisibilityrepo.ModerationSubject) (*publicvisibilityrepo.ModerationSubject, error) {
	f.createSubjectCalls++
	f.gotSubject = subject
	if f.subjectErr != nil {
		return nil, f.subjectErr
	}
	return ptrext.Of(subject), nil
}

type fakeFeedbackInserter struct {
	insertCalls       int
	insertIdemCalls   int
	gotTenantID       string
	gotUserID         string
	gotSubjectKey     string
	gotSubjectDisplay string
	gotSubjectHash    string
	gotInput          domain.IngestInput
	gotIdemHash       []byte
	insertID          int64
	idemID            int64
	idemDeduped       bool
	insertErr         error
	idemErr           error
}

func (f *fakeFeedbackInserter) InsertTx(
	_ context.Context,
	_ pgx.Tx,
	tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
	in domain.IngestInput,
) (int64, error) {
	f.insertCalls++
	f.gotTenantID = tenantID
	f.gotUserID = userID
	f.gotSubjectKey = subjectKey
	f.gotSubjectDisplay = subjectDisplay
	f.gotSubjectHash = subjectHash
	f.gotInput = in
	if f.insertErr != nil {
		return 0, f.insertErr
	}
	if f.insertID == 0 {
		f.insertID = 1
	}
	return f.insertID, nil
}

func (f *fakeFeedbackInserter) InsertIdempotentTx(
	_ context.Context,
	_ pgx.Tx,
	tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
	in domain.IngestInput,
	idemHash []byte,
) (int64, bool, error) {
	f.insertIdemCalls++
	f.gotTenantID = tenantID
	f.gotUserID = userID
	f.gotSubjectKey = subjectKey
	f.gotSubjectDisplay = subjectDisplay
	f.gotSubjectHash = subjectHash
	f.gotInput = in
	f.gotIdemHash = append([]byte(nil), idemHash...)
	if f.idemErr != nil {
		return 0, false, f.idemErr
	}
	if f.idemID == 0 {
		f.idemID = 1
	}
	return f.idemID, f.idemDeduped, nil
}

type fakePortalTx struct {
	commitCalls   int
	rollbackCalls int
}

func (f *fakePortalTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected begin")
}

func (f *fakePortalTx) Commit(context.Context) error {
	f.commitCalls++
	return nil
}

func (f *fakePortalTx) Rollback(context.Context) error {
	f.rollbackCalls++
	return nil
}

func (f *fakePortalTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected copyfrom")
}

func (f *fakePortalTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}
func (f *fakePortalTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (f *fakePortalTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected prepare")
}

func (f *fakePortalTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected exec")
}

func (f *fakePortalTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (f *fakePortalTx) QueryRow(context.Context, string, ...any) pgx.Row { return nil }
func (f *fakePortalTx) Conn() *pgx.Conn                                  { return nil }

type fakeTenantReader struct {
	name string
	err  error
}

func (f *fakeTenantReader) GetByID(_ context.Context, _ string) (*tenantrepo.Tenant, error) {
	if f.err != nil {
		return nil, f.err
	}
	return ptrext.Of(tenantrepo.Tenant{Name: f.name}), nil
}

type fakeAuditRecorder struct {
	calls int
	event auditlogsvc.Event
	err   error
}

func (f *fakeAuditRecorder) RecordTx(_ context.Context, _ pgx.Tx, event auditlogsvc.Event) error {
	f.calls++
	f.event = event
	return f.err
}
