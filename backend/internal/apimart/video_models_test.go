package apimart

import (
	"fmt"
	"testing"

	"frame-space/backend/internal/ark"
)

func TestDocumentedVideoModesUseCorrectImageSemantics(t *testing.T) {
	// Expectations come from the operation docs, independently of the registry.
	cases := []struct{ model, modes, referenceField, referenceRole, imageField string }{
		{"seedance-2.5", "tri", "image_urls", "", "image_with_roles"},
		{"seedance-2.0", "tri", "image_with_roles", "reference_image", "image_with_roles"},
		{"seedance-2.0-fast", "tri", "image_with_roles", "reference_image", "image_with_roles"},
		{"kling-v4", "tri", "image_urls", "", "first_frame_image"},
		{"kling-3.0-turbo", "ti", "", "", "first_frame_image"},
		{"kling-v3", "ti", "", "", "image_urls"},
		{"kling-v3-omni", "tri", "image_urls", "", "image_with_roles"},
		{"wan3.0-video", "tri", "image_with_roles", "reference_image", "image_with_roles"},
		{"wan3.0-video-prime", "tri", "image_with_roles", "reference_image", "image_with_roles"},
		{"MiniMax-H3", "tri", "image_urls", "", "first_frame_image"},
		{"MiniMax-H3-Max", "tri", "image_urls", "", "first_frame_image"},
		{"happyhorse-1.1", "tri", "image_urls", "", "first_frame_image"},
		{"happyhorse-1.0", "tri", "image_urls", "", "first_frame_image"},
		{"gemini-omni-1.1-flash", "tri", "image_with_roles", "reference", "first_frame_image"},
		{"viduq4-preview", "ri", "image_with_roles", "reference_image", "first_frame_image"},
		{"viduq3-pro", "ti", "", "", "image_urls"},
		{"grok-imagine-video-1.5", "tr", "image_urls", "", ""},
		{"flux-3-video", "ti", "", "", "image_urls"},
		{"pixverse-v6", "tri", "img_references", "", "image_urls"},
		{"veo3.1-quality-official", "ti", "", "", "first_frame_image"},
		{"veo3.1-fast-official", "ti", "", "", "first_frame_image"},
		{"skyreels-v4-std", "tri", "ref_images", "", "first_frame_image"},
	}
	for _, row := range cases {
		for _, mode := range []string{"text", "reference", "image"} {
			t.Run(row.model+"/"+mode, func(t *testing.T) {
				in := ark.VideoRequest{Model: row.model, Mode: mode, Prompt: "A paper boat moves slowly", Parameters: map[string]any{}}
				if mode != "text" {
					in.References = []ark.VideoReference{{Kind: "image", URL: "https://assets.example.test/boat.png"}}
				}
				payload, ok := videoPayload(in)
				expected := (mode == "text" && row.modes != "ri") || (mode == "reference" && row.referenceField != "") || (mode == "image" && row.imageField != "")
				if ok != expected {
					t.Fatalf("mode support = %v, want %v", ok, expected)
				}
				if !ok {
					return
				}
				if payload["model"] != row.model {
					t.Fatal("wrong provider identity")
				}
				if _, exists := payload["content"]; exists {
					t.Fatal("Ark content leaked into APIMart")
				}
				if mode == "text" {
					for _, key := range []string{"image_urls", "first_frame_image", "image_with_roles", "img_references", "ref_images"} {
						if _, exists := payload[key]; exists {
							t.Fatal("text sent images", key)
						}
					}
					return
				}
				field, role := row.referenceField, row.referenceRole
				if mode == "image" {
					field, role = row.imageField, "first_frame"
				}
				if payload[field] == nil {
					t.Fatalf("missing %s in %+v", field, payload)
				}
				if field == "image_with_roles" {
					items := payload[field].([]map[string]string)
					if len(items) != 1 || items[0]["role"] != role {
						t.Fatal("wrong image role", items)
					}
				}
				if row.model == "seedance-2.5" && mode == "image" && payload["size"] != "adaptive" {
					t.Fatal("first frame requires adaptive")
				}
				if row.model == "viduq3-pro" && mode == "image" && payload["aspect_ratio"] != nil {
					t.Fatal("Vidu image request must omit aspect_ratio")
				}
				if row.model == "skyreels-v4-std" && mode == "reference" && payload["prompt"] != in.Prompt+" @image1" {
					t.Fatal("required tag missing")
				}
			})
		}
	}
}

func TestNativeVideoParametersRejectUnopenedCapabilities(t *testing.T) {
	cases := []struct {
		model, mode string
		parameters  map[string]any
	}{
		{"seedance-2.5", "text", map[string]any{"seed": 1}},
		{"seedance-2.5", "text", map[string]any{"duration": 31}},
		{"seedance-2.5", "text", map[string]any{"duration": 5.5}},
		{"seedance-2.5", "text", map[string]any{"generate_audio": "false"}},
		{"kling-v3", "text", map[string]any{"mode": "1080p"}},
		{"veo3.1-quality-official", "text", map[string]any{"resolution": "4K", "duration": 4}},
		{"veo3.1-quality-official", "text", map[string]any{"sample_count": 4}},
		{"viduq4-preview", "text", map[string]any{}},
		{"grok-imagine-video-1.5", "image", map[string]any{}},
		{"kling-v3-motion-control", "text", map[string]any{}},
		{"MiniMax-H3-Regeneration", "text", map[string]any{}},
		{"wan2.7-videoedit", "text", map[string]any{}},
	}
	for i, c := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			_, ok := videoPayload(ark.VideoRequest{Model: c.model, Mode: c.mode, Prompt: "scene", Parameters: c.parameters})
			if ok {
				t.Fatal("invalid native fields accepted", c)
			}
		})
	}
}

func TestPromptReferenceLabelsFollowModelContract(t *testing.T) {
	for model, expected := range map[string]string{"seedance-2.5": "@图片2", "kling-v4": "@Image2", "kling-v3-omni": "<<<image_2>>>", "wan3.0-video": "图2", "skyreels-v4-std": "@image2"} {
		if got := ReferenceLabel(model, "image", 2); got != expected {
			t.Fatal(model, got, expected)
		}
	}
}
