package apimart

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"frame-space/backend/internal/ark"
)

func TestImageModelsValidateProviderParametersAndReferences(t *testing.T) {
	for id, model := range imageModels {
		input := ImageRequest{Model: id, Prompt: "paper boat"}
		if !NormalizeImageRequest(&input, 0) {
			t.Fatalf("default invalid for %s", id)
		}
		input.Parameters["unexpected"] = "secret"
		if NormalizeImageRequest(&input, 0) {
			t.Fatalf("unexpected field accepted for %s", id)
		}
		if NormalizeImageRequest(&ImageRequest{Model: id, Prompt: "paper boat"}, model.MaxReferenceImages+1) {
			t.Fatalf("too many references for %s", id)
		}
	}
	for _, in := range []ImageRequest{
		{Model: "seedream-5-0-lite", Prompt: "boat", Parameters: map[string]any{"size": "auto"}},
		{Model: "midjourney", Prompt: "boat", Parameters: map[string]any{"version": "7", "niji": true, "hd": true}},
		{Model: "flux-3-image", Prompt: "boat", Parameters: map[string]any{"n": 2}},
		{Model: "muse-image-ext", Prompt: "boat", Parameters: map[string]any{"aspect_ratio": "21:9"}},
		{Model: "muse-image-ext", Prompt: "boat", Parameters: map[string]any{"resolution": "1K"}},
		{Model: "muse-image-ext", Prompt: "boat", Parameters: map[string]any{"n": 2}},
		{Model: "gemini-nano-banana-2.1-ext", Prompt: "boat", Parameters: map[string]any{"n": 2}},
		{Model: "gemini-nano-banana-2.1", Prompt: "boat", Parameters: map[string]any{"n": 3}},
		{Model: "seedream-5-5", Prompt: "boat", Parameters: map[string]any{"n": 2}},
		{Model: "seedream-5-5", Prompt: "boat", Parameters: map[string]any{"layer_decomposition": true}},
		{Model: "seedream-5-5", Prompt: "boat", Parameters: map[string]any{"resolution": "4K"}},
	} {
		if NormalizeImageRequest(&in, 0) {
			t.Fatalf("invalid input accepted: %+v", in)
		}
	}
}

func TestSeedream55ImageGenerationUsesDocumentedPayload(t *testing.T) {
	for _, tc := range []struct {
		name, size, resolution, wantSize string
		refs                             bool
	}{
		{"text", "16:9", "2K", "16:9", false},
		{"reference", "9:16", "1.5K", "9:16", true},
		{"automatic-1K", "auto", "1K", "1K", false},
		{"automatic-1.5K", "auto", "1.5K", "1.5K", true},
		{"automatic-2K", "auto", "2K", "2K", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"code":200,"data":{"id":"task_55","status":"completed","result":{"images":[{"url":["https://cdn.example/output.png"]}]}}}`))
					return
				}
				posts++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body["model"] != "seedream-5-5" || body["size"] != tc.wantSize || body["n"] != float64(1) || body["layer_decomposition"] != nil {
					t.Fatalf("wrong generation payload: %+v", body)
				}
				if tc.size == "auto" && body["resolution"] != nil || tc.size != "auto" && body["resolution"] != tc.resolution {
					t.Fatalf("wrong automatic ratio/resolution: %+v", body)
				}
				if tc.refs && fmt.Sprint(body["image_urls"]) != "[https://assets.example/reference.png]" || !tc.refs && body["image_urls"] != nil {
					t.Fatalf("wrong references: %+v", body)
				}
				_, _ = w.Write([]byte(`{"code":200,"data":[{"task_id":"task_55"}]}`))
			}))
			defer server.Close()
			client := newTestClient(t)
			client.baseURL = server.URL
			input := ImageRequest{Model: "seedream-5-5", Prompt: "boat", Parameters: map[string]any{"size": tc.size, "resolution": tc.resolution}}
			if tc.refs {
				input.Images = []string{"https://assets.example/reference.png"}
			}
			id, err := client.CreateImage(context.Background(), input)
			if err != nil || id != "task_55" || posts != 1 {
				t.Fatalf("submit: id=%s posts=%d error=%v", id, posts, err)
			}
			result, err := client.QueryImage(context.Background(), id, input.Model)
			if err != nil || result.Status != "succeeded" || result.URL != "https://cdn.example/output.png" {
				t.Fatalf("query: %+v %v", result, err)
			}
		})
	}
	if NormalizeImageRequest(&ImageRequest{Model: "seedream-5-5", Prompt: "boat"}, 11) {
		t.Fatal("more than ten reference images were accepted")
	}
}

func TestNanoBatchSubmitsOnceAndReadsAllResultURLs(t *testing.T) {
	for _, count := range []int{2, 4} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					calls++
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body["model"] != "gemini-nano-banana-2.1" || body["n"] != float64(count) || body["resolution"] != "2K" {
						t.Errorf("wrong batch payload: %v", body)
					}
					_, _ = w.Write([]byte(`{"code":200,"data":[{"task_id":"task_nano"}]}`))
					return
				}
				urls := []string{"https://cdn.example/1.png", "https://cdn.example/2.png", "https://cdn.example/3.png", "https://cdn.example/4.png"}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"id": "task_nano", "status": "completed", "result": map[string]any{"images": []any{map[string]any{"url": urls[:count]}}}}})
			}))
			defer server.Close()
			client := newTestClient(t)
			client.baseURL = server.URL
			id, err := client.CreateImage(context.Background(), ImageRequest{Prompt: "boat", Model: "gemini-nano-banana-2.1", Parameters: map[string]any{"n": count, "resolution": "2K"}})
			if err != nil || id != "task_nano" {
				t.Fatal(id, err)
			}
			result, err := client.QueryImage(context.Background(), id, "gemini-nano-banana-2.1")
			if err != nil || result.Status != "succeeded" || len(result.URLs) != count || calls != 1 {
				t.Fatalf("batch response: %+v, calls=%d, err=%v", result, calls, err)
			}
		})
	}
}

func TestNanoExtUsesSingleImageModel(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "gemini-nano-banana-2.1-ext" || body["n"] != float64(1) {
			t.Errorf("incorrect single-image payload: %v", body)
		}
		_, _ = w.Write([]byte(`{"code":200,"data":[{"task_id":"task_nano"}]}`))
	}))
	defer server.Close()
	client := newTestClient(t)
	client.baseURL = server.URL
	if _, err := client.CreateImage(context.Background(), ImageRequest{Prompt: "boat", Model: "gemini-nano-banana-2.1-ext"}); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestMuseImageUsesDocumentedPayload(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/tasks/task_muse" {
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":"task_muse","status":"completed","result":{"images":[{"url":["https://cdn.example/muse.webp"]}]}}}`))
			return
		}
		requests++
		key := "muse-task-1"
		if requests == 2 {
			key = "muse-task-2"
		}
		if r.Method != http.MethodPost || r.URL.Path != "/images/generations" || r.Header.Get("Idempotency-Key") != key {
			t.Errorf("incorrect request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "muse-image-ext" || body["prompt"] != "boat" || body["n"] != float64(1) {
			t.Errorf("incorrect Muse payload: %v", body)
		}
		if _, ok := body["resolution"]; ok {
			t.Error("Muse does not accept a resolution")
		}
		if requests == 1 {
			if _, ok := body["aspect_ratio"]; ok {
				t.Error("automatic ratio must be omitted")
			}
			if _, ok := body["image_urls"]; ok {
				t.Error("no reference images should be sent")
			}
		} else {
			urls, ok := body["image_urls"].([]any)
			if body["aspect_ratio"] != "9:16" || !ok || len(urls) != 1 {
				t.Errorf("fixed ratio or reference missing: %v", body)
			}
		}
		_, _ = w.Write([]byte(`{"code":200,"data":[{"task_id":"task_muse"}]}`))
	}))
	defer server.Close()
	client := newTestClient(t)
	client.baseURL = server.URL
	for _, in := range []ImageRequest{
		{TaskID: "muse-task-1", Prompt: "boat", Model: "muse-image-ext"},
		{TaskID: "muse-task-2", Prompt: "boat", Model: "muse-image-ext", Parameters: map[string]any{"aspect_ratio": "9:16"}, Images: []string{"https://private.example/ref"}},
	} {
		if id, err := client.CreateImage(context.Background(), in); err != nil || id != "task_muse" {
			t.Fatalf("submit: %s %v", id, err)
		}
	}
	if requests != 2 {
		t.Fatalf("unexpected supplier calls: %d", requests)
	}
	result, err := client.QueryImage(context.Background(), "task_muse", "muse-image-ext")
	if err != nil || result.Status != "succeeded" || result.URL != "https://cdn.example/muse.webp" {
		t.Fatalf("invalid Muse result: %+v %v", result, err)
	}
}

func TestImageSubmitAndPollingUseDocumentedContracts(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing authorization")
		}
		requests++
		switch r.URL.Path {
		case "/images/generations":
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Error("invalid JSON")
			}
			if body["model"] != "seedream-5-0-lite" || body["size"] != "16:9" || body["resolution"] != "2K" || body["n"] != float64(1) {
				t.Errorf("incorrect payload: %v", body)
			}
			if _, ok := body["image_urls"]; !ok {
				t.Error("missing references")
			}
			_, _ = w.Write([]byte(`{"code":200,"data":[{"task_id":"task_123"}]}`))
		case "/tasks/task_123":
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":"task_123","status":"completed","result":{"images":[{"url":["https://cdn.example/image.png"]}]}}}`))
		case "/midjourney/generations":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, ok := body["model"]; ok || body["version"] != "8.2" {
				t.Errorf("incorrect MJ payload: %v", body)
			}
			_, _ = w.Write([]byte(`{"code":200,"data":[{"task_id":"task_mj"}]}`))
		case "/midjourney/task_mj":
			_, _ = w.Write([]byte(`{"id":"task_mj","status":"SUCCESS","grid_image_url":"https://cdn.example/grid.png","image_urls":["https://cdn.example/1.png"]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client, err := NewClient("test-key", "")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	input := ImageRequest{Prompt: "boat", Model: "seedream-5-0-lite", Parameters: map[string]any{"size": "16:9", "resolution": "2K", "n": 1}, Images: []string{"https://private.example/ref"}}
	id, err := client.CreateImage(context.Background(), input)
	if err != nil || id != "task_123" {
		t.Fatal(id, err)
	}
	result, err := client.QueryImage(context.Background(), id, input.Model)
	if err != nil || result.Status != "succeeded" || result.URL != "https://cdn.example/image.png" {
		t.Fatal(result, err)
	}
	mj, err := client.CreateImage(context.Background(), ImageRequest{Prompt: "boat", Model: "midjourney", Parameters: map[string]any{"version": "8.2"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err = client.QueryImage(context.Background(), mj, "midjourney")
	if err != nil || result.URL != "https://cdn.example/grid.png" {
		t.Fatal(result, err)
	}
	if requests != 4 {
		t.Fatal("unexpected number of supplier calls", requests)
	}
}

func TestImageAmbiguousSubmissionDoesNotRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) }))
	defer server.Close()
	client, err := NewClient("test-key", "")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	_, err = client.CreateImage(context.Background(), ImageRequest{Prompt: "boat", Model: "z-image-turbo"})
	failure, ok := err.(*ark.APIError)
	if !ok || failure.Code != "SUBMISSION_UNKNOWN" || calls != 1 {
		t.Fatal(err, calls)
	}
}
