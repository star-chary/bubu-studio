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
		{"video", "seedance-2.5", "480p", 20, false, 0, 334},
		{"video", "kling-v2-6-motion-control", "std", 20, false, 1, 198},
		{"video", "kling-v3-motion-control", "std", 20, false, 1, 356},
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
					// Higher Seedance 2.5 resolutions remain in the adapter only for old tasks.
					if id == "seedance-2.5" && raw != "480p" {
						continue
					}
					for _, audio := range []bool{false, true} {
						if _, ok := FixedCredits("video", id, raw.(string), 5, audio, 0); !ok {
							t.Errorf("video %s %v audio=%t is not priced", id, raw, audio)
						}
					}
				}
			}
		}
		if !foundResolution || (!foundDuration && !definition.MotionControl) {
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
			duration := r.Duration
			if definition.MotionControl {
				duration = 20 // Motion-control output duration follows the reference video.
			}
			if _, ok := FixedCredits("video", id, r.Resolution, duration, r.GenerateAudio, 0); !ok {
				t.Errorf("video %s/%s default cannot be quoted: %+v", id, mode, r)
			}
		}
	}
}

func TestSeedanceVideoReferenceCredits(t *testing.T) {
	cases := []struct {
		model, resolution string
		seconds, want     int
	}{
		{"seedance-2.0", "480p", 3, 56},
		{"seedance-2.0", "720p", 3, 120},
		{"seedance-2.0", "1080p", 3, 300},
		{"seedance-2.0", "4k", 3, 616},
		{"seedance-2.0-fast", "480p", 3, 34},
		{"seedance-2.0-fast", "720p", 3, 72},
	}
	for _, tc := range cases {
		got, ok := FixedVideoReferenceCredits(tc.model, tc.resolution, 5, tc.seconds)
		if !ok || got != int64(tc.want) {
			t.Errorf("%s %s: %d, %v, want %d", tc.model, tc.resolution, got, ok, tc.want)
		}
	}
	if got, ok := FixedVideoReferenceCredits("seedance-2.5", "480p", 20, 20); !ok || got != 400 {
		t.Fatalf("20s reference and 20s output: %d, %v", got, ok)
	}
	for _, tc := range []struct {
		model, resolution string
		duration, input   int
	}{
		{"wan3.0-video", "720p", 5, 2}, {"seedance-2.0-fast", "1080p", 5, 2},
		{"seedance-2.0", "720p", 5, 0}, {"seedance-2.0", "720p", 5, 16},
		{"seedance-2.0", "720p", 0, 2},
		{"seedance-2.5", "720p", 20, 20}, {"seedance-2.5", "480p", 20, 31},
	} {
		if _, ok := FixedVideoReferenceCredits(tc.model, tc.resolution, tc.duration, tc.input); ok {
			t.Errorf("accepted unpriced request: %+v", tc)
		}
	}
}
