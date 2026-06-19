package attune

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// idemCounter guarantees distinct fallback keys within a process.
var idemCounter atomic.Uint64

// newIdempotencyKey returns a fresh idempotency key. It never fails: like the
// Node SDK, if the crypto source is unavailable it falls back to a unique
// (not cryptographically strong) key, since idempotency keys need only be unique
// per call, not unpredictable.
func newIdempotencyKey() string { return idempotencyKey(rand.Read) }

// idempotencyKey is the testable core; randRead is the entropy source.
func idempotencyKey(randRead func([]byte) (int, error)) string {
	var b [16]byte
	if _, err := randRead(b[:]); err == nil {
		// RFC 4122 version-4 UUID. Its 36-char form ([0-9a-f-]) satisfies the
		// server's accepted Idempotency-Key shape ([A-Za-z0-9_-]{8,64}).
		b[6] = (b[6] & 0x0f) | 0x40 // version 4
		b[8] = (b[8] & 0x3f) | 0x80 // variant 10
		h := hex.EncodeToString(b[:])
		return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
	}
	// Fallback: crypto/rand unavailable (extremely rare on real platforms). A
	// monotonic counter plus the timestamp guarantees a distinct key per call.
	n := idemCounter.Add(1)
	return "idem-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatUint(n, 36)
}

// hasHeaderControlChar reports whether s contains a CR or LF, which would allow
// HTTP header injection. Used to reject hostile apiKey / idempotencyKey values
// before they reach the request.
func hasHeaderControlChar(s string) bool {
	return strings.ContainsAny(s, "\r\n")
}
