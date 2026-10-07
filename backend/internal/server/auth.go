package server

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
	"github.com/gin-gonic/gin"
)

type AuthConfig struct {
	Origins map[string]bool
	Secure  bool
}

func AuthConfigFromEnv() (AuthConfig, error) {
	origins := os.Getenv("APP_ORIGINS")
	if origins == "" {
		origins = "http://127.0.0.1:5173,http://localhost:5173,http://127.0.0.1:5174,http://localhost:5174"
	}
	value := os.Getenv("AUTH_COOKIE_SECURE")
	if value != "" && value != "true" && value != "false" {
		return AuthConfig{}, errors.New("AUTH_COOKIE_SECURE 必须是 true 或 false")
	}
	cfg := AuthConfig{Origins: map[string]bool{}, Secure: value == "true"}
	for _, origin := range strings.Split(origins, ",") {
		origin = strings.TrimSpace(origin)
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return cfg, errors.New("APP_ORIGINS 必须为不带路径的完整站点来源")
		}
		if u.Scheme == "https" && !cfg.Secure {
			return cfg, errors.New("HTTPS 站点必须启用 AUTH_COOKIE_SECURE")
		}
		if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
			return cfg, errors.New("正式站点必须使用 HTTPS")
		}
		cfg.Origins[origin] = true
	}
	return cfg, nil
}
func (cfg AuthConfig) cookieName() string {
	if cfg.Secure {
		return "__Host-frame_session"
	}
	return "frame_session"
}
func (cfg AuthConfig) cookie(c *gin.Context, token string, expires time.Time) {
	maxAge := int(time.Until(expires).Seconds())
	if token == "" {
		maxAge = -1
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: cfg.cookieName(), Value: token, Path: "/", HttpOnly: true, Secure: cfg.Secure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge})
}
func authFailure(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func (cfg AuthConfig) originAllowed(c *gin.Context) bool {
	origin := c.GetHeader("Origin")
	if origin == "" {
		if ref, err := url.Parse(c.GetHeader("Referer")); err == nil && ref.Host != "" {
			origin = ref.Scheme + "://" + ref.Host
		}
	}
	return cfg.Origins[origin] && c.GetHeader("Sec-Fetch-Site") != "cross-site"
}

func requireSession(db *persistence.Store, cfg AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Next()
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("Vary", "Cookie")
		if db == nil {
			authFailure(c, 503, "AUTH_UNAVAILABLE", "登录服务尚未连接数据库，请启动后端和数据库后重试。")
			return
		}
		if c.Request.Method == "POST" && c.Request.URL.Path == "/api/auth/login" {
			c.Next()
			return
		}
		token, _ := c.Cookie(cfg.cookieName())
		session, err := db.Session(c.Request.Context(), token, false)
		if errors.Is(err, persistence.ErrSession) {
			authFailure(c, 401, "AUTH_REQUIRED", "登录已过期，请重新登录。")
			return
		}
		if err != nil {
			authFailure(c, 503, "AUTH_UNAVAILABLE", "登录服务暂时不可用，请稍后重试。")
			return
		}
		if expected := c.GetHeader("X-Session-User"); expected != "" && expected != session.User.ID {
			authFailure(c, 409, "SESSION_CHANGED", "账号已在其他页面切换，请重新确认登录。")
			return
		}
		write := c.Request.Method != "GET" && c.Request.Method != "HEAD" && c.Request.Method != "OPTIONS"
		if write && (!cfg.originAllowed(c) || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-CSRF-Token")), []byte(session.CSRFToken)) != 1) {
			authFailure(c, 403, "CSRF_INVALID", "登录状态已变化，请重新确认登录后再操作。")
			return
		}
		if write && c.Request.URL.Path != "/api/auth/logout" {
			if _, err := db.Session(c.Request.Context(), token, true); err != nil {
				authFailure(c, 503, "AUTH_UNAVAILABLE", "登录服务暂时不可用，请稍后重试。")
				return
			}
		}
		c.Set("userID", session.User.ID)
		c.Set("session", session)
		c.Set("sessionToken", token)
		c.Next()
	}
}

type attemptBucket struct {
	Count int
	Until time.Time
}
type authLimiter struct {
	mu      sync.Mutex
	entries map[string]attemptBucket
}

func (l *authLimiter) allow(key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.entries) >= 8192 {
		for k, v := range l.entries {
			if !v.Until.After(now) {
				delete(l.entries, k)
			}
		}
	}
	v, exists := l.entries[key]
	if !exists && len(l.entries) >= 8192 {
		return false
	}
	if !v.Until.After(now) {
		v = attemptBucket{Until: now.Add(15 * time.Minute)}
	}
	if v.Count >= limit {
		return false
	}
	v.Count++
	l.entries[key] = v
	return true
}

func registerAuth(router *gin.Engine, db *persistence.Store, cfg AuthConfig) {
	limiter := &authLimiter{entries: map[string]attemptBucket{}}
	slots := make(chan struct{}, 4)
	router.POST("/api/auth/login", func(c *gin.Context) {
		if !cfg.originAllowed(c) || c.GetHeader("X-Requested-With") != "frame-space" {
			authFailure(c, 403, "INVALID_ORIGIN", "登录请求来源无效，请从本站页面登录。")
			return
		}
		if !limiter.allow("ip:"+c.ClientIP(), 40) {
			c.Header("Retry-After", "900")
			authFailure(c, 429, "LOGIN_RATE_LIMITED", "尝试过于频繁，请 15 分钟后再试。")
			return
		}
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if !decodeJSON(c, &body, 4096) {
			return
		}
		email, err := identity.Email(body.Email)
		if err != nil {
			authFailure(c, 400, "INVALID_EMAIL", err.Error())
			return
		}
		if err = identity.ValidatePassword(body.Password); err != nil {
			authFailure(c, 400, "INVALID_PASSWORD", err.Error())
			return
		}
		if !limiter.allow("email:"+identity.TokenHash(email), 12) {
			c.Header("Retry-After", "900")
			authFailure(c, 429, "LOGIN_RATE_LIMITED", "尝试过于频繁，请 15 分钟后再试。")
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			authFailure(c, 429, "LOGIN_BUSY", "登录请求较多，请稍后再试。")
			return
		}
		session, err := db.LoginOrCreate(c.Request.Context(), email, body.Password)
		if errors.Is(err, persistence.ErrCredentials) {
			authFailure(c, 401, "INVALID_CREDENTIALS", "邮箱或密码不正确，或账号不可用。")
			return
		}
		if err != nil {
			authFailure(c, 503, "AUTH_UNAVAILABLE", "登录服务暂时不可用，请稍后重试。")
			return
		}
		// A login always issues a new credential, never adopts a supplied cookie.
		if old, _ := c.Cookie(cfg.cookieName()); identity.ValidToken(old) {
			_ = db.RevokeSession(c.Request.Context(), old)
		}
		cfg.cookie(c, session.Token, session.ExpiresAt)
		c.JSON(200, session)
	})
	router.GET("/api/auth/me", func(c *gin.Context) { session, _ := c.Get("session"); c.JSON(200, session) })
	router.POST("/api/auth/logout", func(c *gin.Context) {
		if err := db.RevokeSession(c.Request.Context(), c.GetString("sessionToken")); err != nil {
			authFailure(c, 503, "AUTH_UNAVAILABLE", "退出失败，请恢复连接后重试。")
			return
		}
		cfg.cookie(c, "", time.Unix(1, 0))
		c.Status(204)
	})
}

// Ownership is immutable in this release. Read checks happen before resource
// lookup, provider calls or storage IO; writes also constrain the SQL owner.
func authorizeCanvas(c *gin.Context, db *persistence.Store, id string) bool {
	if db == nil {
		authFailure(c, 503, "AUTH_UNAVAILABLE", "登录服务暂时不可用，请稍后重试。")
		return false
	}
	if err := db.RequireCanvasOwner(c.Request.Context(), id, c.GetString("userID")); err != nil {
		persistenceFailure(c, err)
		return false
	}
	return true
}
func authorizeTask(c *gin.Context, db *persistence.Store, id string) bool {
	if err := db.RequireTaskOwner(c.Request.Context(), id, c.GetString("userID")); err != nil {
		persistenceFailure(c, err)
		return false
	}
	return true
}
