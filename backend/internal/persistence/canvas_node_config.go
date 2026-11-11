package persistence

import (
	"context"
	"encoding/json"
	"errors"

	"frame-space/backend/internal/apimart"
	"frame-space/backend/internal/ark"
	"github.com/jackc/pgx/v5"
)

// CanvasNodeModel identifies a model chosen for a saved node. Parameters use
// the same provider-native names saved by the canvas.
type CanvasNodeModel struct {
	Provider   string         `json:"provider"`
	ModelID    string         `json:"modelId"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type CanvasNodePatch struct {
	Name         *string           `json:"name,omitempty"`
	Prompt       *string           `json:"prompt,omitempty"`
	PromptParts  *[]PromptPart     `json:"promptParts,omitempty"`
	Model        *CanvasNodeModel  `json:"model,omitempty"`
	ImageOptions *ImageNodeOptions `json:"imageOptions,omitempty"`
	VideoOptions *VideoOptions     `json:"videoOptions,omitempty"`
	VideoMode    *string           `json:"videoMode,omitempty"`
	ReferenceIDs *[]string         `json:"referenceIds,omitempty"`
	FirstFrameID *string           `json:"firstFrameId,omitempty"`
}

// ConfigureOwnedCanvasNode applies only the specified fields to the current
// JSON snapshot. The caller's version prevents silently overwriting new edits.
func (s *Store) ConfigureOwnedCanvasNode(ctx context.Context, userID, canvasID, nodeID string, expectedVersion int64, patch CanvasNodePatch) (Node, int64, error) {
	var empty Node
	if !ValidID(userID) || !ValidID(canvasID) || !ValidID(nodeID) || expectedVersion < 0 ||
		(patch.Name == nil && patch.Prompt == nil && patch.PromptParts == nil && patch.Model == nil && patch.ImageOptions == nil && patch.VideoOptions == nil && patch.VideoMode == nil && patch.ReferenceIDs == nil && patch.FirstFrameID == nil) {
		return empty, 0, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return empty, 0, err
	}
	defer tx.Rollback(context.Background())
	var raw []byte
	var version int64
	err = tx.QueryRow(ctx, `SELECT snapshot,version FROM canvases WHERE id=$1 AND owner_user_id=$2
		AND EXISTS (SELECT 1 FROM users WHERE id=$2 AND status='active') FOR UPDATE`, canvasID, userID).Scan(&raw, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, 0, ErrNotFound
	}
	if err != nil {
		return empty, 0, err
	}
	if version != expectedVersion {
		return empty, version, ErrConflict
	}
	var snapshot Snapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return empty, 0, err
	}
	index := -1
	for i := range snapshot.Nodes {
		if snapshot.Nodes[i].ID == nodeID {
			index = i
			break
		}
	}
	if index < 0 {
		return empty, 0, ErrNotFound
	}
	node := snapshot.Nodes[index]
	if node.Data.Origin == "upload" || (node.Data.Kind != "image" && node.Data.Kind != "video") {
		return empty, 0, ErrInvalid
	}
	var document map[string]json.RawMessage
	var rawNodes []json.RawMessage
	var rawNode, data map[string]json.RawMessage
	if json.Unmarshal(raw, &document) != nil || json.Unmarshal(document["nodes"], &rawNodes) != nil || json.Unmarshal(rawNodes[index], &rawNode) != nil || json.Unmarshal(rawNode["data"], &data) != nil {
		return empty, 0, ErrInvalid
	}
	set := func(key string, value any) error {
		encoded, e := json.Marshal(value)
		if e == nil {
			data[key] = encoded
		}
		return e
	}
	if patch.Name != nil {
		_ = set("name", *patch.Name)
	}
	if patch.Prompt != nil {
		_ = set("prompt", *patch.Prompt)
		if patch.PromptParts == nil {
			delete(data, "promptParts")
		}
	}
	if patch.PromptParts != nil {
		_ = set("promptParts", *patch.PromptParts)
	}
	if patch.Model != nil {
		model := patch.Model
		if model.ModelID == "" {
			return empty, 0, ErrInvalid
		}
		switch model.Provider {
		case ProviderAPIMart:
			if !apimart.Billable(node.Data.Kind, model.ModelID) || model.ModelID == "gemini-nano-banana-2.1-ext" {
				return empty, 0, ErrModelGenerationNotEnabled
			}
			var parameters []apimart.VideoParameter
			if node.Data.Kind == "image" {
				definition, ok := apimart.ImageDefinition(model.ModelID)
				if !ok {
					return empty, 0, ErrModelGenerationNotEnabled
				}
				parameters = definition.Parameters
				delete(data, "imageModel")
				delete(data, "imageOptions")
			} else {
				definition, ok := apimart.VideoDefinition(model.ModelID)
				if !ok {
					return empty, 0, ErrModelGenerationNotEnabled
				}
				parameters = definition.Parameters
				delete(data, "videoModel")
				delete(data, "videoOptions")
			}
			values := make(map[string]any, len(parameters)+len(model.Parameters))
			for _, parameter := range parameters {
				value := parameter.Default
				if parameter.Kind == "fixed" {
					value = parameter.Fixed
				}
				if node.Data.Kind == "image" && parameter.Key == "n" {
					value = 1
				}
				if supplied, ok := apimart.ParameterValue(parameter, model.Parameters); ok {
					value = supplied
				}
				if value != nil {
					values[parameter.Key] = value
				}
			}
			for key, value := range model.Parameters {
				values[key] = value
			}
			if err = set("modelSelection", ModelSelection{Provider: ProviderAPIMart, ModelID: model.ModelID, Parameters: values}); err != nil {
				return empty, 0, ErrInvalid
			}
		case "ark":
			if len(model.Parameters) != 0 {
				return empty, 0, ErrInvalid
			}
			delete(data, "modelSelection")
			if node.Data.Kind == "image" {
				if _, e := ark.ResolveImageModel(model.ModelID); e != nil {
					return empty, 0, ErrInvalid
				}
				_ = set("imageModel", model.ModelID)
				delete(data, "imageOptions")
			} else {
				if _, ok := ark.VideoSpec(model.ModelID); !ok {
					return empty, 0, ErrInvalid
				}
				_ = set("videoModel", model.ModelID)
				delete(data, "videoOptions")
			}
		default:
			return empty, 0, ErrInvalid
		}
	}
	if patch.ImageOptions != nil {
		if node.Data.Kind != "image" || len(data["modelSelection"]) != 0 {
			return empty, 0, ErrInvalid
		}
		options := *patch.ImageOptions
		if options.Count == 0 {
			options.Count = 1
		}
		if options.Count != 1 {
			return empty, 0, ErrInvalid
		}
		_ = set("imageOptions", options)
	}
	if patch.VideoOptions != nil {
		if node.Data.Kind != "video" || len(data["modelSelection"]) != 0 {
			return empty, 0, ErrInvalid
		}
		_ = set("videoOptions", patch.VideoOptions)
	}
	if patch.VideoMode != nil {
		if node.Data.Kind != "video" || (*patch.VideoMode != "text" && *patch.VideoMode != "image" && *patch.VideoMode != "reference") {
			return empty, 0, ErrInvalid
		}
		_ = set("videoMode", *patch.VideoMode)
	}
	if patch.ReferenceIDs != nil {
		_ = set("referenceIds", *patch.ReferenceIDs)
	}
	if patch.FirstFrameID != nil {
		_ = set("firstFrameId", *patch.FirstFrameID)
	}
	rawNode["data"], err = json.Marshal(data)
	if err == nil {
		rawNodes[index], err = json.Marshal(rawNode)
	}
	if err == nil {
		document["nodes"], err = json.Marshal(rawNodes)
	}
	if err == nil {
		raw, err = json.Marshal(document)
	}
	if err != nil || len(raw) > (2<<20)-256 || json.Unmarshal(raw, &snapshot) != nil || snapshot.Validate() != nil {
		return empty, 0, ErrInvalid
	}
	err = tx.QueryRow(ctx, `UPDATE canvases SET snapshot=$3,version=version+1,updated_at=now() WHERE id=$1 AND owner_user_id=$2 RETURNING version`, canvasID, userID, raw).Scan(&version)
	if err != nil {
		return empty, 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, 0, err
	}
	return snapshot.Nodes[index], version, nil
}
