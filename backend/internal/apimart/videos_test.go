package apimart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"frame-space/backend/internal/ark"
)

func validInput() ark.VideoRequest {
	return ark.VideoRequest{TaskID: "local-task", Model: VideoModel, Prompt: "A paper boat moves slowly", Mode: "text", Resolution: "720p", Ratio: "16:9", Duration: 5, GenerateAudio: true}
}

func TestMuseVideoRequestUsesDocumentedDefaultsAndIdempotencyKey(t *testing.T) {
	for _, mode := range []string{"text", "reference"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Idempotency-Key") != "muse-task" {
					t.Error("missing task idempotency key")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body["model"] != "muse-video-ext" || body["n"] != float64(1) || body["duration"] != float64(10) || body["resolution"] != "720p" || body["aspect_ratio"] != nil || body["audio"] != nil {
					t.Fatalf("unexpected Muse payload: %+v", body)
				}
				if mode == "reference" && fmt.Sprint(body["image_urls"]) != "[https://assets.example.test/ref.png]" {
					t.Fatal("reference image not sent", body)
				}
				if mode == "text" && body["image_urls"] != nil {
					t.Fatal("text request sent references", body)
				}
				fmt.Fprint(w, `{"code":200,"data":[{"task_id":"muse_provider_task"}]}`)
			}))
			defer srv.Close()
			client := newTestClient(t)
			client.baseURL = srv.URL
			in := ark.VideoRequest{TaskID: "muse-task", Model: "muse-video-ext", Mode: mode, Prompt: "A paper boat", Parameters: map[string]any{}}
			if mode == "reference" {
				in.References = []ark.VideoReference{{Kind: "image", URL: "https://assets.example.test/ref.png"}}
			}
			id, err := client.CreateVideo(context.Background(), in)
			if err != nil || id != "muse_provider_task" || calls != 1 {
				t.Fatal(id, err, calls)
			}
		})
	}
	for _, parameters := range []map[string]any{{"audio": false}, {"duration": 5}, {"resolution": "1080p"}, {"n": 2}} {
		if _, ok := videoPayload(ark.VideoRequest{Model: "muse-video-ext", Mode: "text", Prompt: "scene", Parameters: parameters}); ok {
			t.Fatalf("invalid Muse parameters accepted: %+v", parameters)
		}
	}
	in := ark.VideoRequest{Model: "muse-video-ext", Mode: "text", Prompt: "scene", Parameters: map[string]any{"aspect_ratio": "9:16"}}
	payload, ok := videoPayload(in)
	if !ok || payload["aspect_ratio"] != "9:16" {
		t.Fatalf("explicit Muse ratio was not sent: %+v", payload)
	}
	refs := make([]ark.VideoReference, 10)
	for i := range refs {
		refs[i] = ark.VideoReference{Kind: "image", URL: fmt.Sprintf("https://assets.example.test/%d.png", i)}
	}
	in.Mode, in.References = "reference", refs[:9]
	if _, ok := videoPayload(in); !ok {
		t.Fatal("nine references should be accepted")
	}
	in.References = refs
	if _, ok := videoPayload(in); ok {
		t.Fatal("more than nine references should be rejected")
	}
}

func TestVideoSubmissionAndResultUseAPIMartContract(t *testing.T) {
	for _, mode := range []string{"text", "reference"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Error("missing authentication")
				}
				if r.Method == http.MethodGet {
					if r.URL.Path != "/tasks/task_test" || r.URL.Query().Get("language") != "zh" {
						t.Error("unexpected query endpoint")
					}
					fmt.Fprint(w, `{"code":200,"data":{"id":"task_test","status":"completed","result":{"videos":[{"url":["https://cdn.example.test/video.mp4"]}]}}}`)
					return
				}
				if r.URL.Path != "/videos/generations" {
					t.Error("unexpected create endpoint")
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil || body["model"] != VideoModel || body["prompt"] != "A paper boat moves slowly" || body["size"] != "16:9" || body["output_format"] != "mp4" || body["generate_audio"] != true {
					t.Error("invalid APIMart wire format")
				}
				if body["content"] != nil || body["ratio"] != nil || body["first_frame_image"] != nil || body["last_frame_image"] != nil {
					t.Error("Ark or frame-specific fields leaked into ordinary generation")
				}
				if mode == "reference" {
					if fmt.Sprint(body["image_urls"]) != "[https://assets.example.test/ref.png]" || body["omni_reference_task_type"] != "reference" {
						t.Error("reference semantics not preserved")
					}
				} else if body["image_urls"] != nil || body["omni_reference_task_type"] != nil {
					t.Error("text mode submitted reference fields")
				}
				fmt.Fprint(w, `{"code":200,"data":[{"task_id":"task_test","status":"submitted"}]}`)
			}))
			defer srv.Close()
			client := newTestClient(t)
			client.baseURL = srv.URL
			in := validInput()
			in.Mode = mode
			if mode == "reference" {
				in.References = []ark.VideoReference{{Kind: "image", URL: "https://assets.example.test/ref.png"}}
			}
			id, err := client.CreateVideo(context.Background(), in)
			if err != nil || id != "task_test" {
				t.Fatal(id, err)
			}
			out, err := client.QueryVideo(context.Background(), id)
			if err != nil || out.Status != "succeeded" || out.Content.URL != "https://cdn.example.test/video.mp4" || calls != 2 {
				t.Fatal(out, err, calls)
			}
		})
	}
}

func TestAmbiguousVideoSubmissionIsNeverRetried(t *testing.T) {
	for _, status := range []int{200, 408, 499, 500, 502} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				fmt.Fprint(w, `{"code":200,"data":[]}`)
			}))
			defer srv.Close()
			client := newTestClient(t)
			client.baseURL = srv.URL
			_, err := client.CreateVideo(context.Background(), validInput())
			var failure *ark.APIError
			if !errors.As(err, &failure) || failure.Code != "SUBMISSION_UNKNOWN" || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
}

func TestVideoStatesAndUnsafeResults(t *testing.T) {
	for _, tc := range []struct{ status, resultURL, want string }{
		{"pending", "", "queued"}, {"processing", "", "running"}, {"failed", "", "failed"}, {"cancelled", "", "cancelled"},
		{"completed", `"https://cdn.example.test/out.mp4"`, "succeeded"},
		{"completed", `["http://localhost/private"]`, ""}, {"completed", `["https://user:password@example.test/out"]`, ""},
		{"completed", `[]`, ""}, {"unknown", `[]`, ""},
	} {
		t.Run(tc.status+tc.resultURL, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				result := "{}"
				if tc.resultURL != "" {
					result = `{"videos":[{"url":` + tc.resultURL + `}]}`
				}
				fmt.Fprintf(w, `{"code":200,"data":{"id":"task_test","status":%q,"error":{"message":"private signed url https://secret.test/a?token=private"},"result":%s}}`, tc.status, result)
			}))
			defer srv.Close()
			client := newTestClient(t)
			client.baseURL = srv.URL
			out, err := client.QueryVideo(context.Background(), "task_test")
			if tc.want == "" {
				var poll *ark.PollError
				if !errors.As(err, &poll) {
					t.Fatal("unsafe or unknown result accepted", out, err)
				}
			} else if err != nil || out.Status != tc.want {
				t.Fatal(out, err)
			}
			if out.Failure != nil && strings.Contains(out.Failure.Message, "secret.test") {
				t.Fatal("provider details leaked")
			}
		})
	}
}

func TestUnsupportedVideoInputsDoNotReachProvider(t *testing.T) {
	for _, change := range []func(*ark.VideoRequest){
		func(in *ark.VideoRequest) { in.Model = "kling-v3-motion-control" },
		func(in *ark.VideoRequest) { in.Prompt = "" },
		func(in *ark.VideoRequest) { in.Duration = 31 },
		func(in *ark.VideoRequest) { in.Resolution = "4k" },
		func(in *ark.VideoRequest) { in.Mode = "reference" },
		func(in *ark.VideoRequest) {
			in.Mode = "reference"
			in.References = []ark.VideoReference{{Kind: "audio", URL: "https://assets.example.test/ref.wav"}}
		},
	} {
		in := validInput()
		change(&in)
		client := newTestClient(t)
		client.baseURL = "http://127.0.0.1:1"
		_, err := client.CreateVideo(context.Background(), in)
		var failure *ark.APIError
		if !errors.As(err, &failure) || failure.Code != "INVALID_VIDEO_INPUT" {
			t.Fatal("invalid input reached transport", err)
		}
	}
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient("test-secret", "")
	if err != nil {
		t.Fatal(err)
	}
	return client
}
