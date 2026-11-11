package server

import (
	"context"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"time"

	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/storage"
	"github.com/gin-gonic/gin"
)

type promptImageStore interface {
	SavePromptImage(context.Context, io.Reader, string, string) (storage.Asset, error)
	DeletePromptImage(context.Context, string) error
}

func libraryFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, persistence.ErrNotFound):
		authFailure(c, 404, "PROMPT_NOT_FOUND", "提示词或图片不存在，请刷新后重试。")
	case errors.Is(err, persistence.ErrInvalid):
		authFailure(c, 400, "INVALID_PROMPT", "请检查标题、提示词内容和分页参数。")
	case errors.Is(err, persistence.ErrConflict):
		authFailure(c, 409, "PROMPT_CONFLICT", "提示词编号已被使用，请刷新后重试。")
	default:
		persistenceFailure(c, err)
	}
}

func registerPrompts(router *gin.Engine, store AssetStore, db *persistence.Store) {
	router.GET("/api/prompts", func(c *gin.Context) { listLibraryPrompts(c, db) })
	router.GET("/api/prompt-images/:id", func(c *gin.Context) {
		if db == nil {
			authFailure(c, 503, "DATABASE_UNAVAILABLE", "提示词服务暂时不可用，请稍后重试。")
			return
		}
		key, err := db.LibraryImageKey(c.Request.Context(), c.Param("id"))
		if err != nil {
			libraryFailure(c, err)
			return
		}
		if store == nil {
			storageFailure(c, &storage.Error{Status: 503, Code: "STORAGE_NOT_CONFIGURED", Message: "图片存储暂时不可用。"})
			return
		}
		content, err := store.Read(c.Request.Context(), key, "")
		if err != nil {
			storageFailure(c, err)
			return
		}
		defer content.Body.Close()
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Disposition", "inline")
		c.Header("Content-Type", content.ContentType)
		if content.Bytes >= 0 {
			c.Header("Content-Length", strconv.FormatInt(content.Bytes, 10))
		}
		c.Status(content.Status)
		_, _ = io.Copy(c.Writer, content.Body)
	})
}

func listLibraryPrompts(c *gin.Context, db *persistence.Store) {
	if db == nil {
		authFailure(c, 503, "DATABASE_UNAVAILABLE", "提示词服务暂时不可用，请稍后重试。")
		return
	}
	limit, offset, ok := adminPagination(c)
	if !ok {
		return
	}
	items, hasMore, err := db.LibraryPrompts(c.Request.Context(), c.Query("q"), limit, offset)
	if err != nil {
		libraryFailure(c, err)
		return
	}
	c.JSON(200, gin.H{"prompts": items, "hasMore": hasMore})
}

func registerPromptAdmin(api *gin.RouterGroup, store AssetStore, db *persistence.Store) {
	api.GET("/prompts", func(c *gin.Context) {
		cleanupPromptImages(db, store)
		listLibraryPrompts(c, db)
	})
	api.POST("/prompts", func(c *gin.Context) {
		var body struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Content string `json:"content"`
		}
		if !decodeJSON(c, &body, 128<<10) {
			return
		}
		if err := db.SaveLibraryPrompt(c.Request.Context(), body.ID, body.Title, body.Content, true); err != nil {
			libraryFailure(c, err)
			return
		}
		c.JSON(201, gin.H{"id": body.ID})
	})
	api.PUT("/prompts/:id", func(c *gin.Context) {
		var body struct {
			Title   string `json:"title"`
			Content string `json:"content"`
		}
		if !decodeJSON(c, &body, 128<<10) {
			return
		}
		if err := db.SaveLibraryPrompt(c.Request.Context(), c.Param("id"), body.Title, body.Content, false); err != nil {
			libraryFailure(c, err)
			return
		}
		c.Status(204)
	})
	api.DELETE("/prompts/:id", func(c *gin.Context) {
		if err := db.DeleteLibraryPrompt(c.Request.Context(), c.Param("id")); err != nil {
			libraryFailure(c, err)
			return
		}
		cleanupPromptImages(db, store)
		c.Status(204)
	})
	api.PUT("/prompts/:id/images/:imageId", func(c *gin.Context) {
		id, imageID := c.Param("id"), c.Param("imageId")
		if err := db.LibraryPromptExists(c.Request.Context(), id); err != nil {
			libraryFailure(c, err)
			return
		}
		exists, err := db.LibraryImageExists(c.Request.Context(), id, imageID)
		if err != nil {
			libraryFailure(c, err)
			return
		}
		if exists {
			c.JSON(200, gin.H{"id": imageID, "url": "/api/prompt-images/" + imageID})
			return
		}
		media, ok := store.(promptImageStore)
		if !ok {
			storageFailure(c, &storage.Error{Status: 503, Code: "STORAGE_NOT_CONFIGURED", Message: "图片存储暂时不可用。"})
			return
		}
		contentType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if contentType != "application/octet-stream" {
			storageFailure(c, &storage.Error{Status: 415, Code: "INVALID_UPLOAD_TYPE", Message: "请上传图片文件。"})
			return
		}
		if c.Request.ContentLength > storage.MaxImageBytes {
			storageFailure(c, &storage.Error{Status: 413, Code: "FILE_TOO_LARGE", Message: "图片不能超过 20 MiB。"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, storage.MaxImageBytes)
		asset, err := media.SavePromptImage(c.Request.Context(), c.Request.Body, id, imageID)
		if err != nil {
			storageFailure(c, err)
			return
		}
		if err = db.AddLibraryImage(c.Request.Context(), id, imageID, asset.Key); err != nil {
			// A saved object must never become publicly visible without a DB record.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if deleteErr := media.DeletePromptImage(ctx, asset.Key); deleteErr != nil {
				if queueErr := db.QueuePromptImageCleanup(ctx, asset.Key); queueErr != nil {
					log.Printf("prompt image cleanup could not be queued, key=%s: %v", asset.Key, queueErr)
				}
			}
			cancel()
			libraryFailure(c, err)
			return
		}
		c.JSON(201, gin.H{"id": imageID, "url": "/api/prompt-images/" + imageID})
	})
	api.DELETE("/prompts/:id/images/:imageId", func(c *gin.Context) {
		if err := db.DeleteLibraryImage(c.Request.Context(), c.Param("id"), c.Param("imageId")); err != nil {
			libraryFailure(c, err)
			return
		}
		cleanupPromptImages(db, store)
		c.Status(204)
	})
}

// Delete from OSS only after the database transaction committed. Unfinished
// cleanup is retried when the administrator next visits or deletes content.
func cleanupPromptImages(db *persistence.Store, store AssetStore) {
	media, ok := store.(promptImageStore)
	if !ok || db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		key, err := db.PendingPromptImageCleanup(ctx)
		if err != nil {
			return
		}
		if err = media.DeletePromptImage(ctx, key); err != nil {
			log.Printf("prompt image cleanup pending: %v", err)
			return
		}
		if err = db.FinishPromptImageCleanup(ctx, key); err != nil {
			log.Printf("prompt image cleanup record pending: %v", err)
			return
		}
	}
}
