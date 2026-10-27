package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
)

func TestAPIMartVideoTaskIsOwnedIdempotentAndDoesNotReservePoints(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &creditTestStorage{}, db)
	owner := signIn(t, h, "apimart-video@example.test", "api-video-pass1")
	other := signIn(t, h, "other-apimart-video@example.test", "other-video-pass1")
	canvasID, nodeID, taskID := identity.ID(), identity.ID(), identity.ID()
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID}, nil), 201)
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{{ID: nodeID, Data: persistence.NodeData{Kind: "video", Name: "APIMart", ModelSelection: &persistence.ModelSelection{Provider: "apimart", ModelID: "seedance-2.5", Parameters: map[string]any{}}}}}, Viewport: persistence.Viewport{Zoom: 1}}
	assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)
	body := map[string]any{"taskId": taskID, "canvasId": canvasID, "nodeId": nodeID, "provider": "apimart", "model": "seedance-2.5", "mode": "text", "prompt": "A paper boat moves slowly", "resolution": "1080p", "ratio": "16:9", "duration": 5}
	assertStatus(t, authRequest(h, other, http.MethodPost, "/api/videos/generations", body, nil), 404)
	w := authRequest(h, owner, http.MethodPost, "/api/videos/generations", body, nil)
	assertStatus(t, w, 202)
	var task persistence.Task
	if json.Unmarshal(w.Body.Bytes(), &task) != nil || task.Input.Provider != "apimart" || task.Input.Model != "seedance-2.5" || task.CreditPoints != 0 || task.CreditStatus != "legacy" {
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
	if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM credit_ledger WHERE operation <> 'grant'`).Scan(&reserves); err != nil || reserves != 0 {
		t.Fatal("APIMart touched point ledger", reserves, err)
	}
	// A new selection must not change an accepted task's supplier or idempotency.
	snapshot.Nodes[0].Data.ModelSelection = nil
	assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 1, "snapshot": snapshot}, nil), 200)
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/videos/generations", body, nil), 202)
	body["taskId"] = identity.ID()
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/videos/generations", body, nil), 400)
}
