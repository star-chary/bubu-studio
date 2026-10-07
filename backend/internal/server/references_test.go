package server

import (
	"context"
	"encoding/json"
	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
	"net/http/httptest"
	"strings"
	"testing"
)

type referenceGenerator func(context.Context, string, string, []string) (ark.ImageResult, error)

func (f referenceGenerator) Generate(ctx context.Context, prompt, model string, images ...string) (ark.ImageResult, error) {
	return f(ctx, prompt, model, images)
}

type referenceAssets struct {
	stubAssets
	prepared int
	fail     bool
}

func (s *referenceAssets) ReferenceURLs(_ context.Context, scope storage.Scope, keys []string) ([]string, error) {
	s.prepared++
	if s.fail || scope.CanvasID != assetCanvas || scope.NodeID != assetNode || len(keys) != 1 || keys[0] != "saved-key" {
		return nil, &storage.Error{Status: 422, Code: "REFERENCE_UNAVAILABLE", Message: "参考图片无法读取"}
	}
	return []string{"https://oss.example.test/reference.png?signature=private-test"}, nil
}
func TestImageToImagePreparesReferencesBeforeOneModelCall(t *testing.T) {
	for _, fail := range []bool{false, true} {
		store := &referenceAssets{fail: fail}
		calls := 0
		router := newHandlerTestRouter(referenceGenerator(func(_ context.Context, prompt, model string, images []string) (ark.ImageResult, error) {
			calls++
			if len(images) != 1 || !strings.Contains(images[0], "signature=private-test") || prompt != "turn blue" || model != ark.ProImageModel {
				t.Fatal("reference input lost")
			}
			return ark.ImageResult{URL: "https://images.example.test/a.png", Model: model}, nil
		}), store)
		body := `{"prompt":"turn blue","model":"` + ark.ProImageModel + `","canvasId":"` + assetCanvas + `","nodeId":"` + assetNode + `","referenceKeys":["saved-key"]}`
		req := httptest.NewRequest("POST", "/api/images/generations", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if strings.Contains(w.Body.String(), "private-test") {
			t.Fatal("signed input URL must not reach browser")
		}
		if fail {
			if w.Code != 422 || calls != 0 || store.generated != 0 {
				t.Fatal("reference failure must stop paid generation")
			}
		} else if w.Code != 200 || calls != 1 || store.prepared != 1 || store.generated != 1 {
			t.Fatal("one generation must save one image")
		}
	}
}
func TestReferenceLimitsAndDisabledStorageRejectBeforeModel(t *testing.T) {
	for _, tc := range []struct {
		model string
		count int
		store AssetStore
		want  int
	}{
		{ark.ProImageModel, 11, &referenceAssets{}, 400}, {ark.DefaultImageModel, 15, &referenceAssets{}, 400}, {ark.DefaultImageModel, 1, nil, 503},
	} {
		calls := 0
		router := newHandlerTestRouter(generateFunc(func(context.Context, string, string) (ark.ImageResult, error) { calls++; return ark.ImageResult{}, nil }), tc.store)
		body, _ := json.Marshal(map[string]any{"prompt": "test", "model": tc.model, "canvasId": assetCanvas, "nodeId": assetNode, "referenceKeys": make([]string, tc.count)})
		req := httptest.NewRequest("POST", "/api/images/generations", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.want || calls != 0 {
			t.Fatal("invalid references must fail before model call")
		}
	}
}
