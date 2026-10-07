package server

import (
	"context"
	"frame-space/backend/internal/persistence"
	"github.com/gin-gonic/gin"
)

const handlerUserID = "99999999-9999-4999-8999-999999999999"

// Legacy component regressions exercise handlers independently of sessions.
// auth_test.go exercises the exported production router with real login cookies.
// This constructor is compiled ONLY into tests; no runtime auth bypass exists.
func newHandlerTestRouter(generator imageGenerator, store AssetStore, databases ...*persistence.Store) *gin.Engine {
	var db *persistence.Store
	if len(databases) > 0 {
		db = databases[0]
	}
	if db != nil {
		if _, err := db.Pool.Exec(context.Background(), `INSERT INTO users(id,email,password_hash) VALUES($1,'handler@example.test','unused') ON CONFLICT DO NOTHING`, handlerUserID); err != nil {
			panic(err)
		}
	}
	router := gin.New()
	router.Use(gin.Recovery(), func(c *gin.Context) { c.Set("userID", handlerUserID); c.Next() })
	router.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	router.POST("/api/images/generations", imageHandler(generator, store, db))
	router.POST("/api/videos/generations", videoHandler(store, db))
	registerAssets(router, store, db)
	registerPersistence(router, db)
	return router
}
