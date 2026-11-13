package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"frame-space/backend/internal/apimart"
	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listGenerationModelsInput struct {
	Kind string `json:"kind,omitempty"`
}

var (
	errMCPSingleImage      = errors.New("当前工具一次只支持生成一张图片，请将节点数量设为 1")
	errMCPReferencePending = errors.New("参考素材正在生成或保存，请等待完成")
)

type mcpGenerationModel struct {
	Kind               string           `json:"kind"`
	Provider           string           `json:"provider"`
	ModelID            string           `json:"modelId"`
	Name               string           `json:"name"`
	Modes              []string         `json:"modes"`
	Parameters         []map[string]any `json:"parameters"`
	MaxReferenceImages int              `json:"maxReferenceImages"`
	MaxReferenceVideos int              `json:"maxReferenceVideos,omitempty"`
	MaxReferenceAudios int              `json:"maxReferenceAudios,omitempty"`
}

type listGenerationModelsOutput struct {
	Models []mcpGenerationModel `json:"models"`
}

type configureCanvasNodeInput struct {
	CanvasID        string                      `json:"canvasId"`
	NodeID          string                      `json:"nodeId"`
	ExpectedVersion *int64                      `json:"expectedVersion"`
	Patch           persistence.CanvasNodePatch `json:"patch"`
}

type configureCanvasNodeOutput struct {
	CanvasID  string           `json:"canvasId"`
	Node      persistence.Node `json:"node"`
	Version   int64            `json:"version"`
	CanvasURL string           `json:"canvasUrl"`
}

type generationNodeInput struct {
	CanvasID string `json:"canvasId"`
	NodeID   string `json:"nodeId"`
}

type quoteGenerationOutput struct {
	CanvasID      string                    `json:"canvasId"`
	NodeID        string                    `json:"nodeId"`
	Kind          string                    `json:"kind"`
	CanvasVersion int64                     `json:"canvasVersion"`
	Points        int64                     `json:"points"`
	PriceVersion  string                    `json:"priceVersion"`
	QuoteHash     string                    `json:"quoteHash"`
	Credits       persistence.CreditAccount `json:"credits"`
}

type startGenerationInput struct {
	CanvasID      string `json:"canvasId"`
	NodeID        string `json:"nodeId"`
	TaskID        string `json:"taskId"`
	CanvasVersion *int64 `json:"canvasVersion"`
	Points        *int64 `json:"points"`
	PriceVersion  string `json:"priceVersion"`
	QuoteHash     string `json:"quoteHash"`
}

type getGenerationTaskInput struct {
	TaskID string `json:"taskId"`
}

type generationTaskOutput struct {
	TaskID       string              `json:"taskId"`
	CanvasID     string              `json:"canvasId"`
	NodeID       string              `json:"nodeId"`
	Kind         string              `json:"kind"`
	Status       string              `json:"status"`
	CreditPoints int64               `json:"creditPoints"`
	CreditStatus string              `json:"creditStatus"`
	UpdatedAt    string              `json:"updatedAt"`
	CanvasURL    string              `json:"canvasUrl"`
	Error        *mcpCanvasTaskError `json:"error,omitempty"`
	Result       *mcpCanvasResult    `json:"result,omitempty"`
	Asset        *mcpCanvasAsset     `json:"asset,omitempty"`
}

func registerMCPGenerationTools(server *mcp.Server, g *mcpGateway) {
	id := func(description string) map[string]any {
		return map[string]any{"type": "string", "minLength": 36, "maxLength": 36, "description": description}
	}
	parameterObject := map[string]any{"type": "object", "additionalProperties": true}
	modelSchema := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"provider", "modelId"}, "properties": map[string]any{
		"provider": map[string]any{"type": "string", "enum": []string{"ark", "apimart"}},
		"modelId":  map[string]any{"type": "string", "minLength": 1, "maxLength": 200}, "parameters": parameterObject,
	}}
	promptPart := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"type"}, "properties": map[string]any{
		"type": map[string]any{"type": "string", "enum": []string{"text", "reference"}}, "text": map[string]any{"type": "string"},
		"nodeId": id("被引用节点 ID"), "kind": map[string]any{"type": "string", "enum": []string{"image", "video"}}, "name": map[string]any{"type": "string"},
	}}
	patchSchema := map[string]any{"type": "object", "additionalProperties": false, "minProperties": 1, "properties": map[string]any{
		"name": map[string]any{"type": "string", "maxLength": 255}, "prompt": map[string]any{"type": "string", "maxLength": 10000},
		"promptParts": map[string]any{"type": "array", "maxItems": 1000, "items": promptPart}, "model": modelSchema,
		"imageOptions": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"resolution", "ratio"}, "properties": map[string]any{"resolution": map[string]any{"type": "string"}, "ratio": map[string]any{"type": "string"}, "count": map[string]any{"type": "integer", "enum": []int{1}}}},
		"videoOptions": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"resolution", "ratio", "duration", "generateAudio"}, "properties": map[string]any{"resolution": map[string]any{"type": "string"}, "ratio": map[string]any{"type": "string"}, "duration": map[string]any{"type": "integer"}, "generateAudio": map[string]any{"type": "boolean"}}},
		"videoMode":    map[string]any{"type": "string", "enum": []string{"text", "image", "reference"}},
		"referenceIds": map[string]any{"type": "array", "maxItems": 50, "items": id("同画布参考节点 ID")}, "firstFrameId": map[string]any{"type": "string", "maxLength": 36},
	}}
	mcp.AddTool(server, &mcp.Tool{Name: "list_generation_models", Title: "查看生成模型", Description: "列出当前支持报价和生成的图片、视频模型及参数。每个图片任务输出一张；参考模式、参数和供应商必须以实际配置为准。", InputSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"image", "video"}}}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, g.listGenerationModels)
	mcp.AddTool(server, &mcp.Tool{Name: "configure_canvas_node", Title: "配置画布节点", Description: "修改已保存图片或视频节点的提示词、模型、参数和同画布参考。必须提供读取或创建时得到的画布版本；请先保存并离开网页中的画布。修改 prompt 且未提供 promptParts 时会清除旧结构化标签；model.parameters 是整组替换。不会提交生成或扣积分。", InputSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"canvasId", "nodeId", "expectedVersion", "patch"}, "properties": map[string]any{"canvasId": id("画布 ID"), "nodeId": id("目标节点 ID"), "expectedVersion": map[string]any{"type": "integer", "minimum": 0}, "patch": patchSchema}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false}}, g.configureCanvasNode)
	mcp.AddTool(server, &mcp.Tool{Name: "quote_generation", Title: "查询生成报价", Description: "依据已保存节点及参考素材获取本次生成积分和账户余额，不提交任务。每次图片任务生成一张。提交时必须原样使用本次返回的版本、报价和 quoteHash；节点或报价变化后请重新报价。", InputSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"canvasId", "nodeId"}, "properties": map[string]any{"canvasId": id("画布 ID"), "nodeId": id("节点 ID")}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, g.quoteGeneration)
	mcp.AddTool(server, &mcp.Tool{Name: "start_generation", Title: "提交生成任务", Description: "提交已报价的图片或视频任务，会冻结并可能消耗用户积分。只在用户要求生成且接受报价后调用；taskId 应由客户端预先生成，重试同一任务必须复用同一 ID 和报价。结果不确定时先查询任务，切勿创建新任务自动重试。", InputSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"canvasId", "nodeId", "taskId", "canvasVersion", "points", "priceVersion", "quoteHash"}, "properties": map[string]any{"canvasId": id("画布 ID"), "nodeId": id("节点 ID"), "taskId": id("客户端预先生成的任务 UUID"), "canvasVersion": map[string]any{"type": "integer", "minimum": 0}, "points": map[string]any{"type": "integer", "minimum": 1}, "priceVersion": map[string]any{"type": "string", "minLength": 1, "maxLength": 200}, "quoteHash": map[string]any{"type": "string", "minLength": 64, "maxLength": 64}}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: true}}, g.startGeneration)
	mcp.AddTool(server, &mcp.Tool{Name: "get_generation_task", Title: "查询生成任务", Description: "查询用户自己画布的生成任务状态、积分状态和保存结果；返回画布链接，不返回私有对象或供应商地址。", InputSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"taskId"}, "properties": map[string]any{"taskId": id("任务 ID")}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, g.getGenerationTask)
}

func mcpActor(request *mcp.CallToolRequest, scope string) (string, error) {
	if request.Extra == nil || request.Extra.TokenInfo == nil {
		return "", errors.New("需要有效的用户授权")
	}
	if !slices.Contains(request.Extra.TokenInfo.Scopes, scope) {
		return "", errors.New("缺少所需权限")
	}
	return request.Extra.TokenInfo.UserID, nil
}

func parameterSummary(parameter apimart.VideoParameter, singleImage bool) map[string]any {
	if singleImage && parameter.Key == "n" {
		return map[string]any{"key": "n", "kind": "fixed", "fixed": 1}
	}
	out := map[string]any{"key": parameter.Key, "kind": parameter.Kind}
	if parameter.Role != "" {
		out["role"] = parameter.Role
	}
	if len(parameter.Aliases) != 0 {
		out["aliases"] = parameter.Aliases
	}
	if parameter.Default != nil {
		out["default"] = parameter.Default
	}
	if parameter.Fixed != nil {
		out["fixed"] = parameter.Fixed
	}
	if parameter.Enum != nil {
		out["enum"] = parameter.Enum
	}
	if parameter.Minimum != nil {
		out["minimum"] = *parameter.Minimum
	}
	if parameter.Maximum != nil {
		out["maximum"] = *parameter.Maximum
	}
	if len(parameter.Conditions) != 0 {
		out["conditions"] = parameter.Conditions
	}
	if len(parameter.SpecialValues) != 0 {
		out["specialValues"] = parameter.SpecialValues
	}
	return out
}

func (g *mcpGateway) listGenerationModels(_ context.Context, request *mcp.CallToolRequest, input listGenerationModelsInput) (*mcp.CallToolResult, listGenerationModelsOutput, error) {
	output := listGenerationModelsOutput{Models: []mcpGenerationModel{}}
	if _, err := mcpActor(request, mcpReadScope); err != nil {
		return nil, output, err
	}
	if input.Kind != "" && input.Kind != "image" && input.Kind != "video" {
		return nil, output, errors.New("模型类型无效")
	}
	if input.Kind == "" || input.Kind == "image" {
		for _, model := range apimart.ImageDefinitions() {
			if !apimart.Billable("image", model.ModelID) || model.ModelID == "gemini-nano-banana-2.1-ext" {
				continue
			}
			item := mcpGenerationModel{Kind: "image", Provider: "apimart", ModelID: model.ModelID, Name: model.ModelID, Modes: []string{"text", "reference"}, Parameters: []map[string]any{}, MaxReferenceImages: model.MaxReferenceImages}
			for _, p := range model.Parameters {
				item.Parameters = append(item.Parameters, parameterSummary(p, true))
			}
			output.Models = append(output.Models, item)
		}
		for _, model := range []string{ark.DefaultImageModel, ark.ProImageModel} {
			resolutions := []string{"2K", "3K", "4K"}
			if model == ark.ProImageModel {
				resolutions = []string{"1K", "1.5K", "2K"}
			}
			name := "Seedream 5.0 Lite"
			if model == ark.ProImageModel {
				name = "Seedream 5.0 Pro"
			}
			output.Models = append(output.Models, mcpGenerationModel{Kind: "image", Provider: "ark", ModelID: model, Name: name, Modes: []string{"text", "reference"}, MaxReferenceImages: ark.MaxReferenceImages(model), Parameters: []map[string]any{{"key": "resolution", "kind": "enum", "enum": resolutions, "default": "2K"}, {"key": "ratio", "kind": "enum", "enum": []string{"adaptive", "1:1", "4:3", "3:4", "16:9", "9:16", "3:2", "2:3", "21:9"}, "default": "adaptive"}}})
		}
	}
	if input.Kind == "" || input.Kind == "video" {
		for _, model := range apimart.VideoDefinitions() {
			if !model.GenerationEnabled || len(model.Modes) == 0 || !apimart.Billable("video", model.ModelID) {
				continue
			}
			maximumImages := model.ReferenceLimit("reference")
			if slices.Contains(model.Modes, "image") {
				maximumImages = max(1, maximumImages)
			}
			item := mcpGenerationModel{Kind: "video", Provider: "apimart", ModelID: model.ModelID, Name: model.Name, Modes: model.Modes, Parameters: []map[string]any{}, MaxReferenceImages: maximumImages, MaxReferenceVideos: model.MaxReferenceVideos}
			for _, p := range model.Parameters {
				parameter := parameterSummary(p, false)
				if p.Role == "duration" {
					delete(parameter, "specialValues") // Automatic duration cannot be quoted.
				}
				item.Parameters = append(item.Parameters, parameter)
			}
			output.Models = append(output.Models, item)
		}
		for _, model := range []string{ark.VideoModel, ark.VideoModel20, ark.VideoModel20Fast, ark.VideoModel20Mini} {
			spec, _ := ark.VideoSpec(model)
			output.Models = append(output.Models, mcpGenerationModel{Kind: "video", Provider: "ark", ModelID: model, Name: model, Modes: []string{"text", "reference"}, MaxReferenceImages: spec.MaxImages, MaxReferenceVideos: spec.MaxVideos, MaxReferenceAudios: spec.MaxAudios, Parameters: []map[string]any{{"key": "resolution", "kind": "enum", "enum": []string{"480p", "720p"}, "default": "480p"}, {"key": "ratio", "kind": "enum", "enum": []string{"16:9", "4:3", "1:1", "3:4", "9:16", "21:9", "adaptive"}, "default": "16:9"}, {"key": "duration", "kind": "integer", "minimum": 4, "maximum": spec.MaxDuration, "default": 4}, {"key": "generateAudio", "kind": "boolean", "default": true}}})
		}
	}
	return nil, output, nil
}

func (g *mcpGateway) configureCanvasNode(ctx context.Context, request *mcp.CallToolRequest, input configureCanvasNodeInput) (*mcp.CallToolResult, configureCanvasNodeOutput, error) {
	var output configureCanvasNodeOutput
	userID, err := mcpActor(request, mcpWriteScope)
	if err != nil {
		return nil, output, err
	}
	if input.ExpectedVersion == nil {
		return nil, output, errors.New("需要画布版本")
	}
	node, version, err := g.db.ConfigureOwnedCanvasNode(ctx, userID, input.CanvasID, input.NodeID, *input.ExpectedVersion, input.Patch)
	if err != nil {
		return nil, output, g.generationFailure("configure_canvas_node", userID, err)
	}
	return nil, configureCanvasNodeOutput{CanvasID: input.CanvasID, Node: node, Version: version, CanvasURL: g.config.Origin + "/?canvas=" + input.CanvasID}, nil
}

func generationQuoteHash(userID string, version int64, kind string, input persistence.Input, quote persistence.CreditQuote) string {
	raw, _ := json.Marshal(struct {
		UserID  string
		Version int64
		Kind    string
		Input   persistence.Input
		Quote   persistence.CreditQuote
	}{userID, version, kind, input, quote})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (g *mcpGateway) quoteGeneration(ctx context.Context, request *mcp.CallToolRequest, input generationNodeInput) (*mcp.CallToolResult, quoteGenerationOutput, error) {
	var output quoteGenerationOutput
	userID, err := mcpActor(request, mcpReadScope)
	if err != nil {
		return nil, output, err
	}
	version, kind, taskInput, err := g.generationInput(ctx, userID, input.CanvasID, input.NodeID)
	if err != nil {
		return nil, output, g.generationFailure("quote_generation", userID, err)
	}
	quote, err := g.db.QuoteCredits(ctx, userID, kind, taskInput)
	if err != nil {
		return nil, output, g.generationFailure("quote_generation", userID, err)
	}
	credits, err := g.db.Credits(ctx, userID)
	if err != nil {
		return nil, output, g.generationFailure("quote_generation", userID, err)
	}
	return nil, quoteGenerationOutput{CanvasID: input.CanvasID, NodeID: input.NodeID, Kind: kind, CanvasVersion: version, Points: quote.Points, PriceVersion: quote.PriceVersion, QuoteHash: generationQuoteHash(userID, version, kind, taskInput, quote), Credits: credits}, nil
}

func (g *mcpGateway) startGeneration(ctx context.Context, request *mcp.CallToolRequest, input startGenerationInput) (*mcp.CallToolResult, generationTaskOutput, error) {
	var output generationTaskOutput
	userID, err := mcpActor(request, mcpGenerateScope)
	if err != nil {
		return nil, output, err
	}
	if !persistence.ValidID(input.CanvasID) || !persistence.ValidID(input.NodeID) || !persistence.ValidID(input.TaskID) || input.CanvasVersion == nil || *input.CanvasVersion < 0 || input.Points == nil || *input.Points < 1 || len(input.PriceVersion) == 0 || len(input.PriceVersion) > 200 || len(input.QuoteHash) != 64 {
		return nil, output, errors.New("任务、画布或报价参数无效")
	}
	quote := persistence.CreditQuote{Points: *input.Points, PriceVersion: input.PriceVersion}
	if existing, lookup := g.db.Task(ctx, input.TaskID); lookup == nil {
		if err = g.db.RequireTaskOwner(ctx, input.TaskID, userID); err != nil {
			return nil, output, g.generationFailure("start_generation", userID, err)
		}
		if existing.Input.CanvasID != input.CanvasID || existing.Input.NodeID != input.NodeID || existing.CreditPoints != quote.Points || existing.CreditPriceVersion != quote.PriceVersion || generationQuoteHash(userID, *input.CanvasVersion, existing.Kind, existing.Input, quote) != input.QuoteHash {
			return nil, output, errors.New("任务 ID 已用于其他请求")
		}
		return nil, g.generationTask(existing), nil
	} else if !errors.Is(lookup, persistence.ErrNotFound) {
		return nil, output, g.generationFailure("start_generation", userID, lookup)
	}
	version, kind, taskInput, err := g.generationInput(ctx, userID, input.CanvasID, input.NodeID)
	if err != nil {
		return nil, output, g.generationFailure("start_generation", userID, err)
	}
	if version != *input.CanvasVersion || generationQuoteHash(userID, version, kind, taskInput, quote) != input.QuoteHash {
		return nil, output, errors.New("节点或画布已变化，请重新报价")
	}
	if g.assets == nil {
		return nil, output, errors.New("素材保存服务未启用，不能提交生成")
	}
	if kind == "video" {
		if _, ok := g.assets.(persistence.VideoStorage); !ok {
			return nil, output, errors.New("视频保存服务未启用，不能提交生成")
		}
	}
	task, err := g.db.CreateBilledTask(ctx, userID, input.TaskID, kind, taskInput, quote.Points, quote.PriceVersion)
	if err != nil {
		return nil, output, g.generationFailure("start_generation", userID, err)
	}
	return nil, g.generationTask(task), nil
}

func (g *mcpGateway) getGenerationTask(ctx context.Context, request *mcp.CallToolRequest, input getGenerationTaskInput) (*mcp.CallToolResult, generationTaskOutput, error) {
	var output generationTaskOutput
	userID, err := mcpActor(request, mcpReadScope)
	if err != nil {
		return nil, output, err
	}
	if err = g.db.RequireTaskOwner(ctx, input.TaskID, userID); err != nil {
		return nil, output, g.generationFailure("get_generation_task", userID, err)
	}
	task, err := g.db.Task(ctx, input.TaskID)
	if err != nil {
		return nil, output, g.generationFailure("get_generation_task", userID, err)
	}
	return nil, g.generationTask(task), nil
}

func (g *mcpGateway) generationTask(task persistence.Task) generationTaskOutput {
	output := generationTaskOutput{TaskID: task.ID, CanvasID: task.Input.CanvasID, NodeID: task.Input.NodeID, Kind: task.Kind, Status: task.Status, CreditPoints: task.CreditPoints, CreditStatus: task.CreditStatus, UpdatedAt: task.UpdatedAt.UTC().Format(time.RFC3339Nano), CanvasURL: g.config.Origin + "/?canvas=" + task.Input.CanvasID}
	if task.Error != nil {
		output.Error = &mcpCanvasTaskError{Code: task.Error.Code, Message: task.Error.Message}
	}
	if task.Result != nil {
		output.Result = &mcpCanvasResult{TaskID: task.ID, Saved: task.Result.Asset != nil, StorageError: task.Result.StorageError}
		if asset := task.Result.Asset; asset != nil {
			output.Asset = &mcpCanvasAsset{Kind: asset.Kind, Source: asset.Source, ContentType: asset.ContentType, Bytes: asset.Bytes, Media: asset.Media, VideoReference: asset.VideoReference}
		}
	}
	return output
}

func (g *mcpGateway) generationFailure(operation, userID string, err error) error {
	switch {
	case errors.Is(err, persistence.ErrNotFound):
		return errors.New("画布、节点或任务不存在，或不属于当前用户")
	case errors.Is(err, persistence.ErrConflict):
		return errors.New("画布版本或任务请求冲突，请重新读取后重试")
	case errors.Is(err, persistence.ErrBusy):
		return errors.New("该节点已有进行中的生成任务")
	case errors.Is(err, persistence.ErrCreditsInsufficient):
		return errors.New("积分不足，未提交生成")
	case errors.Is(err, persistence.ErrCreditQuoteChanged):
		return errors.New("报价已变化，请重新报价")
	case errors.Is(err, persistence.ErrModelGenerationNotEnabled):
		return errors.New("当前模型不支持生成或没有可用报价")
	case errors.Is(err, persistence.ErrPromptReferences):
		return errors.New("提示词中的素材标签和参考节点不一致")
	case errors.Is(err, persistence.ErrReferenceSize), errors.Is(err, persistence.ErrNanoReferenceSize):
		return errors.New("参考素材大小超过模型限制")
	case errors.Is(err, persistence.ErrInvalid):
		return errors.New("节点、模型参数或参考素材无效，请检查后重试")
	case errors.Is(err, errMCPSingleImage):
		return errMCPSingleImage
	case errors.Is(err, errMCPReferencePending):
		return errMCPReferencePending
	}
	var apiError *ark.APIError
	if errors.As(err, &apiError) {
		return errors.New(apiError.Message)
	}
	log.Printf("MCP %s failed actor=%s", operation, userID)
	return errors.New("操作暂时不可用，请稍后重试")
}

// generationInput reconstructs the same immutable submission data as the canvas
// from an owned saved snapshot and its saved assets, without returning asset keys.
func (g *mcpGateway) generationInput(ctx context.Context, userID, canvasID, nodeID string) (int64, string, persistence.Input, error) {
	var input persistence.Input
	if !persistence.ValidID(canvasID) || !persistence.ValidID(nodeID) {
		return 0, "", input, persistence.ErrInvalid
	}
	if err := g.db.RequireCanvasOwner(ctx, canvasID, userID); err != nil {
		return 0, "", input, err
	}
	canvas, err := g.db.Canvas(ctx, canvasID)
	if err != nil {
		return 0, "", input, err
	}
	var node *persistence.Node
	for i := range canvas.Snapshot.Nodes {
		if canvas.Snapshot.Nodes[i].ID == nodeID {
			node = &canvas.Snapshot.Nodes[i]
			break
		}
	}
	if node == nil {
		return 0, "", input, persistence.ErrNotFound
	}
	if node.Data.Origin == "upload" || (node.Data.Kind != "image" && node.Data.Kind != "video") {
		return 0, "", input, persistence.ErrInvalid
	}
	kind := node.Data.Kind
	prompt, parts, err := canvas.Snapshot.EffectivePrompt(*node)
	if err != nil {
		return 0, "", input, err
	}
	input = persistence.Input{CanvasID: canvasID, NodeID: nodeID, Prompt: prompt, PromptParts: parts}
	if strings.TrimSpace(input.Prompt) == "" || utf8.RuneCountInString(strings.TrimSpace(input.Prompt)) > 2000 {
		return 0, "", input, persistence.ErrInvalid
	}
	mode := node.Data.VideoMode
	if kind == "video" && mode == "" {
		mode = "reference"
	}
	refIDs := node.Data.ReferenceIDs
	if kind == "video" && mode == "text" {
		refIDs = nil
	}
	if kind == "video" && mode == "image" {
		if node.Data.FirstFrameID == "" || !slices.Contains(node.Data.ReferenceIDs, node.Data.FirstFrameID) {
			return 0, "", input, persistence.ErrInvalid
		}
		refIDs = []string{node.Data.FirstFrameID}
	}
	assets := make(map[string]storage.Asset, len(canvas.Assets))
	for _, asset := range canvas.Assets {
		assets[asset.NodeID] = asset
	}
	for _, result := range canvas.Results {
		if result.Result != nil {
			if result.Result.Asset == nil {
				delete(assets, result.Input.NodeID)
			} else {
				assets[result.Input.NodeID] = *result.Result.Asset
			}
		}
	}
	for _, refID := range refIDs {
		for _, task := range canvas.Tasks {
			if task.Input.NodeID == refID && slices.Contains([]string{"queued", "preparing", "submitting", "running", "saving"}, task.Status) {
				return 0, "", input, errMCPReferencePending
			}
		}
		asset, ok := assets[refID]
		if !ok || asset.CanvasID != canvasID || asset.NodeID != refID {
			return 0, "", input, persistence.ErrInvalid
		}
		refKind := map[storage.Kind]string{storage.Image: "image", storage.Video: "video", storage.Audio: "audio"}[asset.Kind]
		if refKind == "" || (kind == "image" && refKind != "image") {
			return 0, "", input, persistence.ErrInvalid
		}
		if kind == "image" {
			input.ReferenceKeys = append(input.ReferenceKeys, asset.Key)
		} else {
			ref := persistence.Reference{NodeID: refID, AssetKey: asset.Key, Kind: refKind}
			if mode == "image" {
				ref.Role = "first_frame"
			}
			input.References = append(input.References, ref)
		}
	}
	if selection := node.Data.ModelSelection; selection != nil {
		if selection.Provider != persistence.ProviderAPIMart || selection.ModelID == "" {
			return 0, "", input, persistence.ErrModelGenerationNotEnabled
		}
		input.Provider, input.Model = selection.Provider, selection.ModelID
		if kind == "image" {
			if n, ok := selection.Parameters["n"]; ok && fmt.Sprint(n) != "1" {
				return 0, "", input, errMCPSingleImage
			}
			if input.Model == "gemini-nano-banana-2.1" {
				input.Model = "gemini-nano-banana-2.1-ext"
			}
			definition, ok := apimart.ImageDefinition(input.Model)
			if !ok {
				return 0, "", input, persistence.ErrModelGenerationNotEnabled
			}
			input.Parameters = make(map[string]any)
			for _, p := range definition.Parameters {
				if value, ok := apimart.ParameterValue(p, selection.Parameters); ok {
					input.Parameters[p.Key] = value
				}
			}
			providerInput := apimart.ImageRequest{Prompt: input.Prompt, Model: input.Model, Parameters: input.Parameters}
			if !apimart.NormalizeImageRequest(&providerInput, len(input.ReferenceKeys)) {
				return 0, "", input, persistence.ErrInvalid
			}
			input.Parameters = providerInput.Parameters
			if len(input.PromptParts) == 0 {
				input.Prompt = strings.TrimSpace(input.Prompt)
			}
		} else {
			definition, ok := apimart.VideoDefinition(input.Model)
			if !ok {
				return 0, "", input, persistence.ErrModelGenerationNotEnabled
			}
			input.Mode, input.Parameters = mode, make(map[string]any)
			for _, p := range definition.Parameters {
				if value, ok := apimart.ParameterValue(p, selection.Parameters); ok {
					input.Parameters[p.Key] = value
				}
			}
			if err := persistence.NormalizeVideoInput(&input); err != nil {
				return 0, "", input, err
			}
		}
	} else if kind == "image" {
		input.Model, err = ark.ResolveImageModel(node.Data.ImageModel)
		if err != nil {
			return 0, "", input, err
		}
		var options *ark.ImageOptions
		if node.Data.ImageOptions != nil {
			if node.Data.ImageOptions.Count > 1 {
				return 0, "", input, errMCPSingleImage
			}
			options = &node.Data.ImageOptions.ImageOptions
		}
		resolved, err := ark.ResolveImageOptions(input.Model, options)
		if err != nil {
			return 0, "", input, err
		}
		input.ImageOptions = &resolved
		if len(input.PromptParts) == 0 {
			input.Prompt = strings.TrimSpace(input.Prompt)
		}
	} else {
		input.Model, input.Mode, input.Duration = node.Data.VideoModel, mode, 4
		audio := true
		if options := node.Data.VideoOptions; options != nil {
			input.Resolution, input.Ratio, input.Duration, audio = options.Resolution, options.Ratio, options.Duration, options.GenerateAudio
		}
		input.GenerateAudio = &audio
		if err := persistence.NormalizeVideoInput(&input); err != nil {
			return 0, "", input, err
		}
	}
	return canvas.Version, kind, input, nil
}
