package persistence

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
)

type Reference struct {
	NodeID   string `json:"nodeId"`
	AssetKey string `json:"assetKey"`
	Kind     string `json:"kind"`
}

// CompilePrompt replaces stable node identities only. User text is never parsed
// as a mention: a literal @name remains literal. Numbers follow attachment order.
func CompilePrompt(input Input, kind string, bindings []Reference) (string, error) {
	if len(input.PromptParts) == 0 {
		return strings.TrimSpace(input.Prompt), nil
	}
	n := Node{ID: input.NodeID, Data: NodeData{Kind: kind, Prompt: input.Prompt, PromptParts: input.PromptParts}}
	if !validPromptParts(n) {
		return "", ErrInvalid
	}
	labels := map[string]string{}
	kinds := map[string]string{}
	counts := map[string]int{}
	for _, r := range bindings {
		counts[r.Kind]++
		prefix := map[string]string{"image": "图片", "video": "视频", "audio": "音频"}[r.Kind]
		labels[r.NodeID] = fmt.Sprintf("%s%d", prefix, counts[r.Kind])
		kinds[r.NodeID] = r.Kind
	}
	var out strings.Builder
	for _, p := range input.PromptParts {
		if p.Type == "text" {
			out.WriteString(p.Text)
			continue
		}
		if labels[p.NodeID] == "" || kinds[p.NodeID] != p.Kind {
			return "", ErrPromptReferences
		}
		out.WriteString(labels[p.NodeID])
	}
	prompt := strings.TrimSpace(out.String())
	if utf8.RuneCountInString(prompt) > 2000 || (kind == "image" && prompt == "") {
		return "", ErrInvalid
	}
	return prompt, nil
}

func (s *Store) bindReferences(ctx context.Context, input Input, kind string) ([]Reference, error) {
	refs := append([]Reference{}, input.References...)
	if kind == "image" {
		if len(refs) > 0 {
			return nil, ErrInvalid
		}
		for _, key := range input.ReferenceKeys {
			refs = append(refs, Reference{AssetKey: key, Kind: "image"})
		}
	}
	if kind == "video" {
		spec, ok := ark.VideoSpec(input.Model)
		if !ok || len(input.ReferenceKeys) > 0 || (input.Mode == "text" && len(refs) != 0) || (input.Mode == "reference" && (len(refs) == 0 || len(refs) > spec.MaxImages+spec.MaxVideos+spec.MaxAudios)) {
			return nil, ErrInvalid
		}
	}
	seenKeys := map[string]bool{}
	seenNodes := map[string]bool{}
	counts := map[string]int{}
	for i, r := range refs {
		var a storage.Asset
		if err := s.Pool.QueryRow(ctx, `SELECT metadata FROM assets WHERE key=$1 AND canvas_id=$2 AND node_id<>$3`, r.AssetKey, input.CanvasID, input.NodeID).Scan(&a); err != nil {
			return nil, mapError(err)
		}
		expected := map[string]storage.Kind{"image": storage.Image, "video": storage.Video, "audio": storage.Audio}[r.Kind]
		if expected == "" || a.Kind != expected || (kind == "video" && a.NodeID != r.NodeID) || seenKeys[r.AssetKey] || seenNodes[a.NodeID] {
			return nil, ErrInvalid
		}
		source, err := s.Node(ctx, input.CanvasID, a.NodeID)
		if err != nil || source.Data.Kind != r.Kind {
			return nil, ErrInvalid
		}
		seenKeys[r.AssetKey] = true
		seenNodes[a.NodeID] = true
		counts[r.Kind]++
		refs[i].NodeID = a.NodeID
	}
	if kind == "video" {
		spec, _ := ark.VideoSpec(input.Model)
		if counts["image"] > spec.MaxImages || counts["video"] > spec.MaxVideos || counts["audio"] > spec.MaxAudios || (input.Model != ark.VideoModel && counts["image"]+counts["video"] == 0 && input.Mode == "reference") {
			return nil, ErrInvalid
		}
	}
	return refs, nil
}

func normalizeVideoOptions(in *Input) error {
	if in.Model == "" {
		in.Model = ark.VideoModel
	}
	if in.Mode == "" {
		in.Mode = "reference"
	}
	if in.Resolution == "" {
		in.Resolution = "480p"
	}
	if in.Ratio == "" {
		in.Ratio = "16:9"
	}
	// The HTTP boundary supplies defaults only for omitted numeric/bool fields.
	spec, ok := ark.VideoSpec(in.Model)
	if !ok || (in.Mode != "reference" && in.Mode != "text") || (in.Resolution != "480p" && in.Resolution != "720p") || in.Duration < 4 || in.Duration > spec.MaxDuration {
		return ErrInvalid
	}
	switch in.Ratio {
	case "16:9", "4:3", "1:1", "3:4", "9:16", "21:9", "adaptive":
	default:
		return ErrInvalid
	}
	return nil
}

func NormalizeVideoInput(in *Input) error {
	if normalizeVideoOptions(in) != nil {
		return ErrInvalid
	}
	if in.Mode == "text" {
		if len(in.References) != 0 || len(in.ReferenceKeys) != 0 || strings.TrimSpace(in.Prompt) == "" {
			return ErrInvalid
		}
		for _, part := range in.PromptParts {
			if part.Type != "text" {
				return ErrInvalid
			}
		}
	} else if len(in.References) == 0 || len(in.References) > 50 || len(in.ReferenceKeys) != 0 {
		return ErrInvalid
	}
	if len(in.PromptParts) == 0 {
		in.Prompt = strings.TrimSpace(in.Prompt)
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.Prompt)) > 2000 || !ValidID(in.CanvasID) || !ValidID(in.NodeID) {
		return ErrInvalid
	}
	return nil
}
