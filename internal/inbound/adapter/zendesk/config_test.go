// SPDX-License-Identifier: Apache-2.0

package zendesk

import (
	"encoding/json"
	"testing"

	"github.com/Phixsura/attune/internal/inbound/inboundtest"
)

func TestValidateSubdomain(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"mycompany", false},
		{"acme", false},
		{"my-company", false},
		{"a1b2c3", false},
		{"a", false},
		{"x-y-z", false},
		{"", true},
		{"  ", true},
		{"-leading", true},
		{"trailing-", true},
		{"has space", true},
		{"UPPER", false}, // lowered internally
		{"has.dot", true},
		{"under_score", true},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			err := validateSubdomain(tc.input)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateSubdomain(%q) err=%v, wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestBaseURL(t *testing.T) {
	got := baseURL("mycompany")
	want := "https://mycompany.zendesk.com"
	if got != want {
		t.Errorf("baseURL() = %q, want %q", got, want)
	}
}

func TestBaseURL_Uppercase(t *testing.T) {
	got := baseURL("  MYCO  ")
	want := "https://myco.zendesk.com"
	if got != want {
		t.Errorf("baseURL() = %q, want %q", got, want)
	}
}

func TestParseConfig_APIToken(t *testing.T) {
	secrets := inboundtest.FakeSecrets{}

	innerToken, _ := secrets.Encrypt([]byte("my-api-token"))
	inner := Config{
		Version:           ConfigVersion,
		AuthMode:          AuthModeAPIToken,
		Subdomain:         "acme",
		Email:             "admin@acme.com",
		APITokenEncrypted: innerToken,
		StartFrom:         "now",
	}
	raw, _ := json.Marshal(inner) // ptrext:allow json-marshal
	encrypted, _ := secrets.Encrypt(raw)

	cfg, cred, err := parseConfig(encrypted, secrets)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Subdomain != "acme" {
		t.Errorf("Subdomain = %q, want acme", cfg.Subdomain)
	}
	if cfg.AuthMode != AuthModeAPIToken {
		t.Errorf("AuthMode = %q, want %q", cfg.AuthMode, AuthModeAPIToken)
	}
	if cred.Mode != AuthModeAPIToken {
		t.Errorf("cred.Mode = %q, want %q", cred.Mode, AuthModeAPIToken)
	}
	if string(cred.APIToken) != "my-api-token" {
		t.Errorf("cred.APIToken = %q, want my-api-token", cred.APIToken)
	}
	if cred.Email != "admin@acme.com" {
		t.Errorf("cred.Email = %q, want admin@acme.com", cred.Email)
	}
}

func TestParseConfig_OAuth(t *testing.T) {
	secrets := inboundtest.FakeSecrets{}

	tokenJSON, _ := json.Marshal(oauthToken{AccessToken: "bearer-token-123"}) // ptrext:allow json-marshal
	innerOAuth, _ := secrets.Encrypt(tokenJSON)
	inner := Config{
		Version:             ConfigVersion,
		AuthMode:            AuthModeOAuth,
		Subdomain:           "acme",
		OAuthTokenEncrypted: innerOAuth,
		StartFrom:           "full",
	}
	raw, _ := json.Marshal(inner) // ptrext:allow json-marshal
	encrypted, _ := secrets.Encrypt(raw)

	cfg, cred, err := parseConfig(encrypted, secrets)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.AuthMode != AuthModeOAuth {
		t.Errorf("AuthMode = %q, want %q", cfg.AuthMode, AuthModeOAuth)
	}
	if cred.Mode != AuthModeOAuth {
		t.Errorf("cred.Mode = %q, want %q", cred.Mode, AuthModeOAuth)
	}
	// Credential now has individual fields instead of an oauthJSON blob.
	if cred.AccessToken != "bearer-token-123" {
		t.Errorf("AccessToken = %q, want bearer-token-123", cred.AccessToken)
	}
}

func TestParseConfig_InvalidVersion(t *testing.T) {
	secrets := inboundtest.FakeSecrets{}

	inner := Config{
		Version:   99,
		AuthMode:  AuthModeAPIToken,
		Subdomain: "acme",
	}
	raw, _ := json.Marshal(inner) // ptrext:allow json-marshal
	encrypted, _ := secrets.Encrypt(raw)

	_, _, err := parseConfig(encrypted, secrets)
	if err == nil {
		t.Fatal("expected error for invalid version")
	}
	if got := err.Error(); got != "zendesk: unsupported config version" {
		t.Errorf("error = %q", got)
	}
}

func TestParseConfig_MissingSubdomain(t *testing.T) {
	secrets := inboundtest.FakeSecrets{}

	inner := Config{
		Version:  ConfigVersion,
		AuthMode: AuthModeAPIToken,
	}
	raw, _ := json.Marshal(inner) // ptrext:allow json-marshal
	encrypted, _ := secrets.Encrypt(raw)

	_, _, err := parseConfig(encrypted, secrets)
	if err == nil {
		t.Fatal("expected error for missing subdomain")
	}
}

func TestParseConfig_MissingToken(t *testing.T) {
	secrets := inboundtest.FakeSecrets{}

	inner := Config{
		Version:   ConfigVersion,
		AuthMode:  AuthModeAPIToken,
		Subdomain: "acme",
		Email:     "admin@acme.com",
		// No APITokenEncrypted
	}
	raw, _ := json.Marshal(inner) // ptrext:allow json-marshal
	encrypted, _ := secrets.Encrypt(raw)

	_, _, err := parseConfig(encrypted, secrets)
	if err == nil {
		t.Fatal("expected error for missing api_token")
	}
}

func TestParseConfig_MissingEmail(t *testing.T) {
	secrets := inboundtest.FakeSecrets{}

	innerToken, _ := secrets.Encrypt([]byte("token"))
	inner := Config{
		Version:           ConfigVersion,
		AuthMode:          AuthModeAPIToken,
		Subdomain:         "acme",
		APITokenEncrypted: innerToken,
		// No Email
	}
	raw, _ := json.Marshal(inner) // ptrext:allow json-marshal
	encrypted, _ := secrets.Encrypt(raw)

	_, _, err := parseConfig(encrypted, secrets)
	if err == nil {
		t.Fatal("expected error for missing email")
	}
}

func TestParseConfig_UnsupportedAuthMode(t *testing.T) {
	secrets := inboundtest.FakeSecrets{}

	inner := Config{
		Version:   ConfigVersion,
		AuthMode:  "basic",
		Subdomain: "acme",
	}
	raw, _ := json.Marshal(inner) // ptrext:allow json-marshal
	encrypted, _ := secrets.Encrypt(raw)

	_, _, err := parseConfig(encrypted, secrets)
	if err == nil {
		t.Fatal("expected error for unsupported auth_mode")
	}
}

func TestParseConfig_DecryptFailure(t *testing.T) {
	secrets := inboundtest.FakeSecrets{}
	// Pass raw bytes that can't be decrypted (too short for FakeSecrets marker).
	_, _, err := parseConfig([]byte{0x00}, secrets)
	if err == nil {
		t.Fatal("expected error for decrypt failure")
	}
}
