// SPDX-License-Identifier: Apache-2.0

package customerrequest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeriveAttribution_IntercomFullProfile(t *testing.T) {
	t.Parallel()
	meta := map[string]any{
		"intercom_workspace_id":          "ws42",
		"intercom_contact_external_id":   "cust-70",
		"intercom_contact_email":         "alice@customer.example",
		"intercom_contact_name":          "Alice Zhang",
		"intercom_company_id":            "co-9",
		"intercom_company_name":          "Customer Co",
		"intercom_company_monthly_spend": float64(1200),
		"intercom_company_plan":          "Pro",
		"intercom_company_size":          float64(85),
		"intercom_company_industry":      "Software",
		"intercom_conversation_id":       "9001",
	}
	attr, ok := deriveAttribution("intercom", "", meta)
	require.True(t, ok)
	// Email wins over external_id — globally unique and what the GDPR
	// subject matcher keys on.
	require.Equal(t, "alice@customer.example", attr.SubjectKey)
	require.Equal(t, "Alice Zhang", attr.SubjectDisplay)
	// Account keys are workspace-scoped: company IDs are only unique
	// within one Intercom workspace.
	require.Equal(t, "intercom:ws42:co-9", attr.AccountKey)
	require.Equal(t, "Customer Co", attr.AccountDisplay)
	require.Equal(t, int64(120000), attr.Profile.RevenueCents) // 1200 → cents
	require.Equal(t, "USD", attr.Profile.RevenueCurrency)
	require.Equal(t, "Pro", attr.Profile.Tier)
	require.Equal(t, "85", attr.Profile.SizeSegment)
	require.Equal(t, "intercom", attr.Profile.CRMProvider)
	require.Equal(t, "intercom:ws42:co-9", attr.Profile.CRMExternalID)
}

func TestDeriveAttribution_ExternalIDFallbackIsScoped(t *testing.T) {
	t.Parallel()
	// No email → external_id fallback, workspace-scoped so "cust-70"
	// from two different workspaces never merges.
	meta := map[string]any{
		"intercom_workspace_id":        "ws42",
		"intercom_contact_external_id": "cust-70",
	}
	attr, ok := deriveAttribution("intercom", "", meta)
	require.True(t, ok)
	require.Equal(t, "intercom:ws42:cust-70", attr.SubjectKey)
}

func TestDeriveAttribution_ClampsOverlongFields(t *testing.T) {
	t.Parallel()
	long := make([]rune, 600)
	for i := range long {
		long[i] = '长'
	}
	meta := map[string]any{
		"intercom_company_id":   "co-9",
		"intercom_company_name": string(long),
		"intercom_company_plan": string(long),
	}
	attr, ok := deriveAttribution("intercom", "", meta)
	require.True(t, ok)
	// DB CHECKs are hard limits — over-long values must be clamped, not
	// allowed to abort the link inside its savepoint.
	require.LessOrEqual(t, len([]rune(attr.AccountDisplay)), 500)
	require.LessOrEqual(t, len([]rune(attr.Profile.Tier)), 120)
}

func TestDeriveAttribution_RoundsFractionalSpend(t *testing.T) {
	t.Parallel()
	meta := map[string]any{
		"intercom_company_id":            "co-9",
		"intercom_company_monthly_spend": 99.99,
	}
	attr, ok := deriveAttribution("intercom", "", meta)
	require.True(t, ok)
	require.Equal(t, int64(9999), attr.Profile.RevenueCents)
}

func TestDeriveAttribution_EmailFallback(t *testing.T) {
	t.Parallel()
	meta := map[string]any{
		"intercom_contact_email": "bob@lead.example",
	}
	attr, ok := deriveAttribution("intercom", "", meta)
	require.True(t, ok)
	require.Equal(t, "bob@lead.example", attr.SubjectKey)
	require.Equal(t, "bob@lead.example", attr.SubjectDisplay)
	require.Empty(t, attr.AccountKey)
	require.Zero(t, attr.Profile.RevenueCents)
}

func TestDeriveAttribution_ZendeskConvention(t *testing.T) {
	t.Parallel()
	meta := map[string]any{
		"zendesk_requester_email":   "carol@acme.example",
		"zendesk_requester_name":    "Carol Wu",
		"zendesk_organization_id":   float64(200),
		"zendesk_organization_name": "Acme Corp",
	}
	attr, ok := deriveAttribution("zendesk", "", meta)
	require.True(t, ok)
	require.Equal(t, "carol@acme.example", attr.SubjectKey)
	require.Equal(t, "Carol Wu", attr.SubjectDisplay)
	// No subdomain in meta → channel-scoped fallback.
	require.Equal(t, "zendesk:200", attr.AccountKey)
	require.Equal(t, "Acme Corp", attr.AccountDisplay)
}

func TestDeriveAttribution_ZendeskSubdomainScope(t *testing.T) {
	t.Parallel()
	meta := map[string]any{
		"zendesk_subdomain":       "acme",
		"zendesk_organization_id": float64(200),
	}
	attr, ok := deriveAttribution("zendesk", "", meta)
	require.True(t, ok)
	require.Equal(t, "zendesk:acme:200", attr.AccountKey)
}

func TestDeriveAttribution_Negative(t *testing.T) {
	t.Parallel()
	// Unlisted channel → no derivation even with matching keys.
	_, ok := deriveAttribution("webhook", "", map[string]any{"webhook_contact_email": "x@y.z"})
	require.False(t, ok)
	// Listed channel without identity keys → no derivation.
	_, ok = deriveAttribution("intercom", "", map[string]any{"intercom_conversation_id": "1"})
	require.False(t, ok)
	// Empty meta.
	_, ok = deriveAttribution("intercom", "", nil)
	require.False(t, ok)
}

func TestFirstMetaStringAndNumber(t *testing.T) {
	t.Parallel()
	meta := map[string]any{
		"a": "  ",
		"b": "value",
		"n": float64(42),
		"z": float64(0),
	}
	require.Equal(t, "value", firstMetaString(meta, "a", "b"))
	require.Equal(t, "42", firstMetaString(meta, "missing", "n"))
	require.Equal(t, "", firstMetaString(meta, "a", "z", "missing"))
	require.Equal(t, float64(42), metaNumber(meta, "n"))
	require.Equal(t, float64(0), metaNumber(meta, "b"))
}

func TestDeriveAttribution_RowSubjectKeyWins(t *testing.T) {
	t.Parallel()
	// The feedback row's own GDPR subject identity must become the link
	// subject verbatim: a metadata-derived key (email vs scoped
	// contact-id) could diverge from the row's, leaving link rows an
	// erasure keyed on the feedback subject would never reach.
	meta := map[string]any{
		"intercom_workspace_id":        "ws42",
		"intercom_contact_external_id": "cust-70",
		"intercom_contact_email":       "alice@customer.example",
	}
	attr, ok := deriveAttribution("intercom", "contact:abc123", meta)
	require.True(t, ok)
	require.Equal(t, "contact:abc123", attr.SubjectKey)

	// Blank row subject falls back to the metadata chain.
	attr, ok = deriveAttribution("intercom", "  ", meta)
	require.True(t, ok)
	require.Equal(t, "alice@customer.example", attr.SubjectKey)
}
