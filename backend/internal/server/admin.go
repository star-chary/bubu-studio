package server

import (
	"errors"
	"strconv"

	"frame-space/backend/internal/persistence"
	"github.com/gin-gonic/gin"
)

func registerAdmin(router *gin.Engine, db *persistence.Store) {
	api := router.Group("/api/admin")
	api.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		value, exists := c.Get("session")
		session, ok := value.(persistence.Session)
		if !exists || !ok || session.User.Role != "admin" {
			authFailure(c, 403, "ADMIN_REQUIRED", "该账号没有后台管理权限。")
			return
		}
		c.Next()
	})
	api.GET("/me", func(c *gin.Context) {
		session, _ := c.Get("session")
		c.JSON(200, gin.H{"session": session, "maxGrantPoints": persistence.MaxAdminGrantPoints, "maxReasonLength": persistence.MaxGrantReasonLength})
	})
	api.GET("/users", func(c *gin.Context) {
		limit, offset, ok := adminPagination(c)
		if !ok {
			return
		}
		users, total, err := db.AdminUsers(c.Request.Context(), c.Query("q"), limit, offset)
		if err != nil {
			adminFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"users": users, "total": total, "limit": limit, "offset": offset})
	})
	api.GET("/users/:id/ledger", func(c *gin.Context) {
		limit, offset, ok := adminPagination(c)
		if !ok {
			return
		}
		user, err := db.AdminUser(c.Request.Context(), c.Param("id"))
		if err != nil {
			adminFailure(c, err)
			return
		}
		entries, hasMore, err := db.AdminCreditEntries(c.Request.Context(), user.ID, limit, offset)
		if err != nil {
			adminFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"user": user, "entries": entries, "hasMore": hasMore})
	})
	api.POST("/users/:id/credits", func(c *gin.Context) {
		var body struct {
			Points    int64  `json:"points"`
			Reason    string `json:"reason"`
			RequestID string `json:"requestId"`
		}
		if !decodeJSON(c, &body, 4096) {
			return
		}
		result, err := db.AdminGrantCredits(c.Request.Context(), c.GetString("userID"), c.Param("id"), body.Points, body.Reason, body.RequestID)
		if err != nil {
			adminFailure(c, err)
			return
		}
		c.JSON(200, result)
	})
}

func adminPagination(c *gin.Context) (int, int, bool) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit < 1 || limit > 100 {
		adminFailure(c, persistence.ErrInvalid)
		return 0, 0, false
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 || offset > 100000 {
		adminFailure(c, persistence.ErrInvalid)
		return 0, 0, false
	}
	return limit, offset, true
}

func adminFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, persistence.ErrAdminRequired):
		authFailure(c, 403, "ADMIN_REQUIRED", "该账号没有后台管理权限。")
	case errors.Is(err, persistence.ErrUserDisabled):
		authFailure(c, 409, "USER_DISABLED", "该用户已停用，不能继续发放积分。")
	case errors.Is(err, persistence.ErrConflict):
		authFailure(c, 409, "GRANT_CONFLICT", "该发放编号已用于其他操作，请核对积分记录。")
	case errors.Is(err, persistence.ErrInvalid):
		authFailure(c, 400, "INVALID_ADMIN_INPUT", "请检查用户、积分数量、发放原因和分页参数。")
	default:
		persistenceFailure(c, err)
	}
}
