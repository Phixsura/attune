package lark

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"
)

// expectedSig recomputes the same digest VerifySignature checks so the
// test stays in lockstep with the algorithm without baking in a magic
// hex string the next developer would have to hand-verify.
func expectedSig(secret, ts, nonce string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(ts))
	h.Write([]byte(nonce))
	h.Write([]byte(secret))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func TestVerifySignature_OK(t *testing.T) {
	secret := "sek"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "abc123"
	body := []byte(`{"hello":"world"}`)
	sig := expectedSig(secret, ts, nonce, body)
	if err := VerifySignature(secret, ts, nonce, sig, body); err != nil {
		t.Fatalf("want nil err, got %v", err)
	}
}

func TestVerifySignature_MissingHeaders(t *testing.T) {
	body := []byte("{}")
	cases := []struct {
		name              string
		ts, nonce, sigHex string
	}{
		{"empty timestamp", "", "n", "deadbeef"},
		{"empty nonce", "1", "", "deadbeef"},
		{"empty signature", "1", "n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := VerifySignature("sek", c.ts, c.nonce, c.sigHex, body)
			if !errors.Is(err, ErrMissingHeaders) {
				t.Fatalf("want ErrMissingHeaders, got %v", err)
			}
		})
	}
}

func TestVerifySignature_BadTimestamp(t *testing.T) {
	err := VerifySignature("sek", "not-a-number", "n", "deadbeef", []byte("{}"))
	if !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("want ErrInvalidTimestamp, got %v", err)
	}
}

func TestVerifySignature_SkewOutOfWindow(t *testing.T) {
	// 10 minutes in the past is well past the 5-minute window.
	tooOld := strconv.FormatInt(time.Now().Unix()-10*60, 10)
	err := VerifySignature("sek", tooOld, "n", "deadbeef", []byte("{}"))
	if !errors.Is(err, ErrTimestampSkew) {
		t.Fatalf("want ErrTimestampSkew, got %v", err)
	}
}

func TestVerifySignature_Mismatch(t *testing.T) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"hello":"world"}`)
	// Signature computed against a DIFFERENT body — caller is trying to
	// replay yesterday's payload with today's signature.
	sig := expectedSig("sek", ts, "n", []byte(`{"hello":"OTHER"}`))
	err := VerifySignature("sek", ts, "n", sig, body)
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("want ErrSignatureMismatch, got %v", err)
	}
}

func TestVerifySignature_TamperedSecret(t *testing.T) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{}`)
	// Attacker knows the algorithm but used the wrong secret.
	sig := expectedSig("wrong-secret", ts, "n", body)
	err := VerifySignature("sek", ts, "n", sig, body)
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("want ErrSignatureMismatch, got %v", err)
	}
}
