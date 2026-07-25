// SPDX-License-Identifier: Apache-2.0

package customerrequest

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/logext"
	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
)

// derivedAttribution is the customer/account identity extracted from an
// inbound-channel feedback row's source_meta. Channel adapters emit
// `<channel>_contact_*` / `<channel>_company_*` keys; this derivation is
// the channel-agnostic bridge that turns them into customer links so a
// promoted request already knows who asked and what they are worth.
type derivedAttribution struct {
	SubjectKey     string
	SubjectDisplay string
	AccountKey     string
	AccountDisplay string
	Profile        repo.AccountProfileInput
}

// metaChannels lists the inbound channels whose adapters emit the
// contact/company source-meta convention. Append-only, like the source
// vocabulary itself.
var metaChannels = []string{"intercom", "zendesk"}

// deriveAttribution extracts attribution from one feedback row's
// source_meta. Returns ok=false when the row carries no usable identity
// (API/webhook rows, portal rows with their own linkage, …).
func deriveAttribution(source string, meta map[string]any) (derivedAttribution, bool) {
	if len(meta) == 0 {
		return derivedAttribution{}, false
	}
	prefix := ""
	for _, ch := range metaChannels {
		if source == ch {
			prefix = ch + "_"
			break
		}
	}
	if prefix == "" {
		return derivedAttribution{}, false
	}

	out := derivedAttribution{
		SubjectKey:     firstMetaString(meta, prefix+"contact_external_id", prefix+"contact_email", prefix+"requester_email"),
		SubjectDisplay: firstMetaString(meta, prefix+"contact_name", prefix+"requester_name", prefix+"contact_email", prefix+"requester_email"),
		AccountKey:     firstMetaString(meta, prefix+"company_id", prefix+"organization_id"),
		AccountDisplay: firstMetaString(meta, prefix+"company_name", prefix+"organization_name"),
	}
	if out.SubjectKey == "" && out.AccountKey == "" {
		return derivedAttribution{}, false
	}

	if out.AccountKey != "" {
		out.Profile = repo.AccountProfileInput{
			AccountKey:     out.AccountKey,
			AccountDisplay: out.AccountDisplay,
			Tier:           firstMetaString(meta, prefix+"company_plan"),
			SizeSegment:    firstMetaString(meta, prefix+"company_size"),
			CRMProvider:    source,
			CRMExternalID:  out.AccountKey,
			// "integration" is the profile-source enum for connector-derived
			// data (DB CHECK: manual|feedback|crm|integration|api); the
			// channel itself travels in CRMProvider.
			Source: "integration",
		}
		// Intercom company.monthly_spend is whole currency units.
		if spend := metaNumber(meta, prefix+"company_monthly_spend"); spend > 0 {
			out.Profile.RevenueCents = int64(spend * 100)
			out.Profile.RevenueCurrency = "USD"
		}
	}
	return out, true
}

// autoLinkCustomersTx derives attribution from every promoted feedback
// row and links the resulting customers inside the promote transaction.
// Best-effort by design: a row without identity, or a link failure, must
// never fail the promote itself.
func (s *Service) autoLinkCustomersTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID, feedbackIDs []int64, actorID string) int {
	seen := map[string]bool{}
	linked := 0
	for _, feedbackID := range feedbackIDs {
		source, meta, err := s.repo.FeedbackSourceMetaTx(ctx, tx, tenantID, feedbackID)
		if err != nil {
			continue
		}
		attr, ok := deriveAttribution(source, meta)
		if !ok {
			continue
		}
		dedupeKey := attr.SubjectKey + "|" + attr.AccountKey
		if seen[dedupeKey] {
			continue
		}
		seen[dedupeKey] = true
		attr.Profile.ActorID = actorID
		// Savepoint-isolate each link: a failed INSERT aborts the whole
		// PostgreSQL transaction otherwise, which would poison the promote.
		sp, err := tx.Begin(ctx)
		if err != nil {
			logext.Warnf(ctx, "[service.customerrequest.autoLink] savepoint failed,feedback_id:%d,err:%+v", feedbackID, err.Error())
			continue
		}
		if _, err := s.repo.LinkCustomerTx(ctx, sp, repo.CustomerLinkInput{
			TenantID:       tenantID,
			RequestID:      requestID,
			SubjectKey:     attr.SubjectKey,
			SubjectDisplay: attr.SubjectDisplay,
			AccountKey:     attr.AccountKey,
			AccountDisplay: attr.AccountDisplay,
			Note:           "Auto-attributed from " + source + " feedback",
			ActorID:        actorID,
			AccountProfile: attr.Profile,
		}); err != nil {
			_ = sp.Rollback(ctx)
			logext.Warnf(ctx, "[service.customerrequest.autoLink] link failed,feedback_id:%d,err:%+v", feedbackID, err.Error())
			continue
		}
		if err := sp.Commit(ctx); err != nil {
			logext.Warnf(ctx, "[service.customerrequest.autoLink] savepoint commit failed,feedback_id:%d,err:%+v", feedbackID, err.Error())
			continue
		}
		linked++
	}
	return linked
}

// firstMetaString returns the first non-empty string value among keys.
// Numeric json values are stringified (company size arrives as float64).
func firstMetaString(meta map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := meta[k].(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		case float64:
			if v > 0 {
				return strconv.FormatFloat(v, 'f', -1, 64)
			}
		}
	}
	return ""
}

// metaNumber reads a numeric meta value (json numbers decode as float64).
func metaNumber(meta map[string]any, key string) float64 {
	if v, ok := meta[key].(float64); ok {
		return v
	}
	return 0
}
