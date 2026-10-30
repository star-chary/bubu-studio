package apimart

import (
	"testing"

	"frame-space/backend/internal/ark"
)

func TestSeedanceReferenceVideosPayload(t *testing.T) {
	for _, model := range []string{"seedance-2.0", "seedance-2.0-fast"} {
		t.Run(model, func(t *testing.T) {
			in := ark.VideoRequest{Model: model, Mode: "reference", Prompt: "参考@视频1", Resolution: "720p", Ratio: "16:9", Duration: 5,
				References: []ark.VideoReference{{Kind: "video", URL: "https://example.test/video.mp4"}}}
			payload, ok := videoPayload(in)
			videos, isList := payload["video_urls"].([]string)
			if !ok || !isList || len(videos) != 1 || videos[0] != in.References[0].URL || payload["image_with_roles"] != nil {
				t.Fatalf("video-only payload: %#v, %v", payload, ok)
			}
			in.References = append(in.References, ark.VideoReference{Kind: "image", URL: "https://example.test/image.png"})
			payload, ok = videoPayload(in)
			images, isList := payload["image_with_roles"].([]map[string]string)
			if !ok || !isList || len(images) != 1 || images[0]["role"] != "reference_image" {
				t.Fatalf("mixed payload: %#v, %v", payload, ok)
			}
			in.References = append(in.References, in.References[0], in.References[0], in.References[0])
			if _, ok := videoPayload(in); ok {
				t.Fatal("accepted four videos")
			}
			if ReferenceLabel(model, "video", 2) != "@视频2" {
				t.Fatal("incorrect video label")
			}
		})
	}
	in := ark.VideoRequest{Model: "wan3.0-video", Mode: "reference", Prompt: "test", Resolution: "480P", Ratio: "16:9", Duration: 5, References: []ark.VideoReference{{Kind: "video", URL: "https://example.test/video.mp4"}}}
	if _, ok := videoPayload(in); ok {
		t.Fatal("unopened model accepted video")
	}
}

func TestSeedance25AndMotionControlPayloads(t *testing.T) {
	image := ark.VideoReference{Kind: "image", URL: "https://example.test/image.png"}
	video := ark.VideoReference{Kind: "video", URL: "https://example.test/video.mp4"}
	seed := ark.VideoRequest{Model: "seedance-2.5", Mode: "reference", Prompt: "参考@视频1", Parameters: map[string]any{"size": "16:9", "resolution": "480p", "duration": 20, "generate_audio": false}, References: []ark.VideoReference{image, video}}
	payload, ok := videoPayload(seed)
	images, imageOK := payload["image_urls"].([]string)
	videos, videoOK := payload["video_urls"].([]string)
	if !ok || !imageOK || !videoOK || len(images) != 1 || images[0] != image.URL || len(videos) != 1 || videos[0] != video.URL || payload["omni_reference_task_type"] != "reference" || payload["duration"] != 20 {
		t.Fatalf("seedance 2.5 payload: %#v, %v", payload, ok)
	}
	for _, model := range []string{"kling-v2-6-motion-control", "kling-v3-motion-control"} {
		t.Run(model, func(t *testing.T) {
			in := ark.VideoRequest{Model: model, Mode: "reference", Prompt: "参考视频中的动作", Parameters: map[string]any{"mode": "std"}, References: []ark.VideoReference{image, video}}
			payload, ok := videoPayload(in)
			if !ok || payload["image_url"] != image.URL || payload["video_url"] != video.URL || payload["character_orientation"] != "video" || payload["keep_original_sound"] != "yes" || payload["mode"] != "std" {
				t.Fatalf("motion payload: %#v, %v", payload, ok)
			}
			if payload["duration"] != nil || payload["image_urls"] != nil || payload["video_urls"] != nil {
				t.Fatalf("unexpected fields in motion payload: %#v", payload)
			}
			for _, refs := range [][]ark.VideoReference{{image}, {video}, {image, image}, {video, video}} {
				in.References = refs
				if _, ok := videoPayload(in); ok {
					t.Fatalf("accepted invalid motion references: %+v", refs)
				}
			}
		})
	}
}
