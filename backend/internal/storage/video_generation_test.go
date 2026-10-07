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
