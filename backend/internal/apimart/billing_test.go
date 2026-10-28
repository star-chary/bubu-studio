package apimart

import (
	"fmt"
	"testing"

	"frame-space/backend/internal/ark"
)

func TestFixedCreditsPricingExamples(t *testing.T) {
	for _, tc := range []struct {
		kind, model, resolution string
		duration                int
		audio                   bool
		references              int
		points                  int64
	}{
		{"image", "seedream-5-0-lite", "2K", 0, false, 0, 6},
		{"image", "seedream-5-0-pro", "2K", 0, false, 1, 12},
		{"image", "seedream-5-0-pro", "2K", 0, false, 10, 14},
		{"image", "qwen-image-3.0-pro", "1K", 0, false, 0, 6},
		{"image", "qwen-image-3.0-pro", "1K", 0, false, 3, 8},
		{"image", "flux-3-image", "4k", 0, false, 10, 84},
		{"video", "kling-v3", "std", 5, false, 0, 60},
		{"video", "kling-v3", "std", 5, true, 0, 88},
		{"video", "MiniMax-H3", "768P", 5, false, 5, 50},
		{"video", "MiniMax-H3", "768P", 5, false, 6, 54},
		{"video", "wan3.0-video", "720P", 5, false, 0, 58},
	} {
		t.Run(fmt.Sprintf("%s/%s/%s/%d/%t/%d", tc.kind, tc.model, tc.resolution, tc.duration, tc.audio, tc.references), func(t *testing.T) {
			points, ok := FixedCredits(tc.kind, tc.model, tc.resolution, tc.duration, tc.audio, tc.references)
			if !ok || points != tc.points {
				t.Fatalf("got %d, %v; want %d", points, ok, tc.points)
			}
		})
	}
	for _, tc := range []struct {
		kind, model, resolution string
		duration                int
	}{
		{"video", "seedance-2.5", "720p", 5},
		{"image", "midjourney", "1K", 0},
		{"video", "wan3.0-video", "720P", -1},
		{"video", "kling-v3", "unknown", 5},
	} {
		if _, ok := FixedCredits(tc.kind, tc.model, tc.resolution, tc.duration, false, 0); ok {
			t.Fatalf("accepted unpriced request: %+v", tc)
		}
	}
}

// Every priced model must have an execution contract, and every exposed
// resolution must have a price for both audio states.
func TestBillingTableCoversExecutionResolutions(t *testing.T) {
	if BillingVersion() == "" {
		t.Fatal("missing pricing version")
	}
	for id := range billing.Images {
		definition, ok := ImageDefinition(id)
		if !ok {
			t.Fatalf("image %s has no execution contract", id)
		}
		for _, field := range definition.Parameters {
			if field.Key == "resolution" {
				for _, raw := range field.Enum {
					if _, ok := FixedCredits("image", id, raw.(string), 0, false, 0); !ok {
						t.Errorf("image %s %v is not priced", id, raw)
					}
				}
			}
		}
	}
	for id := range billing.Videos {
		definition, ok := VideoDefinition(id)
		if !ok {
			t.Fatalf("video %s has no execution contract", id)
		}
		foundResolution, foundDuration := false, false
		for _, field := range definition.Parameters {
			if field.Role == "duration" {
				foundDuration = true
			}
			if field.Role == "resolution" {
				foundResolution = true
				for _, raw := range field.Enum {
					for _, audio := range []bool{false, true} {
						if _, ok := FixedCredits("video", id, raw.(string), 5, audio, 0); !ok {
							t.Errorf("video %s %v audio=%t is not priced", id, raw, audio)
						}
					}
				}
			}
		}
		if !foundResolution || !foundDuration {
			t.Errorf("video %s cannot quote explicit resolution and duration", id)
		}
	}
}

func TestBillableModelDefaultsCanBeNormalizedAndQuoted(t *testing.T) {
	for id := range billing.Images {
		r := ImageRequest{Model: id, Prompt: "A paper boat"}
		if !NormalizeImageRequest(&r, 0) {
			t.Errorf("image %s rejects its default input", id)
			continue
		}
		resolution, _ := r.Parameters["resolution"].(string)
		if _, ok := FixedCredits("image", id, resolution, 0, false, 0); !ok {
			t.Errorf("image %s default cannot be quoted", id)
		}
	}
	for id := range billing.Videos {
		definition, _ := VideoDefinition(id)
		for _, mode := range definition.Modes {
			r := ark.VideoRequest{Model: id, Mode: mode, Prompt: "A paper boat", Parameters: map[string]any{}}
			if !NormalizeVideoRequest(&r) {
				t.Errorf("video %s/%s rejects its default input", id, mode)
				continue
			}
			if _, ok := FixedCredits("video", id, r.Resolution, r.Duration, r.GenerateAudio, 0); !ok {
				t.Errorf("video %s/%s default cannot be quoted: %+v", id, mode, r)
			}
		}
	}
}
