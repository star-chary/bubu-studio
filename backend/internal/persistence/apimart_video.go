package persistence

import "frame-space/backend/internal/apimart"

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
	if enabled && kind == "image" && node.Data.ModelSelection.ModelID == "gemini-nano-banana-2.1" && input.Provider == ProviderAPIMart {
		return validateNanoSelection(node.Data.ModelSelection, input)
	}
	if !enabled || input.Provider != ProviderAPIMart || input.Model != node.Data.ModelSelection.ModelID {
		return ErrModelGenerationNotEnabled
	}
	return nil
}
