package ark

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGenerateRequestContract(t *testing.T) {
	for _, model := range []string{DefaultImageModel, ProImageModel} {
		t.Run(model, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-secret" || r.Header.Get("Content-Type") != "application/json" {
					t.Error("请求方法或鉴权头不符")
				}
				var input map[string]any
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
				}
				if input["prompt"] != "黑洞中的复古列车" || input["model"] != model || input["size"] != "2K" || input["watermark"] != true || input["response_format"] != "url" {
					t.Errorf("请求参数不符：%v", input)
				}
				if model == DefaultImageModel {
					if len(input) != 7 || input["stream"] != false || input["sequential_image_generation"] != "disabled" {
						t.Errorf("Lite 应显式关闭组图和流式：%v", input)
					}
				} else if len(input) != 5 || input["stream"] != nil || input["sequential_image_generation"] != nil {
					t.Errorf("Pro 不应携带不支持的参数：%v", input)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"url":"https://images.example.test/result.png","size":"2048x2048"}]}`))
			}))
			defer upstream.Close()
			client := NewClient("test-secret")
			client.endpoint = upstream.URL
			result, err := client.Generate(context.Background(), "黑洞中的复古列车", model)
			if err != nil || result.URL != "https://images.example.test/result.png" || result.Model != model || result.Size != "2048x2048" || calls.Load() != 1 {
				t.Fatalf("生成结果不符：%+v, %v", result, err)
			}
		})
	}
}

func TestReferencesForwardedInOrderWithSingleImageOutput(t *testing.T) {
	for _, model := range []string{DefaultImageModel, ProImageModel} {
		for _, count := range []int{1, 2} {
			images := []string{"https://oss.example.test/first.png?signature=a", "https://oss.example.test/second.png?signature=b"}[:count]
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input map[string]any
				_ = json.NewDecoder(r.Body).Decode(&input)
				got, ok := input["image"].([]any)
				if !ok || len(got) != count {
					t.Error("missing reference images")
					return
				}
				for i, reference := range images {
					if got[i] != reference {
						t.Error("reference order changed")
					}
				}
				if model == DefaultImageModel && input["sequential_image_generation"] != "disabled" {
					t.Error("Lite must generate a single image")
				}
				if model == ProImageModel && (input["sequential_image_generation"] != nil || input["stream"] != nil) {
					t.Error("Pro does not support batch controls")
				}
				_, _ = w.Write([]byte(`{"data":[{"url":"https://images.example.test/result.png"}]}`))
			}))
			client := NewClient("test-secret")
			client.endpoint = upstream.URL
			_, err := client.Generate(context.Background(), "change the sky", model, images...)
			upstream.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestRejectUnexpectedMultipleOutputs(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"url":"https://images.example.test/a.png"},{"url":"https://images.example.test/b.png"}]}`))
	}))
	defer upstream.Close()
	client := NewClient("test-secret")
	client.endpoint = upstream.URL
	_, err := client.Generate(context.Background(), "one image", DefaultImageModel)
	var failure *APIError
	if !errors.As(err, &failure) || failure.Code != "INVALID_MODEL_RESULT" {
		t.Fatal("single-image mode must not silently discard extra provider outputs")
	}
}

func TestProviderErrorsAreSanitized(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{401, "MODEL_AUTH_FAILED"}, {403, "MODEL_AUTH_FAILED"}, {404, "MODEL_NOT_AVAILABLE"}, {429, "MODEL_LIMITED"}, {400, "GENERATION_REJECTED"}, {500, "MODEL_FAILED"}} {
		t.Run(tc.code+http.StatusText(tc.status), func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"message":"test-secret provider-internal-details"}}`))
			}))
			defer upstream.Close()
			client := NewClient("test-secret")
			client.endpoint = upstream.URL
			_, err := client.Generate(context.Background(), "test", DefaultImageModel)
			var failure *APIError
			if !errors.As(err, &failure) || failure.Code != tc.code || strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "provider-internal") || calls.Load() != 1 {
				t.Fatalf("错误映射或脱敏不符：%v", err)
			}
		})
	}
}

func TestRejectInvalidResultsAndMissingKey(t *testing.T) {
	for _, body := range []string{`broken`, `{"data":[]}`, `{"data":[{"error":{"code":"failed"}}]}`, `{"data":[{"url":"javascript:alert(1)"}]}`, `{"data":[{"url":"https://user:password@host.test/image.png"}]}`} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		client := NewClient("test-secret")
		client.endpoint = upstream.URL
		_, err := client.Generate(context.Background(), "test", DefaultImageModel)
		upstream.Close()
		var failure *APIError
		if !errors.As(err, &failure) || failure.Code != "INVALID_MODEL_RESULT" {
			t.Fatalf("应拒绝无效结果：%v", err)
		}
	}
	_, err := NewClient("").Generate(context.Background(), "test", DefaultImageModel)
	var failure *APIError
	if !errors.As(err, &failure) || failure.Status != 503 {
		t.Fatalf("缺少密钥时应明确报错：%v", err)
	}
}

func TestTimeoutDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer upstream.Close()
	client := NewClient("test-secret")
	client.endpoint = upstream.URL
	client.http.Timeout = 100 * time.Millisecond
	_, err := client.Generate(context.Background(), "test", DefaultImageModel)
	var failure *APIError
	if !errors.As(err, &failure) || failure.Status != 504 || calls.Load() > 1 {
		t.Fatalf("应超时且不重试：%v", err)
	}
}

func TestDoesNotFollowRedirect(t *testing.T) {
	var redirectedCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirectedCalls.Add(1) }))
	defer destination.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()
	client := NewClient("test-secret")
	client.endpoint = upstream.URL
	_, err := client.Generate(context.Background(), "test", DefaultImageModel)
	if err == nil || redirectedCalls.Load() != 0 {
		t.Fatal("不能向重定向地址转发鉴权请求")
	}
}
