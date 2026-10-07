package storage

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "frontend", "tests", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAudioVideoUploadProbesActualFileAndPreservesContent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		kind     Kind
		mime     string
		eligible bool
	}{
		{"reference-audio.wav", Audio, "audio/wav", true},
		{"reference-audio.mp3", Audio, "audio/mpeg", true},
		{"reference-video.mp4", Video, "video/mp4", true},
		{"sample-video.mp4", Video, "video/mp4", false},
		{"sample-image.png", Image, "image/png", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, objects := testStore(t)
			data := fixture(t, tc.name)
			asset, err := store.SaveUpload(context.Background(), bytes.NewReader(data), testScope)
			if err != nil {
				t.Fatal(err)
			}
			if asset.Kind != tc.kind || asset.ContentType != tc.mime || asset.Media == nil || asset.VideoReference == nil || asset.VideoReference.Eligible != tc.eligible {
				t.Fatalf("incorrect inspected asset: %+v", asset)
			}
			if !bytes.Equal(objects.data, data) || objects.puts != 1 {
				t.Fatal("probing changed upload bytes")
			}
			if tc.kind == Audio && (asset.Media.DurationSeconds < 2 || asset.Media.DurationSeconds > 2.1 || asset.Media.AudioCodec == "" || asset.Media.VideoCodec != "" || !strings.Contains(asset.Key, "/audios/")) {
				t.Fatalf("wrong audio metadata: %+v", asset.Media)
			}
			if tc.name == "reference-video.mp4" && (asset.Media.Width != 854 || asset.Media.Height != 480 || asset.Media.FrameRate != 24 || asset.Media.DurationSeconds != 2 || asset.Media.VideoCodec != "h264") {
				t.Fatalf("wrong video metadata: %+v", asset.Media)
			}
			if _, err := store.keys.validateKey(asset.Key); err != nil {
				t.Fatal(err)
			}
			files, _ := os.ReadDir(store.tempDir)
			if len(files) != 0 {
				t.Fatal("temporary upload remains")
			}
		})
	}
}

func TestAudioUploadFailuresDoNotWriteObjects(t *testing.T) {
	data := fixture(t, "reference-audio.wav")
	if _, err := detectMedia(bytes.NewReader(data), MaxAudioBytes+1); err == nil {
		t.Fatal("oversized audio accepted")
	}
	t.Run("probe unavailable", func(t *testing.T) {
		t.Setenv("FFPROBE_PATH", filepath.Join(t.TempDir(), "missing-ffprobe"))
		store, objects := testStore(t)
		_, err := store.SaveUpload(context.Background(), bytes.NewReader(data), testScope)
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != "MEDIA_PROBE_UNAVAILABLE" || objects.puts != 0 {
			t.Fatalf("missing tool must not upload: %v", err)
		}
	})
	t.Run("damaged container", func(t *testing.T) {
		store, objects := testStore(t)
		// Enough RIFF/WAVE header to identify the type, no usable audio data.
		_, err := store.SaveUpload(context.Background(), bytes.NewReader(data[:36]), testScope)
		if err == nil || objects.puts != 0 {
			t.Fatal("damaged audio saved")
		}
		files, _ := os.ReadDir(store.tempDir)
		if len(files) != 0 {
			t.Fatal("failed probe left a temporary file")
		}
	})
}

func TestVideoReferenceRulesIndependentOfOrdinaryStorage(t *testing.T) {
	base := Asset{Kind: Video, ContentType: "video/mp4", Bytes: 200_000_000, Media: &MediaMetadata{Width: 854, Height: 480, DurationSeconds: 2, FrameRate: 24, VideoCodec: "h264"}}
	if !CheckVideoReference(base).Eligible {
		t.Fatal("minimum normal reference rejected")
	}
	cases := []struct {
		name   string
		change func(*Asset)
	}{
		{"decimal byte limit", func(a *Asset) { a.Bytes++ }},
		{"pixels too small", func(a *Asset) { a.Media.Width = 640; a.Media.Height = 480 }},
		{"dimensions", func(a *Asset) { a.Media.Height = 299 }},
		{"aspect ratio", func(a *Asset) { a.Media.Width = 1800 }},
		{"duration short", func(a *Asset) { a.Media.DurationSeconds = 1.999 }},
		{"duration long", func(a *Asset) { a.Media.DurationSeconds = 30.001 }},
		{"duration invalid", func(a *Asset) { a.Media.DurationSeconds = math.NaN() }},
		{"fps low", func(a *Asset) { a.Media.FrameRate = 23.999 }},
		{"fps high", func(a *Asset) { a.Media.FrameRate = 60.001 }},
		{"fps invalid", func(a *Asset) { a.Media.FrameRate = math.Inf(1) }},
		{"video codec", func(a *Asset) { a.Media.VideoCodec = "vp9" }},
		{"audio codec", func(a *Asset) { a.Media.AudioCodec = "opus" }},
		{"container", func(a *Asset) { a.ContentType = "video/webm" }},
		{"legacy metadata", func(a *Asset) { a.Media = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, m := base, *base.Media
			a.Media = &m
			tc.change(&a)
			check := CheckVideoReference(a)
			if check.Eligible || check.Reason == "" {
				t.Fatal("invalid reference not explained")
			}
		})
	}
	a, m := base, *base.Media
	a.Media = &m
	m.DurationSeconds, m.FrameRate, m.VideoCodec = 30, 60, "hevc"
	if !CheckVideoReference(a).Eligible {
		t.Fatal("upper normal limits rejected")
	}
	a.Kind, a.ContentType, a.Bytes = Audio, "audio/wav", MaxAudioBytes
	a.Media = &MediaMetadata{DurationSeconds: 30, AudioCodec: "pcm_s16le"}
	if !CheckVideoReference(a).Eligible {
		t.Fatal("audio-only reference rejected")
	}
	a.Bytes++
	if CheckVideoReference(a).Eligible {
		t.Fatal("oversized audio reference accepted")
	}
}
