package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/persistence"
	"github.com/gin-gonic/gin"
)

type videoBody struct {
	AcceptedPoints int64                    `json:"acceptedPoints"`
	PriceVersion   string                   `json:"priceVersion"`
	TaskID         string                   `json:"taskId"`
	Prompt         string                   `json:"prompt"`
	PromptParts    []persistence.PromptPart `json:"promptParts"`
	CanvasID       string                   `json:"canvasId"`
	NodeID         string                   `json:"nodeId"`
	Model          string                   `json:"model"`
	Mode           string                   `json:"mode"`
	References     []persistence.Reference  `json:"references"`
	Resolution     string                   `json:"resolution"`
	Ratio          string                   `json:"ratio"`
	Duration       int                      `json:"duration"`
	GenerateAudio  bool                     `json:"generateAudio"`
}

func videoHTTPFailure(c *gin.Context, err error) {
	var api *ark.APIError
	if errors.As(err, &api) {
		c.JSON(api.Status, gin.H{"error": api})
		return
	}
	if errors.Is(err, persistence.ErrConflict) {
		c.JSON(409, gin.H{"error": gin.H{"code": "TASK_ID_CONFLICT", "message": "此任务 ID 已用于其他输入。"}})
		return
	}
	persistenceFailure(c, err)
}
func taskHTTPStatus(t persistence.Task) int {
	switch t.Status {
	case "queued", "preparing", "submitting", "running", "saving":
		return 202
	}
	return 200
}
func videoHandler(store AssetStore, db *persistence.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		fail := func(code, message string) { c.JSON(400, gin.H{"error": gin.H{"code": code, "message": message}}) }
		contentType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if contentType != "application/json" {
			c.JSON(415, gin.H{"error": gin.H{"code": "INVALID_CONTENT_TYPE", "message": "请提交 JSON。"}})
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10))
		if err != nil {
			fail("INVALID_REQUEST", "视频请求超过大小限制。")
			return
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(body, &fields) != nil || fields == nil {
			fail("INVALID_REQUEST", "请求必须是 JSON 对象。")
			return
		}
		for _, v := range fields {
			if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				fail("INVALID_REQUEST", "字段不能为 null。")
				return
			}
		}
		in := videoBody{Duration: 4, GenerateAudio: true}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil {
			fail("INVALID_REQUEST", "视频参数类型或字段无效。")
			return
		}
		if !persistence.ValidID(in.TaskID) {
			fail("INVALID_TASK_ID", "请提供任务 UUID。")
			return
		}
		input := persistence.Input{Prompt: in.Prompt, PromptParts: in.PromptParts, CanvasID: in.CanvasID, NodeID: in.NodeID, Model: in.Model, Mode: in.Mode, References: in.References, Resolution: in.Resolution, Ratio: in.Ratio, Duration: in.Duration, GenerateAudio: &in.GenerateAudio}
		if persistence.NormalizeVideoInput(&input) != nil {
			fail("INVALID_VIDEO_INPUT", "视频参数无效，请检查模型、时长、画质、比例和提示词。")
			return
		}
		if db == nil {
			c.JSON(503, gin.H{"error": gin.H{"code": "DATABASE_NOT_CONFIGURED", "message": "视频生成需要 PostgreSQL。"}})
			return
		}
		if !authorizeCanvas(c, db, input.CanvasID) {
			return
		}
		// Resolve an existing idempotency key before checking mutable service state.
		existing, lookup := db.Task(c.Request.Context(), in.TaskID)
		if lookup == nil {
			if !authorizeTask(c, db, existing.ID) {
				return
			}
			t, err := db.CreateBilledTask(c.Request.Context(), c.GetString("userID"), in.TaskID, "video", input, in.AcceptedPoints, in.PriceVersion)
			if err != nil {
				videoHTTPFailure(c, err)
				return
			}
			c.JSON(taskHTTPStatus(existing), t)
			return
		}
		if !errors.Is(lookup, persistence.ErrNotFound) {
			videoHTTPFailure(c, lookup)
			return
		}
		if _, ok := store.(persistence.VideoStorage); !ok {
			c.JSON(503, gin.H{"error": gin.H{"code": "VIDEO_STORAGE_REQUIRED", "message": "视频生成需要启用素材保存服务。"}})
			return
		}
		t, err := db.CreateBilledTask(c.Request.Context(), c.GetString("userID"), in.TaskID, "video", input, in.AcceptedPoints, in.PriceVersion)
		if err != nil {
			videoHTTPFailure(c, err)
			return
		}
		c.JSON(taskHTTPStatus(t), t)
	}
}
