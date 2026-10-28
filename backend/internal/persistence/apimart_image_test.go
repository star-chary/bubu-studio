package persistence

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"frame-space/backend/internal/apimart"
	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
)

type imageProviderFake struct {
	creates, queries, saves atomic.Int32
	block                   atomic.Bool
	entered                 chan struct{}
	createError             error
}

func (f *imageProviderFake) Generate(context.Context, string, string, *ark.ImageOptions, ...string) (ark.ImageResult, error) {
	panic("APIMart must not call Ark")
}
func (f *imageProviderFake) APIMartImageGenerator() APIMartImageGenerator { return f }
func (f *imageProviderFake) CreateImage(context.Context, apimart.ImageRequest) (string, error) {
	f.creates.Add(1)
	return "task_123", f.createError
}
func (f *imageProviderFake) QueryImage(ctx context.Context, id, model string) (apimart.ImageResponse, error) {
	f.queries.Add(1)
	if f.block.Load() {
		select {
		case f.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return apimart.ImageResponse{}, ctx.Err()
	}
	return apimart.ImageResponse{Status: "succeeded", URL: "https://provider.test/image.png"}, nil
}
func (f *imageProviderFake) ReferenceURLs(context.Context, storage.Scope, []string) ([]string, error) {
	panic("unexpected reference")
}
func (f *imageProviderFake) SaveGenerated(_ context.Context, _ string, _ string, scope storage.Scope) (storage.Asset, error) {
	f.saves.Add(1)
	return storage.Asset{Key: "image-output", URL: "/api/assets/content?key=image-output", Kind: storage.Image, Scope: scope, Source: storage.Generated}, nil
}

func imageTaskFixture(t *testing.T) (*Store, string) {
	t.Helper()
	db := testDB(t)
	snapshot := testSnapshot()
	snapshot.Nodes[0].Data.ModelSelection = &ModelSelection{Provider: ProviderAPIMart, ModelID: "seedream-5-0-lite", Parameters: map[string]any{}}
	if _, err := db.SaveCanvas(context.Background(), canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	id := "60606060-6060-4060-8060-606060606060"
	in := Input{Provider: ProviderAPIMart, Model: "seedream-5-0-lite", Prompt: "paper boat", CanvasID: canvasID, NodeID: nodeID, Parameters: map[string]any{"size": "1:1", "resolution": "2K", "n": 1}}
	// Exercise recovery of a task created before APIMart credit billing.
	if _, err := db.CreateTask(context.Background(), id, in); err != nil {
		t.Fatal(err)
	}
	return db, id
}

func TestAPIMartImageWorkerResumesOriginalTaskAfterRestart(t *testing.T) {
	db, id := imageTaskFixture(t)
	fake := &imageProviderFake{entered: make(chan struct{}, 1)}
	fake.block.Store(true)
	stop := startWorker(t, db, fake, fake)
	select {
	case <-fake.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("query never started")
	}
	stop()
	fake.block.Store(false)
	stop = startWorker(t, db, fake, fake)
	defer stop()
	task := waitStatus(t, db, id, "succeeded")
	if fake.creates.Load() != 1 || fake.queries.Load() != 2 || fake.saves.Load() != 1 || task.Result == nil || task.Result.Asset == nil || task.CreditStatus != "legacy" {
		t.Fatalf("incorrect resumed image task: %+v creates=%d queries=%d saves=%d", task, fake.creates.Load(), fake.queries.Load(), fake.saves.Load())
	}
}

func TestAPIMartImageWorkerDoesNotRetryAmbiguousSubmission(t *testing.T) {
	db, id := imageTaskFixture(t)
	fake := &imageProviderFake{createError: &ark.APIError{Code: "SUBMISSION_UNKNOWN", Message: "unknown"}}
	stop := startWorker(t, db, fake, fake)
	task := waitStatus(t, db, id, "interrupted")
	stop()
	stop = startWorker(t, db, fake, fake)
	defer stop()
	if fake.creates.Load() != 1 || fake.queries.Load() != 0 || task.Error == nil || task.Error.Code != "SUBMISSION_UNKNOWN" {
		t.Fatal(task, fake.creates.Load())
	}
}
