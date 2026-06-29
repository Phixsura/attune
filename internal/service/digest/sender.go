// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// sender delivers digest payloads via the outbound adapter framework.
// It looks up the DigestChannel for each target's destination_type and
// renders the digest appropriately (JSON for raw-webhook, card for Lark, etc.).
type sender struct {
	transport *notify.Transport
}

func newSender(transport *notify.Transport) *sender {
	return ptrext.Of(sender{transport: transport})
}

// send delivers the digest view to a single target using the appropriate channel.
func (s *sender) send(
	ctx context.Context, target *notifytarget.NotifyTarget, view any, traceID string,
) error {
	const where = "digest.sender.send"

	ch := outbound.LookupDigest(target.DestinationType)
	if ch == nil {
		return fmt.Errorf("%w: no digest channel for %q", notify.ErrTerminal, target.DestinationType)
	}

	dst := toOutboundTarget(target)

	if ds := outbound.LookupDirectDigest(target.DestinationType); ds != nil {
		return ds.SendDigest(ctx, view, dst)
	}

	rendered, err := ch.RenderDigest(view, dst)
	if err != nil {
		logext.Errorf(ctx, "[%s] render failed,dest_type:%s,err:%+v",
			where, target.DestinationType, err.Error())
		return err
	}

	label := fmt.Sprintf("digest-%s-%s", target.DestinationType, target.TenantID)
	check := func(ctx context.Context, status int, body []byte) error {
		return rendered.Check(ctx, status, body)
	}
	return s.transport.Send(ctx, label, rendered.Build, check)
}

// toOutboundTarget converts a repo NotifyTarget to an outbound.Target.
func toOutboundTarget(t *notifytarget.NotifyTarget) outbound.Target {
	sigVersion := t.SignatureVersion
	if sigVersion == "" {
		sigVersion = outbound.SignatureVersionContentHash
	}
	return outbound.Target{
		ID:               t.ID.String(),
		TenantID:         t.TenantID,
		URL:              t.URL,
		Secret:           t.Secret,
		SignatureVersion: sigVersion,
		DestinationType:  t.DestinationType,
	}
}
