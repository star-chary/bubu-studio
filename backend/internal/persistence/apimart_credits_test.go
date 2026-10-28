package persistence

import (
	"context"
	"errors"
	"testing"

	"frame-space/backend/internal/apimart"
	"frame-space/backend/internal/ark"
)

func TestAPIMartCreditCompletionFailureBoundary(t *testing.T) {
	for _, code := range []string{"MODEL_NOT_CONFIGURED", "INVALID_IMAGE_INPUT", "INVALID_VIDEO_INPUT", "APIMART_REJECTED", "APIMART_AUTH_FAILED", "APIMART_BALANCE_INSUFFICIENT", "APIMART_RATE_LIMITED", "APIMART_CONTENT_REJECTED", "APIMART_GENERATION_FAILED", "APIMART_CANCELLED"} {
		if got := creditCompletion("image", "submitting", "failed", nil, &ark.APIError{Code: code}); got != "release" {
			t.Errorf("%s: %s", code, got)
		}
	}
	if got := creditCompletion("image", "submitting", "interrupted", nil, &ark.APIError{Code: "SUBMISSION_UNKNOWN"}); got != "review" {
		t.Fatal("ambiguous submission was released", got)
	}
}

func TestAPIMartCreditsReleaseAndUncertainReview(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	snapshot := testSnapshot()
	snapshot.Nodes[0].Data.ModelSelection = &ModelSelection{Provider: ProviderAPIMart, ModelID: "seedream-5-0-lite", Parameters: map[string]any{}}
	if _, err := db.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := db.GrantCredits(ctx, testUserID, 30, "apimart-billing-test"); err != nil {
		t.Fatal(err)
	}
	in := Input{Provider: ProviderAPIMart, Model: "seedream-5-0-lite", Prompt: "paper boat", CanvasID: canvasID, NodeID: nodeID, Parameters: map[string]any{"resolution": "2K", "size": "1:1", "n": 1}}
	quote, err := db.QuoteCredits(ctx, testUserID, "image", in)
	if err != nil || quote.Points != 6 || quote.PriceVersion != apimart.BillingVersion() {
		t.Fatalf("quote: %+v %v", quote, err)
	}
	id := "81818181-8181-4181-8181-818181818181"
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", in, quote.Points+2, quote.PriceVersion); !errors.Is(err, ErrCreditQuoteChanged) {
		t.Fatalf("stale price accepted: %v", err)
	}
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", in, quote.Points, quote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE generation_tasks SET status='running' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	db.finish(ctx, id, "failed", nil, &ark.APIError{Code: "APIMART_GENERATION_FAILED"})
	db.finish(ctx, id, "failed", nil, &ark.APIError{Code: "APIMART_GENERATION_FAILED"})
	account, err := db.Credits(ctx, testUserID)
	if err != nil || account != (CreditAccount{Available: 30}) {
		t.Fatalf("failed task not released exactly once: %+v %v", account, err)
	}
	id = "82828282-8282-4282-8282-828282828282"
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", in, quote.Points, quote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE generation_tasks SET status='submitting' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	db.finish(ctx, id, "interrupted", nil, &ark.APIError{Code: "SUBMISSION_UNKNOWN"})
	task, err := db.Task(ctx, id)
	account, accountErr := db.Credits(ctx, testUserID)
	if err != nil || accountErr != nil || task.CreditStatus != "review" || account != (CreditAccount{Available: 24, Reserved: 6}) {
		t.Fatalf("uncertain task should remain reserved: %+v %+v %v %v", task, account, err, accountErr)
	}
}

func TestAPIMartBilledVideoWorkerSettlesQuotedPrice(t *testing.T) {
	db, in, id := videoFixture(t)
	ctx := context.Background()
	snapshot := Snapshot{Nodes: []Node{{ID: nodeID, Data: NodeData{Kind: "video", Name: "video", ModelSelection: &ModelSelection{Provider: ProviderAPIMart, ModelID: "seedance-2.0", Parameters: map[string]any{}}}}}, Viewport: Viewport{Zoom: 1}}
	if _, err := db.SaveCanvas(ctx, canvasID, 2, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := db.GrantCredits(ctx, testUserID, 200, "apimart-video-billing-test"); err != nil {
		t.Fatal(err)
	}
	in.Provider, in.Model, in.Mode, in.Prompt = ProviderAPIMart, "seedance-2.0", "text", "A paper boat"
	in.References = nil
	in.Parameters = map[string]any{"size": "16:9", "resolution": "720p", "duration": 5, "generate_audio": false}
	quote, err := db.QuoteCredits(ctx, testUserID, "video", in)
	if err != nil || quote.Points != 124 {
		t.Fatalf("video quote: %+v %v", quote, err)
	}
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "video", in, quote.Points, quote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	provider := &videoProvidersFake{videoFake: &videoFake{}, apimart: &videoFake{}}
	stop := startWorker(t, db, provider, provider)
	defer stop()
	task := awaitVideo(t, db, id, "succeeded")
	account, err := db.Credits(ctx, testUserID)
	if err != nil || task.CreditStatus != "settled" || task.CreditPoints != quote.Points || account != (CreditAccount{Available: 76}) || provider.apimart.creates.Load() != 1 || provider.videoFake.creates.Load() != 0 {
		t.Fatalf("video settlement: task=%+v account=%+v error=%v", task, account, err)
	}
}

func TestAPIMartUnpricedAndMismatchedModelsCannotQuote(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	snapshot := testSnapshot()
	snapshot.Nodes[0].Data.ModelSelection = &ModelSelection{Provider: ProviderAPIMart, ModelID: "gemini-nano-banana-2.1", Parameters: map[string]any{}}
	if _, err := db.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	in := Input{Provider: ProviderAPIMart, Model: "gemini-nano-banana-2.1", Prompt: "paper boat", CanvasID: canvasID, NodeID: nodeID}
	if _, err := db.QuoteCredits(ctx, testUserID, "image", in); !errors.Is(err, ErrModelGenerationNotEnabled) {
		t.Fatalf("unpriced model accepted: %v", err)
	}
	in.Model = "seedream-5-0-lite"
	if _, err := db.QuoteCredits(ctx, testUserID, "image", in); !errors.Is(err, ErrModelGenerationNotEnabled) {
		t.Fatalf("mismatched saved model accepted: %v", err)
	}
	in.Provider, in.Model = "", ark.DefaultImageModel
	if _, err := db.QuoteCredits(ctx, testUserID, "image", in); !errors.Is(err, ErrModelGenerationNotEnabled) {
		t.Fatalf("provider forgery accepted: %v", err)
	}
	account, err := db.Credits(ctx, testUserID)
	if err != nil || account != (CreditAccount{}) {
		t.Fatalf("rejected quote changed credits: %+v %v", account, err)
	}
}
