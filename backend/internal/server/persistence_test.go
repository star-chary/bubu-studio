package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/persistence"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPersistentHTTPFlow(t *testing.T) {
	connection := os.Getenv("TEST_DATABASE_URL")
	if connection == "" {
		t.Skip("requires real PostgreSQL TEST_DATABASE_URL")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, connection)
	if err != nil {
		t.Fatal("invalid test database")
	}
	defer admin.Close()
	schema := fmt.Sprintf("test_http_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
	address, err := url.Parse(connection)
	if err != nil {
		t.Fatal("invalid test connection")
	}
	query := address.Query()
	query.Set("search_path", schema)
	address.RawQuery = query.Encode()
	db, err := persistence.Open(ctx, address.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var calls atomic.Int32
	generator := generateFunc(func(ctx context.Context, prompt, model string) (ark.ImageResult, error) {
		calls.Add(1)
		return ark.ImageResult{URL: "https://example.test/result.png", Model: model}, nil
	})
	router := newHandlerTestRouter(generator, nil, db)
	request := func(method, path string, body any, want int) *httptest.ResponseRecorder {
		t.Helper()
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s: wanted %d got %d: %s", method, path, want, w.Code, w.Body.String())
		}
		return w
	}
	canvasID := assetCanvas
	nodeID := assetNode
	taskID := "30303030-3030-4030-8030-303030303030"
	request("GET", "/api/persistence/config", nil, 200)
	request("POST", "/api/canvases", map[string]string{"id": canvasID}, 201)
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{{ID: nodeID, Position: persistence.Position{X: 100, Y: 120}, Data: persistence.NodeData{Kind: "image", Name: "HTTP node", Prompt: "draft"}}}, Viewport: persistence.Viewport{Zoom: 1}}
	request("PUT", "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, 200)
	otherCanvasID := "50505050-5050-4050-8050-505050505050"
	request("POST", "/api/canvases", map[string]string{"id": otherCanvasID, "title": "  独立画布  "}, 201)
	// A retry keeps the same document, including its original name and contents.
	request("POST", "/api/canvases", map[string]string{"id": canvasID, "title": "重试不能覆盖"}, 201)
	var library struct {
		Canvases []persistence.CanvasSummary `json:"canvases"`
		HasMore  bool                        `json:"hasMore"`
	}
	if err := json.Unmarshal(request("GET", "/api/canvases?limit=1", nil, 200).Body.Bytes(), &library); err != nil {
		t.Fatal(err)
	}
	if len(library.Canvases) != 1 || library.Canvases[0].ID != otherCanvasID || library.Canvases[0].Title != "独立画布" || library.Canvases[0].NodeCount != 0 || !library.HasMore {
		t.Fatalf("unexpected first page: %+v", library)
	}
	if err := json.Unmarshal(request("GET", "/api/canvases?limit=1&offset=1", nil, 200).Body.Bytes(), &library); err != nil {
		t.Fatal(err)
	}
	if len(library.Canvases) != 1 || library.Canvases[0].ID != canvasID || library.Canvases[0].Title != "未命名画布" || library.Canvases[0].NodeCount != 1 || len(library.Canvases[0].Preview) != 1 || library.HasMore {
		t.Fatalf("unexpected second page: %+v", library)
	}
	for _, query := range []string{"limit=0", "limit=101", "offset=-1", "offset=x"} {
		request("GET", "/api/canvases?"+query, nil, 400)
	}
	request("POST", "/api/canvases", map[string]string{"id": otherCanvasID, "title": strings.Repeat("字", 81)}, 400)
	request("PUT", "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, 409)
	if err := db.GrantCredits(ctx, handlerUserID, 20, "persistent-http-test-grant"); err != nil {
		t.Fatal(err)
	}
	imageQuote, err := db.QuoteCredits(ctx, handlerUserID, "image", persistence.Input{CanvasID: canvasID, NodeID: nodeID, Prompt: "task snapshot", Model: ark.DefaultImageModel})
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"taskId": taskID, "canvasId": canvasID, "nodeId": nodeID, "prompt": "task snapshot", "model": ark.DefaultImageModel, "acceptedPoints": imageQuote.Points, "priceVersion": imageQuote.PriceVersion}
	// The submission context ends immediately; the worker must still finish.
	request("POST", "/api/images/generations", input, 202)
	request("POST", "/api/images/generations", input, 202)
	input["prompt"] = "different"
	request("POST", "/api/images/generations", input, 409)
	request("GET", "/api/tasks/not-a-uuid", nil, 400)
	request("GET", "/api/canvases/"+canvasID+"/tasks?offset=-1", nil, 400)
	request("GET", "/api/tasks/40404040-4040-4040-8040-404040404040", nil, 404)
	snapshot.Nodes[0].Data.Prompt = "later draft"
	request("PUT", "/api/canvases/"+canvasID, map[string]any{"version": 1, "snapshot": snapshot}, 200)
	workerCtx, cancel := context.WithCancel(ctx)
	ready := make(chan error, 1)
	done := make(chan struct{})
	go func() { defer close(done); _ = db.RunWorker(workerCtx, generator, nil, ready) }()
	if err = <-ready; err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); <-done }()
	var task persistence.Task
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task, _ = db.Task(ctx, taskID)
		if task.Status == "succeeded" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if task.Status != "succeeded" || calls.Load() != 1 {
		t.Fatal("persistent task did not complete exactly once")
	}
	response := request("GET", "/api/canvases/"+canvasID, nil, 200)
	var canvas persistence.Canvas
	if err = json.Unmarshal(response.Body.Bytes(), &canvas); err != nil {
		t.Fatal(err)
	}
	if len(canvas.Results) != 1 || canvas.Snapshot.Nodes[0].Data.Prompt != "later draft" {
		t.Fatal("restoration lost independent draft/result")
	}
	response = request("GET", "/api/canvases/"+canvasID+"/tasks?limit=1", nil, http.StatusOK)
	var list struct {
		Tasks   []persistence.Task `json:"tasks"`
		HasMore bool               `json:"hasMore"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &list)
	if len(list.Tasks) != 1 || list.HasMore || list.Tasks[0].Input.Prompt != "task snapshot" {
		t.Fatal("history is not the original task snapshot")
	}
}
