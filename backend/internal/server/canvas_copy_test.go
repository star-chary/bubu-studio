package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/storage"
)

type copyHandlerAssets struct {
	stubAssets
	calls int
}

func (s *copyHandlerAssets) CopyCanvasAsset(_ context.Context, source storage.Asset, target storage.Scope) (storage.Asset, error) {
	s.calls++
	source.CopiedFrom = source.Key
	source.Key = "copy-" + target.NodeID
	source.URL = "/api/assets/content?key=" + source.Key
	source.Scope = target
	return source, nil
}

func TestCanvasCopyHTTPPersistsAndRejectsForeignSource(t *testing.T) {
	db := authDB(t)
	ctx := context.Background()
	store := &copyHandlerAssets{}
	router := newHandlerTestRouter(nil, store, db)
	const target = "30303030-3030-4030-8030-303030303030"
	const otherCanvas = "40404040-4040-4040-8040-404040404040"
	if err := db.CreateCanvas(ctx, assetCanvas, "copy test", handlerUserID); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateCanvas(ctx, otherCanvas, "other", handlerUserID); err != nil {
		t.Fatal(err)
	}
	source := storage.Asset{Key: "source-key", URL: "/api/assets/content?key=source-key", Scope: storage.Scope{CanvasID: assetCanvas, NodeID: assetNode}, Kind: storage.Image, Source: storage.Upload, ContentType: "image/png", Bytes: 2}
	if err := db.RecordAsset(ctx, source); err != nil {
		t.Fatal(err)
	}
	snapshot := persistence.Snapshot{Viewport: persistence.Viewport{Zoom: 1}, Nodes: []persistence.Node{
		{ID: target, Data: persistence.NodeData{Kind: "image", Name: "copy", Origin: "upload", CopySourceKey: source.Key}},
	}}
	if _, err := db.SaveCanvas(ctx, assetCanvas, 0, snapshot); err != nil {
		t.Fatal(err)
	}
	call := func(canvas string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/canvases/"+canvas+"/nodes/"+target+"/asset-copy", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	if w := call(assetCanvas); w.Code != http.StatusCreated {
		t.Fatalf("copy status %d: %s", w.Code, w.Body.String())
	}
	if w := call(assetCanvas); w.Code != http.StatusCreated {
		t.Fatalf("idempotent retry status %d", w.Code)
	}
	canvas, err := db.Canvas(ctx, assetCanvas)
	if err != nil || len(canvas.Assets) != 2 {
		t.Fatalf("copied asset not restored once: %+v %v", canvas.Assets, err)
	}
	if _, err = db.SaveCanvas(ctx, otherCanvas, 0, snapshot); err != nil {
		t.Fatal(err)
	}
	if w := call(otherCanvas); w.Code != http.StatusNotFound {
		t.Fatalf("foreign source status %d", w.Code)
	}
	if store.calls != 1 {
		t.Fatalf("foreign copy reached storage: %d calls", store.calls)
	}
}
