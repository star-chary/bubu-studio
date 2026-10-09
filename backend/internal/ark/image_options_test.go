package ark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestImageOptionsContract(t *testing.T) {
	for _, tc := range []struct{ model, resolution, ratio, size string }{
		{DefaultImageModel, "", "", "2K"},
		{DefaultImageModel, "2K", "16:9", "2848x1600"},
		{DefaultImageModel, "3K", "9:16", "2304x4096"},
		{DefaultImageModel, "4K", "21:9", "6240x2656"},
		{ProImageModel, "1K", "9:16", "800x1424"},
		{ProImageModel, "1.5K", "3:2", "1872x1248"},
		{ProImageModel, "2K", "16:9", "2816x1584"},
		{ProImageModel, "1.5K", "adaptive", "1.5K"},
	} {
		t.Run(tc.model+tc.resolution+tc.ratio, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["size"] != tc.size || body["prompt"] != "keep this prompt" {
					t.Errorf("unexpected request: %v", body)
				}
				if tc.model == ProImageModel && (body["sequential_image_generation"] != nil || body["stream"] != nil) {
					t.Error("Pro received unsupported controls")
				}
				_, _ = w.Write([]byte(`{"data":[{"url":"https://images.example.test/result.png"}]}`))
			}))
			defer upstream.Close()
			client := NewClient("test-key")
			client.endpoint = upstream.URL
			if _, err := client.Generate(context.Background(), "keep this prompt", tc.model, &ImageOptions{tc.resolution, tc.ratio}); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, tc := range []struct{ model, resolution, ratio string }{
		{DefaultImageModel, "1K", "1:1"}, {DefaultImageModel, "1.5K", "1:1"},
		{ProImageModel, "3K", "1:1"}, {ProImageModel, "4K", "1:1"},
		{ProImageModel, "2K", "16:10"}, {"unknown", "2K", "1:1"},
	} {
		_, err := NewClient("").Generate(context.Background(), "test", tc.model, &ImageOptions{tc.resolution, tc.ratio})
		var failure *APIError
		if !errors.As(err, &failure) || failure.Status != 400 {
			t.Fatalf("options were not rejected before key/network use: %+v %v", tc, err)
		}
	}
}

func TestImagePresetsRespectProviderPixelLimits(t *testing.T) {
	for model, presets := range map[string]map[string]map[string]string{DefaultImageModel: liteImageSizes, ProImageModel: proImageSizes} {
		minPixels, maxPixels := 3686400, 16777216
		if model == ProImageModel {
			minPixels, maxPixels = 921600, 4624220
		}
		for resolution, ratios := range presets {
			for ratio, size := range ratios {
				var width, height int
				if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil {
					t.Fatal(err)
				}
				pixels := width * height
				if pixels < minPixels || pixels > maxPixels || width > height*16 || height > width*16 {
					t.Errorf("invalid dimensions for %s %s %s: %s", model, resolution, ratio, size)
				}
				if model == ProImageModel && (resolution == "1K" || resolution == "1.5K") && pixels > 2610000 {
					t.Error("low resolution exceeds its quoted price tier")
				}
			}
		}
	}
}
