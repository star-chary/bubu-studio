package ark

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVideoWireFormatAndPolling(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing authorization")
		}
		if r.Method == "GET" {
			w.Write([]byte(`{"id":"cgt-test","status":"succeeded","content":{"video_url":"https://example.test/result.mp4"},"framespersecond":24,"duration":4}`))
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["generate_audio"] != false || body["duration"] != float64(4) || body["omni_reference_task_type"] != "reference" || body["output_format"] != "mp4" || body["execution_expires_after"] != float64(3600) {
			t.Errorf("wrong parameters: %v", body)
		}
		content := body["content"].([]any)
		if len(content) != 4 || content[0].(map[string]any)["text"] != "图片1的视频风格参考视频1" {
			t.Error("wrong prompt")
		}
		for i, kind := range []string{"image", "video", "audio"} {
			item := content[i+1].(map[string]any)
			if item["type"] != kind+"_url" || item["role"] != "reference_"+kind || item[kind+"_url"].(map[string]any)["url"] != "https://example.test/"+kind {
				t.Error("wrong reference order")
			}
		}
		w.Write([]byte(`{"id":"cgt-test"}`))
	}))
	defer srv.Close()
	c := NewClient("test")
	c.videoEndpoint = srv.URL
	id, err := c.CreateVideo(context.Background(), VideoRequest{Model: VideoModel, Prompt: "图片1的视频风格参考视频1", Duration: 4, Resolution: "480p", Ratio: "16:9", References: []VideoReference{{"image", "https://example.test/image"}, {"video", "https://example.test/video"}, {"audio", "https://example.test/audio"}}})
	if err != nil || id != "cgt-test" {
		t.Fatal(id, err)
	}
	out, err := c.QueryVideo(context.Background(), id)
	if err != nil || out.Status != "succeeded" || out.FramesPerSecond != 24 || calls != 2 {
		t.Fatal(out, err, calls)
	}
}
func TestVideoCreateNeverRetriesAmbiguousResponses(t *testing.T) {
	for _, status := range []int{200, 302, 408, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				w.Write([]byte(`{"secret":"must-not-leak"}`))
			}))
			defer srv.Close()
			c := NewClient("test")
			c.videoEndpoint = srv.URL
			_, err := c.CreateVideo(context.Background(), VideoRequest{Model: VideoModel, References: []VideoReference{{"audio", "https://example.test/a.wav"}}})
			if err == nil || err.(*APIError).Code != "SUBMISSION_UNKNOWN" || calls != 1 || strings.Contains(err.Error(), "must-not-leak") {
				t.Fatal(err, calls)
			}
		})
	}
}
func TestAudioOnlyOmitsEmptyTextAndPollFailureHonorsRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Header().Set("Retry-After", "42")
			w.WriteHeader(429)
			return
		}
		var body struct {
			Content []map[string]any `json:"content"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.Content) != 1 || body.Content[0]["type"] != "audio_url" {
			t.Error("empty text sent")
		}
		w.Write([]byte(`{"id":"a"}`))
	}))
	defer srv.Close()
	c := NewClient("test")
	c.videoEndpoint = srv.URL
	if _, err := c.CreateVideo(context.Background(), VideoRequest{Model: VideoModel, References: []VideoReference{{"audio", "https://example.test/a.wav"}}}); err != nil {
		t.Fatal(err)
	}
	_, err := c.QueryVideo(context.Background(), "a")
	if err == nil || err.(*PollError).RetryAfter.Seconds() != 42 {
		t.Fatal(err)
	}
}

func TestVideoModelAndModeWireFormat(t *testing.T) {
	for _, tc := range []struct {
		name, model, mode string
		withReference     bool
		wantOmni          bool
		wantOutputFormat  bool
	}{
		{"legacy reference", "", "", true, true, true},
		{"2.5 text", VideoModel, "text", false, false, true},
		{"2.5 reference", VideoModel, "reference", true, true, true},
		{"2.0 text", VideoModel20, "text", false, false, false},
		{"2.0 reference", VideoModel20, "reference", true, false, false},
		{"2.0 fast text", VideoModel20Fast, "text", false, false, false},
		{"2.0 mini reference", VideoModel20Mini, "reference", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				wantModel := tc.model
				if wantModel == "" {
					wantModel = VideoModel
				}
				if payload["model"] != wantModel {
					t.Errorf("wrong model: %v", payload["model"])
				}
				_, hasOmni := payload["omni_reference_task_type"]
				_, hasOutputFormat := payload["output_format"]
				if hasOmni != tc.wantOmni || hasOutputFormat != tc.wantOutputFormat {
					t.Errorf("unsupported provider fields: %v", payload)
				}
				content := payload["content"].([]any)
				wantLen := 1
				if tc.withReference {
					wantLen++
				}
				if len(content) != wantLen || content[0].(map[string]any)["text"] != "A moving scene" {
					t.Errorf("wrong content: %v", content)
				}
				if tc.withReference && content[1].(map[string]any)["role"] != "reference_image" {
					t.Errorf("wrong reference role: %v", content[1])
				}
				w.Write([]byte(`{"id":"cgt-test"}`))
			}))
			defer srv.Close()
			client := NewClient("test")
			client.videoEndpoint = srv.URL
			in := VideoRequest{Model: tc.model, Mode: tc.mode, Prompt: "A moving scene", Resolution: "480p", Ratio: "16:9", Duration: 5, GenerateAudio: true}
			if tc.withReference {
				in.References = []VideoReference{{Kind: "image", URL: "https://example.test/image.png"}}
			}
			if _, err := client.CreateVideo(context.Background(), in); err != nil || requests != 1 {
				t.Fatalf("provider request: %v, requests=%d", err, requests)
			}
		})
	}
}

func TestTextVideoRejectsReferencesBeforeProviderCall(t *testing.T) {
	client := NewClient("test")
	for _, in := range []VideoRequest{
		{Model: VideoModel20, Mode: "text", Prompt: "  "},
		{Model: VideoModel20, Mode: "text", Prompt: "scene", References: []VideoReference{{Kind: "image", URL: "https://example.test/image.png"}}},
		{Model: VideoModel20, Mode: "reference", Prompt: "scene"},
		{Model: "unknown", Mode: "text", Prompt: "scene"},
	} {
		if _, err := client.CreateVideo(context.Background(), in); err == nil || err.(*APIError).Code != "INVALID_VIDEO_INPUT" {
			t.Fatalf("invalid request accepted: %+v %v", in, err)
		}
	}
}
