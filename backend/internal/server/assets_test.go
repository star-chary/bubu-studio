package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
)

const assetCanvas = "f2476fbb-2af0-494f-9a38-f8f9bdf1e5ac"
const assetNode = "56b46c49-fc0b-41cf-9db0-01493aa14d3f"

type stubAssets struct {
	uploaded, generated int
	failed              bool
}

func (s *stubAssets) ReferenceURLs(context.Context, storage.Scope, []string) ([]string, error) {
	return nil, errors.New("unexpected reference request")
}

func (s *stubAssets) SaveUpload(ctx context.Context, reader io.Reader, scope storage.Scope) (storage.Asset, error) {
	s.uploaded++
	if scope.CanvasID != assetCanvas || scope.NodeID != assetNode {
		return storage.Asset{}, errors.New("wrong scope")
	}
	_, _ = io.Copy(io.Discard, reader)
	return storage.Asset{Key: "test-key", URL: "/api/assets/content?key=test-key", Scope: scope}, nil
}
func (s *stubAssets) SaveGenerated(ctx context.Context, source, model string, scope storage.Scope) (storage.Asset, error) {
	s.generated++
	if s.failed {
		return storage.Asset{}, errors.New("private-provider-error")
	}
	if source != "https://images.example.test/a.png" || model != ark.ProImageModel || scope.NodeID != assetNode {
		return storage.Asset{}, errors.New("wrong generation snapshot")
	}
	return storage.Asset{URL: "/api/assets/content?key=test-key", Scope: scope}, nil
}
func (s *stubAssets) Read(ctx context.Context, key, byteRange string) (*storage.Content, error) {
	if byteRange != "bytes=0-3" {
		return nil, errors.New("missing range")
	}
	return &storage.Content{Body: io.NopCloser(strings.NewReader("test")), Status: 206, Bytes: 4, ContentRange: "bytes 0-3/100", ContentType: "video/mp4"}, nil
}
func TestUploadRouteRequiresScopeAndBinaryBody(t *testing.T) {
	store := &stubAssets{}
	router := newHandlerTestRouter(nil, store)
	for _, tc := range []struct {
		query, contentType string
		size               int64
		want               int
	}{
		{"?canvasId=" + assetCanvas + "&nodeId=" + assetNode, "application/octet-stream", 4, 201},
		{"?canvasId=../other&nodeId=" + assetNode, "application/octet-stream", 4, 400},
		{"?canvasId=" + assetCanvas + "&nodeId=" + assetNode, "multipart/form-data", 4, 415},
		{"?canvasId=" + assetCanvas + "&nodeId=" + assetNode, "application/octet-stream", storage.MaxVideoBytes + 1, 413},
	} {
		req := httptest.NewRequest("POST", "/api/assets"+tc.query, strings.NewReader("test"))
		req.Header.Set("Content-Type", tc.contentType)
		req.ContentLength = tc.size
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("上传状态不符：%d", w.Code)
		}
	}
	if store.uploaded != 1 {
		t.Fatal("无效上传不能写入 OSS")
	}
	w := httptest.NewRecorder()
	newHandlerTestRouter(nil, nil).ServeHTTP(w, httptest.NewRequest("GET", "/api/storage/config", nil))
	if !strings.Contains(w.Body.String(), `"enabled":false`) || strings.Contains(w.Body.String(), "ACCESS_KEY") {
		t.Fatal("配置接口只公开能力，不能公开凭据")
	}
}

func TestMediaReadRouteForwardsPartialContent(t *testing.T) {
	router := newHandlerTestRouter(nil, &stubAssets{})
	req := httptest.NewRequest("GET", "/api/assets/content?key=test-key", nil)
	req.Header.Set("Range", "bytes=0-3")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusPartialContent || w.Body.String() != "test" || w.Header().Get("Content-Range") != "bytes 0-3/100" || w.Header().Get("Content-Type") != "video/mp4" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("私有视频预览未正确转发范围读取")
	}
}

func TestGeneratedImagePersistsOnceAndKeepsPreviewOnStorageFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		store := &stubAssets{failed: fail}
		modelCalls := 0
		router := newHandlerTestRouter(generateFunc(func(ctx context.Context, prompt, model string) (ark.ImageResult, error) {
			modelCalls++
			return ark.ImageResult{URL: "https://images.example.test/a.png", Model: model}, nil
		}), store)
		request := func(body string) *httptest.ResponseRecorder {
			req := httptest.NewRequest("POST", "/api/images/generations", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			return w
		}
		if request(`{"prompt":"image"}`).Code != 400 || modelCalls != 0 {
			t.Fatal("缺少归属时不能先调用付费模型")
		}
		w := request(`{"prompt":"image","model":"` + ark.ProImageModel + `","canvasId":"` + assetCanvas + `","nodeId":"` + assetNode + `"}`)
		if w.Code != 200 || modelCalls != 1 || store.generated != 1 {
			t.Fatal("应只生成一次并转存一次")
		}
		if fail {
			if !strings.Contains(w.Body.String(), "storageError") || !strings.Contains(w.Body.String(), "https://images.example.test/a.png") || strings.Contains(w.Body.String(), "private-provider") {
				t.Fatal("转存失败应保留临时图片并明确提示")
			}
		} else if !strings.Contains(w.Body.String(), "/api/assets/content?key=test-key") || strings.Contains(w.Body.String(), "storageError") {
			t.Fatal("成功后应返回本项目资源预览地址")
		}
	}
}
