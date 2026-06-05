package console

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/infra/lark"
	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/repo"
)

// OAuthHandler wires /install/start + /install/callback. It owns:
//   - building the Lark authorize URL with state for CSRF protection
//   - exchanging callback codes for user + tenant identity
//   - upserting tenants / tenant_users / tenant_lark_install in one
//     callback transaction-equivalent (each call is its own short tx)
//   - signing the session cookie + redirecting back to /console/
//
// Keep this struct concrete — Wave 3 RBAC may swap a different identity
// provider, but Stage B is Lark-only and YAGNI applies.
type OAuthHandler struct {
	signer       *Signer
	lark         *lark.Client
	tenants      *repo.TenantRepo
	users        *repo.TenantUserRepo
	installs     *repo.LarkInstallRepo
	appID        string
	baseURL      string // origin where /console lives, e.g. https://attune.app
	authorizeURL string // Lark authorize endpoint
}

func NewOAuthHandler(
	signer *Signer,
	larkClient *lark.Client,
	tenants *repo.TenantRepo,
	users *repo.TenantUserRepo,
	installs *repo.LarkInstallRepo,
	appID, baseURL string,
) *OAuthHandler {
	return &OAuthHandler{
		signer:       signer,
		lark:         larkClient,
		tenants:      tenants,
		users:        users,
		installs:     installs,
		appID:        appID,
		baseURL:      strings.TrimRight(baseURL, "/"),
		authorizeURL: "https://open.feishu.cn/open-apis/authen/v1/authorize",
	}
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
		writeError(w, http.StatusInternalServerError, "internal", "无法生成 state")
		return
	}
	postLogin := r.URL.Query().Get("redirect_uri")
	if postLogin == "" || !isSafeRelativeURL(postLogin) {
		postLogin = "/console/"
	}
	state := nonce + ":" + base64.RawURLEncoding.EncodeToString([]byte(postLogin))

	http.SetCookie(w, &http.Cookie{
		Name:     OAuthStateCookie,
		Value:    nonce,
		Path:     "/fb/v1/console/install/",
		HttpOnly: true,
		Secure:   !h.signer.Insecure(),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(oauthStateTTL),
	})

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
		writeError(w, http.StatusBadRequest, "bad_request", "缺少 code 或 state")
		return
	}
	logext.Infof(ctx, "[%s] start", where)
	// Verify state nonce against cookie.
	stateNonce, postLogin, err := parseState(state)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: parse state failed,err:%s", where, err.Error())
		writeError(w, http.StatusBadRequest, "bad_request", "state 不合法")
		return
	}
	ck, err := r.Cookie(OAuthStateCookie)
	if err != nil || ck.Value != stateNonce {
		logext.Warnf(ctx, "[%s] reject: state cookie mismatch", where)
		writeError(w, http.StatusBadRequest, "bad_request", "state cookie 不匹配，请重新发起登录")
		return
	}
	// Wipe the state cookie — it has served its purpose.
	http.SetCookie(w, &http.Cookie{
		Name: OAuthStateCookie, Value: "", Path: "/fb/v1/console/install/",
		HttpOnly: true, Secure: !h.signer.Insecure(), SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})

	// 1. Code → user_access_token + tenant_key + open_id
	tok, err := h.lark.ExchangeUserCode(ctx, code)
	if err != nil {
		slog.ErrorContext(ctx, "oauth: exchange code failed", "err", err)
		logext.Errorf(ctx, "[%s] lark.ExchangeUserCode failed,err:%+v", where, err.Error())
		writeError(w, http.StatusBadGateway, "lark_exchange_failed", "向飞书换 token 失败")
		return
	}
	// 2. user_access_token → display info
	info, err := h.lark.GetUserInfo(ctx, tok.AccessToken)
	if err != nil {
		slog.ErrorContext(ctx, "oauth: user_info failed", "err", err)
		logext.Errorf(ctx, "[%s] lark.GetUserInfo failed,open_id:%s,err:%+v",
			where, tok.OpenID, err.Error())
		writeError(w, http.StatusBadGateway, "lark_userinfo_failed", "向飞书取用户信息失败")
		return
	}
	// 3. Upsert tenant by lark_tenant_key (creates row on first install)
	defaultSlug := "lark-" + shortHash(tok.TenantKey)
	tenantName := info.Name + " 的工作区"
	tenantID, created, err := h.tenants.UpsertByLarkKey(ctx, tok.TenantKey, tenantName, defaultSlug)
	if err != nil {
		slog.ErrorContext(ctx, "oauth: upsert tenant", "err", err)
		logext.Errorf(ctx, "[%s] tenants.UpsertByLarkKey failed,tenant_key:%s,err:%+v",
			where, tok.TenantKey, err.Error())
		writeError(w, http.StatusInternalServerError, "tenant_upsert_failed", "登记 tenant 失败")
		return
	}
	// 4. Upsert user. First user of a brand-new tenant is admin.
	role := "member"
	if created {
		role = "admin"
	}
	userID, err := h.users.Upsert(ctx, tenantID, tok.OpenID, info.Name, info.AvatarURL, role)
	if err != nil {
		slog.ErrorContext(ctx, "oauth: upsert user", "err", err)
		logext.Errorf(ctx, "[%s] users.Upsert failed,tenant_id:%s,open_id:%s,err:%+v",
			where, tenantID, tok.OpenID, err.Error())
		writeError(w, http.StatusInternalServerError, "user_upsert_failed", "登记用户失败")
		return
	}
	// 5. Persist Lark install (tokens for outbound API calls).
	now := time.Now()
	if err := h.installs.Upsert(ctx, &repo.LarkInstall{
		TenantID:              tenantID,
		LarkTenantKey:         tok.TenantKey,
		AppID:                 h.appID,
		AccessToken:           tok.AccessToken,
		AccessTokenExpiresAt:  now.Add(time.Duration(tok.AccessExpiresIn) * time.Second),
		RefreshToken:          tok.RefreshToken,
		RefreshTokenExpiresAt: now.Add(time.Duration(tok.RefreshExpiresIn) * time.Second),
		Scopes:                tok.Scope,
	}); err != nil {
		slog.ErrorContext(ctx, "oauth: upsert install", "err", err)
		logext.Errorf(ctx, "[%s] installs.Upsert failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		writeError(w, http.StatusInternalServerError, "install_upsert_failed", "登记 Lark install 失败")
		return
	}
	// 6. Sign session, redirect.
	if err := h.signer.IssueSessionCookie(w, tenantID, userID); err != nil {
		slog.ErrorContext(ctx, "oauth: sign session", "err", err)
		logext.Errorf(ctx, "[%s] signer.IssueSessionCookie failed,user_id:%s,err:%+v",
			where, userID, err.Error())
		writeError(w, http.StatusInternalServerError, "session_sign_failed", "签 session 失败")
		return
	}
	slog.InfoContext(ctx, "console: oauth login",
		"tenant_id", tenantID, "user_id", userID, "first_install", created)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,user_id:%s,first_install:%t",
		where, tenantID, userID, created)
	http.Redirect(w, r, h.baseURL+postLogin, http.StatusFound)
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
func isSafeRelativeURL(u string) bool {
	if u == "" {
		return false
	}
	if !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") {
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
