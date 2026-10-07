package server

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"frame-space/backend/internal/persistence"
	"github.com/gin-gonic/gin"
)

func registerCredits(router *gin.Engine, db *persistence.Store) {
	api := router.Group("/api/credits")
	api.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		if db == nil {
			c.AbortWithStatusJSON(503, gin.H{"error": gin.H{"code": "DATABASE_NOT_CONFIGURED", "message": "后端尚未连接数据库。"}})
		}
	})
	api.GET("", func(c *gin.Context) {
		account, err := db.Credits(c.Request.Context(), c.GetString("userID"))
		if err != nil {
			persistenceFailure(c, err)
			return
		}
		c.JSON(200, account)
	})
	api.GET("/ledger", func(c *gin.Context) {
		limit, offset := 20, 0
		var err error
		if raw := c.Query("limit"); raw != "" {
			limit, err = strconv.Atoi(raw)
		}
		if raw := c.Query("offset"); raw != "" && err == nil {
			offset, err = strconv.Atoi(raw)
		}
		if err != nil || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
			persistenceFailure(c, persistence.ErrInvalid)
			return
		}
		entries, hasMore, err := db.CreditEntries(c.Request.Context(), c.GetString("userID"), limit, offset)
		if err != nil {
			persistenceFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"entries": entries, "hasMore": hasMore})
	})
	api.POST("/quote", func(c *gin.Context) {
		var body struct {
			Kind           string `json:"kind"`
			TaskID         string `json:"taskId"`
			AcceptedPoints int64  `json:"acceptedPoints"`
			PriceVersion   string `json:"priceVersion"`
			persistence.Input
		}
		if !decodeJSON(c, &body, 64<<10) {
			return
		}
		if body.Kind == "image" {
			prompt := strings.TrimSpace(body.Input.Prompt)
			if prompt == "" || utf8.RuneCountInString(prompt) > 2000 {
				persistenceFailure(c, persistence.ErrInvalid)
				return
			}
			if len(body.Input.PromptParts) == 0 {
				body.Input.Prompt = prompt
			}
		} else if body.Kind != "video" {
			persistenceFailure(c, persistence.ErrInvalid)
			return
		}
		quote, err := db.QuoteCredits(c.Request.Context(), c.GetString("userID"), body.Kind, body.Input)
		if err != nil {
			persistenceFailure(c, err)
			return
		}
		c.JSON(http.StatusOK, quote)
	})
}
