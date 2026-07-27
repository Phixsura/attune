// SPDX-License-Identifier: Apache-2.0

package intercom

import (
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/inbound/inboundtest"
)

func TestParseConfig_HappyPath(t *testing.T) {
	secrets := inboundtest.FakeSecrets{}
	tokenEnc, _ := secrets.Encrypt([]byte("tok-123"))
	blob := buildTestConfig(t, Config{
		Version: ConfigVersion, Region: "eu",
		AccessTokenEncrypted: tokenEnc,
		WorkspaceID:          "ws1", StartFrom: "now",
	}, secrets)

	cfg, token, err := parseConfig(blob, secrets)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Region != "eu" || cfg.WorkspaceID != "ws1" {
		t.Errorf("cfg = %+v", cfg)
	}
	if string(token) != "tok-123" {
		t.Errorf("token = %q", token)
	}
}

func TestParseConfig_Errors(t *testing.T) {
	secrets := inboundtest.FakeSecrets{}
	tokenEnc, _ := secrets.Encrypt([]byte("tok"))

	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"bad version", Config{Version: 99, Region: "us", AccessTokenEncrypted: tokenEnc}, "unsupported config version"},
		{"bad region", Config{Version: ConfigVersion, Region: "mars", AccessTokenEncrypted: tokenEnc}, "invalid region"},
		{"missing token", Config{Version: ConfigVersion, Region: "us"}, "access_token missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blob := buildTestConfig(t, tc.cfg, secrets)
			_, _, err := parseConfig(blob, secrets)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want contains %q", err, tc.want)
			}
		})
	}

	// Outer decrypt failure.
	if _, _, err := parseConfig([]byte{0xFF}, secrets); err == nil {
		t.Error("expected decrypt error")
	}
	// Inner token decrypt failure.
	blob := buildTestConfig(t, Config{
		Version: ConfigVersion, Region: "us",
		AccessTokenEncrypted: []byte{0xFF},
	}, secrets)
	if _, _, err := parseConfig(blob, secrets); err == nil || !strings.Contains(err.Error(), "decrypt access_token") {
		t.Errorf("err = %v", err)
	}
	// Malformed JSON inside envelope.
	badJSON, _ := secrets.Encrypt([]byte("{not json"))
	if _, _, err := parseConfig(badJSON, secrets); err == nil {
		t.Error("expected unmarshal error")
	}
}

func TestNormalizeFilterStates(t *testing.T) {
	got, err := normalizeFilterStates([]string{" Open ", "closed", "open", ""})
	if err != nil {
		t.Fatalf("normalizeFilterStates: %v", err)
	}
	if len(got) != 2 || got[0] != "open" || got[1] != "closed" {
		t.Errorf("got %v", got)
	}

	if _, err := normalizeFilterStates([]string{"bogus"}); err == nil {
		t.Error("expected error for invalid state")
	}

	empty, err := normalizeFilterStates(nil)
	if err != nil || empty != nil {
		t.Errorf("nil input → %v, %v", empty, err)
	}
}

func TestValidateConnConfig(t *testing.T) {
	in, err := ValidateConnConfig(" US ", " tok ", "", []string{"OPEN"}, nil, nil, 25)
	if err != nil {
		t.Fatalf("ValidateConnConfig: %v", err)
	}
	if in.Region != "us" || in.AccessToken != "tok" || in.StartFrom != "now" {
		t.Errorf("in = %+v", in)
	}
	if len(in.FilterStates) != 1 || in.FilterStates[0] != "open" {
		t.Errorf("FilterStates = %v", in.FilterStates)
	}
	if in.MaxDetailFetches != 25 {
		t.Errorf("MaxDetailFetches = %d", in.MaxDetailFetches)
	}

	if _, err := ValidateConnConfig("mars", "tok", "", nil, nil, nil, 0); err == nil {
		t.Error("bad region accepted")
	}
	if _, err := ValidateConnConfig("us", "", "", nil, nil, nil, 0); err == nil {
		t.Error("empty token accepted")
	}
	if _, err := ValidateConnConfig("us", "tok", "sometime", nil, nil, nil, 0); err == nil {
		t.Error("bad start_from accepted")
	}
	if _, err := ValidateConnConfig("us", "tok", "full", nil, nil, nil, -1); err == nil {
		t.Error("negative budget accepted")
	}
	if _, err := ValidateConnConfig("us", "tok", "", []string{"weird"}, nil, nil, 0); err == nil {
		t.Error("bad filter state accepted")
	}

	full, err := ValidateConnConfig("au", "tok", "full", nil, nil, nil, 0)
	if err != nil || full.StartFrom != "full" {
		t.Errorf("full = %+v, %v", full, err)
	}
}

func TestValidateConnConfig_TagFilters(t *testing.T) {
	in, err := ValidateConnConfig("us", "tok", "", nil,
		[]string{" feature-request ", "Billing", "feature-request", ""},
		[]string{"spam", " SPAM "}, 0)
	if err != nil {
		t.Fatalf("ValidateConnConfig: %v", err)
	}
	if len(in.FilterTags) != 2 || in.FilterTags[0] != "feature-request" || in.FilterTags[1] != "Billing" {
		t.Errorf("FilterTags = %v", in.FilterTags)
	}
	if len(in.FilterExcludeTags) != 1 || in.FilterExcludeTags[0] != "spam" {
		t.Errorf("FilterExcludeTags = %v", in.FilterExcludeTags)
	}
}
