package storage

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

type timedMemoryObjects struct {
	*memoryObjects
	ttls []time.Duration
}

func (m *timedMemoryObjects) SignGetFor(ctx context.Context, key string, ttl time.Duration) (string, error) {
	m.ttls = append(m.ttls, ttl)
	return m.SignGet(ctx, key)
}

func TestVideoPreflightReadsActualMediaAndSignsTwoHours(t *testing.T) {
	for _, name := range []string{"sample-image.png", "reference-video.mp4", "reference-audio.wav", "sample-video.mp4"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("../../../frontend/tests/fixtures/" + name)
			if err != nil {
				t.Fatal(err)
			}
			store, m := testStore(t)
			timed := &timedMemoryObjects{memoryObjects: m}
			store.objects = timed
			source := testScope
			source.NodeID = "30303030-3030-4030-8030-303030303030"
			asset, err := store.SaveUpload(context.Background(), bytes.NewReader(data), source)
			if err != nil {
				t.Fatal(err)
			}
			urls, err := store.VideoReferenceURLs(context.Background(), testScope, []string{asset.Key})
			if name == "sample-video.mp4" {
				if err == nil || len(timed.ttls) != 0 {
					t.Fatal("invalid model reference signed")
				}
				return
			}
			if err != nil || len(urls) != 1 || timed.ttls[0] != 2*time.Hour {
				t.Fatal(urls, err, timed.ttls)
			}
			timed.ttls = nil
			if _, err := store.VideoReferenceURLs(context.Background(), source, []string{asset.Key}); err == nil {
				t.Fatal("self-reference accepted")
			}
			if _, err := store.VideoReferenceURLs(context.Background(), testScope, []string{asset.Key, asset.Key}); err == nil {
				t.Fatal("duplicate accepted")
			}
			m.data = []byte("corrupt contents")
			if _, err := store.VideoReferenceURLs(context.Background(), testScope, []string{asset.Key}); err == nil || len(timed.ttls) != 0 {
				t.Fatal("corrupt object signed")
			}
			entries, _ := os.ReadDir(store.tempDir)
			if len(entries) > 0 {
				t.Fatal("preflight temp files leaked")
			}
		})
	}
}

func TestVideoPreflightAppliesSelectedModelLimitsBeforeSigning(t *testing.T) {
	data, err := os.ReadFile("../../../frontend/tests/fixtures/reference-audio.wav")
	if err != nil {
		t.Fatal(err)
	}
	store, m := testStore(t)
	timed := &timedMemoryObjects{memoryObjects: m}
	store.objects = timed
	source := testScope
	source.NodeID = "30303030-3030-4030-8030-303030303030"
	asset, err := store.SaveUpload(context.Background(), bytes.NewReader(data), source)
	if err != nil {
		t.Fatal(err)
	}
	miniLimits := VideoReferenceLimits{MaxImages: 9, MaxVideos: 3, MaxAudios: 3, MaxMediaSeconds: 15, AllowAudioOnly: false}
	if _, err := store.VideoReferenceURLsWithLimits(context.Background(), testScope, []string{asset.Key}, miniLimits); err == nil || len(timed.ttls) != 0 {
		t.Fatal("2.0-family audio-only reference was signed")
	}
	if _, err := store.VideoReferenceURLsWithLimits(context.Background(), testScope, []string{asset.Key}, defaultVideoReferenceLimits); err != nil || len(timed.ttls) != 1 {
		t.Fatalf("2.5 audio reference rejected: %v, signatures=%d", err, len(timed.ttls))
	}
}

func TestAPIMartSeedancePreflightRequiresQuotedVideoDuration(t *testing.T) {
	data, err := os.ReadFile("../../../frontend/tests/fixtures/reference-video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	store, memory := testStore(t)
	timed := &timedMemoryObjects{memoryObjects: memory}
	store.objects = timed
	source := testScope
	source.NodeID = "30303030-3030-4030-8030-303030303030"
	asset, err := store.SaveUpload(context.Background(), bytes.NewReader(data), source)
	if err != nil {
		t.Fatal(err)
	}
	limits := VideoReferenceLimits{MaxImages: 9, MaxVideos: 3, MaxMediaSeconds: 15, APIMartModel: "seedance-2.0", ExpectedVideoSeconds: map[string]float64{asset.Key: asset.Media.DurationSeconds}}
	if _, err := store.VideoReferenceURLsWithLimits(context.Background(), testScope, []string{asset.Key}, limits); err != nil {
		t.Fatal(err)
	}
	timed.ttls = nil
	limits.ExpectedVideoSeconds[asset.Key] = asset.Media.DurationSeconds - 0.5
	if _, err := store.VideoReferenceURLsWithLimits(context.Background(), testScope, []string{asset.Key}, limits); err == nil || len(timed.ttls) != 0 {
		t.Fatal("changed duration was signed")
	}
}

func TestAPIMartReferenceModelLimits(t *testing.T) {
	video := Asset{Kind: Video, ContentType: "video/mp4", Bytes: 1000, Media: &MediaMetadata{Width: 1920, Height: 1080, VideoCodec: "h264", FrameRate: 24, DurationSeconds: 20}}
	for _, model := range []string{"seedance-2.5", "kling-v2-6-motion-control", "kling-v3-motion-control"} {
		if check := CheckAPIMartVideoReference(video, model); !check.Eligible {
			t.Fatalf("%s rejected valid 20-second reference: %+v", model, check)
		}
	}
	if CheckAPIMartVideoReference(video, "seedance-2.0").Eligible {
		t.Fatal("Seedance 2.0 accepted a 1080p reference")
	}
	video.Media.DurationSeconds = 2.5
	if CheckAPIMartVideoReference(video, "kling-v2-6-motion-control").Eligible {
		t.Fatal("motion control accepted reference shorter than three seconds")
	}
	video.Media.DurationSeconds = 20
	video.Bytes = 100_000_001
	if CheckAPIMartVideoReference(video, "kling-v3-motion-control").Eligible {
		t.Fatal("motion control accepted video above the project's 100 MB limit")
	}
	image := Asset{Kind: Image, ContentType: "image/png", Bytes: 10_000_001, Media: &MediaMetadata{Width: 960, Height: 540}}
	if CheckAPIMartVideoReference(image, "kling-v3-motion-control").Eligible {
		t.Fatal("motion control accepted image above 10 MB")
	}
	video.Bytes = 1000
	video.ContentType, video.Media.AudioCodec = "video/quicktime", "pcm_s16le"
	if CheckAPIMartVideoReference(video, "seedance-2.5").Eligible {
		t.Fatal("Seedance 2.5 accepted an unsupported PCM audio track")
	}
}

func TestAPIMartSeedance25ReferenceDimensions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		width       int
		height      int
		wantAllowed bool
	}{
		{"short side below 480", 1000, 450, true},
		{"short side above 1080", 2560, 1440, true},
		{"below minimum total pixels", 1000, 409, false},
		{"above maximum total pixels", 3840, 2200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			video := Asset{Kind: Video, ContentType: "video/mp4", Bytes: 1000, Media: &MediaMetadata{
				Width: tc.width, Height: tc.height, VideoCodec: "h264", FrameRate: 24, DurationSeconds: 5,
			}}
			if check := CheckAPIMartVideoReference(video, "seedance-2.5"); check.Eligible != tc.wantAllowed {
				t.Fatalf("reference %dx%d: %+v, want allowed=%t", tc.width, tc.height, check, tc.wantAllowed)
			}
		})
	}
}

func TestGeneratedVideoAcceptsMP4AndRejectsOtherMedia(t *testing.T) {
	for _, name := range []string{"reference-video.mp4", "sample-image.png"} {
		data, err := os.ReadFile("../../../frontend/tests/fixtures/" + name)
		if err != nil {
			t.Fatal(err)
		}
		store, _ := testStore(t)
		asset, err := store.save(context.Background(), bytes.NewReader(data), testScope, Generated, "test", Video)
		if strings.HasSuffix(name, ".mp4") {
			if err != nil || asset.Kind != Video || asset.Media == nil || !strings.Contains(asset.Key, "/generated/videos/") {
				t.Fatal(asset, err)
			}
		} else if err == nil {
			t.Fatal("image accepted as generated video")
		}
	}
}
