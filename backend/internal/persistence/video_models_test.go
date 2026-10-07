package persistence

import (
	"context"
	"math"
	"testing"

	"frame-space/backend/internal/ark"
)

func TestNormalizeVideoInputModelModes(t *testing.T) {
	base := Input{CanvasID: canvasID, NodeID: nodeID, Resolution: "480p", Ratio: "16:9", Duration: 5, Prompt: "A moving scene"}
	image := Reference{NodeID: promptSourceID, AssetKey: "image", Kind: "image"}
	for _, tc := range []struct {
		name  string
		input Input
		valid bool
	}{
		{"legacy reference defaults", func() Input { in := base; in.References = []Reference{image}; return in }(), true},
		{"2.5 text", func() Input { in := base; in.Model = ark.VideoModel; in.Mode = "text"; in.Duration = 30; return in }(), true},
		{"2.0 text", func() Input { in := base; in.Model = ark.VideoModel20; in.Mode = "text"; in.Duration = 15; return in }(), true},
		{"2.0 fast text", func() Input { in := base; in.Model = ark.VideoModel20Fast; in.Mode = "text"; return in }(), true},
		{"2.0 mini text", func() Input { in := base; in.Model = ark.VideoModel20Mini; in.Mode = "text"; return in }(), true},
		{"2.0 over duration", func() Input { in := base; in.Model = ark.VideoModel20; in.Mode = "text"; in.Duration = 16; return in }(), false},
		{"2.5 over duration", func() Input { in := base; in.Model = ark.VideoModel; in.Mode = "text"; in.Duration = 31; return in }(), false},
		{"1080p outside first release", func() Input {
			in := base
			in.Model = ark.VideoModel20
			in.Mode = "text"
			in.Resolution = "1080p"
			return in
		}(), false},
		{"unknown model", func() Input { in := base; in.Model = "unknown"; in.Mode = "text"; return in }(), false},
		{"text without prompt", func() Input { in := base; in.Model = ark.VideoModel20; in.Mode = "text"; in.Prompt = " "; return in }(), false},
		{"text with reference", func() Input {
			in := base
			in.Model = ark.VideoModel20
			in.Mode = "text"
			in.References = []Reference{image}
			return in
		}(), false},
		{"text with prompt mention", func() Input {
			in := base
			in.Model = ark.VideoModel20
			in.Mode = "text"
			in.PromptParts = []PromptPart{{Type: "reference", NodeID: promptSourceID, Kind: "image", Name: "source"}}
			return in
		}(), false},
		{"reference without material", func() Input { in := base; in.Model = ark.VideoModel20; in.Mode = "reference"; return in }(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeVideoInput(&tc.input)
			if (got == nil) != tc.valid {
				t.Fatalf("validation: %v", got)
			}
			if tc.name == "legacy reference defaults" && (tc.input.Model != ark.VideoModel || tc.input.Mode != "reference") {
				t.Fatalf("legacy defaults changed: %+v", tc.input)
			}
		})
	}
}

func TestVideoTextQuoteUsesFullModelRatesAndOutputPixels(t *testing.T) {
	store := &Store{} // Text-only has no reference assets to query.
	for _, tc := range []struct {
		model, resolution, ratio string
		points                   int64
	}{
		{ark.VideoModel, "480p", "16:9", 82},
		{ark.VideoModel20, "480p", "16:9", 56},
		{ark.VideoModel20Fast, "480p", "16:9", 46},
		{ark.VideoModel20Mini, "480p", "16:9", 28},
		{ark.VideoModel20, "720p", "16:9", 120},
		{ark.VideoModel20Fast, "720p", "16:9", 96},
		{ark.VideoModel20Mini, "720p", "16:9", 60},
	} {
		in := Input{Model: tc.model, Mode: "text", Prompt: "scene", Resolution: tc.resolution, Ratio: tc.ratio, Duration: 5}
		yuan, err := store.videoQuoteYuan(context.Background(), in, nil)
		points := int64(2 * math.Ceil(12*yuan-1e-9))
		if err != nil || points != tc.points {
			t.Errorf("%s %s %s: got %d points, want %d (error %v)", tc.model, tc.resolution, tc.ratio, points, tc.points, err)
		}
	}
}

func TestSnapshotVideoModelAndModeAreSavedButDraftReferencesRemain(t *testing.T) {
	videoID := "45454545-4545-4454-8454-454545454545"
	s := Snapshot{Viewport: Viewport{Zoom: 1}, Nodes: []Node{
		{ID: videoID, Data: NodeData{Kind: "video", Name: "Video", VideoModel: ark.VideoModel20Mini, VideoMode: "text", VideoOptions: &VideoOptions{Resolution: "720p", Ratio: "9:16", Duration: 15}, ReferenceIDs: []string{nodeID}}},
		{ID: nodeID, Data: NodeData{Kind: "image", Name: "Draft reference"}},
	}}
	if err := s.Validate(); err != nil {
		t.Fatalf("saved draft rejected: %v", err)
	}
	s.Nodes[0].Data.VideoOptions.Duration = 16
	if err := s.Validate(); err == nil {
		t.Fatal("saved options exceed model duration")
	}
	s.Nodes[0].Data.VideoOptions.Duration = 15
	s.Nodes[0].Data.VideoModel = "unknown"
	if err := s.Validate(); err == nil {
		t.Fatal("saved unknown model accepted")
	}
}
