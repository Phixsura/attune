// Package lark is the infra-layer adapter for Lark/Feishu webhook
// events: signature verification and envelope decoding. Handlers
// depend on this package to translate raw HTTP bytes into typed
// Event values — keeping protocol details out of the HTTP layer.
package lark

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

// skewSeconds is the maximum clock drift Lark accepts between the
// request timestamp and our wall clock. Five minutes matches the
// official docs and is the same value the previous handler used.
const skewSeconds = 5 * 60

// Signature errors. Handlers map all of these to 401 — the distinction
// matters mostly for logs and tests.
var (
	ErrMissingHeaders    = errors.New("missing lark signature headers")
	ErrInvalidTimestamp  = errors.New("invalid timestamp")
	ErrTimestampSkew     = errors.New("timestamp out of window")
	ErrSignatureMismatch = errors.New("signature mismatch")
)

// VerifySignature validates the three Lark request headers (timestamp,
// nonce, signature) against the raw body using the configured signing
// secret. Returns one of the Err* sentinels on failure.
//
// Algorithm (per Lark Open Platform docs):
//
//	hex( sha256( timestamp || nonce || signing_secret || body ) )
//
// All four pieces are concatenated as raw bytes — timestamp/nonce/secret
// as UTF-8, body as the request payload.
func VerifySignature(secret, timestamp, nonce, gotHex string, body []byte) error {
	if timestamp == "" || nonce == "" || gotHex == "" {
		return ErrMissingHeaders
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrInvalidTimestamp
	}
	skew := time.Now().Unix() - ts
	if skew < -skewSeconds || skew > skewSeconds {
		return ErrTimestampSkew
	}
	h := sha256.New()
	h.Write([]byte(timestamp))
	h.Write([]byte(nonce))
	h.Write([]byte(secret))
	h.Write(body)
	want := hex.EncodeToString(h.Sum(nil))
	if !hmac.Equal([]byte(gotHex), []byte(want)) {
		return ErrSignatureMismatch
	}
	return nil
}
