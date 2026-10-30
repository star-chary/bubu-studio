package apimart

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"frame-space/backend/internal/ark"
)

// This release-time contract is also imported by the frontend. It contains
// product capabilities and native basic parameters, never credentials.
//
//go:embed video_models.json
var videoModelsJSON []byte

type ParameterCondition struct {
	When map[string]any `json:"when"`
	Enum []any          `json:"enum"`
}

type VideoParameter struct {
	Key           string               `json:"key"`
	Role          string               `json:"role"`
	Kind          string               `json:"kind"`
	Enum          []any                `json:"enum"`
	Default       any                  `json:"default"`
	Fixed         any                  `json:"fixed"`
	Minimum       *float64             `json:"minimum"`
	Maximum       *float64             `json:"maximum"`
	Conditions    []ParameterCondition `json:"conditions"`
	SpecialValues []struct {
		Value any `json:"value"`
	} `json:"specialValues"`
}

type VideoModelDefinition struct {
	ModelID                  string           `json:"modelId"`
	GenerationEnabled        bool             `json:"generationEnabled"`
	Modes                    []string         `json:"modes"`
	MaxReferenceImages       *int             `json:"maxReferenceImages"`
	MaxReferenceVideos       int              `json:"maxReferenceVideos"`
	MaxReferenceVideoSeconds float64          `json:"maxReferenceVideoSeconds"`
	MotionControl            bool             `json:"motionControl"`
	VideoReferenceTag        string           `json:"videoReferenceTag"`
	ImageInput               string           `json:"imageInput"`
	ReferenceInput           string           `json:"referenceInput"`
	ReferenceRole            string           `json:"referenceRole"`
	ReferenceTag             string           `json:"referenceTag"`
	OmitImageRatio           bool             `json:"omitImageRatio"`
	OmitParameters           []string         `json:"omitParameters"`
	ImageParameters          map[string]any   `json:"imageParameters"`
	TextExtra                map[string]any   `json:"textExtra"`
	ReferenceExtra           map[string]any   `json:"referenceExtra"`
	ImageExtra               map[string]any   `json:"imageExtra"`
	Parameters               []VideoParameter `json:"parameters"`
}

var videoModels = func() map[string]VideoModelDefinition {
	var definitions []VideoModelDefinition
	if err := json.Unmarshal(videoModelsJSON, &definitions); err != nil {
		panic("invalid static APIMart video contract")
	}
	out := make(map[string]VideoModelDefinition, len(definitions))
	for _, model := range definitions {
		out[model.ModelID] = model
	}
	return out
}()

func VideoDefinition(id string) (VideoModelDefinition, bool) {
	m, ok := videoModels[id]
	return m, ok && m.GenerationEnabled && len(m.Modes) > 0
}

func (m VideoModelDefinition) Supports(mode string) bool { return slices.Contains(m.Modes, mode) }

func (m VideoModelDefinition) ReferenceLimit(mode string) int {
	if mode == "image" {
		return 1
	}
	if mode != "reference" {
		return 0
	}
	if m.MaxReferenceImages == nil {
		return 50 // Existing platform input bound; not an upstream capability claim.
	}
	return *m.MaxReferenceImages
}

func ReferenceLabel(model, kind string, index int) string {
	m, ok := VideoDefinition(model)
	if !ok {
		return ""
	}
	tag := m.ReferenceTag
	if kind == "video" {
		tag = m.VideoReferenceTag
	} else if kind != "image" {
		return ""
	}
	return strings.ReplaceAll(tag, "{n}", fmt.Sprint(index))
}

func sameValue(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func validParameter(p VideoParameter, value any, values map[string]any) bool {
	allowed := p.Enum
	for _, c := range p.Conditions {
		matches := true
		for key, expected := range c.When {
			matches = matches && sameValue(values[key], expected)
		}
		if matches && c.Enum != nil {
			allowed = c.Enum
			break
		}
	}
	switch p.Kind {
	case "fixed":
		return sameValue(value, p.Fixed)
	case "enum":
		return slices.ContainsFunc(allowed, func(v any) bool { return sameValue(value, v) })
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		encoded, err := json.Marshal(value)
		var number float64
		if err != nil || json.Unmarshal(encoded, &number) != nil || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number {
			return false
		}
		for _, special := range p.SpecialValues {
			if sameValue(value, special.Value) {
				return true
			}
		}
		return (p.Minimum == nil || number >= *p.Minimum) && (p.Maximum == nil || number <= *p.Maximum)
	}
	return false
}

// NormalizeVideoRequest accepts only the product's native basic fields. Legacy
// Seedance tasks without Parameters still derive those fields from their input.
func NormalizeVideoRequest(in *ark.VideoRequest) bool {
	m, ok := VideoDefinition(in.Model)
	if !ok || !m.Supports(in.Mode) || strings.TrimSpace(in.Prompt) == "" {
		return false
	}
	values := make(map[string]any, len(m.Parameters))
	for _, p := range m.Parameters {
		value := p.Default
		if in.Parameters != nil {
			if v, exists := in.Parameters[p.Key]; exists {
				value = v
			}
		} else {
			switch p.Role {
			case "ratio":
				if in.Ratio != "" {
					value = in.Ratio
				}
			case "resolution":
				if in.Resolution != "" {
					value = in.Resolution
				}
			case "duration":
				value = in.Duration
			case "audio":
				value = in.GenerateAudio
			}
		}
		if p.Kind == "fixed" {
			if supplied, exists := in.Parameters[p.Key]; exists && !sameValue(supplied, p.Fixed) {
				return false
			}
			value = p.Fixed
		}
		if in.Mode == "image" {
			if v, exists := m.ImageParameters[p.Key]; exists {
				value = v
			}
		}
		if value == nil {
			return false
		}
		values[p.Key] = value
	}
	for key := range in.Parameters {
		if _, exists := values[key]; !exists {
			return false
		}
	}
	in.Resolution, in.Ratio, in.Duration, in.GenerateAudio = "", "", 0, false
	for _, p := range m.Parameters {
		if !validParameter(p, values[p.Key], values) {
			return false
		}
		switch p.Role {
		case "ratio":
			in.Ratio, _ = values[p.Key].(string)
		case "resolution":
			in.Resolution, _ = values[p.Key].(string)
		case "duration":
			encoded, _ := json.Marshal(values[p.Key])
			_ = json.Unmarshal(encoded, &in.Duration)
		case "audio":
			in.GenerateAudio, _ = values[p.Key].(bool)
		}
	}
	in.Parameters = values
	return true
}

func videoPayload(in ark.VideoRequest) (map[string]any, bool) {
	if !NormalizeVideoRequest(&in) {
		return nil, false
	}
	m, _ := VideoDefinition(in.Model)
	limit := m.ReferenceLimit(in.Mode)
	if (in.Mode == "text" && len(in.References) != 0) || (in.Mode != "text" && (len(in.References) == 0 || len(in.References) > limit+m.MaxReferenceVideos)) {
		return nil, false
	}
	images := make([]string, 0, len(in.References))
	videos := make([]string, 0, len(in.References))
	for _, r := range in.References {
		if !validResultURL(r.URL) {
			return nil, false
		}
		switch r.Kind {
		case "image":
			images = append(images, r.URL)
		case "video":
			videos = append(videos, r.URL)
		default:
			return nil, false
		}
	}
	if len(images) > limit || len(videos) > m.MaxReferenceVideos || (len(videos) > 0 && in.Mode != "reference") {
		return nil, false
	}
	out := map[string]any{"model": in.Model, "prompt": in.Prompt}
	for _, p := range m.Parameters {
		if slices.Contains(m.OmitParameters, p.Key) || (in.Mode == "image" && m.OmitImageRatio && p.Role == "ratio") {
			continue
		}
		out[p.Key] = in.Parameters[p.Key]
	}
	if m.MotionControl {
		if in.Mode != "reference" || len(images) != 1 || len(videos) != 1 {
			return nil, false
		}
		out["image_url"], out["video_url"] = images[0], videos[0]
		for key, value := range m.ReferenceExtra {
			out[key] = value
		}
		return out, true
	}
	extra := m.TextExtra
	if in.Mode != "text" {
		field, role := m.ReferenceInput, m.ReferenceRole
		extra = m.ReferenceExtra
		if in.Mode == "image" {
			field, role, extra = m.ImageInput, "first_frame", m.ImageExtra
		}
		if len(videos) > 0 {
			out["video_urls"] = videos
		}
		if len(images) > 0 {
			switch field {
			case "first_frame_image":
				out[field] = images[0]
			case "image_with_roles":
				items := make([]map[string]string, 0, len(images))
				for _, u := range images {
					items = append(items, map[string]string{"url": u, "role": role})
				}
				out[field] = items
			case "ref_images":
				items := make([]map[string]any, 0, len(images))
				for i, u := range images {
					tag := ReferenceLabel(in.Model, "image", i+1)
					items = append(items, map[string]any{"tag": tag, "type": "image", "image_urls": []string{u}})
					if !strings.Contains(in.Prompt, tag) {
						out["prompt"] = out["prompt"].(string) + " " + tag
					}
				}
				out[field] = items
			default:
				out[field] = images
			}
		}
	}
	for key, value := range extra {
		out[key] = value
	}
	if strings.HasPrefix(in.Model, "seedance-") {
		out["output_format"] = "mp4"
	}
	return out, true
}
