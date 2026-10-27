package server

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
	"github.com/gin-gonic/gin"
	"github.com/go-oauth2/oauth2/v4"
	oautherrors "github.com/go-oauth2/oauth2/v4/errors"
)

func oauthError(c *gin.Context, status int, code, description string) {
	c.AbortWithStatusJSON(status, gin.H{"error": code, "error_description": description})
}
func (g *mcpGateway) registerClient(c *gin.Context) {
	if !g.limiter.allow("registration:global", 100) || !g.limiter.allow("registration:"+c.ClientIP(), 10) {
		oauthError(c, 429, "temporarily_unavailable", "注册过于频繁，请稍后重试")
		return
	}
	var body struct {
		Name       string   `json:"client_name"`
		Redirects  []string `json:"redirect_uris"`
		AuthMethod string   `json:"token_endpoint_auth_method"`
		Grants     []string `json:"grant_types"`
		Responses  []string `json:"response_types"`
		Scope      string   `json:"scope"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	decoder := json.NewDecoder(c.Request.Body)
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
		oauthError(c, 400, "invalid_client_metadata", "注册参数无效")
		return
	}
	if body.AuthMethod != "none" || (len(body.Grants) > 0 && (len(body.Grants) != 1 || body.Grants[0] != "authorization_code")) || (len(body.Responses) > 0 && (len(body.Responses) != 1 || body.Responses[0] != "code")) || (body.Scope != "" && body.Scope != mcpScope) {
		oauthError(c, 400, "invalid_client_metadata", "支持 public client、none、authorization_code、code 和 users:read")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" || utf8.RuneCountInString(body.Name) > 100 || len(body.Redirects) == 0 || len(body.Redirects) > 5 {
		oauthError(c, 400, "invalid_client_metadata", "需要应用名称和 1～5 个回调地址")
		return
	}
	for _, raw := range body.Redirects {
		if !validMCPRedirect(raw) {
			oauthError(c, 400, "invalid_redirect_uri", "回调地址必须是 HTTPS 或本机回环 HTTP 地址")
			return
		}
	}
	client := &persistence.OAuthClient{ID: identity.Token(), Name: body.Name, RedirectURIs: body.Redirects}
	if err := g.store.RegisterClient(c.Request.Context(), client); err != nil {
		oauthError(c, 503, "temporarily_unavailable", "应用注册暂时不可用")
		return
	}
	c.JSON(201, gin.H{"client_id": client.ID, "client_name": client.Name, "redirect_uris": client.RedirectURIs, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code"}, "response_types": []string{"code"}, "scope": mcpScope})
}
func validMCPRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Host == "" || strings.ContainsAny(u.Host, "'\";<> ") || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))
}

// All parameters are read from exactly one source; duplicate security parameters
// are rejected so different layers cannot interpret the same request differently.
func oauthValues(c *gin.Context) (url.Values, bool) {
	var values url.Values
	var err error
	if c.Request.Method == http.MethodGet {
		values, err = url.ParseQuery(c.Request.URL.RawQuery)
	} else {
		if !strings.HasPrefix(c.GetHeader("Content-Type"), "application/x-www-form-urlencoded") || c.Request.URL.RawQuery != "" {
			oauthError(c, 400, "invalid_request", "请使用表单请求体")
			return nil, false
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		err = c.Request.ParseForm()
		values = c.Request.PostForm
	}
	if err != nil {
		oauthError(c, 400, "invalid_request", "请求参数无效")
		return nil, false
	}
	for _, v := range values {
		if len(v) != 1 || len(v[0]) > 4096 {
			oauthError(c, 400, "invalid_request", "参数重复或过长")
			return nil, false
		}
	}
	return values, true
}

func (g *mcpGateway) browserAdmin(c *gin.Context) (persistence.Session, bool) {
	token, _ := c.Cookie(g.auth.cookieName())
	session, err := g.db.Session(c.Request.Context(), token, false)
	if errors.Is(err, persistence.ErrSession) {
		if c.Request.Method == http.MethodGet {
			returnTo := c.Request.URL.RequestURI()
			c.Redirect(http.StatusSeeOther, "/admin/?mcp_return="+url.QueryEscape(returnTo))
		} else {
			oauthError(c, 401, "access_denied", "登录已过期，请重新登录管理台")
		}
		return session, false
	}
	if err != nil {
		oauthError(c, 503, "temporarily_unavailable", "登录服务暂时不可用")
		return session, false
	}
	if session.User.Role != "admin" {
		oauthError(c, 403, "access_denied", "需要管理员账号")
		return session, false
	}
	return session, true
}

func (g *mcpGateway) validBrowserPost(c *gin.Context, session persistence.Session, v url.Values) bool {
	if c.GetHeader("Origin") != g.config.Origin || c.GetHeader("Sec-Fetch-Site") == "cross-site" || subtle.ConstantTimeCompare([]byte(v.Get("csrf_token")), []byte(session.CSRFToken)) != 1 {
		oauthError(c, 403, "access_denied", "页面已失效，请重新打开授权页面")
		return false
	}
	return true
}

func (g *mcpGateway) authorize(c *gin.Context) {
	v, ok := oauthValues(c)
	if !ok {
		return
	}
	clientInfo, err := g.store.GetByID(c.Request.Context(), v.Get("client_id"))
	if err != nil {
		oauthError(c, 503, "temporarily_unavailable", "授权服务暂时不可用")
		return
	}
	client, ok := clientInfo.(*persistence.OAuthClient)
	if !ok || client == nil || !slices.Contains(client.RedirectURIs, v.Get("redirect_uri")) {
		oauthError(c, 400, "invalid_request", "应用或回调地址无效")
		return
	}
	challenge, decodeErr := base64.RawURLEncoding.DecodeString(v.Get("code_challenge"))
	if v.Get("response_type") != "code" || v.Get("scope") != mcpScope || v.Get("resource") != g.config.URL || v.Get("code_challenge_method") != "S256" || decodeErr != nil || len(challenge) != 32 || len(v.Get("code_challenge")) != 43 || len(v.Get("state")) < 1 || len(v.Get("state")) > 1024 {
		oauthError(c, 400, "invalid_request", "需要 code、users:read、正确 resource、state 和 S256 PKCE")
		return
	}
	session, ok := g.browserAdmin(c)
	if !ok {
		return
	}
	if c.Request.Method == http.MethodGet {
		g.renderOAuthPage(c, oauthPage{Title: "授权应用读取用户", Client: client, Email: session.User.Email, CSRF: session.CSRFToken, Values: v})
		return
	}
	if !g.validBrowserPost(c, session, v) {
		return
	}
	redirect, _ := url.Parse(v.Get("redirect_uri"))
	query := redirect.Query()
	query.Set("state", v.Get("state"))
	query.Set("iss", g.config.Origin)
	if v.Get("decision") != "allow" {
		query.Set("error", "access_denied")
	} else {
		if !g.limiter.allow("grant:"+session.User.ID, 30) {
			oauthError(c, 429, "temporarily_unavailable", "授权过于频繁")
			return
		}
		var token oauth2.TokenInfo
		err = g.store.Transaction(c.Request.Context(), "oauth-client:"+session.User.ID+":"+client.ID, func(ctx context.Context) error {
			var issueErr error
			token, issueErr = g.manager.GenerateAuthToken(ctx, oauth2.Code, &oauth2.TokenGenerateRequest{ClientID: client.ID, UserID: session.User.ID, RedirectURI: v.Get("redirect_uri"), Scope: mcpScope, CodeChallenge: v.Get("code_challenge"), CodeChallengeMethod: oauth2.CodeChallengeS256})
			return issueErr
		})
		if err != nil {
			oauthError(c, 503, "temporarily_unavailable", "授权暂时不可用，请重新尝试")
			return
		}
		query.Set("code", token.GetCode())
	}
	redirect.RawQuery = query.Encode()
	c.Redirect(http.StatusSeeOther, redirect.String())
}

func (g *mcpGateway) exchangeToken(c *gin.Context) {
	v, ok := oauthValues(c)
	if !ok {
		return
	}
	if !g.limiter.allow("exchange:"+c.ClientIP(), 120) {
		oauthError(c, 429, "temporarily_unavailable", "兑换过于频繁")
		return
	}
	if v.Get("grant_type") != "authorization_code" {
		oauthError(c, 400, "unsupported_grant_type", "只支持 authorization_code；过期后请重新授权")
		return
	}
	if v.Get("resource") != g.config.URL || v.Get("client_id") == "" || v.Get("redirect_uri") == "" || !validPKCEVerifier(v.Get("code_verifier")) {
		oauthError(c, 400, "invalid_request", "resource、client_id、redirect_uri 或 code_verifier 无效")
		return
	}
	info, err := g.store.GetByCode(c.Request.Context(), v.Get("code"))
	if err != nil {
		oauthError(c, 503, "temporarily_unavailable", "授权服务暂时不可用")
		return
	}
	if info == nil {
		oauthError(c, 400, "invalid_grant", "授权码已使用、过期或权限已失效")
		return
	}
	extension, validResource := info.(oauth2.ExtendableTokenInfo)
	if !validResource || extension.GetExtension().Get("resource") != g.config.URL {
		oauthError(c, 400, "invalid_grant", "授权码不属于此服务")
		return
	}
	var token oauth2.TokenInfo
	err = g.store.Transaction(c.Request.Context(), "oauth-client:"+info.GetUserID()+":"+info.GetClientID(), func(ctx context.Context) error {
		var exchangeErr error
		token, exchangeErr = g.manager.GenerateAccessToken(ctx, oauth2.AuthorizationCode, &oauth2.TokenGenerateRequest{ClientID: v.Get("client_id"), RedirectURI: v.Get("redirect_uri"), Code: v.Get("code"), CodeVerifier: v.Get("code_verifier")})
		return exchangeErr
	})
	if err != nil {
		switch {
		case errors.Is(err, oautherrors.ErrInvalidAuthorizeCode), errors.Is(err, oautherrors.ErrInvalidClient), errors.Is(err, oautherrors.ErrInvalidCodeChallenge), errors.Is(err, oautherrors.ErrMissingCodeVerifier), errors.Is(err, persistence.ErrInvalid), errors.Is(err, persistence.ErrAdminRequired):
			oauthError(c, 400, "invalid_grant", "授权码、PKCE、回调地址或管理员权限无效")
		default:
			oauthError(c, 503, "temporarily_unavailable", "授权兑换暂时不可用，请稍后重试")
		}
		return
	}
	c.Header("Pragma", "no-cache")
	c.JSON(200, gin.H{"access_token": token.GetAccess(), "token_type": "Bearer", "expires_in": int(token.GetAccessExpiresIn().Seconds()), "scope": token.GetScope()})
}

func validPKCEVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("-._~", r)) {
			return false
		}
	}
	return true
}

func (g *mcpGateway) revokeToken(c *gin.Context) {
	v, ok := oauthValues(c)
	if !ok {
		return
	}
	info, err := g.store.GetByAccess(c.Request.Context(), v.Get("token"))
	if err != nil {
		oauthError(c, 503, "temporarily_unavailable", "撤销暂时不可用")
		return
	}
	if info != nil && info.GetClientID() == v.Get("client_id") {
		if err = g.store.RemoveByAccess(c.Request.Context(), v.Get("token")); err != nil {
			oauthError(c, 503, "temporarily_unavailable", "撤销暂时不可用")
			return
		}
	}
	c.Status(200)
}

func (g *mcpGateway) connections(c *gin.Context) {
	session, ok := g.browserAdmin(c)
	if !ok {
		return
	}
	if c.Request.Method == http.MethodPost {
		v, ok := oauthValues(c)
		if !ok || !g.validBrowserPost(c, session, v) {
			return
		}
		if err := g.store.RevokeClient(c.Request.Context(), session.User.ID, v.Get("client_id")); err != nil {
			oauthError(c, 503, "temporarily_unavailable", "撤销暂时不可用")
			return
		}
		c.Redirect(http.StatusSeeOther, "/oauth/connections")
		return
	}
	connections, err := g.store.Connections(c.Request.Context(), session.User.ID)
	if err != nil {
		oauthError(c, 503, "temporarily_unavailable", "授权记录暂时不可用")
		return
	}
	g.renderOAuthPage(c, oauthPage{Title: "已授权的 AI 应用", Email: session.User.Email, CSRF: session.CSRFToken, Connections: connections})
}
