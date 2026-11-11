package persistence

import (
	"context"
	"testing"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
)

type museVideoFake struct {
	*videoFake
	requests chan ark.VideoRequest
	limits   chan storage.VideoReferenceLimits
}

func (f *museVideoFake) VideoGeneratorFor(provider string) VideoGenerator {
	if provider == ProviderAPIMart {
		return f
	}
	return nil
}
func (f *museVideoFake) CreateVideo(_ context.Context, input ark.VideoRequest) (string, error) {
	f.creates.Add(1)
	f.requests <- input
	return "muse-task", nil
}
func (f *museVideoFake) VideoReferenceURLsWithLimits(_ context.Context, _ storage.Scope, _ []string, limits storage.VideoReferenceLimits) ([]string, error) {
	f.limits <- limits
	return []string{"https://assets.example.test/reference.png"}, nil
}

func TestMuseVideoWorkerRoutesReferenceAndSettlesOneGeneration(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	snapshot := Snapshot{Nodes: []Node{
		{ID: nodeID, Data: NodeData{Kind: "video", Name: "Muse", ModelSelection: &ModelSelection{Provider: ProviderAPIMart, ModelID: "muse-video-ext", Parameters: map[string]any{}}}},
		{ID: promptSourceID, Data: NodeData{Kind: "image", Name: "source", Origin: "upload"}},
	}, Viewport: Viewport{Zoom: 1}}
	if _, err := db.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	asset := storage.Asset{Key: "muse-source", Kind: storage.Image, ContentType: "image/png", Bytes: 1000, Media: &storage.MediaMetadata{Width: 960, Height: 540}, Scope: storage.Scope{CanvasID: canvasID, NodeID: promptSourceID}}
	if err := db.RecordAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	if err := db.GrantCredits(ctx, testUserID, 20, "muse-video-test"); err != nil {
		t.Fatal(err)
	}
	input := Input{Provider: ProviderAPIMart, Model: "muse-video-ext", Mode: "reference", Prompt: "A moving boat", CanvasID: canvasID, NodeID: nodeID, Parameters: map[string]any{}, References: []Reference{{NodeID: promptSourceID, AssetKey: asset.Key, Kind: "image"}}}
	quote, err := db.QuoteCredits(ctx, testUserID, "video", input)
	if err != nil || quote.Points != 6 {
		t.Fatalf("quote: %+v %v", quote, err)
	}
	id := "62626262-6262-4262-8262-626262626262"
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "video", input, quote.Points, quote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	fake := &museVideoFake{videoFake: &videoFake{}, requests: make(chan ark.VideoRequest, 1), limits: make(chan storage.VideoReferenceLimits, 1)}
	stop := startWorker(t, db, fake, fake)
	defer stop()
	task := awaitVideo(t, db, id, "succeeded")
	request, limits := <-fake.requests, <-fake.limits
	account, err := db.Credits(ctx, testUserID)
	if err != nil || task.CreditStatus != "settled" || account != (CreditAccount{Available: 14}) || fake.creates.Load() != 1 || fake.queries.Load() != 1 || fake.saves.Load() != 1 {
		t.Fatalf("incorrect video settlement: task=%+v account=%+v err=%v", task, account, err)
	}
	if request.Model != "muse-video-ext" || request.Duration != 10 || request.Resolution != "720p" || !request.GenerateAudio || len(request.References) != 1 || limits.MaxImageBytes != 11<<20 {
		t.Fatalf("incorrect model request or preflight: %+v %+v", request, limits)
	}
}
