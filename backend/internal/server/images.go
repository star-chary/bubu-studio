package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/storage"
	"github.com/gin-gonic/gin"
)

type imageGenerator interface {
	Generate(context.Context, string, string, *ark.ImageOptions, ...string) (ark.ImageResult, error)
}

func imageHandler(generator imageGenerator, store AssetStore, db *persistence.Store) gin.HandlerFunc {
	// Legacy no-database handler guard (production routes require a database).
	// Persisted requests use the independent image queue and return above this guard.
	var generating atomic.Bool
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		fail := func(status int, code, message string) {
			c.JSON(status, gin.H{"error": &ark.APIError{Status: status, Code: code, Message: message}})
		}
		mediaType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if mediaType != "application/json" {
			fail(415, "INVALID_CONTENT_TYPE", "请使用 JSON 提交提示词。")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		var input struct {
			Provider       string                   `json:"provider"`
			Parameters     map[string]any           `json:"parameters"`
			AcceptedPoints int64                    `json:"acceptedPoints"`
			PriceVersion   string                   `json:"priceVersion"`
			PromptParts    []persistence.PromptPart `json:"promptParts"`
			TaskID         string                   `json:"taskId"`
			Prompt         string                   `json:"prompt"`
			Model          string                   `json:"model"`
			CanvasID       string                   `json:"canvasId"`
			NodeID         string                   `json:"nodeId"`
			ReferenceKeys  []string                 `json:"referenceKeys"`
			ImageOptions   *ark.ImageOptions        `json:"imageOptions"`
		}
		if err := decoder.Decode(&input); err != nil {
			fail(400, "INVALID_REQUEST", "请求格式无效，请检查提示词、模型及画布和节点 ID。")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			fail(400, "INVALID_REQUEST", "请提交一个有效的 JSON 对象。")
			return
		}
		prompt := strings.TrimSpace(input.Prompt)
		if prompt == "" || utf8.RuneCountInString(prompt) > 2000 {
			fail(400, "INVALID_PROMPT", "请输入 1～2000 字的提示词。")
			return
		}
		if db != nil {
			if !persistence.ValidID(input.TaskID) {
				fail(400, "INVALID_TASK_ID", "请提供唯一的任务 UUID。")
				return
			}
			if !authorizeCanvas(c, db, input.CanvasID) {
				return
			}
			if input.Provider == persistence.ProviderAPIMart {
				request := persistence.Input{Provider: input.Provider, Parameters: input.Parameters, Prompt: input.Prompt, PromptParts: input.PromptParts, Model: input.Model, ImageOptions: input.ImageOptions, CanvasID: input.CanvasID, NodeID: input.NodeID, ReferenceKeys: input.ReferenceKeys}
				existing, lookup := db.Task(c.Request.Context(), input.TaskID)
				if lookup == nil {
					if !authorizeTask(c, db, existing.ID) {
						return
					}
				} else if !errors.Is(lookup, persistence.ErrNotFound) {
					persistenceFailure(c, lookup)
					return
				} else {
					if err := db.RequireGenerationEnabled(c.Request.Context(), input.CanvasID, input.NodeID); err != nil {
						persistenceFailure(c, err)
						return
					}
					if store == nil {
						fail(503, "IMAGE_STORAGE_REQUIRED", "图片生成需要启用素材保存服务。")
						return
					}
				}
				task, err := db.CreateAPIMartImageTask(c.Request.Context(), c.GetString("userID"), input.TaskID, request)
				if err != nil {
					videoHTTPFailure(c, err)
					return
				}
				c.JSON(taskHTTPStatus(task), task)
				return
			}
			if input.Provider != "" || input.Parameters != nil {
				fail(400, "INVALID_MODEL", "图片供应商或参数无效。")
				return
			}
			// 已有任务的幂等返回保持原语义，不因节点供应商变化而改判。
			if existing, e := db.Task(c.Request.Context(), input.TaskID); e == nil {
				if !authorizeTask(c, db, existing.ID) {
					return
				}
				task, err := db.CreateBilledTask(c.Request.Context(), c.GetString("userID"), input.TaskID, "image", persistence.Input{Prompt: func() string {
					if len(input.PromptParts) > 0 {
						return input.Prompt
					}
					return prompt
				}(), PromptParts: input.PromptParts, Model: input.Model, ImageOptions: input.ImageOptions, CanvasID: input.CanvasID, NodeID: input.NodeID, ReferenceKeys: input.ReferenceKeys}, input.AcceptedPoints, input.PriceVersion)
				if err != nil {
					persistenceFailure(c, err)
					return
				}
				c.JSON(http.StatusAccepted, task)
				return
			} else if !errors.Is(e, persistence.ErrNotFound) {
				persistenceFailure(c, e)
				return
			}
			// 目录草稿节点：拒绝发生在 Ark 模型解析、报价与任务创建之前，
			// 依据是已保存节点的供应商选择，不信任客户端提交的标记。
			if err := db.RequireGenerationEnabled(c.Request.Context(), input.CanvasID, input.NodeID); err != nil {
				persistenceFailure(c, err)
				return
			}
			model, err := ark.ResolveImageModel(input.Model)
			if err != nil {
				fail(400, "INVALID_MODEL", err.Error())
				return
			}
			if _, err := ark.ResolveImageOptions(model, input.ImageOptions); err != nil {
				var failure *ark.APIError
				if errors.As(err, &failure) {
					fail(failure.Status, failure.Code, failure.Message)
				}
				return
			}
			if len(input.ReferenceKeys) > ark.MaxReferenceImages(model) {
				fail(400, "TOO_MANY_REFERENCES", fmt.Sprintf("所选模型最多支持 %d 张参考图片。", ark.MaxReferenceImages(model)))
				return
			}
			task, err := db.CreateBilledTask(c.Request.Context(), c.GetString("userID"), input.TaskID, "image", persistence.Input{Prompt: func() string {
				if len(input.PromptParts) > 0 {
					return input.Prompt
				}
				return prompt
			}(), PromptParts: input.PromptParts, Model: model, ImageOptions: input.ImageOptions, CanvasID: input.CanvasID, NodeID: input.NodeID, ReferenceKeys: input.ReferenceKeys}, input.AcceptedPoints, input.PriceVersion)
			if err != nil {
				persistenceFailure(c, err)
				return
			}
			c.JSON(http.StatusAccepted, task)
			return
		}
		if input.Provider != "" || input.Parameters != nil {
			fail(400, "INVALID_MODEL", "图片供应商或参数无效。")
			return
		}
		model, err := ark.ResolveImageModel(input.Model)
		if err != nil {
			fail(400, "INVALID_MODEL", err.Error())
			return
		}
		if _, err := ark.ResolveImageOptions(model, input.ImageOptions); err != nil {
			var failure *ark.APIError
			if errors.As(err, &failure) {
				fail(failure.Status, failure.Code, failure.Message)
			}
			return
		}
		if len(input.ReferenceKeys) > ark.MaxReferenceImages(model) {
			fail(400, "TOO_MANY_REFERENCES", fmt.Sprintf("所选模型最多支持 %d 张参考图片。", ark.MaxReferenceImages(model)))
			return
		}
		if len(input.ReferenceKeys) > 0 && store == nil {
			fail(503, "REFERENCE_STORAGE_REQUIRED", "图生图需要先启用素材保存服务，并保存参考图片。")
			return
		}
		scope := storage.Scope{CanvasID: input.CanvasID, NodeID: input.NodeID}
		if store != nil || db != nil {
			if err := storage.ValidateScope(scope); err != nil {
				fail(400, "INVALID_ASSET_SCOPE", err.Error())
				return
			}
		}
		if len(input.PromptParts) > 0 {
			fail(503, "DATABASE_REQUIRED", "素材标签生成需要启用画布保存服务。")
			return
		}
		if !generating.CompareAndSwap(false, true) {
			fail(409, "GENERATION_BUSY", "已有图片正在生成，请等待完成后再试。")
			return
		}
		defer generating.Store(false)
		var referenceURLs []string
		if len(input.ReferenceKeys) > 0 {
			referenceURLs, err = store.ReferenceURLs(c.Request.Context(), scope, input.ReferenceKeys)
			if err != nil {
				storageFailure(c, err)
				return
			}
		}
		result, err := generator.Generate(c.Request.Context(), prompt, model, input.ImageOptions, referenceURLs...)
		if err != nil {
			var apiError *ark.APIError
			if errors.As(err, &apiError) {
				fail(apiError.Status, apiError.Code, apiError.Message)
			} else {
				fail(502, "GENERATION_FAILED", "图片生成失败，请稍后再试。")
			}
			return
		}
		response := struct {
			ark.ImageResult
			Asset        *storage.Asset `json:"asset,omitempty"`
			StorageError string         `json:"storageError,omitempty"`
		}{ImageResult: result}
		if store != nil {
			asset, err := store.SaveGenerated(c.Request.Context(), result.URL, result.Model, scope)
			if err != nil {
				// Generation has already succeeded and may have been billed. Preserve
				// the temporary preview; never re-run the model to retry storage.
				response.StorageError = "图片已生成，但云端保存失败，当前展示临时图片，请及时下载。生成按钮会重新生成图片，不是重试保存。"
			} else {
				response.URL = asset.URL
				response.Asset = &asset
			}
		}
		c.JSON(http.StatusOK, response)
	}
}
