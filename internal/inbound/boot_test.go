// SPDX-License-Identifier: Apache-2.0

package inbound_test

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/inbound"
)

func TestBootstrapValidate_AcceptsHex(t *testing.T) {
	raw := hex.EncodeToString(make([]byte, 32)) // 64 hex chars
	t.Setenv(inbound.MasterKeyEnv, raw)
	key, err := inbound.BootstrapValidate()
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Errorf("len(key) = %d; want 32", len(key))
	}
}

func TestBootstrapValidate_AcceptsBase64(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString(make([]byte, 32)) // 44 chars
	t.Setenv(inbound.MasterKeyEnv, raw)
	key, err := inbound.BootstrapValidate()
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Errorf("len(key) = %d; want 32", len(key))
	}
}

func TestBootstrapValidate_RejectsEmpty(t *testing.T) {
	t.Setenv(inbound.MasterKeyEnv, "")
	if _, err := inbound.BootstrapValidate(); err == nil {
		t.Error("expected error on empty env")
	}
}

func TestBootstrapValidate_RejectsShort(t *testing.T) {
	t.Setenv(inbound.MasterKeyEnv, hex.EncodeToString(make([]byte, 16))) // 16 bytes only
	_, err := inbound.BootstrapValidate()
	if err == nil {
		t.Fatal("expected error on short key")
	}
	if !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("err missing length hint: %v", err)
	}
}

func TestBootstrapValidate_RejectsJunk(t *testing.T) {
	t.Setenv(inbound.MasterKeyEnv, "not a real key, neither hex nor base64!!")
	if _, err := inbound.BootstrapValidate(); err == nil {
		t.Error("expected error on junk input")
	}
}
