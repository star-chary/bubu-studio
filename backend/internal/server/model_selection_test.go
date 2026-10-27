package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
)

// 绕过前端直接调用旧生成接口：服务端必须结合已保存节点拒绝 APIMart 草稿，
// 返回稳定错误码 MODEL_GENERATION_NOT_ENABLED，且不创建任务、不冻结积分。
func TestApimartNodeHTTPGenerationRejected(t *testing.T) {
	db := authDB(t)
	media := &creditTestStorage{}
	h := NewRouter(nil, media, db)
	client := signIn(t, h, "apimart-gate@example.test", "gate test password")
	canvasID, imageNodeID, videoNodeID := identity.ID(), identity.ID(), identity.ID()
	assertStatus(t, authRequest(h, client, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID, "title": "APIMart gate"}, nil), 201)
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{
		{ID: imageNodeID, Data: persistence.NodeData{Kind: "image", Name: "Image", Origin: "generated",
			ModelSelection: &persistence.ModelSelection{Provider: persistence.ProviderAPIMart, ModelID: "gpt-image-1", Parameters: map[string]any{"size": "1024x1024"}}}},
		{ID: videoNodeID, Data: persistence.NodeData{Kind: "video", Name: "Video", Origin: "generated",
			ModelSelection: &persistence.ModelSelection{Provider: persistence.ProviderAPIMart, ModelID: "seedance-2-5", Parameters: map[string]any{"duration": 5}}}},
	}, Viewport: persistence.Viewport{Zoom: 1}}
	assertStatus(t, authRequest(h, client, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)

	quote := func(kind string, body map[string]any) {
		t.Helper()
		w := authRequest(h, client, http.MethodPost, "/api/credits/quote", body, nil)
		assertStatus(t, w, 400)
		var response struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error.Code != "MODEL_GENERATION_NOT_ENABLED" {
			t.Fatalf("quote must be rejected with MODEL_GENERATION_NOT_ENABLED: %s, %v", w.Body.String(), err)
		}
	}
	quote("image", map[string]any{"kind": "image", "canvasId": canvasID, "nodeId": imageNodeID, "prompt": "A picture", "model": "gpt-image-1"})
	quote("video", map[string]any{"kind": "video", "canvasId": canvasID, "nodeId": videoNodeID, "prompt": "A scene", "model": "seedance-2-5", "mode": "text", "resolution": "480p", "ratio": "16:9", "duration": 5})

	imageTask, videoTask := identity.ID(), identity.ID()
	for _, request := range []struct {
		path string
		body map[string]any
	}{
		{"/api/images/generations", map[string]any{"taskId": imageTask, "canvasId": canvasID, "nodeId": imageNodeID, "prompt": "A picture", "model": "gpt-image-1", "acceptedPoints": 6, "priceVersion": "test"}},
		{"/api/videos/generations", map[string]any{"taskId": videoTask, "canvasId": canvasID, "nodeId": videoNodeID, "prompt": "A scene", "model": "seedance-2-5", "mode": "text", "resolution": "480p", "ratio": "16:9", "duration": 5, "acceptedPoints": 6, "priceVersion": "test"}},
	} {
		w := authRequest(h, client, http.MethodPost, request.path, request.body, nil)
		assertStatus(t, w, 400)
		var response struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error.Code != "MODEL_GENERATION_NOT_ENABLED" {
			t.Fatalf("generation must be rejected: %s, %v", w.Body.String(), err)
		}
	}
	var taskCount int
	if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM generation_tasks`).Scan(&taskCount); err != nil || taskCount != 0 {
		t.Fatalf("apimart requests created %d tasks: %v", taskCount, err)
	}
	var reserveCount int
	if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM credit_ledger WHERE operation='reserve'`).Scan(&reserveCount); err != nil || reserveCount != 0 {
		t.Fatalf("apimart requests froze credits: %d, %v", reserveCount, err)
	}
	if media.videoCalls.Load() != 0 {
		t.Fatal("apimart request reached video storage")
	}
}

// 同名模型 ID 不得因与 Ark 相同而按 Ark 处理：节点草稿决定供应商。
func TestApimartSelectionWinsOverArkModelID(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &creditTestStorage{}, db)
	client := signIn(t, h, "apimart-modelid@example.test", "model id password")
	canvasID, nodeID := identity.ID(), identity.ID()
	assertStatus(t, authRequest(h, client, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID, "title": "Model ID"}, nil), 201)
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{
		{ID: nodeID, Data: persistence.NodeData{Kind: "image", Name: "Image", Origin: "generated",
			ModelSelection: &persistence.ModelSelection{Provider: persistence.ProviderAPIMart, ModelID: "doubao-seedream-5-0-260128", Parameters: map[string]any{}}}},
	}, Viewport: persistence.Viewport{Zoom: 1}}
	assertStatus(t, authRequest(h, client, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)
	w := authRequest(h, client, http.MethodPost, "/api/images/generations", map[string]any{"taskId": identity.ID(), "canvasId": canvasID, "nodeId": nodeID, "prompt": "A picture", "model": "doubao-seedream-5-0-260128", "acceptedPoints": 6, "priceVersion": "test"}, nil)
	assertStatus(t, w, 400)
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error.Code != "MODEL_GENERATION_NOT_ENABLED" {
		t.Fatalf("expected generation rejection for apimart draft with ark-like id: %s, %v", w.Body.String(), err)
	}
}

func TestModelCatalogEndpointRemoved(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &creditTestStorage{}, db)
	client := signIn(t, h, "catalog@example.test", "catalog password")
	w := authRequest(h, client, http.MethodGet, "/api/model-catalog", nil, nil)
	assertStatus(t, w, http.StatusNotFound)
}
