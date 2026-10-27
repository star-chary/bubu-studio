package persistence

import (
	"context"

	"frame-space/backend/internal/apimart"
)

func enabledAPIMartVideoNode(node Node) bool {
	sel := node.Data.ModelSelection
	if node.Data.Kind != "video" || node.Data.Origin == "upload" || sel == nil || sel.Provider != ProviderAPIMart {
		return false
	}
	_, ok := apimart.VideoDefinition(sel.ModelID)
	return ok
}

func validateTaskProvider(node Node, input Input, kind string) error {
	if node.Data.ModelSelection == nil {
		if input.Provider != "" {
			return ErrInvalid
		}
		return nil
	}
	enabled := kind == "video" && enabledAPIMartVideoNode(node) || kind == "image" && enabledAPIMartImageNode(node)
	if !enabled || input.Provider != ProviderAPIMart || input.Model != node.Data.ModelSelection.ModelID {
		return ErrModelGenerationNotEnabled
	}
	return nil
}

// This first APIMart integration does not quote, reserve or settle platform
// points. It still requires an owned canvas and the explicitly supported node.
func (s *Store) CreateAPIMartVideoTask(ctx context.Context, userID, id string, input Input) (Task, error) {
	if _, ok := apimart.VideoDefinition(input.Model); input.Provider != ProviderAPIMart || !ok || !ValidID(id) {
		return Task{}, ErrInvalid
	}
	if err := s.RequireCanvasOwner(ctx, input.CanvasID, userID); err != nil {
		return Task{}, err
	}
	return s.CreateVideoTask(ctx, id, input)
}
