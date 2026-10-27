package apimart

import (
	"context"
	"encoding/json"
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
	} {
		if NormalizeImageRequest(&in, 0) {
			t.Fatalf("invalid input accepted: %+v", in)
		}
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
	client := &Client{apiKey: "test-key", baseURL: server.URL, http: server.Client()}
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
	client := &Client{apiKey: "test-key", baseURL: server.URL, http: server.Client()}
	_, err := client.CreateImage(context.Background(), ImageRequest{Prompt: "boat", Model: "z-image-turbo"})
	failure, ok := err.(*ark.APIError)
	if !ok || failure.Code != "SUBMISSION_UNKNOWN" || calls != 1 {
		t.Fatal(err, calls)
	}
}
