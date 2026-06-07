// Package lark is a thin client over the Lark Open API endpoints attune
// actually needs. It is intentionally small — no global SDK
// abstractions, just typed wrappers around the 3 calls the console uses:
//
// - GetAppAccessToken — `auth/v3/app_access_token/internal`. Cached
// in-memory with expiry; refresh on demand.
// - ExchangeUserCode — `authen/v1/oidc/access_token`. Turns an OAuth
// `code` into a user_access_token + open_id + tenant_key.
// - GetUserInfo — `authen/v1/user_info` (using user_access_token).
// Returns name + avatar for /me rendering.
//
// A follow-up will add /chats, /im/v1/messages, etc. — each gets its own
// method here, never an inline call in handlers.
package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/logext"
)

// upstreamBodyLogCap caps the bytes logged when echoing a Lark API
// upstream request/response body. The Authorization header set in
// postJSON never enters the body and is never logged.
const upstreamBodyLogCap = 4096

const (
	defaultBase       = "https://open.feishu.cn"
	endpointAppToken  = "/open-apis/auth/v3/app_access_token/internal"
	endpointOIDCToken = "/open-apis/authen/v1/oidc/access_token"
	endpointUserInfo  = "/open-apis/authen/v1/user_info"
)

// Client is safe for concurrent use. Token cache is RWMutex-protected.
type Client struct {
	httpClient *http.Client
	baseURL    string
	appID      string
	appSecret  string

	mu             sync.RWMutex
	appAccessToken string
	appTokenExpiry time.Time
}

func New(appID, appSecret string) *Client {
	return &Client{
		httpClient: &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport), Timeout: 10 * time.Second},
		baseURL:    defaultBase,
		appID:      appID,
		appSecret:  appSecret,
	}
}

// UserTokenResponse is the decoded body of the OIDC exchange. Attune
// only needs the AccessToken + RefreshToken + their expiries + the open
// + tenant identities. The rest of Lark's payload (scope, token_type,
// etc.) is ignored here.
type UserTokenResponse struct {
	AccessToken      string
	AccessExpiresIn  int // seconds
	RefreshToken     string
	RefreshExpiresIn int // seconds
	OpenID           string
	TenantKey        string
	Scope            string
}

type UserInfo struct {
	OpenID    string
	Name      string
	AvatarURL string
	Email     string
}

// ExchangeUserCode swaps the OAuth `code` query param for a user-scoped
// token. Requires a valid app_access_token in the Authorization header.
func (c *Client) ExchangeUserCode(ctx context.Context, code string) (*UserTokenResponse, error) {
	const where = "lark.Client.ExchangeUserCode"
	logext.Infof(ctx, "[%s] start,code_len:%d", where, len(code))
	appToken, err := c.appAccessTokenLocked(ctx)
	if err != nil {
		logext.Errorf(ctx, "[%s] app token failed,err:%+v", where, err.Error())
		return nil, err
	}
	body := map[string]string{
		"grant_type": "authorization_code",
		"code":       code,
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			AccessToken      string `json:"access_token"`
			ExpiresIn        int    `json:"expires_in"`
			RefreshToken     string `json:"refresh_token"`
			RefreshExpiresIn int    `json:"refresh_expires_in"`
			OpenID           string `json:"open_id"`
			TenantKey        string `json:"tenant_key"`
			Scope            string `json:"scope"`
		} `json:"data"`
	}
	if err := c.postJSON(ctx, endpointOIDCToken, appToken, body, &resp); err != nil {
		logext.Errorf(ctx, "[%s] postJSON failed,err:%+v", where, err.Error())
		return nil, err
	}
	if resp.Code != 0 {
		logext.Warnf(ctx, "[%s] reject: lark code=%d,msg:%s", where, resp.Code, resp.Msg)
		return nil, fmt.Errorf("lark oidc exchange: code=%d msg=%q", resp.Code, resp.Msg)
	}
	logext.Infof(ctx, "[%s] OK,open_id:%s,tenant_key:%s", where,
		resp.Data.OpenID, resp.Data.TenantKey)
	return &UserTokenResponse{
		AccessToken:      resp.Data.AccessToken,
		AccessExpiresIn:  resp.Data.ExpiresIn,
		RefreshToken:     resp.Data.RefreshToken,
		RefreshExpiresIn: resp.Data.RefreshExpiresIn,
		OpenID:           resp.Data.OpenID,
		TenantKey:        resp.Data.TenantKey,
		Scope:            resp.Data.Scope,
	}, nil
}

// GetUserInfo uses a user_access_token (NOT app_access_token) to fetch
// the user's name + avatar. Called once at install time.
func (c *Client) GetUserInfo(ctx context.Context, userAccessToken string) (*UserInfo, error) {
	const where = "lark.Client.GetUserInfo"
	url := c.baseURL + endpointUserInfo
	logext.Infof(ctx, "[%s] upstream REQUEST,url:%s", where, url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logext.Errorf(ctx, "[%s] build request failed,err:%+v", where, err.Error())
		return nil, fmt.Errorf("build user_info request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+userAccessToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logext.Errorf(ctx, "[%s] upstream RESPONSE (transport err),err:%+v", where, err.Error())
		return nil, fmt.Errorf("call user_info: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	logext.Infof(ctx, "[%s] upstream RESPONSE,status:%d,body_bytes:%d,body:%s",
		where, resp.StatusCode, len(raw), truncateBytes(raw, upstreamBodyLogCap))
	var body struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OpenID    string `json:"open_id"`
			Name      string `json:"name"`
			AvatarURL string `json:"avatar_url"`
			Email     string `json:"email"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		logext.Errorf(ctx, "[%s] decode failed,err:%+v", where, err.Error())
		return nil, fmt.Errorf("decode user_info: %w", err)
	}
	if body.Code != 0 {
		logext.Warnf(ctx, "[%s] reject: lark code=%d,msg:%s", where, body.Code, body.Msg)
		return nil, fmt.Errorf("lark user_info: code=%d msg=%q", body.Code, body.Msg)
	}
	logext.Infof(ctx, "[%s] OK,open_id:%s,name:%s", where, body.Data.OpenID, body.Data.Name)
	return &UserInfo{
		OpenID:    body.Data.OpenID,
		Name:      body.Data.Name,
		AvatarURL: body.Data.AvatarURL,
		Email:     body.Data.Email,
	}, nil
}

// truncateBytes truncates a body to `limit` bytes for log emission.
func truncateBytes(b []byte, limit int) string {
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "...(truncated)"
}

// appAccessTokenLocked returns a cached app token, refreshing if within
// 60s of expiry. Lock granularity is intentionally coarse — token fetch
// is ~100ms and the race window between expiry and refresh is fine.
func (c *Client) appAccessTokenLocked(ctx context.Context) (string, error) {
	c.mu.RLock()
	if time.Until(c.appTokenExpiry) > 60*time.Second {
		t := c.appAccessToken
		c.mu.RUnlock()
		return t, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Until(c.appTokenExpiry) > 60*time.Second {
		return c.appAccessToken, nil // raced
	}
	var resp struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		AppAccessToken    string `json:"app_access_token"`
		Expire            int    `json:"expire"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	body := map[string]string{"app_id": c.appID, "app_secret": c.appSecret}
	if err := c.postJSON(ctx, endpointAppToken, "", body, &resp); err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("lark app_access_token: code=%d msg=%q", resp.Code, resp.Msg)
	}
	c.appAccessToken = resp.AppAccessToken
	c.appTokenExpiry = time.Now().Add(time.Duration(resp.Expire) * time.Second)
	return c.appAccessToken, nil
}

func (c *Client) postJSON(
	ctx context.Context, path, bearer string, body any, out any,
) error {
	const where = "lark.Client.postJSON"
	buf, err := json.Marshal(body)
	if err != nil {
		logext.Errorf(ctx, "[%s] marshal failed,path:%s,err:%+v", where, path, err.Error())
		return fmt.Errorf("marshal request body: %w", err)
	}
	url := c.baseURL + path
	// Upstream request body is logged for ops; the Bearer token sits
	// in a header (postJSON sets it) and never enters the body.
	logext.Infof(ctx, "[%s] upstream REQUEST,url:%s,body_bytes:%d,body:%s",
		where, url, len(buf), truncateBytes(buf, upstreamBodyLogCap))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		logext.Errorf(ctx, "[%s] build request failed,path:%s,err:%+v", where, path, err.Error())
		return fmt.Errorf("build request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logext.Errorf(ctx, "[%s] upstream RESPONSE (transport err),path:%s,err:%+v",
			where, path, err.Error())
		return fmt.Errorf("call %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	logext.Infof(ctx, "[%s] upstream RESPONSE,path:%s,status:%d,body_bytes:%d,body:%s",
		where, path, resp.StatusCode, len(raw), truncateBytes(raw, upstreamBodyLogCap))
	if resp.StatusCode >= 500 {
		return fmt.Errorf("lark %s returned %d: %s", path, resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		logext.Errorf(ctx, "[%s] decode failed,path:%s,err:%+v", where, path, err.Error())
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	return nil
}
