// SPDX-License-Identifier: Apache-2.0

package outbound

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// SignatureVersionContentHash is the column value indicating content-hash
// signing (new default).
const SignatureVersionContentHash = "v2-content-hash"

// SignatureVersionBytes is the column value indicating legacy byte-order
// signing (migration compatibility).
const SignatureVersionBytes = "v2-bytes"

// ContentHashSign computes the HMAC-SHA256 signature over the canonical
// JSON representation of the envelope. The canonical form sorts keys
// alphabetically at every nesting level, ensuring the signature is
// stable regardless of marshal field order.
//
// Returns "sha256=<hex>" — the same wire format as the legacy SignRaw,
// so customers' existing verifiers parse it identically.
func ContentHashSign(envelope *Envelope, secret string) (string, error) {
	canonical, err := canonicalJSON(envelope)
	if err != nil {
		return "", fmt.Errorf("canonical json: %w", err)
	}
	hash := sha256.Sum256(canonical)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(hash[:])
	return "sha256=" + hex.EncodeToString(h.Sum(nil)), nil
}

// BytesSign computes the HMAC-SHA256 signature over the raw JSON bytes.
// Used only for migration compatibility when signature_version = "v2-bytes".
func BytesSign(body []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

// canonicalJSON marshals the envelope to JSON with keys sorted alphabetically
// at every nesting level. This produces a deterministic byte sequence
// regardless of struct field order or map iteration order.
func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return json.Marshal(sortKeys(obj))
}

// sortKeys recursively sorts map keys and returns the sorted structure.
func sortKeys(v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sorted := make(map[string]any, len(x))
		for _, k := range keys {
			sorted[k] = sortKeys(x[k])
		}
		return sorted
	case []any:
		for i, elem := range x {
			x[i] = sortKeys(elem)
		}
		return x
	default:
		return v
	}
}
