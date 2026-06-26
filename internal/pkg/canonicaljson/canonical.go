// SPDX-License-Identifier: Apache-2.0

// Package canonicaljson implements a subset of RFC 8785 (JSON Canonicalization
// Scheme) sufficient for attune's audit evidence hash chains: sorted object
// keys, compact encoding, and deterministic number serialization.
package canonicaljson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Marshal serializes v to canonical JSON per RFC 8785.
//
// The output is deterministic: object keys are sorted lexicographically,
// no whitespace is emitted between tokens, and numbers use the shortest
// representation per IEEE 754 (no trailing zeros, no leading + on exponents).
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil { // ptrext:allow write-target
		return nil, err
	}
	return buf.Bytes(), nil
}

// MarshalEntry re-decodes raw JSON into a canonical form.
// Use this when the input is already JSON bytes (e.g. from PostgreSQL JSONB).
func MarshalEntry(raw json.RawMessage) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return Marshal(v)
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch val := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if val {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case float64:
		buf.WriteString(canonicalNumber(val))
	case json.Number:
		f, err := val.Float64()
		if err != nil {
			buf.WriteString(val.String())
			return nil
		}
		buf.WriteString(canonicalNumber(f))
	case string:
		writeString(buf, val)
	case []any:
		return writeArray(buf, val)
	case map[string]any:
		return writeObject(buf, val)
	default:
		return writeViaJSON(buf, val)
	}
	return nil
}

func writeArray(buf *bytes.Buffer, val []any) error {
	buf.WriteByte('[')
	for i, elem := range val {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := writeCanonical(buf, elem); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

func writeObject(buf *bytes.Buffer, val map[string]any) error {
	keys := make([]string, 0, len(val))
	for k := range val {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		writeString(buf, k)
		buf.WriteByte(':')
		if err := writeCanonical(buf, val[k]); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

func writeViaJSON(buf *bytes.Buffer, val any) error {
	b, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("canonicaljson: unsupported type %T: %w", val, err)
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil { // ptrext:allow out-param
		buf.Write(b)
		return nil
	}
	return writeCanonical(buf, decoded)
}

// canonicalNumber formats a float64 per RFC 8785 §3.2.2.3, which
// requires ECMAScript's Number.prototype.toString() semantics:
//   - fixed notation when the exponent n satisfies -6 < n ≤ 21
//   - exponential notation otherwise, with no zero-padded exponent
//
// Go's strconv.FormatFloat('g') uses different thresholds and zero-pads
// exponents, so we implement the ES6 algorithm directly.
func canonicalNumber(f float64) string {
	if f == 0 {
		return "0"
	}
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return formatES6Float(f)
}

// formatES6Float implements ECMAScript 2024 §6.1.6.1.20 Number::toString
// for non-zero, non-integer float64 values.
func formatES6Float(f float64) string {
	sign := ""
	abs := f
	if f < 0 {
		sign = "-"
		abs = -f
	}

	s := strconv.FormatFloat(abs, 'e', -1, 64)
	eIdx := strings.IndexByte(s, 'e')
	mantissa := s[:eIdx]
	exp, _ := strconv.Atoi(s[eIdx+1:])

	digits := strings.Replace(mantissa, ".", "", 1)
	k := len(digits)
	n := exp + 1

	var result string
	switch {
	case k <= n && n <= 21:
		result = digits + strings.Repeat("0", n-k)
	case 0 < n && n <= k:
		result = digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		result = "0." + strings.Repeat("0", -n) + digits
	case k == 1:
		result = digits + "e" + formatExponent(n-1)
	default:
		result = digits[:1] + "." + digits[1:] + "e" + formatExponent(n-1)
	}
	return sign + result
}

func formatExponent(e int) string {
	if e >= 0 {
		return "+" + strconv.Itoa(e)
	}
	return strconv.Itoa(e)
}

// writeString emits a JSON string with minimal escape sequences per RFC 8785.
func writeString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
}
