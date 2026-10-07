package server

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/storage"
	"github.com/gin-gonic/gin"
)

type AssetStore interface {
	ReferenceURLs(context.Context, storage.Scope, []string) ([]string, error)
	SaveUpload(context.Context, io.Reader, storage.Scope) (storage.Asset, error)
	SaveGenerated(context.Context, string, string, storage.Scope) (storage.Asset, error)
	Read(context.Context, string, string) (*storage.Content, error)
}

func storageFailure(c *gin.Context, err error) {
	var failure *storage.Error
	if !errors.As(err, &failure) {
		failure = &storage.Error{Status: 502, Code: "STORAGE_FAILED", Message: "文件保存服务暂时不可用。"}
	}
	c.JSON(failure.Status, gin.H{"error": gin.H{"code": failure.Code, "message": failure.Message}})
}

func registerAssets(router *gin.Engine, store AssetStore, db *persistence.Store) {
	router.GET("/api/storage/config", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(200, gin.H{"enabled": store != nil, "maxImageBytes": storage.MaxImageBytes, "maxVideoBytes": storage.MaxVideoBytes, "maxAudioBytes": storage.MaxAudioBytes})
	})
	router.POST("/api/assets", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if store == nil {
			storageFailure(c, &storage.Error{Status: 503, Code: "STORAGE_NOT_CONFIGURED", Message: "后端尚未启用 OSS，当前只能本地预览。"})
			return
		}
		// Raw binary avoids multipart temp files on the system drive. Requiring
		// this non-simple Content-Type also prevents cross-site form uploads.
		contentType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if contentType != "application/octet-stream" {
			storageFailure(c, &storage.Error{Status: 415, Code: "INVALID_UPLOAD_TYPE", Message: "请按二进制方式上传单个文件。"})
			return
		}
		scope := storage.Scope{CanvasID: c.Query("canvasId"), NodeID: c.Query("nodeId")}
		if err := storage.ValidateScope(scope); err != nil {
			storageFailure(c, &storage.Error{Status: 400, Code: "INVALID_ASSET_SCOPE", Message: err.Error()})
			return
		}
		if c.Request.ContentLength > storage.MaxVideoBytes {
			storageFailure(c, &storage.Error{Status: 413, Code: "FILE_TOO_LARGE", Message: "视频不能超过 200 MB。"})
			return
		}
		if db != nil {
			if !authorizeCanvas(c, db, scope.CanvasID) {
				return
			}
			node, err := db.Node(c.Request.Context(), scope.CanvasID, scope.NodeID)
			if err != nil {
				persistenceFailure(c, err)
				return
			}
			if node.Data.Origin != "upload" {
				persistenceFailure(c, persistence.ErrInvalid)
				return
			}
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, storage.MaxVideoBytes)
		asset, err := store.SaveUpload(c.Request.Context(), c.Request.Body, scope)
		if err != nil {
			storageFailure(c, err)
			return
		}
		if db != nil {
			if err := db.RecordAsset(c.Request.Context(), asset); err != nil {
				persistenceFailure(c, err)
				return
			}
		}
		c.JSON(http.StatusCreated, asset)
	})
	router.GET("/api/assets/content", func(c *gin.Context) {
		if store == nil {
			storageFailure(c, &storage.Error{Status: 503, Code: "STORAGE_NOT_CONFIGURED", Message: "后端尚未启用 OSS。"})
			return
		}
		if db != nil {
			if err := db.RequireAssetOwner(c.Request.Context(), c.Query("key"), c.GetString("userID")); err != nil {
				persistenceFailure(c, err)
				return
			}
		}
		content, err := store.Read(c.Request.Context(), c.Query("key"), c.GetHeader("Range"))
		if err != nil {
			storageFailure(c, err)
			return
		}
		defer content.Body.Close()
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Disposition", "inline")
		c.Header("Content-Type", content.ContentType)
		c.Header("Accept-Ranges", "bytes")
		if content.ContentRange != "" {
			c.Header("Content-Range", content.ContentRange)
		}
		if content.ETag != "" {
			c.Header("ETag", content.ETag)
		}
		if content.Bytes >= 0 {
			c.Header("Content-Length", strconv.FormatInt(content.Bytes, 10))
		}
		c.Status(content.Status)
		_, _ = io.Copy(c.Writer, content.Body)
	})
}
