// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/notify/sig"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// sender POSTs a digest payload to a raw-webhook target, reusing the shared
// notify.Transport (the retry loop) and sig.SignRaw (the canonical HMAC
// signature customers already verify). digest_runs is the across-tick retry
// ledger; this Transport handles within-call retries.
type sender struct {
	transport *notify.Transport
}

func newSender(transport *notify.Transport) *sender {
	return ptrext.Of(sender{transport: transport})
}

func (s *sender) send(
	ctx context.Context, target *notifytarget.NotifyTarget, payload []byte, traceID string,
) error {
	label := "digest-" + target.TenantID
	build := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Attune-Signature", sig.SignRaw(payload, target.Secret))
		req.Header.Set("X-Trace-ID", traceID)
		req.Header.Set("User-Agent", "attune/1.0")
		return req, nil
	}
	return s.transport.Send(ctx, label, build, checkDigestResponse(label))
}

// checkDigestResponse classifies the webhook response: 2xx ok, 408/429
// retryable, other 4xx terminal, everything else retryable.
func checkDigestResponse(label string) notify.ResponseChecker {
	return func(ctx context.Context, status int, body []byte) error {
		switch {
		case status >= 200 && status < 300:
			return nil
		case status == 408 || status == 429:
			return fmt.Errorf("digest %s retryable status=%d", label, status)
		case status >= 400 && status < 500:
			return fmt.Errorf("%w: digest %s status=%d", notify.ErrTerminal, label, status)
		default:
			return fmt.Errorf("digest %s status=%d", label, status)
		}
	}
}
