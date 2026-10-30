package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/storage"
)

func TestAPIMartVideoTaskIsOwnedIdempotentAndReservesPoints(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &creditTestStorage{}, db)
	owner := signIn(t, h, "apimart-video@example.test", "api-video-pass1")
	other := signIn(t, h, "other-apimart-video@example.test", "other-video-pass1")
	canvasID, nodeID, taskID := identity.ID(), identity.ID(), identity.ID()
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID}, nil), 201)
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{{ID: nodeID, Data: persistence.NodeData{Kind: "video", Name: "APIMart", ModelSelection: &persistence.ModelSelection{Provider: "apimart", ModelID: "seedance-2.0", Parameters: map[string]any{}}}}}, Viewport: persistence.Viewport{Zoom: 1}}
	assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)
	body := map[string]any{"taskId": taskID, "canvasId": canvasID, "nodeId": nodeID, "provider": "apimart", "model": "seedance-2.0", "mode": "text", "prompt": "A paper boat moves slowly", "resolution": "720p", "ratio": "16:9", "duration": 5}
	assertStatus(t, authRequest(h, other, http.MethodPost, "/api/videos/generations", body, nil), 404)
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/videos/generations", body, nil), 409)
	quote := quoteAPIMart(t, h, owner, "video", body)
	body["acceptedPoints"], body["priceVersion"] = quote.Points, quote.PriceVersion
	w := authRequest(h, owner, http.MethodPost, "/api/videos/generations", body, nil)
	assertStatus(t, w, 202)
	var task persistence.Task
	if json.Unmarshal(w.Body.Bytes(), &task) != nil || task.Input.Provider != "apimart" || task.Input.Model != "seedance-2.0" || task.CreditPoints != quote.Points || task.CreditStatus != "reserved" {
		t.Fatal("invalid APIMart task snapshot", w.Body.String())
	}
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/videos/generations", body, nil), 202)
	changed := map[string]any{}
	for key, value := range body {
		changed[key] = value
	}
	changed["duration"] = 6
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/videos/generations", changed, nil), 409)
	var taskCount, reserves int
	if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM generation_tasks`).Scan(&taskCount); err != nil || taskCount != 1 {
		t.Fatal(taskCount, err)
	}
	if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM credit_ledger WHERE operation = 'reserve'`).Scan(&reserves); err != nil || reserves != 1 {
		t.Fatal("unexpected APIMart reservation count", reserves, err)
	}
	// A new selection must not change an accepted task's supplier or idempotency.
	snapshot.Nodes[0].Data.ModelSelection = nil
	assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 1, "snapshot": snapshot}, nil), 200)
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/videos/generations", body, nil), 202)
	body["taskId"] = identity.ID()
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/videos/generations", body, nil), 400)
}

func TestMotionControlHTTPQuoteAndSubmitWithoutDuration(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &creditTestStorage{}, db) // No worker or provider call.
	owner := signIn(t, h, "motion-video@example.test", "motion-pass1")
	canvasID, nodeID, imageID, videoID, taskID := identity.ID(), identity.ID(), identity.ID(), identity.ID(), identity.ID()
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID}, nil), 201)
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{
		{ID: nodeID, Data: persistence.NodeData{Kind: "video", Name: "motion", ModelSelection: &persistence.ModelSelection{Provider: "apimart", ModelID: "kling-v2-6-motion-control", Parameters: map[string]any{}}}},
		{ID: imageID, Data: persistence.NodeData{Kind: "image", Name: "person", Origin: "upload"}},
		{ID: videoID, Data: persistence.NodeData{Kind: "video", Name: "motion", Origin: "upload"}},
	}, Viewport: persistence.Viewport{Zoom: 1}}
	assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)
	for _, asset := range []storage.Asset{
		{Key: "person", Kind: storage.Image, ContentType: "image/png", Bytes: 1000, Scope: storage.Scope{CanvasID: canvasID, NodeID: imageID}, Media: &storage.MediaMetadata{Width: 960, Height: 540}},
		{Key: "motion", Kind: storage.Video, ContentType: "video/mp4", Bytes: 1000, Scope: storage.Scope{CanvasID: canvasID, NodeID: videoID}, Media: &storage.MediaMetadata{Width: 854, Height: 480, VideoCodec: "h264", FrameRate: 24, DurationSeconds: 20}},
	} {
		if err := db.RecordAsset(t.Context(), asset); err != nil {
			t.Fatal(err)
		}
	}
	body := map[string]any{
		"taskId": taskID, "canvasId": canvasID, "nodeId": nodeID, "provider": "apimart",
		"model": "kling-v2-6-motion-control", "mode": "reference", "prompt": "跟随动作",
		"parameters": map[string]any{"mode": "std"},
		"references": []persistence.Reference{{NodeID: imageID, AssetKey: "person", Kind: "image"}, {NodeID: videoID, AssetKey: "motion", Kind: "video"}},
	}
	quote := quoteAPIMart(t, h, owner, "video", body)
	if quote.Points != 198 {
		t.Fatalf("20-second motion quote = %d, want 198", quote.Points)
	}
	body["acceptedPoints"], body["priceVersion"] = quote.Points, quote.PriceVersion
	w := authRequest(h, owner, http.MethodPost, "/api/videos/generations", body, nil)
	assertStatus(t, w, 202)
	var task persistence.Task
	if json.Unmarshal(w.Body.Bytes(), &task) != nil || task.Input.Duration != 0 || task.Input.Resolution != "std" || task.CreditStatus != "reserved" || task.CreditPoints != 198 {
		t.Fatalf("unexpected motion task: %s", w.Body.String())
	}
}

func TestAPIMartUnpricedVideoAndAutomaticDurationAreRejected(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &creditTestStorage{}, db)
	owner := signIn(t, h, "unpriced-video@example.test", "api-video-pass1")
	canvasID, nodeID := identity.ID(), identity.ID()
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID}, nil), 201)
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{{ID: nodeID, Data: persistence.NodeData{Kind: "video", Name: "APIMart", ModelSelection: &persistence.ModelSelection{Provider: "apimart", ModelID: "seedance-2.5", Parameters: map[string]any{}}}}}, Viewport: persistence.Viewport{Zoom: 1}}
	assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)
	body := map[string]any{"kind": "video", "canvasId": canvasID, "nodeId": nodeID, "provider": "apimart", "model": "seedance-2.5", "mode": "text", "prompt": "A paper boat", "resolution": "720p", "ratio": "16:9", "duration": 5}
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/credits/quote", body, nil), 400)
	delete(body, "kind")
	body["taskId"], body["acceptedPoints"], body["priceVersion"] = identity.ID(), 124, "apimart-2026-10-28-v1"
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/videos/generations", body, nil), 400)

	snapshot.Nodes[0].Data.ModelSelection.ModelID = "wan3.0-video"
	assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 1, "snapshot": snapshot}, nil), 200)
	body["kind"], body["model"] = "video", "wan3.0-video"
	body["parameters"] = map[string]any{"size": "16:9", "resolution": "720P", "duration": -1, "audio": true}
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/credits/quote", body, nil), 400)
	var count int
	if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM generation_tasks`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unpriced request created tasks: %d %v", count, err)
	}
}
