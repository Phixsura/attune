// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestVerifyHMAC_HappyPath(t *testing.T) {
	secret := []byte("test-secret-32-bytes-padding-zzz")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"content":"hello"}`)
	sig := computeSig(secret, ts, body)
	if !verifyHMAC(secret, ts, body, "sha256="+sig) {
		t.Error("expected verify to pass")
	}
}

func TestVerifyHMAC_AcceptsBareHexHeader(t *testing.T) {
	// Some clients omit the "sha256=" prefix. The implementation should
	// tolerate either form.
	secret := []byte("test-secret-bare")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`x`)
	sig := computeSig(secret, ts, body)
	if !verifyHMAC(secret, ts, body, sig) {
		t.Error("bare hex header should still verify")
	}
}

func TestVerifyHMAC_RejectsStaleTimestamp(t *testing.T) {
	secret := []byte("s")
	if verifyHMAC(secret, "1", []byte(""), "sha256=deadbeef") {
		t.Error("ancient timestamp should fail")
	}
}

func TestVerifyHMAC_RejectsFutureTimestamp(t *testing.T) {
	secret := []byte("s")
	future := strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
	if verifyHMAC(secret, future, []byte(""), "sha256=anything") {
		t.Error("future-by-10min should fail")
	}
}

func TestVerifyHMAC_RejectsTamperedSig(t *testing.T) {
	secret := []byte("s")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte("x")
	sig := computeSig(secret, ts, body)
	tampered := strings.Repeat("0", len(sig))
	if verifyHMAC(secret, ts, body, "sha256="+tampered) {
		t.Error("tampered sig should fail")
	}
}

func TestVerifyHMAC_RejectsMalformedTimestamp(t *testing.T) {
	secret := []byte("s")
	if verifyHMAC(secret, "not-a-number", []byte(""), "sha256=anything") {
		t.Error("malformed timestamp should fail")
	}
}

func TestVerifyHMACAgainstStub_AlwaysFalse(t *testing.T) {
	if verifyHMACAgainstStub([]byte("stub"), "0", []byte(""), "") {
		t.Error("stub verify must always return false")
	}
}
