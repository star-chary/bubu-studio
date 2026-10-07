package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"frame-space/backend/internal/persistence"
	"github.com/gin-gonic/gin"
)

func persistenceFailure(c *gin.Context, err error) {
	status, code, message := 503, "DATABASE_UNAVAILABLE", "数据库暂时不可用，请稍后重试。"
	switch {
	case errors.Is(err, persistence.ErrNotFound):
		status, code, message = 404, "NOT_FOUND", "画布、节点或任务不存在，请先保存画布。"
	case errors.Is(err, persistence.ErrConflict):
		status, code, message = 409, "VERSION_CONFLICT", "画布已在其他页面更新，或任务 ID 已用于其他输入。请刷新后重试。"
	case errors.Is(err, persistence.ErrBusy):
		status, code, message = 409, "GENERATION_BUSY", "该节点已有未完成的任务，请等待完成后再试。其他节点可以继续提交。"
	case errors.Is(err, persistence.ErrInvalid):
		status, code, message = 400, "INVALID_CANVAS", "画布、节点或参考素材数据无效。"
	case errors.Is(err, persistence.ErrPromptReferences):
		status, code, message = 422, "INVALID_PROMPT_REFERENCE", "提示词中的素材标签已失效，请重新选择参考素材。"
	case errors.Is(err, persistence.ErrCreditsInsufficient):
		status, code, message = 402, "CREDITS_INSUFFICIENT", "测试积分不足，当前无法提交生成任务。"
	case errors.Is(err, persistence.ErrCreditQuoteChanged):
		status, code, message = 409, "CREDIT_QUOTE_CHANGED", "本次生成积分已变化，请确认新报价后重试。"
	}
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
func decodeJSON(c *gin.Context, target any, limit int64) bool {
	contentType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if contentType != "application/json" {
		c.JSON(415, gin.H{"error": gin.H{"code": "INVALID_CONTENT_TYPE", "message": "请提交 JSON。"}})
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		persistenceFailure(c, persistence.ErrInvalid)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		persistenceFailure(c, persistence.ErrInvalid)
		return false
	}
	return true
}
func registerPersistence(router *gin.Engine, db *persistence.Store) {
	router.GET("/api/persistence/config", func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.JSON(200, gin.H{"enabled": db != nil}) })
	api := router.Group("/api")
	api.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if db == nil {
			c.AbortWithStatusJSON(503, gin.H{"error": gin.H{"code": "DATABASE_NOT_CONFIGURED", "message": "后端尚未配置 PostgreSQL。"}})
			return
		}
		for _, key := range []string{"canvasId", "taskId"} {
			if id := c.Param(key); id != "" && !persistence.ValidID(id) {
				c.Abort()
				persistenceFailure(c, persistence.ErrInvalid)
				return
			}
		}
		if id := c.Param("canvasId"); id != "" && !authorizeCanvas(c, db, id) {
			c.Abort()
			return
		}
		if id := c.Param("taskId"); id != "" && !authorizeTask(c, db, id) {
			c.Abort()
			return
		}
	})
	api.POST("/canvases", func(c *gin.Context) {
		var body struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		if !decodeJSON(c, &body, 1024) {
			return
		}
		body.Title = strings.TrimSpace(body.Title)
		if body.Title == "" {
			body.Title = "未命名画布"
		}
		if !persistence.ValidID(body.ID) || utf8.RuneCountInString(body.Title) > 80 {
			persistenceFailure(c, persistence.ErrInvalid)
			return
		}
		if err := db.CreateCanvas(c.Request.Context(), body.ID, body.Title, c.GetString("userID")); err != nil {
			persistenceFailure(c, err)
			return
		}
		canvas, err := db.Canvas(c.Request.Context(), body.ID)
		if err != nil {
			persistenceFailure(c, err)
			return
		}
		c.JSON(201, canvas)
	})
	api.GET("/canvases", func(c *gin.Context) {
		limit, offset := 24, 0
		var err error
		if value := c.Query("limit"); value != "" {
			limit, err = strconv.Atoi(value)
		}
		if value := c.Query("offset"); value != "" && err == nil {
			offset, err = strconv.Atoi(value)
		}
		if err != nil || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
			persistenceFailure(c, persistence.ErrInvalid)
			return
		}
		canvases, err := db.Canvases(c.Request.Context(), c.GetString("userID"), limit+1, offset)
		if err != nil {
			persistenceFailure(c, err)
			return
		}
		more := len(canvases) > limit
		if more {
			canvases = canvases[:limit]
		}
		c.JSON(200, gin.H{"canvases": canvases, "hasMore": more})
	})
	api.POST("/tasks/:taskId/storage-retries", func(c *gin.Context) {
		var body struct{}
		if !decodeJSON(c, &body, 1024) {
			return
		}
		task, err := db.RetryVideoStorage(c.Request.Context(), c.Param("taskId"))
		if err != nil {
			videoHTTPFailure(c, err)
			return
		}
		c.JSON(taskHTTPStatus(task), task)
	})
	api.GET("/canvases/:canvasId", func(c *gin.Context) {
		canvas, err := db.Canvas(c.Request.Context(), c.Param("canvasId"))
		if err != nil {
			persistenceFailure(c, err)
			return
		}
		c.JSON(200, canvas)
	})
	api.PUT("/canvases/:canvasId", func(c *gin.Context) {
		var body struct {
			Version  int64                `json:"version"`
			Snapshot persistence.Snapshot `json:"snapshot"`
		}
		if !decodeJSON(c, &body, 2<<20) {
			return
		}
		if err := body.Snapshot.Validate(); err != nil {
			persistenceFailure(c, err)
			return
		}
		version, err := db.SaveOwnedCanvas(c.Request.Context(), c.Param("canvasId"), c.GetString("userID"), body.Version, body.Snapshot)
		if err != nil {
			persistenceFailure(c, err)
			return
		}
		c.JSON(200, gin.H{"version": version})
	})
	api.GET("/tasks/:taskId", func(c *gin.Context) {
		task, err := db.Task(c.Request.Context(), c.Param("taskId"))
		if err != nil {
			persistenceFailure(c, err)
			return
		}
		c.JSON(200, task)
	})
	api.GET("/canvases/:canvasId/tasks", func(c *gin.Context) {
		if c.Query("latest") == "true" {
			tasks, err := db.LatestTasks(c.Request.Context(), c.Param("canvasId"), false)
			if err != nil {
				persistenceFailure(c, err)
				return
			}
			c.JSON(200, gin.H{"tasks": tasks})
			return
		}
		limit := 20
		offset := 0
		var err error
		if value := c.Query("limit"); value != "" {
			limit, err = strconv.Atoi(value)
		}
		if err == nil {
			if value := c.Query("offset"); value != "" {
				offset, err = strconv.Atoi(value)
			}
		}
		if err != nil || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
			persistenceFailure(c, persistence.ErrInvalid)
			return
		}
		tasks, err := db.Tasks(c.Request.Context(), c.Param("canvasId"), limit+1, offset)
		if err != nil {
			persistenceFailure(c, err)
			return
		}
		more := len(tasks) > limit
		if more {
			tasks = tasks[:limit]
		}
		c.JSON(200, gin.H{"tasks": tasks, "hasMore": more})
	})
}
