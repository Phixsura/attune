// SPDX-License-Identifier: Apache-2.0

// Package portal orchestrates the public submission portal: config assembly,
// validated submission intake, raw feedback persistence, and moderation
// subject creation.
package portal

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	publicvisibilityrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	tenantrepo "github.com/Phixsura/attune/internal/repo/tenant"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

var (
	ErrValidation = errors.New("portal submission validation failed")
	ErrNotFound   = errors.New("portal submission not found")
	ErrDisabled   = errors.New("portal submission disabled")
	ErrConflict   = errors.New("portal submission conflict")
)

var portalIdempotencyKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type visibilityPolicyReader interface {
	GetPolicy(ctx context.Context, tenantID string) (publicvisibilityrepo.Policy, error)
}

type visibilityTxRepo interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	ResolveTenantIDBySlug(ctx context.Context, slug string) (string, error)
	CreateModerationSubjectTx(ctx context.Context, tx pgx.Tx, subject publicvisibilityrepo.ModerationSubject) (*publicvisibilityrepo.ModerationSubject, error)
}

type feedbackInserter interface {
	InsertTx(
		ctx context.Context,
		tx pgx.Tx,
		tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
		in domain.IngestInput,
	) (int64, error)
	InsertIdempotentTx(
		ctx context.Context,
		tx pgx.Tx,
		tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
		in domain.IngestInput,
		idemHash []byte,
	) (int64, bool, error)
}

type tenantReader interface {
	GetByID(ctx context.Context, id string) (*tenantrepo.Tenant, error)
}

type auditRecorder interface {
	RecordTx(ctx context.Context, tx pgx.Tx, event auditlogsvc.Event) error
}

type Service struct {
	visibility     visibilityPolicyReader
	visibilityRepo visibilityTxRepo
	feedback       feedbackInserter
	tenants        tenantReader
	audit          auditRecorder
	now            func() time.Time
}

type SubmissionConfig struct {
	TenantID              string
	TenantSlug            string
	TenantName            string
	PortalAccessMode      publicvisibilityrepo.AccessMode
	ChangelogEnabled      bool
	SubmissionWriteMode   publicvisibilityrepo.WriteMode
	SubmitterIdentityMode publicvisibilityrepo.IdentityMode
	Form                  publicvisibilityrepo.PortalSubmissionForm
	CanSubmit             bool
}

type SubmitInput struct {
	TenantSlug     string
	Kind           string
	Title          string
	Details        string
	PageURL        string
	DisplayName    string
	Organization   string
	CustomFields   map[string]any
	Honeypot       string
	IdempotencyKey string
	UserAgent      string
}

type SubmitResult struct {
	SubmissionID    string
	Kind            string
	ModerationState publicvisibilityrepo.ModerationState
	Acknowledgement string
}

type portalSubmissionDraft struct {
	kind            string
	title           string
	details         string
	pageURL         string
	customFields    map[string]any
	identity        string
	privateContact  map[string]any
	idempotencyKey  string
	userAgent       string
	acknowledgement string
	feedbackInput   domain.IngestInput
}

func New(
	visibility visibilityPolicyReader,
	visibilityRepo visibilityTxRepo,
	feedback feedbackInserter,
	tenants tenantReader,
	audit auditRecorder,
) *Service {
	return ptrext.Of(Service{
		visibility:     visibility,
		visibilityRepo: visibilityRepo,
		feedback:       feedback,
		tenants:        tenants,
		audit:          audit,
		now:            time.Now,
	})
}

func (s *Service) GetSubmissionConfig(ctx context.Context, tenantSlug string) (SubmissionConfig, error) {
	tenantSlug = strings.TrimSpace(tenantSlug)
	if tenantSlug == "" {
		return SubmissionConfig{}, ErrNotFound
	}
	tenantID, err := s.visibilityRepo.ResolveTenantIDBySlug(ctx, tenantSlug)
	if errors.Is(err, publicvisibilityrepo.ErrNotFound) {
		return SubmissionConfig{}, ErrNotFound
	}
	if err != nil {
		return SubmissionConfig{}, err
	}
	policy, err := s.visibility.GetPolicy(ctx, tenantID)
	if err != nil {
		return SubmissionConfig{}, err
	}
	if policy.PortalAccessMode != publicvisibilityrepo.AccessModePublic {
		return SubmissionConfig{}, ErrNotFound
	}
	name := tenantSlug
	if s.tenants != nil {
		if tenant, err := s.tenants.GetByID(ctx, tenantID); err == nil && strings.TrimSpace(tenant.Name) != "" {
			name = strings.TrimSpace(tenant.Name)
		}
	}
	form := effectiveSubmissionForm(policy.PortalSubmissionForm)
	return SubmissionConfig{
		TenantID:              tenantID,
		TenantSlug:            tenantSlug,
		TenantName:            name,
		PortalAccessMode:      policy.PortalAccessMode,
		ChangelogEnabled:      policy.ChangelogEnabled,
		SubmissionWriteMode:   policy.SubmissionWriteMode,
		SubmitterIdentityMode: policy.SubmitterIdentityMode,
		Form:                  form,
		CanSubmit:             policy.SubmissionWriteMode != publicvisibilityrepo.WriteModeDisabled,
	}, nil
}

func (s *Service) Submit(ctx context.Context, in SubmitInput) (SubmitResult, error) {
	tenantID, policy, err := s.resolveSubmissionPolicy(ctx, in.TenantSlug)
	if err != nil {
		return SubmitResult{}, err
	}
	draft, err := buildPortalSubmissionDraft(in, policy)
	if err != nil {
		return SubmitResult{}, err
	}
	return s.persistSubmission(ctx, tenantID, policy, draft)
}

func (s *Service) resolveSubmissionPolicy(ctx context.Context, tenantSlug string) (string, publicvisibilityrepo.Policy, error) {
	tenantSlug = strings.TrimSpace(tenantSlug)
	if tenantSlug == "" {
		return "", publicvisibilityrepo.Policy{}, ErrNotFound
	}
	tenantID, err := s.visibilityRepo.ResolveTenantIDBySlug(ctx, tenantSlug)
	if errors.Is(err, publicvisibilityrepo.ErrNotFound) {
		return "", publicvisibilityrepo.Policy{}, ErrNotFound
	}
	if err != nil {
		return "", publicvisibilityrepo.Policy{}, err
	}
	policy, err := s.visibility.GetPolicy(ctx, tenantID)
	if err != nil {
		return "", publicvisibilityrepo.Policy{}, err
	}
	if policy.PortalAccessMode != publicvisibilityrepo.AccessModePublic {
		return "", publicvisibilityrepo.Policy{}, ErrNotFound
	}
	if policy.SubmissionWriteMode == publicvisibilityrepo.WriteModeDisabled {
		return "", publicvisibilityrepo.Policy{}, ErrDisabled
	}
	return tenantID, policy, nil
}

func buildPortalSubmissionDraft(in SubmitInput, policy publicvisibilityrepo.Policy) (portalSubmissionDraft, error) {
	if strings.TrimSpace(in.Honeypot) != "" {
		return portalSubmissionDraft{}, ErrValidation
	}
	kind, err := normalizeKind(in.Kind)
	if err != nil {
		return portalSubmissionDraft{}, err
	}
	form := effectiveSubmissionForm(policy.PortalSubmissionForm)
	title := bounded(strings.TrimSpace(in.Title), 120)
	details := bounded(strings.TrimSpace(in.Details), 4000)
	if title == "" || details == "" {
		return portalSubmissionDraft{}, ErrValidation
	}
	pageURL := strings.TrimSpace(in.PageURL)
	if pageURL != "" {
		if err := validatePageURL(pageURL); err != nil {
			return portalSubmissionDraft{}, ErrValidation
		}
	}
	customFields, err := normalizeCustomFields(form.Fields, in.CustomFields)
	if err != nil {
		return portalSubmissionDraft{}, err
	}
	identity, privateContact, err := normalizeIdentity(policy.SubmissionWriteMode, policy.SubmitterIdentityMode, in.DisplayName, in.Organization)
	if err != nil {
		return portalSubmissionDraft{}, err
	}
	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	if idempotencyKey != "" && !portalIdempotencyKeyRe.MatchString(idempotencyKey) {
		return portalSubmissionDraft{}, ErrValidation
	}
	feedbackInput := domain.IngestInput{
		Content:        composeContent(title, details),
		Source:         "portal",
		Type:           kind,
		SourceUser:     identity,
		SourceMeta:     buildSourceMeta(kind, title, details, pageURL, privateContact, customFields, strings.TrimSpace(in.UserAgent)),
		PageURL:        pageURL,
		IdempotencyKey: idempotencyKey,
	}
	if err := feedbackInput.Validate(nil); err != nil {
		return portalSubmissionDraft{}, ErrValidation
	}
	return portalSubmissionDraft{
		kind:            kind,
		title:           title,
		details:         details,
		pageURL:         pageURL,
		customFields:    customFields,
		identity:        identity,
		privateContact:  privateContact,
		idempotencyKey:  idempotencyKey,
		userAgent:       strings.TrimSpace(in.UserAgent),
		acknowledgement: form.Acknowledgement,
		feedbackInput:   feedbackInput,
	}, nil
}

func (s *Service) persistSubmission(ctx context.Context, tenantID string, policy publicvisibilityrepo.Policy, draft portalSubmissionDraft) (SubmitResult, error) {
	tx, err := s.visibilityRepo.Begin(ctx)
	if err != nil {
		return SubmitResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	userID := "portal_" + uuid.NewString()
	subjectKey, subjectDisplay := subjectkey.Normalize(draft.identity, userID)
	subjectHash := ""
	if subjectKey != "" {
		subjectHash = subjectkey.Hash(tenantID, subjectKey)
	}

	submissionID, deduped, err := s.insertPortalSubmissionTx(ctx, tx, tenantID, userID, subjectKey, subjectDisplay, subjectHash, draft)
	if err != nil {
		return SubmitResult{}, err
	}

	subjectState := policy.DefaultRequestState
	if !deduped {
		subjectState, err = s.recordPortalSubmissionSubjectTx(ctx, tx, tenantID, userID, submissionID, subjectHash, draft, policy)
		if err != nil {
			return SubmitResult{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return SubmitResult{}, err
	}
	return SubmitResult{
		SubmissionID:    strconv.FormatInt(submissionID, 10),
		Kind:            draft.kind,
		ModerationState: subjectState,
		Acknowledgement: draft.acknowledgement,
	}, nil
}

func (s *Service) insertPortalSubmissionTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	userID string,
	subjectKey string,
	subjectDisplay string,
	subjectHash string,
	draft portalSubmissionDraft,
) (int64, bool, error) {
	if draft.idempotencyKey != "" {
		submissionID, deduped, err := s.feedback.InsertIdempotentTx(
			ctx,
			tx,
			tenantID,
			userID,
			subjectKey,
			subjectDisplay,
			subjectHash,
			draft.feedbackInput,
			hashPortalSubmission(tenantID, draft.kind, draft.title, draft.details, draft.pageURL, draft.identity, draft.privateContact, draft.customFields),
		)
		if err != nil {
			if errors.Is(err, feedbackrepo.ErrIdempotencyConflict) {
				return 0, false, ErrConflict
			}
			return 0, false, err
		}
		return submissionID, deduped, nil
	}
	submissionID, err := s.feedback.InsertTx(ctx, tx, tenantID, userID, subjectKey, subjectDisplay, subjectHash, draft.feedbackInput)
	if err != nil {
		return 0, false, err
	}
	return submissionID, false, nil
}

func (s *Service) recordPortalSubmissionSubjectTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	userID string,
	submissionID int64,
	subjectHash string,
	draft portalSubmissionDraft,
	policy publicvisibilityrepo.Policy,
) (publicvisibilityrepo.ModerationState, error) {
	subject, err := s.visibilityRepo.CreateModerationSubjectTx(ctx, tx, publicvisibilityrepo.ModerationSubject{
		TenantID:               tenantID,
		Surface:                publicvisibilityrepo.SurfacePortalSubmission,
		SubjectID:              strconv.FormatInt(submissionID, 10),
		State:                  policy.DefaultRequestState,
		SubmittedByDisplay:     draft.identity,
		SubmittedByFingerprint: subjectHash,
	})
	if err != nil {
		return publicvisibilityrepo.ModerationStatePending, err
	}
	if s.audit != nil {
		if err := s.audit.RecordTx(ctx, tx, auditlogsvc.Event{
			TenantID: tenantID,
			Actor: auditlogsvc.Actor{
				Type:      "portal",
				ID:        userID,
				UserAgent: draft.userAgent,
			},
			Action:     "portal_submission.create",
			TargetType: "portal_submission",
			TargetID:   strconv.FormatInt(submissionID, 10),
			Summary:    "Created portal submission",
			Before:     nil,
			After:      portalSubmissionAuditFields(draft.kind, draft.title, draft.pageURL, draft.privateContact, draft.customFields, subject.State),
		}); err != nil {
			return publicvisibilityrepo.ModerationStatePending, err
		}
	}
	return subject.State, nil
}

func effectiveSubmissionForm(form publicvisibilityrepo.PortalSubmissionForm) publicvisibilityrepo.PortalSubmissionForm {
	if strings.TrimSpace(form.Headline) == "" {
		form.Headline = "Send feedback"
	}
	if strings.TrimSpace(form.Description) == "" {
		form.Description = "Share bugs, ideas, or anything blocking your work."
	}
	if strings.TrimSpace(form.Acknowledgement) == "" {
		form.Acknowledgement = "Thanks. We will review your submission."
	}
	if strings.TrimSpace(form.SubmitButtonLabel) == "" {
		form.SubmitButtonLabel = "Submit feedback"
	}
	return form
}

func normalizeKind(raw string) (string, error) {
	switch kind := strings.ToLower(strings.TrimSpace(raw)); kind {
	case "request", "bug", "general":
		return kind, nil
	default:
		return "", ErrValidation
	}
}

func normalizeIdentity(
	writeMode publicvisibilityrepo.WriteMode,
	mode publicvisibilityrepo.IdentityMode,
	displayName string,
	organization string,
) (string, map[string]any, error) {
	displayName = bounded(strings.TrimSpace(displayName), 120)
	organization = bounded(strings.TrimSpace(organization), 120)
	private := map[string]any{}
	if displayName != "" {
		private["display_name"] = displayName
	}
	if organization != "" {
		private["organization"] = organization
	}
	switch writeMode {
	case publicvisibilityrepo.WriteModeDisabled:
		return "", nil, ErrDisabled
	case publicvisibilityrepo.WriteModeAnonymous:
		return "", nil, nil
	case publicvisibilityrepo.WriteModeIdentified:
		switch mode {
		case publicvisibilityrepo.IdentityModeOrganization:
			if organization == "" {
				return "", nil, ErrValidation
			}
			return organization, private, nil
		case publicvisibilityrepo.IdentityModeDisplayName, publicvisibilityrepo.IdentityModeAnonymous:
			if displayName == "" {
				return "", nil, ErrValidation
			}
			return displayName, private, nil
		default:
			return "", nil, ErrValidation
		}
	default:
		return "", nil, ErrValidation
	}
}

func normalizeCustomFields(fields []publicvisibilityrepo.PortalSubmissionField, raw map[string]any) (map[string]any, error) {
	if len(fields) == 0 {
		if len(raw) == 0 {
			return nil, nil
		}
		return nil, ErrValidation
	}
	defs := make(map[string]publicvisibilityrepo.PortalSubmissionField, len(fields))
	for _, field := range fields {
		defs[field.Key] = field
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		field, ok := defs[key]
		if !ok {
			return nil, ErrValidation
		}
		normalized, err := normalizeCustomFieldValue(field, value)
		if err != nil {
			return nil, err
		}
		if normalized != nil {
			out[key] = normalized
		}
		delete(defs, key)
	}
	for _, field := range defs {
		if field.Required {
			return nil, ErrValidation
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func normalizeCustomFieldValue(field publicvisibilityrepo.PortalSubmissionField, value any) (any, error) {
	switch field.Kind {
	case publicvisibilityrepo.PortalSubmissionFieldKindText, publicvisibilityrepo.PortalSubmissionFieldKindTextarea:
		return normalizeCustomFieldText(field.Required, value, 500)
	case publicvisibilityrepo.PortalSubmissionFieldKindSelect:
		return normalizeCustomFieldSelect(field, value)
	case publicvisibilityrepo.PortalSubmissionFieldKindMultiSelect:
		return normalizeCustomFieldMultiSelect(field, value)
	case publicvisibilityrepo.PortalSubmissionFieldKindBoolean:
		return normalizeCustomFieldBoolean(value)
	default:
		return nil, ErrValidation
	}
}

func normalizeCustomFieldText(required bool, value any, limit int) (any, error) {
	text, ok := value.(string)
	if !ok {
		return nil, ErrValidation
	}
	text = bounded(strings.TrimSpace(text), limit)
	if text == "" {
		if required {
			return nil, ErrValidation
		}
		return nil, nil
	}
	return text, nil
}

func normalizeCustomFieldSelect(field publicvisibilityrepo.PortalSubmissionField, value any) (any, error) {
	text, ok := value.(string)
	if !ok {
		return nil, ErrValidation
	}
	text = bounded(strings.TrimSpace(text), 80)
	if text == "" {
		if field.Required {
			return nil, ErrValidation
		}
		return nil, nil
	}
	if !stringSliceContains(field.Options, text) {
		return nil, ErrValidation
	}
	return text, nil
}

func normalizeCustomFieldMultiSelect(field publicvisibilityrepo.PortalSubmissionField, value any) (any, error) {
	values, err := normalizeStringArray(value)
	if err != nil {
		return nil, ErrValidation
	}
	if len(values) == 0 {
		if field.Required {
			return nil, ErrValidation
		}
		return nil, nil
	}
	for _, item := range values {
		if !stringSliceContains(field.Options, item) {
			return nil, ErrValidation
		}
	}
	return values, nil
}

func normalizeCustomFieldBoolean(value any) (any, error) {
	b, ok := value.(bool)
	if !ok {
		return nil, ErrValidation
	}
	return b, nil
}

func normalizeStringArray(value any) ([]string, error) {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, ErrValidation
			}
			text = bounded(strings.TrimSpace(text), 80)
			if text == "" {
				return nil, ErrValidation
			}
			out = append(out, text)
		}
		return out, nil
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := bounded(strings.TrimSpace(item), 80)
			if text == "" {
				return nil, ErrValidation
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, ErrValidation
	}
}

func validatePageURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("unsupported url scheme")
	}
	if strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("missing url host")
	}
	return nil
}

func composeContent(title, details string) string {
	return strings.TrimSpace(title) + "\n\n" + strings.TrimSpace(details)
}

func buildSourceMeta(
	kind string,
	title string,
	details string,
	pageURL string,
	privateContact map[string]any,
	customFields map[string]any,
	userAgent string,
) map[string]any {
	portal := map[string]any{
		"kind":    kind,
		"title":   title,
		"details": details,
	}
	if pageURL != "" {
		portal["page_url"] = pageURL
	}
	if len(privateContact) > 0 {
		portal["private_contact"] = privateContact
	}
	if len(customFields) > 0 {
		portal["custom_fields"] = customFields
	}
	if userAgent != "" {
		portal["user_agent"] = userAgent
	}
	return map[string]any{"portal_submission": portal}
}

func portalSubmissionAuditFields(
	kind string,
	title string,
	pageURL string,
	privateContact map[string]any,
	customFields map[string]any,
	state publicvisibilityrepo.ModerationState,
) map[string]any {
	fields := map[string]any{
		"kind":  kind,
		"title": bounded(title, 120),
		"state": state,
	}
	if pageURL != "" {
		fields["page_url"] = pageURL
	}
	if len(privateContact) > 0 {
		fields["private_contact_keys"] = mapKeys(privateContact)
	}
	if len(customFields) > 0 {
		fields["custom_field_count"] = len(customFields)
	}
	return fields
}

func bounded(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hashPortalSubmission(
	tenantID string,
	kind string,
	title string,
	details string,
	pageURL string,
	sourceUser string,
	privateContact map[string]any,
	customFields map[string]any,
) []byte {
	canonical := struct {
		TenantID       string         `json:"tenant_id"`
		Kind           string         `json:"kind"`
		Title          string         `json:"title"`
		Details        string         `json:"details"`
		PageURL        string         `json:"page_url,omitempty"`
		SourceUser     string         `json:"source_user,omitempty"`
		PrivateContact map[string]any `json:"private_contact,omitempty"`
		CustomFields   map[string]any `json:"custom_fields,omitempty"`
	}{
		TenantID:       tenantID,
		Kind:           kind,
		Title:          title,
		Details:        details,
		PageURL:        pageURL,
		SourceUser:     sourceUser,
		PrivateContact: privateContact,
		CustomFields:   customFields,
	}
	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	return sum[:]
}
