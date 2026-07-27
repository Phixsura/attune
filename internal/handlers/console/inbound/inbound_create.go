// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// slugSanitize is the regex used to derive a URL-safe slug from the
// human-facing source name. Anything outside [a-z0-9-] gets folded to
// "-"; multiple "-" collapse to one, then we trim. The slug ends up in
// the public webhook URL so it's lower-case ASCII only.
var slugSanitize = regexp.MustCompile(`[^a-z0-9]+`)

// Create handles POST /fb/v1/console/inbound/sources.
func (h *Handler) Create(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateInboundSourceRequest) (dispatcher.Result[*attunev1.CreateInboundSourceResponse], error) {
	const where = "console.inbound.Create"
	auth := ctx.Auth
	channel := strings.TrimSpace(strings.ToLower(req.GetChannel()))
	name := strings.TrimSpace(req.GetName())
	if name == "" || len(name) > 200 {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "name must be non-empty and ≤ 200 characters")
	}
	slug := slugify(name)
	if slug == "" {
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "name must contain at least one alphanumeric character")
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,channel:%s,name:%s,slug:%s",
		where, auth.TenantID, channel, name, slug)

	switch channel {
	case channelWebhook:
		return h.createWebhook(ctx, auth, name, slug)
	case channelEmail:
		return h.createEmail(ctx, auth, req, name, slug)
	case channelSlack:
		return h.createSlack(ctx, auth, req, name, slug)
	case channelZendesk:
		return h.createZendesk(ctx, auth, req, name, slug)
	case channelIntercom:
		return h.createIntercom(ctx, auth, req, name, slug)
	default:
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "channel must be webhook, email, slack, zendesk, or intercom")
	}
}

// slugify — keep only [a-z0-9], collapse runs of separators to '-'.
// Empty input → empty output (caller treats as a validation error).
func slugify(in string) string {
	lower := strings.ToLower(strings.TrimSpace(in))
	replaced := slugSanitize.ReplaceAllString(lower, "-")
	return strings.Trim(replaced, "-")
}
