package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/storage"
)

// The HTTP video route requires storage, but its provider runs only in the
// worker. Any storage use in these submission tests is an unexpected call.
type creditTestStorage struct {
	stubAssets
	videoCalls atomic.Int32
}

func (s *creditTestStorage) VideoReferenceURLsWithLimits(context.Context, storage.Scope, []string, storage.VideoReferenceLimits) ([]string, error) {
	s.videoCalls.Add(1)
	return nil, errors.New("unexpected video reference request")
}

func (s *creditTestStorage) SaveGeneratedVideo(context.Context, string, string, storage.Scope) (storage.Asset, error) {
	s.videoCalls.Add(1)
	return storage.Asset{}, errors.New("unexpected video save")
}

func TestCreditHTTPRequiresBalanceAndKeepsAccountsPrivate(t *testing.T) {
	db := authDB(t)
	ctx := context.Background()
	var imageCalls atomic.Int32
	media := &creditTestStorage{}
	h := NewRouter(generateFunc(func(context.Context, string, string) (ark.ImageResult, error) {
		imageCalls.Add(1)
		return ark.ImageResult{}, errors.New("unexpected image provider call")
	}), media, db)

	alice := signIn(t, h, "credits-alice@example.test", "alice test password")
	bob := signIn(t, h, "credits-bob@example.test", "bob test password")
	canvasID, imageNodeID, videoNodeID, sourceNodeID := identity.ID(), identity.ID(), identity.ID(), identity.ID()
	assertStatus(t, authRequest(h, alice, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID, "title": "Credits test"}, nil), 201)
	snapshot := persistence.Snapshot{
		Nodes: []persistence.Node{
			{ID: imageNodeID, Data: persistence.NodeData{Kind: "image", Name: "Image", Origin: "generated"}},
			{ID: videoNodeID, Data: persistence.NodeData{Kind: "video", Name: "Video", Origin: "generated"}},
			{ID: sourceNodeID, Data: persistence.NodeData{Kind: "image", Name: "Reference", Origin: "upload"}},
		},
		Viewport: persistence.Viewport{Zoom: 1},
	}
	assertStatus(t, authRequest(h, alice, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)
	refKey := "credits-reference-image"
	if err := db.RecordAsset(ctx, storage.Asset{Key: refKey, Kind: storage.Image, Source: storage.Upload, Scope: storage.Scope{CanvasID: canvasID, NodeID: sourceNodeID}}); err != nil {
		t.Fatal(err)
	}

	imageInput := map[string]any{"kind": "image", "canvasId": canvasID, "nodeId": imageNodeID, "prompt": "A lighthouse", "model": ark.DefaultImageModel}
	videoInput := map[string]any{
		"kind": "video", "canvasId": canvasID, "nodeId": videoNodeID, "prompt": "A short scene",
		"model": ark.VideoModel, "mode": "reference", "resolution": "480p", "ratio": "16:9", "duration": 5,
		"references": []persistence.Reference{{NodeID: sourceNodeID, AssetKey: refKey, Kind: "image"}},
	}
	quote := func(input map[string]any, want int64) persistence.CreditQuote {
		t.Helper()
		w := authRequest(h, alice, http.MethodPost, "/api/credits/quote", input, nil)
		assertStatus(t, w, 200)
		var q persistence.CreditQuote
		if err := json.Unmarshal(w.Body.Bytes(), &q); err != nil || q.Points != want || q.PriceVersion == "" {
			t.Fatalf("invalid quote: %s, error: %v", w.Body.String(), err)
		}
		return q
	}
	imageQuote := quote(imageInput, 6)
	videoQuote := quote(videoInput, 82)
	// A prompt with reference parts keeps its original whitespace projection.
	// Trimming it at the quote endpoint would reject valid saved drafts.
	mentionedImage := map[string]any{"kind": "image", "canvasId": canvasID, "nodeId": imageNodeID,
		"prompt": "  See @Reference  ", "model": ark.DefaultImageModel, "referenceKeys": []string{refKey},
		"promptParts": []persistence.PromptPart{{Type: "text", Text: "  See "}, {Type: "reference", NodeID: sourceNodeID, Kind: "image", Name: "Reference"}, {Type: "text", Text: "  "}},
	}
	quote(mentionedImage, 6)

	assertStatus(t, authRequest(h, nil, http.MethodGet, "/api/credits", nil, nil), 401)
	assertStatus(t, authRequest(h, alice, http.MethodPost, "/api/credits/quote", imageInput, map[string]string{"X-CSRF-Token": ""}), 403)
	assertStatus(t, authRequest(h, alice, http.MethodPost, "/api/credits/quote", imageInput, map[string]string{"Origin": "https://attacker.example"}), 403)
	account := func(client *authClient, available, reserved int64) {
		t.Helper()
		w := authRequest(h, client, http.MethodGet, "/api/credits", nil, nil)
		assertStatus(t, w, 200)
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("credit account response may be cached")
		}
		var a persistence.CreditAccount
		if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil || a.Available != available || a.Reserved != reserved {
			t.Fatalf("wrong account: %s, error: %v", w.Body.String(), err)
		}
	}
	account(alice, 0, 0)
	account(bob, 0, 0)

	imageTaskID, videoTaskID := identity.ID(), identity.ID()
	imageSubmit := map[string]any{"taskId": imageTaskID, "canvasId": canvasID, "nodeId": imageNodeID, "prompt": "A lighthouse", "model": ark.DefaultImageModel, "acceptedPoints": imageQuote.Points, "priceVersion": imageQuote.PriceVersion}
	videoSubmit := map[string]any{"taskId": videoTaskID, "canvasId": canvasID, "nodeId": videoNodeID, "prompt": "A short scene", "model": ark.VideoModel, "mode": "reference", "resolution": "480p", "ratio": "16:9", "duration": 5, "references": videoInput["references"], "acceptedPoints": videoQuote.Points, "priceVersion": videoQuote.PriceVersion}
	for _, request := range []struct {
		path string
		body map[string]any
	}{
		{"/api/images/generations", imageSubmit},
		{"/api/videos/generations", videoSubmit},
	} {
		w := authRequest(h, alice, http.MethodPost, request.path, request.body, nil)
		assertStatus(t, w, 402)
		var response struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error.Code != "CREDITS_INSUFFICIENT" {
			t.Fatalf("expected insufficient credits: %s, error: %v", w.Body.String(), err)
		}
	}
	var taskCount int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM generation_tasks`).Scan(&taskCount); err != nil || taskCount != 0 {
		t.Fatalf("zero-balance request created %d tasks: %v", taskCount, err)
	}
	if imageCalls.Load() != 0 || media.videoCalls.Load() != 0 {
		t.Fatal("zero-balance request reached a provider or video storage")
	}

	if err := db.GrantCredits(ctx, alice.session.User.ID, 200, "test-credit-http-grant"); err != nil {
		t.Fatal(err)
	}
	account(alice, 200, 0)
	account(bob, 0, 0)
	assertStatus(t, authRequest(h, alice, http.MethodPost, "/api/images/generations", imageSubmit, map[string]string{"X-CSRF-Token": ""}), 403)
	assertStatus(t, authRequest(h, alice, http.MethodPost, "/api/videos/generations", videoSubmit, map[string]string{"X-CSRF-Token": ""}), 403)
	account(alice, 200, 0)
	var task persistence.Task
	w := authRequest(h, alice, http.MethodPost, "/api/images/generations", imageSubmit, nil)
	assertStatus(t, w, 202)
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil || task.ID != imageTaskID || task.CreditPoints != 6 || task.CreditStatus != "reserved" {
		t.Fatalf("wrong billed task: %s, error: %v", w.Body.String(), err)
	}
	account(alice, 194, 6)
	assertStatus(t, authRequest(h, alice, http.MethodPost, "/api/images/generations", imageSubmit, nil), 202)
	account(alice, 194, 6)

	ledger := func(client *authClient, path string) []persistence.CreditEntry {
		t.Helper()
		w := authRequest(h, client, http.MethodGet, path, nil, nil)
		assertStatus(t, w, 200)
		var response struct {
			Entries []persistence.CreditEntry `json:"entries"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("invalid ledger: %s, error: %v", w.Body.String(), err)
		}
		return response.Entries
	}
	aliceEntries := ledger(alice, "/api/credits/ledger")
	if len(aliceEntries) != 2 || aliceEntries[0].Operation != "reserve" || aliceEntries[0].AvailableDelta != -6 || aliceEntries[0].ReservedDelta != 6 || aliceEntries[0].TaskID == nil || *aliceEntries[0].TaskID != imageTaskID || aliceEntries[1].Operation != "grant" || aliceEntries[1].AvailableDelta != 200 {
		t.Fatalf("wrong ledger: %+v", aliceEntries)
	}
	if entries := ledger(bob, "/api/credits/ledger?userId="+alice.session.User.ID); len(entries) != 0 {
		t.Fatalf("cross-user ledger leak: %+v", entries)
	}
	account(bob, 0, 0)
	assertStatus(t, authRequest(h, bob, http.MethodGet, "/api/credits/ledger", nil, map[string]string{"X-Session-User": alice.session.User.ID}), 409)
	if imageCalls.Load() != 0 || media.videoCalls.Load() != 0 {
		t.Fatal("HTTP submission directly reached a provider or video storage")
	}
}

func TestTextVideoQuoteAndSubmitRepriceSelectedModel(t *testing.T) {
	db := authDB(t)
	ctx := context.Background()
	media := &creditTestStorage{}
	h := NewRouter(nil, media, db)
	client := signIn(t, h, "text-video@example.test", "video test password")
	canvasID, videoNodeID, taskID := identity.ID(), identity.ID(), identity.ID()
	assertStatus(t, authRequest(h, client, http.MethodPost, "/api/canvases", map[string]string{"id": canvasID, "title": "Text video"}, nil), 201)
	snapshot := persistence.Snapshot{Nodes: []persistence.Node{{ID: videoNodeID, Data: persistence.NodeData{Kind: "video", Name: "Text video", VideoModel: ark.VideoModel20Mini, VideoMode: "text", VideoOptions: &persistence.VideoOptions{Resolution: "480p", Ratio: "16:9", Duration: 5}}}}, Viewport: persistence.Viewport{Zoom: 1}}
	assertStatus(t, authRequest(h, client, http.MethodPut, "/api/canvases/"+canvasID, map[string]any{"version": 0, "snapshot": snapshot}, nil), 200)
	input := map[string]any{"kind": "video", "canvasId": canvasID, "nodeId": videoNodeID, "prompt": "A moving scene", "model": ark.VideoModel20Mini, "mode": "text", "resolution": "480p", "ratio": "16:9", "duration": 5}
	w := authRequest(h, client, http.MethodPost, "/api/credits/quote", input, nil)
	assertStatus(t, w, 200)
	var quote persistence.CreditQuote
	if err := json.Unmarshal(w.Body.Bytes(), &quote); err != nil || quote.Points != 28 || quote.PriceVersion != persistence.CreditPriceVersion {
		t.Fatalf("mini text quote: %s %v", w.Body.String(), err)
	}
	submit := map[string]any{"taskId": taskID, "canvasId": canvasID, "nodeId": videoNodeID, "prompt": "A moving scene", "model": ark.VideoModel20Mini, "mode": "text", "resolution": "480p", "ratio": "16:9", "duration": 5, "acceptedPoints": quote.Points, "priceVersion": quote.PriceVersion}
	assertStatus(t, authRequest(h, client, http.MethodPost, "/api/videos/generations", submit, nil), 402)
	if err := db.GrantCredits(ctx, client.session.User.ID, 28, "test-mini-text-grant"); err != nil {
		t.Fatal(err)
	}
	// The server must reject a model change made after accepting the old quote.
	submit["model"] = ark.VideoModel20Fast
	assertStatus(t, authRequest(h, client, http.MethodPost, "/api/videos/generations", submit, nil), 409)
	submit["model"] = ark.VideoModel20Mini
	assertStatus(t, authRequest(h, client, http.MethodPost, "/api/videos/generations", submit, nil), 202)
	account, err := db.Credits(ctx, client.session.User.ID)
	if err != nil || account != (persistence.CreditAccount{Reserved: 28}) {
		t.Fatalf("text task reservation: %+v %v", account, err)
	}
	if media.videoCalls.Load() != 0 {
		t.Fatal("text task submission read reference storage")
	}
	input["references"] = []persistence.Reference{{NodeID: identity.ID(), AssetKey: "x", Kind: "image"}}
	assertStatus(t, authRequest(h, client, http.MethodPost, "/api/credits/quote", input, nil), 400)
}
