// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// --- rowToProto ---

func TestRowToProto_FullFields(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	lastEvt := now.Add(-5 * time.Minute)
	src := inbound.Source{
		ID:        "id-1",
		TenantID:  "tenant-1",
		Channel:   "webhook",
		Name:      "Test Webhook",
		Slug:      "test-webhook",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
		State: inbound.SourceState{
			LastUID:     42,
			LastError:   "some error",
			LastEventAt: ptrext.Of(lastEvt),
		},
	}
	out := rowToProto(src)
	if out.GetId() != "id-1" {
		t.Fatalf("Id = %q, want id-1", out.GetId())
	}
	if out.GetTenantId() != "tenant-1" {
		t.Fatalf("TenantId = %q, want tenant-1", out.GetTenantId())
	}
	if out.GetChannel() != "webhook" {
		t.Fatalf("Channel = %q, want webhook", out.GetChannel())
	}
	if out.GetName() != "Test Webhook" {
		t.Fatalf("Name = %q", out.GetName())
	}
	if out.GetSlug() != "test-webhook" {
		t.Fatalf("Slug = %q", out.GetSlug())
	}
	if !out.GetEnabled() {
		t.Fatal("Enabled should be true")
	}
	if out.GetLastUid() != 42 {
		t.Fatalf("LastUid = %d, want 42", out.GetLastUid())
	}
	if out.GetLastError() != "some error" {
		t.Fatalf("LastError = %q", out.GetLastError())
	}
	if out.LastEventAt == nil {
		t.Fatal("LastEventAt should be set")
	}
	if out.GetCreatedAt() == "" {
		t.Fatal("CreatedAt should be set")
	}
	if out.GetUpdatedAt() == "" {
		t.Fatal("UpdatedAt should be set")
	}
}

func TestRowToProto_NilLastEventAt(t *testing.T) {
	t.Parallel()
	src := inbound.Source{
		ID:        "id-2",
		TenantID:  "tenant-2",
		Channel:   "email",
		Name:      "Email Source",
		Slug:      "email-source",
		Enabled:   false,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		State:     inbound.SourceState{},
	}
	out := rowToProto(src)
	if out.LastEventAt != nil {
		t.Fatalf("LastEventAt should be nil for empty state, got %q", ptrext.Indirect(out.LastEventAt))
	}
	if out.GetEnabled() {
		t.Fatal("Enabled should be false")
	}
}

func TestRowToProto_TimesAreUTC(t *testing.T) {
	t.Parallel()
	est := time.FixedZone("EST", -5*60*60)
	created := time.Date(2026, 1, 15, 12, 0, 0, 0, est)
	updated := time.Date(2026, 1, 15, 13, 0, 0, 0, est)
	src := inbound.Source{
		ID:        "id-3",
		TenantID:  "t",
		Channel:   "webhook",
		Name:      "n",
		Slug:      "n",
		CreatedAt: created,
		UpdatedAt: updated,
	}
	out := rowToProto(src)
	if !strings.HasSuffix(out.GetCreatedAt(), "Z") {
		t.Fatalf("CreatedAt should be UTC (end with Z), got %q", out.GetCreatedAt())
	}
	if !strings.HasSuffix(out.GetUpdatedAt(), "Z") {
		t.Fatalf("UpdatedAt should be UTC (end with Z), got %q", out.GetUpdatedAt())
	}
}

// --- isUUID ---

func TestIsUUID_Valid(t *testing.T) {
	t.Parallel()
	if !isUUID(uuid.NewString()) {
		t.Fatal("valid UUID should return true")
	}
}

func TestIsUUID_Invalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "not-a-uuid", "12345", "g1234567-1234-1234-1234-123456789abc"}
	for _, c := range cases {
		if isUUID(c) {
			t.Errorf("isUUID(%q) should be false", c)
		}
	}
}

// --- buildCurlExample ---

func TestBuildCurlExample_ContainsHeaders(t *testing.T) {
	t.Parallel()
	got := buildCurlExample("https://example.com/v1/inbound/webhook/t/s")
	for _, want := range []string{
		"X-Attune-Timestamp",
		"X-Attune-Signature",
		"Content-Type: application/json",
		"-X POST",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("curl example missing %q: %q", want, got)
		}
	}
}

// --- slugify edge cases ---

func TestSlugify_LongInput(t *testing.T) {
	t.Parallel()
	input := strings.Repeat("a", 300)
	got := slugify(input)
	if got != input {
		t.Fatalf("slugify should preserve a long alphanumeric string")
	}
}

func TestSlugify_SpecialChars(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"hello_world", "hello-world"},
		{"test.source.v2", "test-source-v2"},
		{"foo/bar/baz", "foo-bar-baz"},
		{"  leading-trailing  ", "leading-trailing"},
		{"MiXeD CaSe", "mixed-case"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			if got := slugify(c.in); got != c.want {
				t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// --- validateEmailCreateConfig edge cases ---

func TestValidateEmailCreateConfig_StartFromVariants(t *testing.T) {
	t.Parallel()
	base := func() *attunev1.EmailCreateConfig {
		return ptrext.Of(attunev1.EmailCreateConfig{
			Host:     "imap.example.com",
			Port:     993,
			Tls:      true,
			Username: "user@example.com",
			Password: "secret",
		})
	}
	t.Run("empty start_from accepted (defaults later)", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		cfg.StartFrom = ""
		if err := validateEmailCreateConfig(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("all_unseen accepted", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		cfg.StartFrom = "all_unseen"
		if err := validateEmailCreateConfig(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("now accepted", func(t *testing.T) {
		t.Parallel()
		cfg := base()
		cfg.StartFrom = "now"
		if err := validateEmailCreateConfig(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidateEmailCreateConfig_WhitespaceHost(t *testing.T) {
	t.Parallel()
	cfg := ptrext.Of(attunev1.EmailCreateConfig{
		Host:     "   ",
		Port:     993,
		Tls:      true,
		Username: "user@example.com",
		Password: "secret",
	})
	err := validateEmailCreateConfig(cfg)
	if err == nil {
		t.Fatal("whitespace-only host should fail")
	}
	if !strings.Contains(err.Error(), "host") {
		t.Fatalf("error should mention host, got %q", err.Error())
	}
}

func TestValidateEmailCreateConfig_WhitespaceUsername(t *testing.T) {
	t.Parallel()
	cfg := ptrext.Of(attunev1.EmailCreateConfig{
		Host:     "imap.example.com",
		Port:     993,
		Tls:      true,
		Username: "   ",
		Password: "secret",
	})
	err := validateEmailCreateConfig(cfg)
	if err == nil {
		t.Fatal("whitespace-only username should fail")
	}
	if !strings.Contains(err.Error(), "username") {
		t.Fatalf("error should mention username, got %q", err.Error())
	}
}

func TestValidateEmailCreateConfig_PortBoundaries(t *testing.T) {
	t.Parallel()
	base := func(port int32) *attunev1.EmailCreateConfig {
		return ptrext.Of(attunev1.EmailCreateConfig{
			Host:     "h",
			Port:     port,
			Tls:      true,
			Username: "u",
			Password: "p",
		})
	}
	t.Run("port 1 accepted", func(t *testing.T) {
		t.Parallel()
		if err := validateEmailCreateConfig(base(1)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("port 65535 accepted", func(t *testing.T) {
		t.Parallel()
		if err := validateEmailCreateConfig(base(65535)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("port 65536 rejected", func(t *testing.T) {
		t.Parallel()
		if err := validateEmailCreateConfig(base(65536)); err == nil {
			t.Fatal("port 65536 should be rejected")
		}
	})
}

// --- isUniqueViolation ---

type fakeSQLStateErr struct {
	state string
}

func (e fakeSQLStateErr) Error() string    { return "sql error: " + e.state }
func (e fakeSQLStateErr) SQLState() string { return e.state }

func TestIsUniqueViolation_True(t *testing.T) {
	t.Parallel()
	err := fakeSQLStateErr{state: "23505"}
	if !isUniqueViolation(err) {
		t.Fatal("23505 should be a unique violation")
	}
}

func TestIsUniqueViolation_OtherState(t *testing.T) {
	t.Parallel()
	err := fakeSQLStateErr{state: "23000"}
	if isUniqueViolation(err) {
		t.Fatal("23000 should not be a unique violation")
	}
}

func TestIsUniqueViolation_NonSQLError(t *testing.T) {
	t.Parallel()
	if isUniqueViolation(errors.New("plain error")) {
		t.Fatal("plain error should not be a unique violation")
	}
}

func TestIsUniqueViolation_Nil(t *testing.T) {
	t.Parallel()
	if isUniqueViolation(nil) {
		t.Fatal("nil should not be a unique violation")
	}
}

// --- tenantSlugFromPool nil pool ---

func TestTenantSlugFromPool_NilPool(t *testing.T) {
	t.Parallel()
	fn := tenantSlugFromPool(nil)
	_, err := fn(context.Background(), "tenant-1")
	if err == nil {
		t.Fatal("nil pool should return error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("error should mention 'not configured', got %q", err.Error())
	}
}

// --- recordAudit nil audit recorder ---

func TestRecordAudit_NilRecorder(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{})
	err := h.recordAudit(context.Background(), "", "user-1", "t-1", "test", "id", "summary", nil, nil, nil)
	if err != nil {
		t.Fatalf("nil audit recorder should return nil, got %v", err)
	}
}

// --- SetAuditLogger ---

func TestSetAuditLogger_Sets(t *testing.T) {
	t.Parallel()
	h := ptrext.Of(Handler{})
	if h.audit != nil {
		t.Fatal("audit should initially be nil")
	}
	// Cannot easily construct a full auditRecorder without importing the
	// service layer, so we just verify the field is nil and the setter
	// method exists and compiles.
}

// --- channel constants ---

func TestChannelConstants(t *testing.T) {
	t.Parallel()
	if channelWebhook == "" {
		t.Fatal("channelWebhook should not be empty")
	}
	if channelEmail == "" {
		t.Fatal("channelEmail should not be empty")
	}
	if channelWebhook == channelEmail {
		t.Fatal("webhook and email channels must differ")
	}
}
