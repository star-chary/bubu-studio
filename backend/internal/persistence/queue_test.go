package persistence

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
)

// Separate real users/canvases, sharing node UUIDs deliberately: node ownership
// is scoped by canvas, so one user's generation must not block another's.
func queueInputs(t *testing.T, db *Store, count int) []Input {
	t.Helper()
	ctx := context.Background()
	inputs := make([]Input, count)
	for i := range count {
		user := fmt.Sprintf("a0000000-0000-4000-8000-%012d", i)
		canvas := fmt.Sprintf("b0000000-0000-4000-8000-%012d", i)
		if _, err := db.Pool.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'unused')`, user, fmt.Sprintf("queue-%d@example.test", i)); err != nil {
			t.Fatal(err)
		}
		if err := db.CreateCanvas(ctx, canvas, "queue", user); err != nil {
			t.Fatal(err)
		}
		snapshot := testSnapshot()
		snapshot.Nodes[0].Data.Kind = "video"
		snapshot.Nodes = append(snapshot.Nodes, Node{ID: promptSourceID, Data: NodeData{Kind: "image", Name: "source", Origin: "upload"}})
		if _, err := db.SaveCanvas(ctx, canvas, 0, snapshot); err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprintf("source-%d", i)
		if err := db.RecordAsset(ctx, storage.Asset{Key: key, Kind: storage.Image, Scope: storage.Scope{CanvasID: canvas, NodeID: promptSourceID}}); err != nil {
			t.Fatal(err)
		}
		inputs[i] = Input{CanvasID: canvas, NodeID: nodeID, Prompt: fmt.Sprintf("job-%d", i), Model: ark.VideoModel, Mode: "reference", Resolution: "480p", Ratio: "16:9", Duration: 4, References: []Reference{{promptSourceID, key, "image"}}}
	}
	return inputs
}

func enqueueVideos(t *testing.T, db *Store, inputs []Input) []string {
	t.Helper()
	ids := make([]string, len(inputs))
	var group sync.WaitGroup
	for i, input := range inputs {
		ids[i] = fmt.Sprintf("c0000000-0000-4000-8000-%012d", i)
		group.Add(1)
		go func(id string, in Input) {
			defer group.Done()
			task, err := db.CreateVideoTask(context.Background(), id, in)
			if err != nil || task.Status != "queued" {
				t.Errorf("concurrent enqueue: %s %v", task.Status, err)
			}
		}(ids[i], input)
	}
	group.Wait()
	return ids
}

type concurrentVideoFake struct {
	videoFake
	started                                              chan string
	finishOne                                            chan struct{}
	saveGate                                             chan struct{}
	active, maximum, transfers, maxTransfers, imageCalls atomic.Int32
	mu                                                   sync.Mutex
	created                                              map[string]int
	failID                                               string
}

func newConcurrentVideoFake() *concurrentVideoFake {
	return &concurrentVideoFake{started: make(chan string, 32), finishOne: make(chan struct{}, 32), saveGate: make(chan struct{}), created: map[string]int{}}
}

func maximum(counter *atomic.Int32, n int32) {
	for previous := counter.Load(); n > previous; previous = counter.Load() {
		if counter.CompareAndSwap(previous, n) {
			return
		}
	}
}

func (f *concurrentVideoFake) CreateVideo(_ context.Context, in ark.VideoRequest) (string, error) {
	id := "cgt-" + in.Prompt
	f.mu.Lock()
	f.created[id]++
	f.mu.Unlock()
	f.creates.Add(1)
	f.started <- id
	return id, nil
}

func (f *concurrentVideoFake) QueryVideo(ctx context.Context, id string) (ark.VideoResponse, error) {
	f.queries.Add(1)
	maximum(&f.maximum, f.active.Add(1))
	defer f.active.Add(-1)
	if id == f.failID {
		return ark.VideoResponse{ID: id, Status: "failed"}, nil
	}
	select {
	case <-ctx.Done():
		return ark.VideoResponse{}, ctx.Err()
	case <-f.finishOne:
	}
	out := ark.VideoResponse{ID: id, Status: "succeeded", Model: ark.VideoModel, Duration: 4}
	out.Content.URL = "https://provider.test/" + id + ".mp4"
	return out, nil
}

func (f *concurrentVideoFake) Generate(context.Context, string, string, ...string) (ark.ImageResult, error) {
	f.imageCalls.Add(1)
	return ark.ImageResult{URL: "https://provider.test/image.png", Model: ark.DefaultImageModel}, nil
}

func (f *concurrentVideoFake) SaveGenerated(_ context.Context, _, _ string, scope storage.Scope) (storage.Asset, error) {
	return storage.Asset{Key: "image-output", URL: "/api/assets/content?key=image-output", Kind: storage.Image, Scope: scope}, nil
}

func (f *concurrentVideoFake) SaveGeneratedVideo(ctx context.Context, _, _ string, scope storage.Scope) (storage.Asset, error) {
	maximum(&f.maxTransfers, f.transfers.Add(1))
	defer f.transfers.Add(-1)
	select {
	case <-ctx.Done():
		return storage.Asset{}, ctx.Err()
	case <-f.saveGate:
	}
	f.saves.Add(1)
	key := "output-" + scope.CanvasID
	return storage.Asset{Key: key, URL: "/api/assets/content?key=" + key, Kind: storage.Video, Scope: scope, Source: storage.Generated, Media: &storage.MediaMetadata{Width: 854, Height: 480, DurationSeconds: 4}}, nil
}

func receiveStarted(t *testing.T, f *concurrentVideoFake) string {
	t.Helper()
	select {
	case id := <-f.started:
		return id
	case <-time.After(4 * time.Second):
		t.Fatal("next task did not start")
		return ""
	}
}

func TestPostgresQueueTenUsersThreeVideosAndRestart(t *testing.T) {
	db := testDB(t)
	inputs := queueInputs(t, db, 10)
	ids := enqueueVideos(t, db, inputs)
	if t.Failed() {
		return
	}
	for _, id := range ids {
		awaitVideo(t, db, id, "queued")
	}
	fake := newConcurrentVideoFake()
	stop := startWorker(t, db, fake, fake)
	t.Cleanup(func() { stop() })
	for range 3 {
		receiveStarted(t, fake)
	}
	// Exactly three jobs are claimed even while provider calls are slow.
	var queued, claimed int
	if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE status='queued'),count(*) FILTER (WHERE claimed_at IS NOT NULL) FROM generation_tasks`).Scan(&queued, &claimed); err != nil {
		t.Fatal(err)
	}
	if queued != 7 || claimed != 3 {
		t.Fatalf("queued=%d claimed=%d", queued, claimed)
	}

	// A blocked video lane must not block image generation.
	imageID := "d0000000-0000-4000-8000-000000000000"
	if _, err := db.CreateTask(context.Background(), imageID, input()); err != nil {
		t.Fatal(err)
	}
	awaitVideo(t, db, imageID, "succeeded")
	if fake.imageCalls.Load() != 1 {
		t.Fatal("image queue blocked by videos")
	}

	// One result enters a blocked transfer; the fourth video starts immediately.
	fake.finishOne <- struct{}{}
	receiveStarted(t, fake)
	if fake.saves.Load() != 0 {
		t.Fatal("unexpected completed transfer")
	}
	// Fill both transfer slots, then leave a third saved result queued for transfer.
	fake.finishOne <- struct{}{}
	receiveStarted(t, fake)
	fake.finishOne <- struct{}{}
	receiveStarted(t, fake)
	deadline := time.Now().Add(3 * time.Second)
	for fake.transfers.Load() != 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if fake.transfers.Load() != 2 || fake.maxTransfers.Load() > 2 {
		t.Fatal("transfer concurrency not bounded")
	}
	stop()
	// The same database survives executor restart: queued work starts, provider IDs
	// resume, saving resumes, but no already-created provider task is re-created.
	close(fake.saveGate)
	for range 20 {
		fake.finishOne <- struct{}{}
	}
	stop = startWorker(t, db, fake, fake)
	for _, id := range ids {
		awaitVideo(t, db, id, "succeeded")
	}
	stop()
	if fake.creates.Load() != 10 || fake.maximum.Load() > 3 || fake.saves.Load() != 10 {
		t.Fatalf("creates=%d maximum=%d saves=%d", fake.creates.Load(), fake.maximum.Load(), fake.saves.Load())
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for id, count := range fake.created {
		if count != 1 {
			t.Errorf("duplicate model submission %s: %d", id, count)
		}
	}
}

func TestPostgresQueueFailureReleasesSlot(t *testing.T) {
	db := testDB(t)
	ids := enqueueVideos(t, db, queueInputs(t, db, 4))
	fake := newConcurrentVideoFake()
	var failedID, firstPrompt string
	if err := db.Pool.QueryRow(context.Background(), `SELECT id::text,input->>'prompt' FROM generation_tasks ORDER BY created_at,id LIMIT 1`).Scan(&failedID, &firstPrompt); err != nil {
		t.Fatal(err)
	}
	fake.failID = "cgt-" + firstPrompt
	close(fake.saveGate)
	stop := startWorker(t, db, fake, fake)
	defer stop()
	for range 4 {
		receiveStarted(t, fake)
	}
	awaitVideo(t, db, failedID, "failed")
	for range 3 {
		fake.finishOne <- struct{}{}
	}
	for _, id := range ids {
		if id != failedID {
			awaitVideo(t, db, id, "succeeded")
		}
	}
	if fake.maximum.Load() > 3 {
		t.Fatal("video concurrency exceeded")
	}
}

func TestPostgresQueueClaimsAndNodeExclusion(t *testing.T) {
	db := testDB(t)
	inputs := queueInputs(t, db, 10)
	ids := enqueueVideos(t, db, inputs)
	var group sync.WaitGroup
	claims := make(chan string, 20)
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			task, err := db.claimTask(context.Background(), "video")
			if err == nil {
				claims <- task.ID
			} else if !errors.Is(err, ErrNotFound) {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	close(claims)
	seen := map[string]bool{}
	for id := range claims {
		if seen[id] {
			t.Fatal("task claimed twice")
		}
		seen[id] = true
	}
	if len(seen) != 10 {
		t.Fatalf("claims=%d", len(seen))
	}
	for i, id := range ids {
		if _, err := db.CreateVideoTask(context.Background(), id, inputs[i]); err != nil {
			t.Fatal("idempotency during preparation", err)
		}
		if _, err := db.CreateVideoTask(context.Background(), fmt.Sprintf("e0000000-0000-4000-8000-%012d", i), inputs[i]); !errors.Is(err, ErrBusy) {
			t.Fatal("same node not excluded", err)
		}
	}
	// Preparation is safe to recover, because it precedes paid submission.
	fake := newConcurrentVideoFake()
	close(fake.saveGate)
	for range 10 {
		fake.finishOne <- struct{}{}
	}
	stop := startWorker(t, db, fake, fake)
	defer stop()
	for _, id := range ids {
		awaitVideo(t, db, id, "succeeded")
	}
	if fake.creates.Load() != 10 {
		t.Fatal("preparation recovery lost tasks")
	}
}

func TestWorkerConfigPersonalVideoCap(t *testing.T) {
	for key, value := range map[string]string{"VIDEO_GENERATION_CONCURRENCY": "3", "IMAGE_GENERATION_CONCURRENCY": "1", "ASSET_SAVE_CONCURRENCY": "2"} {
		t.Setenv(key, value)
	}
	if c, err := WorkerConfigFromEnv(); err != nil || c != DefaultWorkerConfig() {
		t.Fatal(c, err)
	}
	for _, value := range []string{"0", "4", "-1", "invalid"} {
		t.Setenv("VIDEO_GENERATION_CONCURRENCY", value)
		if _, err := WorkerConfigFromEnv(); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}

func TestPostgresQueueMigrationPreservesTask(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	id := "f0000000-0000-4000-8000-000000000000"
	if _, err := db.CreateTask(ctx, id, input()); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the previous global constraint, keeping the existing data.
	_, err := db.Pool.Exec(ctx, `DROP INDEX generation_node_active; DROP INDEX generation_dispatch_queue;
		ALTER TABLE generation_tasks DROP COLUMN claimed_at;
		CREATE UNIQUE INDEX generation_one_active ON generation_tasks ((1)) WHERE status IN ('queued','submitting','running','saving');
		DELETE FROM schema_migrations WHERE version='005_generation_queue.sql'`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	task, err := db.Task(ctx, id)
	if err != nil || task.Status != "queued" || task.Input.Prompt != input().Prompt {
		t.Fatal("migration changed existing task", err)
	}
	if err := db.migrate(ctx); err != nil {
		t.Fatal("migration not idempotent", err)
	}
	inputs := queueInputs(t, db, 1)
	if _, err := db.CreateVideoTask(ctx, "f0000000-0000-4000-8000-000000000001", inputs[0]); err != nil {
		t.Fatal("global constraint still present", err)
	}
}

func TestPostgresQueueWaitDoesNotExpireProviderWindow(t *testing.T) {
	db, in, id := videoFixture(t)
	if _, err := db.CreateVideoTask(context.Background(), id, in); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(context.Background(), `UPDATE generation_tasks SET created_at=now()-interval '72 hours' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	fake := &videoFake{}
	stop := startWorker(t, db, fake, fake)
	defer stop()
	awaitVideo(t, db, id, "succeeded")
	if fake.creates.Load() != 1 || fake.queries.Load() != 1 {
		t.Fatal("local queue time counted as provider execution time")
	}
}
