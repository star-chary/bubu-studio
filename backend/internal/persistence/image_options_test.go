package persistence

import (
	"context"
	"errors"
	"testing"

	"frame-space/backend/internal/ark"
)

type imageOptionsGenerator struct{ received chan ark.ImageOptions }

func (g imageOptionsGenerator) Generate(_ context.Context, _, model string, options *ark.ImageOptions, _ ...string) (ark.ImageResult, error) {
	if options != nil {
		g.received <- *options
	}
	return ark.ImageResult{URL: "https://images.example.test/result.png", Model: model}, nil
}

func TestImageOptionsSnapshotValidation(t *testing.T) {
	for _, tc := range []struct {
		model, resolution, ratio string
		count                    int
		valid                    bool
	}{
		{ark.DefaultImageModel, "4K", "21:9", 4, true}, {ark.ProImageModel, "1.5K", "9:16", 2, true},
		{ark.ProImageModel, "4K", "1:1", 1, false}, {ark.DefaultImageModel, "1K", "1:1", 1, false},
		{ark.DefaultImageModel, "2K", "other", 1, false}, {ark.DefaultImageModel, "2K", "1:1", 3, false},
	} {
		snapshot := testSnapshot()
		snapshot.Nodes[0].Data.ImageModel = tc.model
		snapshot.Nodes[0].Data.ImageOptions = &ImageNodeOptions{ark.ImageOptions{Resolution: tc.resolution, Ratio: tc.ratio}, tc.count}
		if err := snapshot.Validate(); (err == nil) != tc.valid {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}

func TestImageOptionsPersistAndReachWorkerWithCorrectQuote(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	for _, tc := range []struct {
		model, resolution string
		points            int64
	}{
		{ark.DefaultImageModel, "4K", 6}, {ark.ProImageModel, "1K", 8}, {ark.ProImageModel, "1.5K", 8}, {ark.ProImageModel, "2K", 16},
	} {
		in := input()
		in.Model = tc.model
		in.ImageOptions = &ark.ImageOptions{Resolution: tc.resolution, Ratio: "9:16"}
		quote, err := db.QuoteCredits(ctx, testUserID, "image", in)
		if err != nil || quote.Points != tc.points {
			t.Fatalf("quote for %+v: %+v %v", tc, quote, err)
		}
	}
	in := input()
	in.Model = ark.ProImageModel
	in.ImageOptions = &ark.ImageOptions{Resolution: "4K", Ratio: "1:1"}
	if _, err := db.QuoteCredits(ctx, testUserID, "image", in); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid model options quoted", err)
	}
	in.ImageOptions = &ark.ImageOptions{Resolution: "1.5K", Ratio: "9:16"}
	snapshot := testSnapshot()
	snapshot.Nodes[0].Data.ImageModel = in.Model
	snapshot.Nodes[0].Data.ImageOptions = &ImageNodeOptions{*in.ImageOptions, 4}
	if _, err := db.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	canvas, err := db.Canvas(ctx, canvasID)
	if err != nil || canvas.Snapshot.Nodes[0].Data.ImageOptions.Count != 4 {
		t.Fatal("lost node options", err)
	}
	if err := db.GrantCredits(ctx, testUserID, 100, "image-options-test"); err != nil {
		t.Fatal(err)
	}
	id := "71717171-7171-4171-8171-717171717171"
	quote, _ := db.QuoteCredits(ctx, testUserID, "image", in)
	task, err := db.CreateBilledTask(ctx, testUserID, id, "image", in, quote.Points, quote.PriceVersion)
	if err != nil || task.Input.ImageOptions == nil || *task.Input.ImageOptions != *in.ImageOptions {
		t.Fatal("lost task options", err)
	}
	changed := in
	changed.ImageOptions = &ark.ImageOptions{Resolution: "2K", Ratio: "9:16"}
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", changed, 16, quote.PriceVersion); !errors.Is(err, ErrConflict) {
		t.Fatal("task ID reused with different size", err)
	}
	generator := imageOptionsGenerator{received: make(chan ark.ImageOptions, 1)}
	stop := startWorker(t, db, generator, &fakeObjects{})
	defer stop()
	task = waitStatus(t, db, id, "succeeded")
	select {
	case options := <-generator.received:
		if options != *in.ImageOptions {
			t.Fatal("worker received different size", options)
		}
	default:
		t.Fatal("worker lost options")
	}
	if task.CreditPoints != 8 || task.CreditStatus != "settled" {
		t.Fatalf("wrong settlement: %+v", task)
	}
}
