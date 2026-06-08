// Package sig holds the wire-canonical HMAC signing helpers + the v1
// envelope-version constant. Centralized here so notify/test_send.go,
// notify/adapter/rawwebhook, and service/outbox can all reference the
// same byte-exact implementation of SignRaw — no "must stay in sync"
// comments, no drift.
//
// The package depends only on stdlib crypto; no other internal/notify
// types — so it can be imported from anywhere in the outbound subsystem
// without creating cycles.
package sig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// EnvelopeVersion is the version tag stamped on every v1 outbound
// envelope (raw-webhook + outbox payloads). Customers' verifiers may
// pin on this — never break the value within a major.
const EnvelopeVersion = "1"

// SignRaw is the HMAC-SHA256 signature over the request body, encoded
// as "sha256=<hex>" — the canonical raw-webhook format that customer
// verifiers parse against. Stable wire contract.
func SignRaw(body []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}
