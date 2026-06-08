// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// replayWindow — how far ±now() a request's X-Attune-Timestamp may sit
// before we reject it as a replay. Matches Stripe / Slack convention.
const replayWindow = 300 * time.Second

// computeSig — Stripe-style HMAC: hex(SHA256_HMAC(secret, ts + "." + body)).
func computeSig(secret []byte, ts string, body []byte) string {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write([]byte(ts))
	_, _ = h.Write([]byte("."))
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// verifyHMAC — true iff the X-Attune-Signature header matches and the
// timestamp is within ±replayWindow of now. Constant-time compare via
// hmac.Equal so the timing channel doesn't leak the prefix length of
// the signature.
func verifyHMAC(secret []byte, ts string, body []byte, header string) bool {
	if !timestampFresh(ts) {
		return false
	}
	want := strings.TrimPrefix(header, "sha256=")
	got := computeSig(secret, ts, body)
	return hmac.Equal([]byte(want), []byte(got))
}

// verifyHMACAgainstStub — used by the handler when the source slug
// lookup fails. Runs full HMAC against a per-process stub secret so the
// response time matches the auth_err path; ALWAYS returns false. This
// is the enumeration-resistance primitive: tenant/source enumeration
// via 404 vs 401 timing is blocked because we never short-circuit.
func verifyHMACAgainstStub(stub []byte, ts string, body []byte, header string) bool {
	_ = verifyHMAC(stub, ts, body, header)
	return false
}

func timestampFresh(ts string) bool {
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	drift := time.Since(time.Unix(t, 0))
	if drift < 0 {
		drift = -drift
	}
	return drift <= replayWindow
}
