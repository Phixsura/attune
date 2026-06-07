package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/lark"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	larkrepo "github.com/Phixsura/attune/internal/repo/lark"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// OAuthHandler wires /install/start + /install/callback. It owns:
// - building the Lark authorize URL with state for CSRF protection
// - exchanging callback codes for user + tenant identity
// - upserting tenants / tenant_users / tenant_lark_install in one
// callback transaction-equivalent (each call is its own short tx)
// - signing the session cookie + redirecting back to /console/
//
// Keep this struct concrete — Future RBAC may swap a different identity
// provider, but the console is Lark-only today and YAGNI applies.
type OAuthHandler struct {
	signer       *session.Signer
	lark         *lark.Client
	tenants      *tenant.TenantRepo
	users        *tenant.TenantUserRepo
	installs     *larkrepo.LarkInstallRepo
	appID        string
	baseURL      string // origin where /console lives, e.g. https://attune.app
	authorizeURL string // Lark authorize endpoint
}

func NewOAuthHandler(
	signer *session.Signer,
	larkClient *lark.Client,
	tenants *tenant.TenantRepo,
	users *tenant.TenantUserRepo,
	installs *larkrepo.LarkInstallRepo,
	appID, baseURL string,
) *OAuthHandler {
	return ptrext.Of(OAuthHandler{
		signer:       signer,
		lark:         larkClient,
		tenants:      tenants,
		users:        users,
		installs:     installs,
		appID:        appID,
		baseURL:      strings.TrimRight(baseURL, "/"),
		authorizeURL: "https://open.feishu.cn/open-apis/authen/v1/authorize",
	})
}

// Start handles GET /fb/v1/console/install/start.
// Builds the Lark authorize URL with a signed state cookie + 302s.
func (h *OAuthHandler) Start(w http.ResponseWriter, r *http.Request) {
	const where = "console.OAuthHandler.Start"
	ctx := r.Context()
	// state = random nonce + base64-encoded redirect_uri (post-login).
	// Persist the nonce in a short-lived cookie so the callback can
	// verify the round-trip without server-side state storage.
	nonce, err := randomNonce(24)
	if err != nil {
		logext.Errorf(ctx, "[%s] randomNonce failed,err:%+v", where, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "failed to generate state")
		return
	}
	postLogin := r.URL.Query().Get("redirect_uri")
	if postLogin == "" || !isSafeRelativeURL(postLogin) {
		postLogin = "/console/"
	}
	state := nonce + ":" + base64.RawURLEncoding.EncodeToString([]byte(postLogin))

	http.SetCookie(w, ptrext.Of(http.Cookie{
		Name:     session.OAuthStateCookie,
		Value:    nonce,
		Path:     "/fb/v1/console/install/",
		HttpOnly: true,
		Secure:   !h.signer.Insecure(),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(session.OAuthStateTTL),
	}))

	q := url.Values{}
	q.Set("app_id", h.appID)
	q.Set("redirect_uri", h.baseURL+"/fb/v1/console/install/callback")
	q.Set("state", state)
	logext.Infof(ctx, "[%s] OK redirect to lark,post_login:%s", where, postLogin)
	http.Redirect(w, r, h.authorizeURL+"?"+q.Encode(), http.StatusFound)
}

// Callback handles GET /fb/v1/console/install/callback.
// On success: upserts tenant + user + install rows, issues session
// cookie, 302s to the redirect_uri embedded in state.
func (h *OAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	const where = "console.OAuthHandler.Callback"
	ctx := r.Context()
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		logext.Warnf(ctx, "[%s] reject: missing code/state", where)
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "missing code or state")
		return
	}
	logext.Infof(ctx, "[%s] start", where)
	// Verify state nonce against cookie.
	stateNonce, postLogin, err := parseState(state)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: parse state failed,err:%s", where, err.Error())
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "invalid state")
		return
	}
	ck, err := r.Cookie(session.OAuthStateCookie)
	if err != nil || ck.Value != stateNonce {
		logext.Warnf(ctx, "[%s] reject: state cookie mismatch", where)
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "state cookie mismatch — please retry login")
		return
	}
	// Wipe the state cookie — it has served its purpose.
	http.SetCookie(w, ptrext.Of(http.Cookie{
		Name: session.OAuthStateCookie, Value: "", Path: "/fb/v1/console/install/",
		HttpOnly: true, Secure: !h.signer.Insecure(), SameSite: http.SameSiteLaxMode, MaxAge: -1,
	}))

	// 1-5. Exchange code, upsert tenant/user/install.
	tenantID, userID, created, err := h.resolveAndUpsert(ctx, code)
	if err != nil {
		slog.ErrorContext(ctx, "oauth: exchange/upsert failed", "err", err)
		respond.Error(ctx, w, http.StatusBadGateway, err.Error(), "Lark token exchange or upsert failed")
		return
	}
	// 6. Sign session, redirect.
	if err := h.signer.IssueSessionCookie(w, tenantID, userID); err != nil {
		slog.ErrorContext(ctx, "oauth: sign session", "err", err)
		logext.Errorf(ctx, "[%s] signer.IssueSessionCookie failed,user_id:%s,err:%+v",
			where, userID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "session_sign_failed", "failed to sign session")
		return
	}
	slog.InfoContext(ctx, "console: oauth login",
		"tenant_id", tenantID, "user_id", userID, "first_install", created)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,user_id:%s,first_install:%t",
		where, tenantID, userID, created)
	// postLogin is extracted from the OAuth state nonce we ourselves
	// generated in Begin(), verified against the state cookie above.
	// Validate it is a same-origin path and redirect.
	if !redirectIsSafe(h.baseURL, postLogin) {
		logext.Errorf(ctx, "[%s] reject: redirect escapes base URL", where)
		respond.Error(ctx, w, http.StatusInternalServerError, "redirect_failed", "redirect failed")
		return
	}
	base, err := url.Parse(h.baseURL)
	if err != nil {
		logext.Errorf(ctx, "[%s] reject: invalid base URL", where)
		respond.Error(ctx, w, http.StatusInternalServerError, "redirect_failed", "redirect failed")
		return
	}
	// Parse postLogin in isolation so we can lift its Path / RawQuery /
	// Fragment, then build a fresh target URL whose Scheme and Host are
	// explicitly inherited from baseURL. This makes it structurally
	// impossible for postLogin to influence the redirect origin — and
	// it gives CodeQL a sanitisation path it can track (the user-tainted
	// rel.Path/RawQuery/Fragment never reach Scheme/Host).
	rel, err := url.Parse(strings.ReplaceAll(postLogin, "\\", "/"))
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: invalid post-login redirect,err:%s", where, err.Error())
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "invalid state")
		return
	}
	if !strings.HasPrefix(rel.Path, "/console") {
		logext.Errorf(ctx, "[%s] reject: redirect path outside /console", where)
		respond.Error(ctx, w, http.StatusInternalServerError, "redirect_failed", "redirect failed")
		return
	}
	dst := ptrext.Of(url.URL{
		Scheme:   base.Scheme,
		Host:     base.Host,
		Path:     rel.Path,
		RawQuery: rel.RawQuery,
		Fragment: rel.Fragment,
	})
	http.Redirect(w, r, dst.String(), http.StatusFound)
}

// resolveAndUpsert performs the Lark OAuth code exchange followed by
// upserting tenant, user, and install rows. Extracted to keep Callback
// under the NLOC=100 gate.
func (h *OAuthHandler) resolveAndUpsert(ctx context.Context, code string) (tenantID, userID string, created bool, err error) {
	// 1. Code → user_access_token + tenant_key + open_id
	tok, err := h.lark.ExchangeUserCode(ctx, code)
	if err != nil {
		return "", "", false, fmt.Errorf("lark_exchange_failed")
	}
	// 2. user_access_token → display info
	info, err := h.lark.GetUserInfo(ctx, tok.AccessToken)
	if err != nil {
		return "", "", false, fmt.Errorf("lark_userinfo_failed")
	}
	// 3. Upsert tenant by lark_tenant_key (creates row on first install)
	defaultSlug := "lark-" + shortHash(tok.TenantKey)
	tenantID, created, err = h.tenants.UpsertByLarkKey(ctx, tok.TenantKey, info.Name+"'s workspace", defaultSlug)
	if err != nil {
		return "", "", false, fmt.Errorf("tenant_upsert_failed")
	}
	// 4. Upsert user. First user of a brand-new tenant is admin.
	role := "member"
	if created {
		role = "admin"
	}
	userID, err = h.users.Upsert(ctx, tenantID, tok.OpenID, info.Name, info.AvatarURL, role)
	if err != nil {
		return "", "", false, fmt.Errorf("user_upsert_failed")
	}
	// 5. Persist Lark install (tokens for outbound API calls).
	now := time.Now()
	if err := h.installs.Upsert(ctx, ptrext.Of(larkrepo.LarkInstall{
		TenantID:              tenantID,
		LarkTenantKey:         tok.TenantKey,
		AppID:                 h.appID,
		AccessToken:           tok.AccessToken,
		AccessTokenExpiresAt:  now.Add(time.Duration(tok.AccessExpiresIn) * time.Second),
		RefreshToken:          tok.RefreshToken,
		RefreshTokenExpiresAt: now.Add(time.Duration(tok.RefreshExpiresIn) * time.Second),
		Scopes:                tok.Scope,
	})); err != nil {
		return "", "", false, fmt.Errorf("install_upsert_failed")
	}
	return tenantID, userID, created, nil
}

// redirectIsSafe validates that a redirect path combined with the
// configured base URL stays within the same origin. postLogin originates
// from the OAuth state we ourselves generated, so it is trusted content,
// but CodeQL demands explicit origin validation.
//
// Second-character check: both '/' (protocol-relative URL) and '\\'
// (browser-quirk path that some clients interpret as host) must be
// rejected at position 1 — otherwise "/\evil.com" would slip through
// as a same-origin path here and become an open redirect downstream.
func redirectIsSafe(baseURL, postLogin string) bool {
	// postLogin must be a non-empty path that starts with a single '/',
	// rejecting both '//host' (protocol-relative) and '/\host'
	// (browser-quirk authority).
	if len(postLogin) == 0 || postLogin[0] != '/' {
		return false
	}
	if len(postLogin) >= 2 && (postLogin[1] == '/' || postLogin[1] == '\\') {
		return false
	}
	// It must not contain newlines or other control characters that could
	// be used for header injection.
	for _, r := range postLogin {
		if r < 32 || r > 126 {
			return false
		}
	}
	// The combined URL must stay within the configured baseURL origin.
	base, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	combined, err := url.Parse(baseURL + postLogin)
	if err != nil {
		return false
	}
	return combined.Scheme == base.Scheme && combined.Host == base.Host
}

func randomNonce(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func parseState(state string) (nonce, postLogin string, err error) {
	parts := strings.SplitN(state, ":", 2)
	if len(parts) != 2 {
		return "", "", errors.New("missing colon")
	}
	dec, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("decode redirect_uri: %w", err)
	}
	pl := string(dec)
	if !isSafeRelativeURL(pl) {
		return "", "", errors.New("redirect_uri must be relative under /console")
	}
	return parts[0], pl, nil
}

// isSafeRelativeURL allows only relative paths under /console — prevents
// open-redirect attacks where state could be crafted to bounce a user
// to an attacker domain after login.
//
// Second-character check parallels redirectIsSafe: reject both
// '//host' and '/\host' authority-style paths.
func isSafeRelativeURL(u string) bool {
	if len(u) == 0 || u[0] != '/' {
		return false
	}
	if len(u) >= 2 && (u[1] == '/' || u[1] == '\\') {
		return false
	}
	if !strings.HasPrefix(u, "/console") {
		return false
	}
	return true
}

func shortHash(s string) string {
	// Cheap deterministic slug — first 8 chars of base64 of bytes.
	// Not cryptographic; just unique-enough to seed a default slug.
	h := []byte(s)
	if len(h) > 8 {
		h = h[:8]
	}
	return strings.ToLower(base64.RawURLEncoding.EncodeToString(h))
}
