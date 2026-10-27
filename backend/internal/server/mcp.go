package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
	"github.com/gin-gonic/gin"
	"github.com/go-oauth2/oauth2/v4"
	"github.com/go-oauth2/oauth2/v4/manage"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpScope = "users:read"

type MCPConfig struct{ URL, Origin, Host string }

func MCPConfigFromEnv() (MCPConfig, error) {
	raw := strings.TrimSpace(os.Getenv("MCP_PUBLIC_URL"))
	if raw == "" {
		return MCPConfig{}, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "/mcp" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return MCPConfig{}, errors.New("MCP_PUBLIC_URL 必须是完整的 HTTPS /mcp 地址")
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return MCPConfig{}, errors.New("MCP 仅允许 HTTPS，本机回环地址可以使用 HTTP")
	}
	cfg, err := AuthConfigFromEnv()
	if err != nil {
		return MCPConfig{}, err
	}
	origin := u.Scheme + "://" + u.Host
	if !cfg.Origins[origin] {
		return MCPConfig{}, errors.New("MCP_PUBLIC_URL 的来源必须包含在 APP_ORIGINS 中")
	}
	return MCPConfig{URL: raw, Origin: origin, Host: u.Host}, nil
}

type mcpGateway struct {
	db      *persistence.Store
	store   *persistence.OAuthStore
	manager *manage.Manager
	config  MCPConfig
	auth    AuthConfig
	limiter *authLimiter
}

type mcpCodeGenerator struct{}

func (mcpCodeGenerator) Token(context.Context, *oauth2.GenerateBasic) (string, error) {
	return identity.Token(), nil
}

type mcpAccessGenerator struct{}

func (mcpAccessGenerator) Token(context.Context, *oauth2.GenerateBasic, bool) (string, string, error) {
	return identity.Token(), "", nil
}

func registerMCP(router *gin.Engine, db *persistence.Store, authConfig AuthConfig) {
	cfg, err := MCPConfigFromEnv()
	if err != nil {
		panic(err)
	}
	if cfg.URL == "" {
		return
	}
	store := &persistence.OAuthStore{DB: db}
	manager := manage.NewDefaultManager()
	manager.MapClientStorage(store)
	manager.MapTokenStorage(store)
	manager.MapAuthorizeGenerate(mcpCodeGenerator{})
	manager.MapAccessGenerate(mcpAccessGenerator{})
	manager.SetAuthorizeCodeExp(5 * time.Minute)
	manager.SetAuthorizeCodeTokenCfg(&manage.Config{AccessTokenExp: time.Hour})
	manager.SetExtractExtensionHandler(func(_ *oauth2.TokenGenerateRequest, info oauth2.ExtendableTokenInfo) {
		if info.GetExtension().Get("resource") == "" {
			info.GetExtension().Set("resource", cfg.URL)
		}
	})
	manager.SetValidateURIHandler(func(registered, requested string) error {
		var redirects []string
		if json.Unmarshal([]byte(registered), &redirects) != nil || !slices.Contains(redirects, requested) {
			return persistence.ErrInvalid
		}
		return nil
	})
	g := &mcpGateway{db, store, manager, cfg, authConfig, &authLimiter{entries: map[string]attemptBucket{}}}
	group := router.Group("", g.guard)
	group.GET("/.well-known/oauth-protected-resource/mcp", g.resourceMetadata)
	group.GET("/.well-known/oauth-protected-resource", g.resourceMetadata)
	group.GET("/.well-known/oauth-authorization-server", g.authorizationMetadata)
	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server"} {
		group.OPTIONS(path, func(c *gin.Context) {
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type")
			c.Status(http.StatusNoContent)
		})
	}
	group.POST("/oauth/register", g.registerClient)
	group.GET("/oauth/authorize", g.authorize)
	group.POST("/oauth/authorize", g.authorize)
	group.POST("/oauth/token", g.exchangeToken)
	group.POST("/oauth/revoke", g.revokeToken)
	group.GET("/oauth/connections", g.connections)
	group.POST("/oauth/connections", g.connections)

	sdkServer := mcp.NewServer(&mcp.Implementation{Name: "bubu-users", Version: "1.0.0"}, nil)
	inputSchema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"q":      map[string]any{"type": "string", "maxLength": 254, "default": "", "description": "邮箱包含搜索或完整用户 ID；空字符串查询全部用户。"},
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 20, "description": "本次返回数量，默认 20，最多 100。"},
			"offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 100000, "default": 0, "description": "跳过的条数；下一页加上 limit。"},
		},
	}
	mcp.AddTool(sdkServer, &mcp.Tool{
		Name: "list_users", Title: "查询平台用户",
		Description: "仅供已授权管理员分页查询 BuBu 平台用户。支持邮箱包含搜索或完整用户 ID 搜索，按注册时间和 ID 倒序。返回账号、角色、状态、注册时间、可用及冻结积分。total 是匹配总数，users 仅是当前页。此工具只读，不支持日期范围筛选。",
		InputSchema: inputSchema,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}, g.listUsers)
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return sdkServer }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 32 << 10,
		// We enforce the exact configured Host below, including behind local Nginx.
		DisableLocalhostProtection: true,
	})
	protected := mcpauth.RequireBearerToken(g.verifyToken, &mcpauth.RequireBearerTokenOptions{
		ResourceMetadataURL: cfg.Origin + "/.well-known/oauth-protected-resource/mcp", Scopes: []string{mcpScope},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := mcpauth.TokenInfoFromContext(r.Context())
		if !g.limiter.allow("actor:"+actor.UserID, 600) {
			w.Header().Set("Retry-After", "900")
			http.Error(w, "调用过于频繁，请稍后重试", http.StatusTooManyRequests)
			return
		}
		transport.ServeHTTP(w, r)
	}))
	group.Any("/mcp", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer cancel()
		protected.ServeHTTP(c.Writer, c.Request.WithContext(ctx))
	})
}

func (g *mcpGateway) guard(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	// Preserve the same-origin form Origin; suppress referrers on OAuth callbacks.
	c.Header("Referrer-Policy", "same-origin")
	c.Header("X-Content-Type-Options", "nosniff")
	if !strings.EqualFold(c.Request.Host, g.config.Host) {
		c.AbortWithStatus(http.StatusMisdirectedRequest)
		return
	}
	if origin := c.GetHeader("Origin"); !strings.HasPrefix(c.Request.URL.Path, "/.well-known/") && origin != "" && origin != g.config.Origin {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if db := g.db; db == nil {
		c.AbortWithStatusJSON(503, gin.H{"error": "temporarily_unavailable"})
		return
	}
	if !g.limiter.allow("request:"+c.ClientIP(), 3000) {
		c.Header("Retry-After", "900")
		c.AbortWithStatus(429)
		return
	}
	c.Next()
}

func (g *mcpGateway) resourceMetadata(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.JSON(200, gin.H{"resource": g.config.URL, "authorization_servers": []string{g.config.Origin}, "scopes_supported": []string{mcpScope}, "bearer_methods_supported": []string{"header"}, "resource_name": "BuBu 用户查询"})
}
func (g *mcpGateway) authorizationMetadata(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.JSON(200, gin.H{
		"issuer": g.config.Origin, "authorization_endpoint": g.config.Origin + "/oauth/authorize", "token_endpoint": g.config.Origin + "/oauth/token",
		"registration_endpoint": g.config.Origin + "/oauth/register", "revocation_endpoint": g.config.Origin + "/oauth/revoke",
		"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code"},
		"token_endpoint_auth_methods_supported": []string{"none"}, "revocation_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{mcpScope},
		"authorization_response_iss_parameter_supported": true,
	})
}

func (g *mcpGateway) verifyToken(ctx context.Context, token string, _ *http.Request) (*mcpauth.TokenInfo, error) {
	info, err := g.store.GetByAccess(ctx, token)
	if err != nil {
		log.Print("MCP credential lookup failed")
		return nil, errors.New("授权服务暂时不可用")
	}
	if info == nil {
		return nil, mcpauth.ErrInvalidToken
	}
	extension, ok := info.(oauth2.ExtendableTokenInfo)
	if !ok || extension.GetExtension().Get("resource") != g.config.URL {
		return nil, mcpauth.ErrInvalidToken
	}
	return &mcpauth.TokenInfo{UserID: info.GetUserID(), Scopes: strings.Fields(info.GetScope()), Expiration: info.GetAccessCreateAt().Add(info.GetAccessExpiresIn())}, nil
}

type listUsersInput struct {
	Query  string `json:"q,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
	Offset *int   `json:"offset,omitempty"`
}
type listUsersOutput struct {
	Users  []persistence.AdminUser `json:"users"`
	Total  int64                   `json:"total"`
	Limit  int                     `json:"limit"`
	Offset int                     `json:"offset"`
}

func (g *mcpGateway) listUsers(ctx context.Context, request *mcp.CallToolRequest, input listUsersInput) (*mcp.CallToolResult, listUsersOutput, error) {
	var output listUsersOutput
	if request.Extra == nil || request.Extra.TokenInfo == nil {
		return nil, output, errors.New("需要有效的管理员授权")
	}
	actor := request.Extra.TokenInfo
	if !slices.Contains(actor.Scopes, mcpScope) {
		return nil, output, errors.New("缺少 users:read 权限")
	}
	limit, offset := 20, 0
	if input.Limit != nil {
		limit = *input.Limit
	}
	if input.Offset != nil {
		offset = *input.Offset
	}
	users, total, err := g.db.AdminUsersForActor(ctx, actor.UserID, input.Query, limit, offset)
	if err != nil {
		switch {
		case errors.Is(err, persistence.ErrAdminRequired):
			return nil, output, errors.New("当前账号没有管理员权限，请重新授权")
		case errors.Is(err, persistence.ErrInvalid):
			return nil, output, errors.New("参数无效：q 最长 254 字符，limit 为 1～100，offset 为 0～100000")
		default:
			log.Printf("MCP list_users failed actor=%s", actor.UserID)
			return nil, output, errors.New("用户查询暂时不可用，请稍后重试")
		}
	}
	log.Printf("MCP list_users actor=%s limit=%d offset=%d returned=%d", actor.UserID, limit, offset, len(users))
	return nil, listUsersOutput{users, total, limit, offset}, nil
}
