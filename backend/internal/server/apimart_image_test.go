package server

import (
	"context"
	"encoding/json"
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
