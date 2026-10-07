package server

import (
	"frame-space/backend/internal/persistence"
	"net/http"

	"github.com/gin-gonic/gin"
)

func NewRouter(generator imageGenerator, store AssetStore, databases ...*persistence.Store) *gin.Engine {
	var db *persistence.Store
	if len(databases) > 0 {
		db = databases[0]
	}
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
	router.Use(gin.Logger(), gin.Recovery())
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

	return router
}
