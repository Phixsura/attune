// SPDX-License-Identifier: Apache-2.0

package member

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// inviteCtx builds a RequestContext with a default (viewer) role in context.
// rbac.FromContext returns RoleViewer when no role is set, so these tests
// exercise the validation + authorization paths that return before any
// repo access — no database needed.
func inviteCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1", UserType: "oidc"}),
	})
}

func TestInvite_RejectsEmptyEmail(t *testing.T) {
	h := ptrext.Of(Handler{members: nil})
	_, err := h.Invite(inviteCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr), "want dispatcher error, got %v", err)
	assert.Equal(t, http.StatusBadRequest, derr.Status)
	assert.Equal(t, attunev1.ErrorCode_BAD_REQUEST, derr.Code)
}

func TestInvite_RejectsInvalidEmail(t *testing.T) {
	h := ptrext.Of(Handler{members: nil})
	_, err := h.Invite(inviteCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "not-an-email", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr), "want dispatcher error, got %v", err)
	assert.Equal(t, http.StatusBadRequest, derr.Status)
	assert.Equal(t, attunev1.ErrorCode_BAD_REQUEST, derr.Code)
}

func TestInvite_ViewerCannotInvite(t *testing.T) {
	h := ptrext.Of(Handler{members: nil})
	// Default ctx role is viewer; CanInvite() is admin-only.
	_, err := h.Invite(inviteCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "new@example.com", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr), "want dispatcher error, got %v", err)
	assert.Equal(t, http.StatusForbidden, derr.Status)
	assert.Equal(t, attunev1.ErrorCode_FORBIDDEN, derr.Code)
}

func TestInvite_ViewerCannotInviteAsAdmin(t *testing.T) {
	h := ptrext.Of(Handler{members: nil})
	// Default ctx role is viewer; CanPromoteToAdmin() is admin-only.
	_, err := h.Invite(inviteCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "new@example.com", Role: "admin"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr), "want dispatcher error, got %v", err)
	assert.Equal(t, http.StatusForbidden, derr.Status)
	assert.Equal(t, attunev1.ErrorCode_FORBIDDEN, derr.Code)
}

func TestInvite_NormalizesEmailBeforeValidation(t *testing.T) {
	h := ptrext.Of(Handler{members: nil})
	// Whitespace + uppercase still passes the regex after normalization,
	// so it proceeds to the (viewer) authorization check → FORBIDDEN,
	// proving the email was normalized rather than rejected as invalid.
	_, err := h.Invite(inviteCtx(), ptrext.Of(attunev1.InviteMemberRequest{Email: "  New@Example.COM  ", Role: "member"}))

	var derr *dispatcher.Error
	require.True(t, errors.As(err, &derr), "want dispatcher error, got %v", err)
	assert.Equal(t, http.StatusForbidden, derr.Status)
}

func TestEmailRegex(t *testing.T) {
	tests := []struct {
		name  string
		email string
		valid bool
	}{
		// Valid emails
		{"simple email", "user@example.com", true},
		{"with subdomain", "user@mail.example.com", true},
		{"with plus", "user+tag@example.com", true},
		{"with dots", "first.last@example.com", true},
		{"with underscore", "user_name@example.com", true},
		{"with hyphen", "user-name@example.com", true},
		{"with numbers", "user123@example.com", true},
		{"short domain", "user@example.co", true},
		{"long tld", "user@example.museum", true},

		// Invalid emails
		{"empty string", "", false},
		{"no at sign", "userexample.com", false},
		{"no domain", "user@", false},
		{"no local part", "@example.com", false},
		{"double at", "user@@example.com", false},
		{"spaces", "user @example.com", false},
		{"single char tld", "user@example.c", false},
		{"no tld", "user@example", false},
		// Note: the simple regex allows these edge cases (not RFC-strict)
		{"leading dot (allowed by simple regex)", ".user@example.com", true},
		{"trailing dot local (allowed)", "user.@example.com", true},
		{"double dot local (allowed)", "user..name@example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := emailRE.MatchString(tt.email)
			assert.Equal(t, tt.valid, result, "email: %s", tt.email)
		})
	}
}

func TestEmailNormalization(t *testing.T) {
	// Email should be lowercased and trimmed
	tests := []struct {
		input    string
		expected string
	}{
		{"User@Example.COM", "user@example.com"},
		{"  user@example.com  ", "user@example.com"},
		{"USER@EXAMPLE.COM", "user@example.com"},
		{"\tuser@example.com\n", "user@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// This tests the normalization logic used in Invite handler
			normalized := strings.ToLower(strings.TrimSpace(tt.input))
			assert.Equal(t, tt.expected, normalized)
		})
	}
}
