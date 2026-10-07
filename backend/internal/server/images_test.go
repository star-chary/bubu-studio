package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"frame-space/backend/internal/ark"
	"github.com/gin-gonic/gin"
)

type generateFunc func(context.Context, string, string) (ark.ImageResult, error)

func (f generateFunc) Generate(ctx context.Context, prompt, model string, _ ...string) (ark.ImageResult, error) {
	return f(ctx, prompt, model)
}

func TestInputValidationBeforeModelCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls atomic.Int32
	router := newHandlerTestRouter(generateFunc(func(ctx context.Context, prompt, model string) (ark.ImageResult, error) {
		calls.Add(1)
		return ark.ImageResult{}, nil
	}), nil)
	for _, body := range []string{`{}`, `null`, `{"prompt":"   "}`, `{"prompt":1}`, `broken`, `{"prompt":"valid","image":"local-image"}`, `{"prompt":"valid"} {}`, `{"prompt":"` + strings.Repeat("中", 2001) + `"}`, `{"prompt":"` + strings.Repeat("中", 10000) + `"}`} {
		req := httptest.NewRequest("POST", "/api/images/generations", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("无效参数应返回 400，实际 %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/images/generations", strings.NewReader(`{"prompt":"valid"}`)))
	if w.Code != 415 || calls.Load() != 0 {
		t.Fatal("无效请求不应调用模型")
	}
	for _, body := range []string{`{"prompt":"valid","model":"arbitrary-model"}`, `{"prompt":"valid","model":123}`} {
		req := httptest.NewRequest("POST", "/api/images/generations", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 400 || calls.Load() != 0 {
			t.Fatal("未知或格式错误的模型不能调用供应商")
		}
	}
}

func TestSelectedModelAndDefaultReachGenerator(t *testing.T) {
	for _, model := range []string{"", ark.DefaultImageModel, ark.ProImageModel} {
		t.Run("model="+model, func(t *testing.T) {
			want := model
			if want == "" {
				want = ark.DefaultImageModel
			}
			calls := 0
			router := newHandlerTestRouter(generateFunc(func(ctx context.Context, prompt, selected string) (ark.ImageResult, error) {
				calls++
				if prompt != "列车" || selected != want {
					t.Errorf("模型或提示词转发错误：%s, %s", selected, prompt)
				}
				return ark.ImageResult{URL: "https://images.example.test/a.png", Model: selected}, nil
			}), nil)
			body := map[string]string{"prompt": " 列车 "}
			if model != "" {
				body["model"] = model
			}
			encoded, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", "/api/images/generations", strings.NewReader(string(encoded)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			var result ark.ImageResult
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || calls != 1 || result.Model != want {
				t.Fatalf("返回模型不符：%s", w.Body.String())
			}
		})
	}
}

func TestGenerationResponseAndError(t *testing.T) {
	for _, fail := range []bool{false, true} {
		router := newHandlerTestRouter(generateFunc(func(ctx context.Context, prompt, model string) (ark.ImageResult, error) {
			if prompt != "列车" {
				t.Errorf("应去除两端空白：%q", prompt)
			}
			if fail {
				return ark.ImageResult{}, &ark.APIError{Status: 429, Code: "MODEL_LIMITED", Message: "额度受限"}
			}
			return ark.ImageResult{URL: "https://images.example.test/a.png", Model: ark.DefaultImageModel}, nil
		}), nil)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/images/generations", strings.NewReader(`{"prompt":" 列车 "}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if fail && (w.Code != 429 || !strings.Contains(w.Body.String(), "MODEL_LIMITED")) {
			t.Fatal("应返回可识别的模型错误")
		}
		if !fail && (w.Code != 200 || !strings.Contains(w.Body.String(), "https://images.example.test/a.png")) {
			t.Fatal("应返回图片结果")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("生成结果不应被接口缓存")
		}
	}
}

func TestConcurrentGenerationIsRejectedAndSlotReleased(t *testing.T) {
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	router := newHandlerTestRouter(generateFunc(func(ctx context.Context, prompt, model string) (ark.ImageResult, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return ark.ImageResult{URL: "https://images.example.test/a.png"}, nil
	}), nil)
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/images/generations", strings.NewReader(`{"prompt":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		return w
	}
	go func() { defer close(finished); request() }()
	<-started
	second := request()
	close(release)
	<-finished
	if second.Code != 409 || calls.Load() != 1 {
		t.Fatal("并发请求应在调用模型前被拒绝")
	}
	if request().Code != 200 || calls.Load() != 2 {
		t.Fatal("结束后应可发起下一次生成")
	}
}
