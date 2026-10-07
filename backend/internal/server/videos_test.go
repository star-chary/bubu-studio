package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVideoHTTPRejectsMalformedInputBeforeDependencies(t *testing.T) {
	router := newHandlerTestRouter(nil, nil)
	valid := map[string]any{"taskId": "30303030-3030-4030-8030-303030303030", "canvasId": assetCanvas, "nodeId": assetNode, "references": []map[string]string{{"nodeId": assetNode, "assetKey": "not-yet-validated", "kind": "audio"}}}
	for _, field := range []string{"duration", "generateAudio", "prompt", "resolution", "references", "mode", "model", "promptParts"} {
		for _, value := range []any{nil, []string{"invalid"}} {
			t.Run(field, func(t *testing.T) {
				body := map[string]any{}
				for k, v := range valid {
					body[k] = v
				}
				body[field] = value
				payload, _ := json.Marshal(body)
				req := httptest.NewRequest("POST", "/api/videos/generations", strings.NewReader(string(payload)))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code != 400 {
					t.Fatalf("wanted 400 got %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
	for _, body := range []string{"null", "[]", "{} {}", `{"duration":4.5}`, `{"duration":0}`, `{"unknown":true}`} {
		req := httptest.NewRequest("POST", "/api/videos/generations", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	// Empty prompt is valid when a reference exists, and reaches service checking.
	payload, _ := json.Marshal(valid)
	req := httptest.NewRequest("POST", "/api/videos/generations", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
}
