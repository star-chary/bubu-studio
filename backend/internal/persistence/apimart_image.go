package persistence

import (
	"strings"
	"unicode/utf8"

	"frame-space/backend/internal/apimart"
)

func enabledAPIMartImageNode(node Node) bool {
	sel := node.Data.ModelSelection
	if node.Data.Kind != "image" || node.Data.Origin == "upload" || sel == nil || sel.Provider != ProviderAPIMart {
		return false
	}
	_, ok := apimart.ImageDefinition(sel.ModelID)
	return ok
}

func normalizeAPIMartImageInput(in *Input) error {
	if in.Provider != ProviderAPIMart || in.ImageOptions != nil || len(in.References) != 0 || !ValidID(in.CanvasID) || !ValidID(in.NodeID) || utf8.RuneCountInString(strings.TrimSpace(in.Prompt)) > 2000 {
		return ErrInvalid
	}
	request := apimart.ImageRequest{Prompt: in.Prompt, Model: in.Model, Parameters: in.Parameters}
	if !apimart.NormalizeImageRequest(&request, len(in.ReferenceKeys)) {
		return ErrInvalid
	}
	in.Parameters = request.Parameters
	if len(in.PromptParts) == 0 {
		in.Prompt = strings.TrimSpace(in.Prompt)
	}
	return nil
}
