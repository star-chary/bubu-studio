package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"frame-space/backend/internal/apimart"
	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/storage"
)

type fullImageFake struct {
	stubAssets
	creates, queries, saves atomic.Int32
}

type seedream55ImageFake struct {
	*fullImageFake
	requests chan apimart.ImageRequest
}

func (f *seedream55ImageFake) APIMartImageGenerator() persistence.APIMartImageGenerator { return f }
func (f *seedream55ImageFake) CreateImage(_ context.Context, input apimart.ImageRequest) (string, error) {
	f.creates.Add(1)
	f.requests <- input
	return "image_task_123", nil
}
func (f *seedream55ImageFake) APIMartImageReferenceURLs(_ context.Context, _ storage.Scope, keys []string) ([]string, error) {
	if len(keys) != 1 || keys[0] != "seedream55-ref" {
		return nil, fmt.Errorf("unexpected reference keys")
	}
	return []string{"https://assets.example/reference.png"}, nil
}

func (f *fullImageFake) Generate(context.Context, string, string, *ark.ImageOptions, ...string) (ark.ImageResult, error) {
	panic("unexpected Ark request")
}
func (f *fullImageFake) APIMartImageGenerator() persistence.APIMartImageGenerator { return f }
func (f *fullImageFake) CreateImage(context.Context, apimart.ImageRequest) (string, error) {
	f.creates.Add(1)
	return "image_task_123", nil
}
func (f *fullImageFake) QueryImage(context.Context, string, string) (apimart.ImageResponse, error) {
	f.queries.Add(1)
	return apimart.ImageResponse{Status: "succeeded", URL: "https://provider.test/output.png"}, nil
}
func (f *fullImageFake) SaveGenerated(_ context.Context, _, _ string, scope storage.Scope) (storage.Asset, error) {
	f.saves.Add(1)
	return storage.Asset{Key: "output", URL: "/api/assets/content?key=output", Kind: storage.Image, Source: storage.Generated, Scope: scope}, nil
}

func quoteAPIMart(t *testing.T, h http.Handler, owner *authClient, kind string, body map[string]any) persistence.CreditQuote {
	t.Helper()
	request := make(map[string]any, len(body)+1)
	for key, value := range body {
		request[key] = value
	}
	request["kind"] = kind
	w := authRequest(h, owner, http.MethodPost, "/api/credits/quote", request, nil)
	assertStatus(t, w, 200)
	var quote persistence.CreditQuote
	if json.Unmarshal(w.Body.Bytes(), &quote) != nil || quote.Points <= 0 || quote.PriceVersion != apimart.BillingVersion() {
		t.Fatalf("invalid quote: %s", w.Body.String())
	}
	return quote
}

func TestAPIMartImageHTTPWorkerAndStorageFlow(t *testing.T) {
	db := authDB(t)
	fake := &fullImageFake{}
	h := NewRouter(fake, fake, db)
	owner := signIn(t, h, "full-apimart-image@example.test", "api-image-pass1")
	canvasID, nodeID, taskID := identity.ID(), identity.ID(), identity.ID()
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID}, nil), 201)
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{{ID: nodeID, Data: persistence.NodeData{Kind: "image", Name: "APIMart", ModelSelection: &persistence.ModelSelection{Provider: "apimart", ModelID: "seedream-5-0-lite", Parameters: map[string]any{}}}}}, Viewport: persistence.Viewport{Zoom: 1}}
	assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)
	ctx, cancel := context.WithCancel(context.Background())
	ready, done := make(chan error, 1), make(chan struct{})
	go func() { defer close(done); _ = db.RunWorker(ctx, fake, fake, ready) }()
	if err := <-ready; err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() { cancel(); <-done }()
	body := map[string]any{"taskId": taskID, "canvasId": canvasID, "nodeId": nodeID, "provider": "apimart", "model": "seedream-5-0-lite", "prompt": "A paper boat", "parameters": map[string]any{"size": "1:1", "resolution": "2K", "n": 1}}
	quote := quoteAPIMart(t, h, owner, "image", body)
	body["acceptedPoints"], body["priceVersion"] = quote.Points, quote.PriceVersion
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/images/generations", body, nil), 202)
	var task persistence.Task
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		w := authRequest(h, owner, http.MethodGet, "/api/tasks/"+taskID, nil, nil)
		assertStatus(t, w, 200)
		if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		if task.Status == "succeeded" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if task.Status != "succeeded" || task.CreditStatus != "settled" || task.CreditPoints != quote.Points || task.Result == nil || task.Result.Asset == nil || task.Result.URL != "/api/assets/content?key=output" || fake.creates.Load() != 1 || fake.queries.Load() != 1 || fake.saves.Load() != 1 {
		t.Fatalf("incomplete HTTP to worker flow: %+v", task)
	}
	account, err := db.Credits(t.Context(), owner.session.User.ID)
	if err != nil || account.Available != persistence.SignupTestCredits-quote.Points || account.Reserved != 0 {
		t.Fatalf("incorrect settled balance: %+v %v", account, err)
	}
}

func TestSeedream55HTTPWorkerAndStorageFlow(t *testing.T) {
	db := authDB(t)
	fake := &seedream55ImageFake{fullImageFake: &fullImageFake{}, requests: make(chan apimart.ImageRequest, 1)}
	h := NewRouter(fake, fake, db)
	owner := signIn(t, h, "seedream55-worker@example.test", "seedream55-pass1")
	canvasID, nodeID, sourceID, taskID := identity.ID(), identity.ID(), identity.ID(), identity.ID()
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID}, nil), 201)
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{
		{ID: nodeID, Data: persistence.NodeData{Kind: "image", Name: "Seedream", ModelSelection: &persistence.ModelSelection{Provider: "apimart", ModelID: "seedream-5-5", Parameters: map[string]any{}}}},
		{ID: sourceID, Data: persistence.NodeData{Kind: "image", Name: "Reference", Origin: "upload"}},
	}, Viewport: persistence.Viewport{Zoom: 1}}
	assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)
	if err := db.RecordAsset(t.Context(), storage.Asset{Key: "seedream55-ref", Kind: storage.Image, Bytes: 1000, Scope: storage.Scope{CanvasID: canvasID, NodeID: sourceID}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready, done := make(chan error, 1), make(chan struct{})
	go func() { defer close(done); _ = db.RunWorker(ctx, fake, fake, ready) }()
	if err := <-ready; err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() { cancel(); <-done }()
	body := map[string]any{"taskId": taskID, "canvasId": canvasID, "nodeId": nodeID, "provider": "apimart", "model": "seedream-5-5", "prompt": "A paper boat", "parameters": map[string]any{"size": "1:1", "resolution": "2K", "n": 1}, "referenceKeys": []string{"seedream55-ref"}}
	quote := quoteAPIMart(t, h, owner, "image", body)
	if quote.Points != 6 {
		t.Fatalf("wrong reference quote: %+v", quote)
	}
	body["acceptedPoints"], body["priceVersion"] = quote.Points, quote.PriceVersion
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/images/generations", body, nil), 202)
	var task persistence.Task
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		w := authRequest(h, owner, http.MethodGet, "/api/tasks/"+taskID, nil, nil)
		assertStatus(t, w, 200)
		if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		if task.Status == "succeeded" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if task.Status != "succeeded" || task.CreditStatus != "settled" || task.CreditPoints != 6 || task.Result == nil || task.Result.Asset == nil || fake.creates.Load() != 1 || fake.queries.Load() != 1 || fake.saves.Load() != 1 {
		t.Fatalf("incomplete Seedream flow: %+v", task)
	}
	request := <-fake.requests
	if request.Model != "seedream-5-5" || request.Parameters["resolution"] != "2K" || len(request.Images) != 1 {
		t.Fatalf("incorrect worker request: %+v", request)
	}
}

func TestAPIMartImageTaskIsOwnedIdempotentAndReservesPoints(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &creditTestStorage{}, db)
	owner := signIn(t, h, "apimart-image@example.test", "api-image-pass1")
	other := signIn(t, h, "other-apimart-image@example.test", "other-image-pass1")
	canvasID, nodeID, taskID := identity.ID(), identity.ID(), identity.ID()
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID}, nil), 201)
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{{ID: nodeID, Data: persistence.NodeData{Kind: "image", Name: "APIMart", ModelSelection: &persistence.ModelSelection{Provider: "apimart", ModelID: "seedream-5-0-lite", Parameters: map[string]any{}}}}}, Viewport: persistence.Viewport{Zoom: 1}}
	assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)
	body := map[string]any{"taskId": taskID, "canvasId": canvasID, "nodeId": nodeID, "provider": "apimart", "model": "seedream-5-0-lite", "prompt": "A paper boat", "parameters": map[string]any{"size": "16:9", "resolution": "2K", "n": 1}}
	assertStatus(t, authRequest(h, other, http.MethodPost, "/api/images/generations", body, nil), 404)
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/images/generations", body, nil), 409)
	quote := quoteAPIMart(t, h, owner, "image", body)
	body["acceptedPoints"], body["priceVersion"] = quote.Points, quote.PriceVersion
	w := authRequest(h, owner, http.MethodPost, "/api/images/generations", body, nil)
	assertStatus(t, w, 202)
	var task persistence.Task
	if json.Unmarshal(w.Body.Bytes(), &task) != nil || task.Input.Provider != "apimart" || task.CreditStatus != "reserved" || task.CreditPoints != quote.Points {
		t.Fatal(w.Body.String())
	}
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/images/generations", body, nil), 202)
	changed := make(map[string]any)
	for k, v := range body {
		changed[k] = v
	}
	changed["prompt"] = "Different"
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/images/generations", changed, nil), 409)
	var tasks, reserves int
	if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM generation_tasks`).Scan(&tasks); err != nil || tasks != 1 {
		t.Fatal(tasks, err)
	}
	if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM credit_ledger WHERE operation = 'reserve'`).Scan(&reserves); err != nil || reserves != 1 {
		t.Fatal(reserves, err)
	}
	snapshot.Nodes[0].Data.ModelSelection = nil
	assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 1, "snapshot": snapshot}, nil), 200)
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/images/generations", body, nil), 202)
	body["taskId"] = identity.ID()
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/images/generations", body, nil), 400)
}

func TestSeedream55HTTPQuoteAndReferenceFees(t *testing.T) {
	db := authDB(t)
	h := NewRouter(nil, &creditTestStorage{}, db)
	owner := signIn(t, h, "seedream55@example.test", "seedream55-pass1")
	canvasID, nodeID, sourceA, sourceB := identity.ID(), identity.ID(), identity.ID(), identity.ID()
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID}, nil), 201)
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{
		{ID: nodeID, Data: persistence.NodeData{Kind: "image", Name: "Seedream 5.5", ModelSelection: &persistence.ModelSelection{Provider: "apimart", ModelID: "seedream-5-5", Parameters: map[string]any{}}}},
		{ID: sourceA, Data: persistence.NodeData{Kind: "image", Name: "reference A", Origin: "upload"}},
		{ID: sourceB, Data: persistence.NodeData{Kind: "image", Name: "reference B", Origin: "upload"}},
	}, Viewport: persistence.Viewport{Zoom: 1}}
	assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)
	for _, asset := range []storage.Asset{
		{Key: "seedream55-a", Kind: storage.Image, Bytes: 1000, Scope: storage.Scope{CanvasID: canvasID, NodeID: sourceA}},
		{Key: "seedream55-b", Kind: storage.Image, Bytes: 1000, Scope: storage.Scope{CanvasID: canvasID, NodeID: sourceB}},
	} {
		if err := db.RecordAsset(t.Context(), asset); err != nil {
			t.Fatal(err)
		}
	}
	body := map[string]any{"taskId": identity.ID(), "canvasId": canvasID, "nodeId": nodeID, "provider": "apimart", "model": "seedream-5-5", "prompt": "A paper boat", "parameters": map[string]any{"size": "1:1", "resolution": "1K", "n": 1}}
	for _, tc := range []struct {
		resolution string
		keys       []string
		points     int64
	}{
		{"1K", nil, 4},
		{"1.5K", []string{"seedream55-a"}, 4},
		{"1.5K", []string{"seedream55-a", "seedream55-b"}, 6},
		{"2K", []string{"seedream55-a"}, 6},
	} {
		body["parameters"] = map[string]any{"size": "1:1", "resolution": tc.resolution, "n": 1}
		if len(tc.keys) == 0 {
			delete(body, "referenceKeys")
		} else {
			body["referenceKeys"] = tc.keys
		}
		quote := quoteAPIMart(t, h, owner, "image", body)
		if quote.Points != tc.points {
			t.Fatalf("%s with %d references: got %d want %d", tc.resolution, len(tc.keys), quote.Points, tc.points)
		}
	}
	quote := quoteAPIMart(t, h, owner, "image", body)
	body["acceptedPoints"], body["priceVersion"] = quote.Points, quote.PriceVersion
	w := authRequest(h, owner, http.MethodPost, "/api/images/generations", body, nil)
	assertStatus(t, w, 202)
	assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/images/generations", body, nil), 202)
	var task persistence.Task
	if json.Unmarshal(w.Body.Bytes(), &task) != nil || task.CreditStatus != "reserved" || task.CreditPoints != 6 || len(task.Bindings) != 1 {
		t.Fatalf("incorrect task: %s", w.Body.String())
	}
	var reserves int
	if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM credit_ledger WHERE operation='reserve'`).Scan(&reserves); err != nil || reserves != 1 {
		t.Fatalf("expected one reservation: %d %v", reserves, err)
	}
}

func TestNanoImageHTTPQuoteAndBatchTargets(t *testing.T) {
	for _, tc := range []struct {
		count  int
		model  string
		points int64
	}{
		{1, "gemini-nano-banana-2.1-ext", 4},
		{2, "gemini-nano-banana-2.1", 12},
		{4, "gemini-nano-banana-2.1", 24},
	} {
		t.Run(tc.model+"-"+string(rune('0'+tc.count)), func(t *testing.T) {
			db := authDB(t)
			h := NewRouter(nil, &creditTestStorage{}, db)
			owner := signIn(t, h, "nano-http@example.test", "nano-http-pass1")
			canvasID, taskID := identity.ID(), identity.ID()
			assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID}, nil), 201)
			var ids []string
			snapshot := persistence.Snapshot{Viewport: persistence.Viewport{Zoom: 1}}
			for i := 0; i < tc.count; i++ {
				id := identity.ID()
				ids = append(ids, id)
				count := 1
				if i == 0 {
					count = tc.count
				}
				snapshot.Nodes = append(snapshot.Nodes, persistence.Node{ID: id, Data: persistence.NodeData{Kind: "image", Name: "Nano", ModelSelection: &persistence.ModelSelection{Provider: "apimart", ModelID: "gemini-nano-banana-2.1", Parameters: map[string]any{"n": count}}}})
			}
			assertStatus(t, authRequest(h, owner, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)
			body := map[string]any{"taskId": taskID, "canvasId": canvasID, "nodeId": ids[0], "provider": "apimart", "model": tc.model, "prompt": "A paper boat", "parameters": map[string]any{"size": "1:1", "resolution": "1K", "n": tc.count}}
			quote := quoteAPIMart(t, h, owner, "image", body)
			if quote.Points != tc.points {
				t.Fatalf("unexpected quote: %+v", quote)
			}
			body["acceptedPoints"], body["priceVersion"] = quote.Points, quote.PriceVersion
			if tc.count > 1 {
				body["outputNodeIds"] = []string{ids[0], ids[0]}
				assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/images/generations", body, nil), 400)
				body["outputNodeIds"] = ids
			}
			assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/images/generations", body, nil), 202)
			assertStatus(t, authRequest(h, owner, http.MethodPost, "/api/images/generations", body, nil), 202)
			var tasks, reserves int
			if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM generation_tasks`).Scan(&tasks); err != nil || tasks != tc.count {
				t.Fatalf("wrong task count: %d %v", tasks, err)
			}
			if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM credit_ledger WHERE operation='reserve'`).Scan(&reserves); err != nil || reserves != 1 {
				t.Fatalf("expected one reservation: %d %v", reserves, err)
			}
		})
	}
}
