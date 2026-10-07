package persistence

import (
	"context"
	"errors"
	"sync"
	"testing"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
)

func TestCreditsGrantReserveSettleAndRelease(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	initial, err := db.Credits(ctx, testUserID)
	if err != nil || initial != (CreditAccount{}) {
		t.Fatalf("initial account: %+v %v", initial, err)
	}
	quote, err := db.QuoteCredits(ctx, testUserID, "image", input())
	if err != nil || quote.Points != 6 || quote.PriceVersion != CreditPriceVersion {
		t.Fatalf("image quote: %+v %v", quote, err)
	}
	firstID := "31313131-3131-4131-8131-313131313131"
	if _, err := db.CreateBilledTask(ctx, testUserID, firstID, "image", input(), quote.Points, quote.PriceVersion); !errors.Is(err, ErrCreditsInsufficient) {
		t.Fatalf("zero balance should reject submission: %v", err)
	}
	if _, err := db.Task(ctx, firstID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("zero-balance request created task: %v", err)
	}
	for range 2 {
		if err := db.GrantCredits(ctx, testUserID, 20, "credits-test-grant"); err != nil {
			t.Fatal(err)
		}
	}
	account, err := db.Credits(ctx, testUserID)
	if err != nil || account != (CreditAccount{Available: 20}) {
		t.Fatalf("idempotent grant: %+v %v", account, err)
	}
	task, err := db.CreateBilledTask(ctx, testUserID, firstID, "image", input(), quote.Points, quote.PriceVersion)
	if err != nil || task.CreditStatus != "reserved" || task.CreditPoints != 6 {
		t.Fatalf("reserve task: %+v %v", task, err)
	}
	if _, err := db.CreateBilledTask(ctx, testUserID, firstID, "image", input(), quote.Points, quote.PriceVersion); err != nil {
		t.Fatalf("idempotent submit: %v", err)
	}
	account, err = db.Credits(ctx, testUserID)
	if err != nil || account != (CreditAccount{Available: 14, Reserved: 6}) {
		t.Fatalf("reservation: %+v %v", account, err)
	}
	asset := storage.Asset{Key: "credits-generated-image", URL: "https://assets.example.test/image.png", Kind: storage.Image, Scope: storage.Scope{CanvasID: canvasID, NodeID: nodeID}}
	db.finish(ctx, firstID, "succeeded", &Result{ImageResult: ark.ImageResult{URL: asset.URL, Model: ark.DefaultImageModel}, Asset: &asset}, nil)
	db.finish(ctx, firstID, "succeeded", &Result{Asset: &asset}, nil)
	task, err = db.Task(ctx, firstID)
	account, accountErr := db.Credits(ctx, testUserID)
	if err != nil || accountErr != nil || task.CreditStatus != "settled" || account != (CreditAccount{Available: 14}) {
		t.Fatalf("settlement: task=%+v account=%+v errors=%v %v", task, account, err, accountErr)
	}
	secondID := "32323232-3232-4232-8232-323232323232"
	if _, err := db.CreateBilledTask(ctx, testUserID, secondID, "image", input(), quote.Points+2, quote.PriceVersion); !errors.Is(err, ErrCreditQuoteChanged) {
		t.Fatalf("stale quote accepted: %v", err)
	}
	if _, err := db.CreateBilledTask(ctx, testUserID, secondID, "image", input(), quote.Points, quote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	db.finish(ctx, secondID, "failed", nil, &ark.APIError{Code: "REFERENCE_STORAGE_REQUIRED"})
	account, err = db.Credits(ctx, testUserID)
	if err != nil || account != (CreditAccount{Available: 14}) {
		t.Fatalf("pre-provider failure should release: %+v %v", account, err)
	}
	entries, _, err := db.CreditEntries(ctx, testUserID, 20, 0)
	if err != nil || len(entries) != 5 || entries[0].Operation != "release" || entries[2].Operation != "settle" {
		t.Fatalf("ledger: %+v %v", entries, err)
	}
}

func TestCreditsConcurrentReservationsCannotOverdraw(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	otherNodeID := "25252525-2525-4252-8252-252525252525"
	snapshot := testSnapshot()
	snapshot.Nodes = append(snapshot.Nodes, Node{ID: otherNodeID, Data: NodeData{Kind: "image", Name: "second"}})
	if _, err := db.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := db.GrantCredits(ctx, testUserID, 6, "credits-concurrent-grant"); err != nil {
		t.Fatal(err)
	}
	quote, err := db.QuoteCredits(ctx, testUserID, "image", input())
	if err != nil {
		t.Fatal(err)
	}
	requests := []struct{ id, nodeID string }{
		{"33333333-3333-4333-8333-333333333333", nodeID},
		{"34343434-3434-4343-8343-343434343434", otherNodeID},
	}
	results := make(chan error, len(requests))
	var wg sync.WaitGroup
	for _, request := range requests {
		wg.Go(func() {
			in := input()
			in.NodeID = request.nodeID
			_, err := db.CreateBilledTask(ctx, testUserID, request.id, "image", in, quote.Points, quote.PriceVersion)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	successes, insufficient := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrCreditsInsufficient):
			insufficient++
		default:
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	account, err := db.Credits(ctx, testUserID)
	if err != nil || successes != 1 || insufficient != 1 || account != (CreditAccount{Reserved: 6}) {
		t.Fatalf("overdraw: successes=%d insufficient=%d account=%+v err=%v", successes, insufficient, account, err)
	}
}

func TestCreditsConcurrentSameTaskIdReservesOnce(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.GrantCredits(ctx, testUserID, 6, "credits-same-task-grant"); err != nil {
		t.Fatal(err)
	}
	quote, err := db.QuoteCredits(ctx, testUserID, "image", input())
	if err != nil {
		t.Fatal(err)
	}
	id := "35353535-3535-4353-8353-353535353535"
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			task, err := db.CreateBilledTask(ctx, testUserID, id, "image", input(), quote.Points, quote.PriceVersion)
			if err != nil || task.ID != id {
				t.Errorf("concurrent retry failed: task=%+v error=%v", task, err)
			}
		})
	}
	wg.Wait()
	account, err := db.Credits(ctx, testUserID)
	if err != nil || account != (CreditAccount{Reserved: 6}) {
		t.Fatalf("duplicate reservation: %+v %v", account, err)
	}
	entries, _, err := db.CreditEntries(ctx, testUserID, 20, 0)
	if err != nil || len(entries) != 2 {
		t.Fatalf("duplicate ledger entry: %+v %v", entries, err)
	}
}

func TestCreditsUncertainProviderResultStaysReserved(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.GrantCredits(ctx, testUserID, 6, "credits-uncertain-grant"); err != nil {
		t.Fatal(err)
	}
	quote, err := db.QuoteCredits(ctx, testUserID, "image", input())
	if err != nil {
		t.Fatal(err)
	}
	id := "37373737-3737-4373-8373-373737373737"
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", input(), quote.Points, quote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE generation_tasks SET status='running' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	db.finish(ctx, id, "interrupted", nil, &ark.APIError{Code: "SUBMISSION_UNKNOWN"})
	db.finish(ctx, id, "failed", nil, &ark.APIError{Code: "GENERATION_REJECTED"})
	task, err := db.Task(ctx, id)
	account, accountErr := db.Credits(ctx, testUserID)
	if err != nil || accountErr != nil || task.Status != "interrupted" || task.CreditStatus != "review" || account != (CreditAccount{Reserved: 6}) {
		t.Fatalf("uncertain provider result was charged or released: task=%+v account=%+v errors=%v %v", task, account, err, accountErr)
	}
	entries, _, err := db.CreditEntries(ctx, testUserID, 20, 0)
	if err != nil || len(entries) != 2 {
		t.Fatalf("uncertain result changed ledger: %+v %v", entries, err)
	}
}

func TestCreditsTemporaryImageResultStaysReserved(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.GrantCredits(ctx, testUserID, 6, "credits-temporary-result-grant"); err != nil {
		t.Fatal(err)
	}
	quote, err := db.QuoteCredits(ctx, testUserID, "image", input())
	if err != nil {
		t.Fatal(err)
	}
	id := "38383838-3838-4383-8383-383838383838"
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", input(), quote.Points, quote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	db.finish(ctx, id, "succeeded", &Result{ImageResult: ark.ImageResult{URL: "https://provider.example.test/temporary.png", Model: ark.DefaultImageModel}, StorageError: "save failed"}, nil)
	task, err := db.Task(ctx, id)
	account, accountErr := db.Credits(ctx, testUserID)
	if err != nil || accountErr != nil || task.CreditStatus != "review" || account != (CreditAccount{Reserved: 6}) {
		t.Fatalf("temporary image should stay frozen: task=%+v account=%+v errors=%v %v", task, account, err, accountErr)
	}
	if _, err := db.CreateBilledTask(ctx, testUserID, "39393939-3939-4393-8393-393939393939", "image", input(), quote.Points, quote.PriceVersion); !errors.Is(err, ErrCreditsInsufficient) {
		t.Fatalf("temporary result allowed another paid call: %v", err)
	}
}

func TestCreditsVideoQuoteReferenceImage(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	videoNodeID := "26262626-2626-4262-8262-262626262626"
	snapshot := testSnapshot()
	snapshot.Nodes = append(snapshot.Nodes, Node{ID: videoNodeID, Data: NodeData{Kind: "video", Name: "video"}})
	if _, err := db.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	asset := storage.Asset{Key: "credits-reference-image", URL: "https://assets.example.test/reference.png", Kind: storage.Image, Scope: storage.Scope{CanvasID: canvasID, NodeID: nodeID}}
	if err := db.RecordAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	in := Input{CanvasID: canvasID, NodeID: videoNodeID, Model: ark.VideoModel, Prompt: "moving scene", Mode: "reference", Resolution: "480p", Ratio: "16:9", Duration: 5, References: []Reference{{NodeID: nodeID, AssetKey: asset.Key, Kind: "image"}}}
	quote, err := db.QuoteCredits(ctx, testUserID, "video", in)
	if err != nil || quote.Points != 82 {
		t.Fatalf("480p quote: %+v %v", quote, err)
	}
	in.Resolution = "720p"
	quote, err = db.QuoteCredits(ctx, testUserID, "video", in)
	if err != nil || quote.Points != 182 {
		t.Fatalf("720p quote: %+v %v", quote, err)
	}
	if _, err := db.CreateBilledTask(ctx, testUserID, "36363636-3636-4363-8363-363636363636", "video", in, quote.Points, quote.PriceVersion); !errors.Is(err, ErrCreditsInsufficient) {
		t.Fatalf("zero-balance video request accepted: %v", err)
	}
}
