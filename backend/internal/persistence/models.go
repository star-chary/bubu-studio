package persistence

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
)

var (
	ErrNotFound                  = errors.New("not found")
	ErrConflict                  = errors.New("version or request conflict")
	ErrBusy                      = errors.New("generation busy")
	ErrInvalid                   = errors.New("invalid canvas or node")
	ErrPromptReferences          = errors.New("invalid prompt reference binding")
	ErrModelGenerationNotEnabled = errors.New("model generation not enabled")
)

// ProviderAPIMart 是节点模型草稿与目录共用的供应商标识。
// 没有 modelSelection 字段的历史节点按旧 Ark 格式读取，不迁移为 APIMart 任务。
const ProviderAPIMart = "apimart"

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Viewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}
type VideoOptions struct {
	Resolution    string `json:"resolution"`
	Ratio         string `json:"ratio"`
	Duration      int    `json:"duration"`
	GenerateAudio bool   `json:"generateAudio"`
}
type ImageNodeOptions struct {
	ark.ImageOptions
	Count int `json:"count"`
}

// ModelSelection 保存目录模型的选择草稿：模型身份由“提供商＋模型 ID”确定，
// 参数只保存用户实际选择的值；提示词与参考素材继续使用现有字段。
// 草稿允许尚未填写完整，也不因目录暂不可用、模型下架或参数规则更新而拒绝保存。
type ModelSelection struct {
	Provider       string         `json:"provider"`
	ModelID        string         `json:"modelId"`
	CatalogVersion string         `json:"catalogVersion,omitempty"`
	SchemaVersion  string         `json:"schemaVersion,omitempty"`
	Parameters     map[string]any `json:"parameters,omitempty"`
}
type NodeData struct {
	ImageOptions   *ImageNodeOptions `json:"imageOptions,omitempty"`
	VideoOptions   *VideoOptions     `json:"videoOptions,omitempty"`
	VideoModel     string            `json:"videoModel,omitempty"`
	VideoMode      string            `json:"videoMode,omitempty"`
	FirstFrameID   string            `json:"firstFrameId,omitempty"`
	Kind           string            `json:"kind"`
	Name           string            `json:"name"`
	Prompt         string            `json:"prompt,omitempty"`
	PromptParts    []PromptPart      `json:"promptParts,omitempty"`
	ImageModel     string            `json:"imageModel,omitempty"`
	ModelSelection *ModelSelection   `json:"modelSelection,omitempty"`
	Origin         string            `json:"origin,omitempty"`
	ReferenceIDs   []string          `json:"referenceIds,omitempty"`
}
type Node struct {
	ID       string   `json:"id"`
	Position Position `json:"position"`
	Data     NodeData `json:"data"`
}
type Snapshot struct {
	Nodes    []Node   `json:"nodes"`
	Viewport Viewport `json:"viewport"`
}
type Canvas struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Version   int64           `json:"version"`
	Snapshot  Snapshot        `json:"snapshot"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Assets    []storage.Asset `json:"assets"`
	Tasks     []Task          `json:"tasks"`
	Results   []Task          `json:"results"`
}
type CanvasPreviewNode struct {
	Position Position `json:"position"`
	Kind     string   `json:"kind"`
}
type CanvasSummary struct {
	ID        string              `json:"id"`
	Title     string              `json:"title"`
	UpdatedAt time.Time           `json:"updatedAt"`
	NodeCount int                 `json:"nodeCount"`
	Preview   []CanvasPreviewNode `json:"preview"`
}
type Input struct {
	Provider      string            `json:"provider,omitempty"`
	Parameters    map[string]any    `json:"parameters,omitempty"`
	ImageOptions  *ark.ImageOptions `json:"imageOptions,omitempty"`
	PromptParts   []PromptPart      `json:"promptParts,omitempty"`
	References    []Reference       `json:"references,omitempty"`
	Mode          string            `json:"mode,omitempty"`
	Resolution    string            `json:"resolution,omitempty"`
	Ratio         string            `json:"ratio,omitempty"`
	Duration      int               `json:"duration,omitempty"`
	GenerateAudio *bool             `json:"generateAudio,omitempty"`
	Prompt        string            `json:"prompt"`
	Model         string            `json:"model"`
	CanvasID      string            `json:"canvasId"`
	NodeID        string            `json:"nodeId"`
	ReferenceKeys []string          `json:"referenceKeys,omitempty"`
}
type Result struct {
	Resolution      string  `json:"resolution,omitempty"`
	Ratio           string  `json:"ratio,omitempty"`
	Duration        int     `json:"duration,omitempty"`
	FramesPerSecond float64 `json:"framesPerSecond,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	HasAudio        *bool   `json:"hasAudio,omitempty"`
	ark.ImageResult
	Asset        *storage.Asset `json:"asset,omitempty"`
	StorageError string         `json:"storageError,omitempty"`
}
type Task struct {
	CreditPoints       int64         `json:"creditPoints"`
	CreditPriceVersion string        `json:"creditPriceVersion,omitempty"`
	CreditStatus       string        `json:"creditStatus"`
	Kind               string        `json:"kind"`
	CompiledPrompt     string        `json:"compiledPrompt"`
	Bindings           []Reference   `json:"bindings"`
	ProviderTaskID     string        `json:"providerTaskId,omitempty"`
	ProviderStatus     string        `json:"providerStatus,omitempty"`
	ProviderURL        string        `json:"-"`
	PollingError       *ark.APIError `json:"pollingError,omitempty"`
	ID                 string        `json:"id"`
	Input              Input         `json:"input"`
	Status             string        `json:"status"`
	Result             *Result       `json:"result,omitempty"`
	Error              *ark.APIError `json:"error,omitempty"`
	CreatedAt          time.Time     `json:"createdAt"`
	StartedAt          *time.Time    `json:"startedAt,omitempty"`
	FinishedAt         *time.Time    `json:"finishedAt,omitempty"`
	UpdatedAt          time.Time     `json:"updatedAt"`
}

func ValidID(id string) bool {
	return storage.ValidateScope(storage.Scope{CanvasID: id, NodeID: id}) == nil
}
func jsonDepth(value any, depth int) bool {
	if depth > 8 {
		return false
	}
	switch v := value.(type) {
	case map[string]any:
		for _, child := range v {
			if !jsonDepth(child, depth+1) {
				return false
			}
		}
	case []any:
		for _, child := range v {
			if !jsonDepth(child, depth+1) {
				return false
			}
		}
	}
	return true
}

// validateModelSelection 只做草稿结构校验：长度、大小与嵌套深度。
// 不引用任何 Ark 模型白名单或目录规则，目录不可用不影响历史画布保存。
func validateModelSelection(sel *ModelSelection) error {
	if sel.Provider != ProviderAPIMart {
		return ErrInvalid
	}
	if utf8.RuneCountInString(sel.ModelID) > 200 || utf8.RuneCountInString(sel.CatalogVersion) > 64 || utf8.RuneCountInString(sel.SchemaVersion) > 64 {
		return ErrInvalid
	}
	if len(sel.Parameters) > 50 {
		return ErrInvalid
	}
	for key, value := range sel.Parameters {
		if utf8.RuneCountInString(key) > 64 || !jsonDepth(value, 0) {
			return ErrInvalid
		}
		if text, ok := value.(string); ok && utf8.RuneCountInString(text) > 2000 {
			return ErrInvalid
		}
	}
	payload, err := json.Marshal(sel.Parameters)
	if err != nil || len(payload) > 64<<10 {
		return ErrInvalid
	}
	return nil
}

func (s Snapshot) Validate() error {
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= 1e7 }
	if s.Nodes == nil || len(s.Nodes) > 300 || !finite(s.Viewport.X) || !finite(s.Viewport.Y) || s.Viewport.Zoom < .25 || s.Viewport.Zoom > 2 {
		return ErrInvalid
	}
	nodes := map[string]Node{}
	for _, n := range s.Nodes {
		if !ValidID(n.ID) || !finite(n.Position.X) || !finite(n.Position.Y) || (n.Data.Kind != "image" && n.Data.Kind != "video" && n.Data.Kind != "audio") || utf8.RuneCountInString(n.Data.Name) > 255 || strings.TrimSpace(n.Data.Name) == "" || utf8.RuneCountInString(n.Data.Prompt) > 10000 {
			return ErrInvalid
		}
		if _, ok := nodes[n.ID]; ok {
			return ErrInvalid
		}
		// 目录草稿节点只做结构校验，不套用 Ark 的模型白名单、分辨率与时长规则。
		apimart := n.Data.ModelSelection != nil
		if apimart {
			if err := validateModelSelection(n.Data.ModelSelection); err != nil {
				return ErrInvalid
			}
		}
		if options := n.Data.ImageOptions; options != nil {
			if n.Data.Kind != "image" {
				return ErrInvalid
			}
			if !apimart {
				if options.Count != 1 && options.Count != 2 && options.Count != 4 {
					return ErrInvalid
				}
				if _, err := ark.ResolveImageOptions(n.Data.ImageModel, &options.ImageOptions); err != nil {
					return ErrInvalid
				}
			}
		}
		if n.Data.Kind == "video" {
			if !apimart {
				in := Input{CanvasID: n.ID, NodeID: n.ID, Model: n.Data.VideoModel, Mode: n.Data.VideoMode, Duration: 4}
				if v := n.Data.VideoOptions; v != nil {
					in.Resolution, in.Ratio, in.Duration = v.Resolution, v.Ratio, v.Duration
				}
				if normalizeVideoOptions(&in) != nil {
					return ErrInvalid
				}
			}
		} else if n.Data.VideoOptions != nil || n.Data.VideoModel != "" || n.Data.VideoMode != "" {
			return ErrInvalid
		}
		if !validPromptParts(n) {
			return ErrInvalid
		}
		if n.Data.Origin != "" && n.Data.Origin != "upload" && n.Data.Origin != "generated" {
			return ErrInvalid
		}
		if n.Data.Kind == "audio" && (n.Data.Origin != "upload" || len(n.Data.ReferenceIDs) != 0 || n.Data.Prompt != "" || n.Data.ImageModel != "") {
			return ErrInvalid
		}
		if !apimart {
			if _, err := ark.ResolveImageModel(n.Data.ImageModel); err != nil {
				return ErrInvalid
			}
		}
		nodes[n.ID] = n
	}
	for _, n := range s.Nodes {
		seen := map[string]bool{}
		for _, id := range n.Data.ReferenceIDs {
			source, ok := nodes[id]
			if !ok || id == n.ID || seen[id] || (n.Data.Kind == "image" && source.Data.Kind != "image") {
				return ErrInvalid
			}
			seen[id] = true
		}
		if n.Data.FirstFrameID != "" {
			source, ok := nodes[n.Data.FirstFrameID]
			if n.Data.Kind != "video" || !ok || !seen[n.Data.FirstFrameID] || source.Data.Kind != "image" {
				return ErrInvalid
			}
		}
	}
	return nil
}
func sameInput(a, b Input) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
