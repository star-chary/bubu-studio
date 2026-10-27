package server

import (
	"frame-space/backend/internal/persistence"
	"net/http"

	"github.com/gin-gonic/gin"
)

// NewRouter 组装生产路由。db 为空时登录与业务接口返回未配置。
func NewRouter(generator imageGenerator, store AssetStore, db *persistence.Store) *gin.Engine {
	router := gin.New()
	proxies, err := TrustedProxiesFromEnv()
	if err != nil {
		panic(err)
	}
	if err := router.SetTrustedProxies(proxies); err != nil {
		panic(err)
	}
	// The trusted Nginx peer must overwrite this header with $remote_addr.
	// Ignore alternate headers that may have been supplied by the browser.
	router.RemoteIPHeaders = []string{"X-Forwarded-For"}
	// 请求日志和异常恢复属于通用中间件，不放进具体接口里重复编写。
	router.Use(gin.LoggerWithConfig(gin.LoggerConfig{SkipPaths: []string{"/mcp", "/oauth/authorize", "/oauth/token", "/oauth/register", "/oauth/revoke", "/oauth/connections"}}), gin.Recovery())
	cfg, err := AuthConfigFromEnv()
	if err != nil {
		panic(err)
	}
	router.Use(requireSession(db, cfg))
	registerAuth(router, db, cfg)
	// 仅用于确认 HTTP 服务可以响应，不代表任何业务或外部依赖已接通。
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.POST("/api/images/generations", imageHandler(generator, store, db))
	router.POST("/api/videos/generations", videoHandler(store, db))
	registerAssets(router, store, db)
	registerPersistence(router, db)
	registerCredits(router, db)
	registerAdmin(router, db)
	registerMCP(router, db, cfg)

	return router
}
