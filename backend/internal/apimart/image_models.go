package apimart

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
)

// The execution limits are shared with the canvas and are never credentials.
//
//go:embed image_models.json
var imageModelsJSON []byte

type ImageModelDefinition struct {
	ModelID                 string           `json:"modelId"`
	MaxReferenceImages      int              `json:"maxReferenceImages"`
	MaxReferenceBytes       int64            `json:"maxReferenceBytes"`
	MaxSingleReferenceBytes int64            `json:"maxSingleReferenceBytes"`
	Parameters              []VideoParameter `json:"parameters"`
}

var imageModels = func() map[string]ImageModelDefinition {
	var definitions []ImageModelDefinition
	if err := json.Unmarshal(imageModelsJSON, &definitions); err != nil {
		panic("invalid static APIMart image contract")
	}
	out := make(map[string]ImageModelDefinition, len(definitions))
	for _, model := range definitions {
		out[model.ModelID] = model
	}
	return out
}()

func ImageDefinition(id string) (ImageModelDefinition, bool) {
	m, ok := imageModels[id]
	return m, ok
}

// ImageDefinitions provides a stable read-only list for the MCP model catalog.
func ImageDefinitions() []ImageModelDefinition {
	items := make([]ImageModelDefinition, 0, len(imageModels))
	for _, model := range imageModels {
		items = append(items, model)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ModelID < items[j].ModelID })
	return items
}

type ImageRequest struct {
	TaskID     string
	Prompt     string
	Model      string
	Parameters map[string]any
	Images     []string
}

// Each task produces one image; batch counts are handled by the canvas.
func NormalizeImageRequest(in *ImageRequest, referenceCount int) bool {
	m, ok := ImageDefinition(in.Model)
	if !ok || strings.TrimSpace(in.Prompt) == "" || referenceCount < 0 || referenceCount > m.MaxReferenceImages {
		return false
	}
	values := make(map[string]any, len(m.Parameters))
	for _, p := range m.Parameters {
		value := p.Default
		if p.Kind == "fixed" {
			value = p.Fixed
		}
		if v, exists := in.Parameters[p.Key]; exists {
			value = v
		}
		values[p.Key] = value
	}
	for key := range in.Parameters {
		if _, exists := values[key]; !exists {
			return false
		}
	}
	for _, p := range m.Parameters {
		if values[p.Key] == nil || !validParameter(p, values[p.Key], values) {
			return false
		}
	}
	if in.Model == "seedream-5-0-lite" && values["size"] == "auto" && referenceCount == 0 {
		return false
	}
	if in.Model == "midjourney" {
		version, _ := values["version"].(string)
		niji, _ := values["niji"].(bool)
		hd, _ := values["hd"].(bool)
		if (niji && version != "7") || (hd && version == "7") {
			return false
		}
	}
	in.Parameters = values
	return true
}
