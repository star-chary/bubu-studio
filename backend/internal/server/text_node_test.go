package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
)

func TestTextNodeHTTPRoundTripAndOldClientProtection(t *testing.T) {
	db := authDB(t)
	router := newHandlerTestRouter(nil, nil, db)
	canvasID, textID, targetID := identity.ID(), identity.ID(), identity.ID()
	if err := db.CreateCanvas(context.Background(), canvasID, "文本画布", handlerUserID); err != nil {
		t.Fatal(err)
	}
	request := func(method string, body any) *httptest.ResponseRecorder {
		t.Helper()
		payload, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/canvases/"+canvasID, bytes.NewReader(payload))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	snapshot := persistence.Snapshot{FormatVersion: 1, Viewport: persistence.Viewport{Zoom: 1}, Nodes: []persistence.Node{
		{ID: textID, Data: persistence.NodeData{Kind: "text", Name: "场景", Text: "雪山"}},
		{ID: targetID, Data: persistence.NodeData{Kind: "image", Name: "图片", TextReferenceIDs: []string{textID}}},
	}}
	if w := request(http.MethodPut, map[string]any{"version": 0, "snapshot": snapshot}); w.Code != http.StatusOK {
		t.Fatalf("text save failed: %d %s", w.Code, w.Body.String())
	}
	var canvas persistence.Canvas
	w := request(http.MethodGet, nil)
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &canvas) != nil || canvas.Snapshot.Nodes[0].Data.Text != "雪山" || len(canvas.Snapshot.Nodes[1].Data.TextReferenceIDs) != 1 {
		t.Fatalf("text round trip failed: %d %s", w.Code, w.Body.String())
	}
	legacy := persistence.Snapshot{Viewport: persistence.Viewport{Zoom: 1}, Nodes: []persistence.Node{}}
	if w = request(http.MethodPut, map[string]any{"version": canvas.Version, "snapshot": legacy}); w.Code != http.StatusConflict {
		t.Fatalf("old client was not rejected: %d %s", w.Code, w.Body.String())
	}
	w = request(http.MethodGet, nil)
	if json.Unmarshal(w.Body.Bytes(), &canvas) != nil || len(canvas.Snapshot.Nodes) != 2 || canvas.Snapshot.Nodes[0].Data.Text != "雪山" {
		t.Fatal("old client changed saved text")
	}
}
