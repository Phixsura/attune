// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	portalVisitorCookiePrefix = "attune_portal_visitor"
	portalVisitorCookieTTL    = 180 * 24 * time.Hour
)

type visitorSecretStore interface {
	EncryptValue(plaintext, aad []byte) (secretstore.EncryptedValue, error)
	DecryptValue(value secretstore.EncryptedValue, aad []byte) ([]byte, error)
}

type portalVisitorPayload struct {
	Version   int    `json:"v"`
	VisitorID string `json:"visitor_id"`
	ExpiresAt int64  `json:"expires_at"`
}

func loadOrMintPortalVisitor(
	r *http.Request,
	secrets visitorSecretStore,
	tenantScope string,
	refresh bool,
) (string, *http.Cookie, error) {
	if r == nil {
		return "", nil, errors.New("portal visitor request unavailable")
	}
	tenantScope = strings.TrimSpace(tenantScope)
	if tenantScope == "" {
		return "", nil, errors.New("portal visitor tenant scope required")
	}
	if secrets == nil {
		return "", nil, errors.New("portal visitor secret store not configured")
	}
	now := time.Now().UTC()
	cookieName := portalVisitorCookieName(tenantScope)
	if cookie, err := r.Cookie(cookieName); err == nil {
		if visitorID, refreshed, ok, err := decodePortalVisitorCookie(cookie.Value, secrets, tenantScope, now, refresh); err != nil {
			return "", nil, err
		} else if ok {
			return visitorID, refreshed, nil
		}
	}
	visitorID := uuid.NewString()
	cookie, err := issuePortalVisitorCookie(secrets, tenantScope, visitorID, now)
	if err != nil {
		return "", nil, err
	}
	return visitorID, cookie, nil
}

func ensurePortalVisitor(
	r *http.Request,
	setCookie func(*http.Cookie),
	secrets visitorSecretStore,
	tenantScope string,
	refresh bool,
) (string, error) {
	visitorID, cookie, err := loadOrMintPortalVisitor(r, secrets, tenantScope, refresh)
	if err != nil {
		return "", err
	}
	if cookie != nil && setCookie != nil {
		setCookie(cookie)
	}
	return visitorID, nil
}

func decodePortalVisitorCookie(raw string, secrets visitorSecretStore, tenantScope string, now time.Time, refresh bool) (string, *http.Cookie, bool, error) {
	plaintext, err := decryptPortalVisitor(raw, secrets, tenantScope)
	if err != nil {
		return "", nil, false, nil
	}
	var payload portalVisitorPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return "", nil, false, nil
	}
	if payload.VisitorID == "" || payload.ExpiresAt <= now.Unix() {
		return "", nil, false, nil
	}
	if !refresh {
		return payload.VisitorID, nil, true, nil
	}
	cookie, err := issuePortalVisitorCookie(secrets, tenantScope, payload.VisitorID, now)
	if err != nil {
		return "", nil, false, err
	}
	return payload.VisitorID, cookie, true, nil
}

func issuePortalVisitorCookie(secrets visitorSecretStore, tenantScope, visitorID string, now time.Time) (*http.Cookie, error) {
	encoded, err := encryptPortalVisitor(secrets, tenantScope, visitorID, now.Add(portalVisitorCookieTTL))
	if err != nil {
		return nil, err
	}
	return ptrext.Of(http.Cookie{
		Name:     portalVisitorCookieName(tenantScope),
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  now.Add(portalVisitorCookieTTL),
	}), nil
}

func encryptPortalVisitor(secrets visitorSecretStore, tenantScope, visitorID string, expiresAt time.Time) (string, error) {
	payload := portalVisitorPayload{
		Version:   1,
		VisitorID: visitorID,
		ExpiresAt: expiresAt.Unix(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encrypted, err := secrets.EncryptValue(raw, portalVisitorAAD(tenantScope))
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encrypted.Ciphertext), nil
}

func decryptPortalVisitor(raw string, secrets visitorSecretStore, tenantScope string) ([]byte, error) {
	ciphertext, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	return secrets.DecryptValue(secretstore.EncryptedValue{Ciphertext: ciphertext}, portalVisitorAAD(tenantScope))
}

func portalVisitorAAD(tenantScope string) []byte {
	return []byte("portal-visitor:" + strings.TrimSpace(tenantScope))
}

func portalVisitorCookieName(tenantScope string) string {
	scope := strings.TrimSpace(tenantScope)
	if scope == "" {
		return portalVisitorCookiePrefix
	}
	sum := sha256.Sum256([]byte(scope))
	return portalVisitorCookiePrefix + "_" + hex.EncodeToString(sum[:8])
}
