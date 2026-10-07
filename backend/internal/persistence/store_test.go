package persistence

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testUserID = "99999999-9999-4999-8999-999999999999"
const canvasID = "10101010-1010-4010-8010-101010101010"
const nodeID = "20202020-2020-4020-8020-202020202020"

func testDB(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is required for real PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal("invalid test database")
	}
	schema := fmt.Sprintf("test_persistence_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{pool}
	if err = s.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); _, _ = admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); admin.Close() })
	if _, err = s.Pool.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'persistence@example.test','unused')`, testUserID); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateCanvas(ctx, canvasID, "未命名画布", testUserID); err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveCanvas(ctx, canvasID, 0, testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func testSnapshot() Snapshot {
	return Snapshot{Nodes: []Node{{ID: nodeID, Position: Position{12, 34}, Data: NodeData{Kind: "image", Name: "test", Prompt: "draft"}}}, Viewport: Viewport{X: 30, Y: 40, Zoom: 1.2}}
}
func input() Input {
	return Input{Prompt: "snapshot", Model: ark.DefaultImageModel, CanvasID: canvasID, NodeID: nodeID}
}

type generateFunc func(context.Context, string, string, ...string) (ark.ImageResult, error)

func (f generateFunc) Generate(c context.Context, p, m string, r ...string) (ark.ImageResult, error) {
	return f(c, p, m, r...)
}
func startWorker(t *testing.T, s *Store, g Generator, o ResultStorage) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan error, 1)
	done := make(chan struct{})
	go func() { defer close(done); _ = s.RunWorker(ctx, g, o, ready) }()
	if err := <-ready; err != nil {
		cancel()
		t.Fatal(err)
	}
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("worker did not stop")
		}
	}
}
func waitStatus(t *testing.T, s *Store, id, want string) Task {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		task, err := s.Task(context.Background(), id)
		if err == nil && task.Status == want {
			return task
		}
		time.Sleep(30 * time.Millisecond)
	}
	task, _ := s.Task(context.Background(), id)
	t.Fatalf("wanted %s, got %+v", want, task)
	return task
}
func TestSnapshotValidation(t *testing.T) {
	s := testSnapshot()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.Nodes[0].Data.ReferenceIDs = []string{nodeID}
	if s.Validate() == nil {
		t.Fatal("self reference accepted")
	}
	s = testSnapshot()
	s.Nodes = append(s.Nodes, s.Nodes[0])
	if s.Validate() == nil {
		t.Fatal("duplicate node accepted")
	}
	s = testSnapshot()
	s.Viewport.Zoom = 0
	if s.Validate() == nil {
		t.Fatal("invalid viewport accepted")
	}
}
func TestPostgresSaveConflictAndRoundTrip(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	c, err := s.Canvas(ctx, canvasID)
	if err != nil || c.Version != 1 || c.Snapshot.Nodes[0].Data.Prompt != "draft" {
		t.Fatalf("round trip: %+v %v", c, err)
	}
	snapshot := testSnapshot()
	snapshot.Nodes[0].Data.Prompt = "new draft"
	var successes atomic.Int32
	var conflicts atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := s.SaveCanvas(ctx, canvasID, 1, snapshot)
			if err == nil {
				successes.Add(1)
			} else if errors.Is(err, ErrConflict) {
				conflicts.Add(1)
			} else {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != 7 {
		t.Fatal("optimistic version check failed")
	}
	c, err = s.Canvas(ctx, canvasID)
	if err != nil || c.Version != 2 || c.Snapshot.Nodes[0].Data.Prompt != "new draft" {
		t.Fatal("saved snapshot lost")
	}
}
func TestPostgresIdempotencyAndSingleActiveTaskPerNode(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	id := "30303030-3030-4030-8030-303030303030"
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			task, err := s.CreateTask(ctx, id, input())
			if err != nil || task.ID != id {
				t.Errorf("duplicate submission: %v", err)
			}
		})
	}
	wg.Wait()
	different := input()
	different.Prompt = "different"
	if _, err := s.CreateTask(ctx, id, different); !errors.Is(err, ErrConflict) {
		t.Fatal("id reused with different input")
	}
	if _, err := s.CreateTask(ctx, "40404040-4040-4040-8040-404040404040", input()); !errors.Is(err, ErrBusy) {
		t.Fatal("second active task on the same node accepted")
	}
	tasks, err := s.Tasks(ctx, canvasID, 20, 0)
	if err != nil || len(tasks) != 1 {
		t.Fatal("idempotency created duplicate rows")
	}
}
func TestPostgresWorkerInputSnapshotAndCompletion(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	id := "30303030-3030-4030-8030-303030303030"
	_, err := s.CreateTask(ctx, id, input())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot()
	snapshot.Nodes[0].Data.Prompt = "edited after submit"
	_, _ = s.SaveCanvas(ctx, canvasID, 1, snapshot)
	var calls atomic.Int32
	stop := startWorker(t, s, generateFunc(func(ctx context.Context, p, m string, _ ...string) (ark.ImageResult, error) {
		calls.Add(1)
		task, e := s.Task(ctx, id)
		if e != nil || task.Status != "running" || p != "snapshot" {
			t.Error("model ran before task persisted or input mutated")
		}
		return ark.ImageResult{URL: "https://example.test/result.png", Model: m}, nil
	}), nil)
	defer stop()
	task := waitStatus(t, s, id, "succeeded")
	if calls.Load() != 1 || task.Result == nil || task.Result.StorageError == "" || task.StartedAt == nil || task.FinishedAt == nil {
		t.Fatal("result/status/timing missing")
	}
	canvas, err := s.Canvas(ctx, canvasID)
	if err != nil || len(canvas.Results) != 1 || len(canvas.Tasks) != 1 || canvas.Snapshot.Nodes[0].Data.Prompt != "edited after submit" {
		t.Fatal("canvas restoration missing result or overwrote draft")
	}
}
func TestPostgresRestartDoesNotRegenerateInterruptedTask(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	id := "30303030-3030-4030-8030-303030303030"
	_, _ = s.CreateTask(ctx, id, input())
	entered := make(chan struct{})
	var calls atomic.Int32
	g := generateFunc(func(ctx context.Context, _, _ string, _ ...string) (ark.ImageResult, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		return ark.ImageResult{}, ctx.Err()
	})
	stop := startWorker(t, s, g, nil)
	<-entered
	// A second backend must not mark the first backend's live task interrupted.
	ready := make(chan error, 1)
	done := make(chan error, 1)
	go func() { done <- s.RunWorker(ctx, g, nil, ready) }()
	if <-ready == nil {
		t.Fatal("second worker acquired singleton lock")
	}
	<-done
	task, _ := s.Task(ctx, id)
	if task.Status != "running" {
		t.Fatal("second worker changed live task")
	}
	stop()
	stop = startWorker(t, s, g, nil)
	defer stop()
	task = waitStatus(t, s, id, "interrupted")
	if calls.Load() != 1 || task.Error.Code != "WORKER_INTERRUPTED" {
		t.Fatal("interrupted task regenerated")
	}
}

type fakeObjects struct {
	calls atomic.Int32
	fail  bool
}

func (f *fakeObjects) ReferenceURLs(context.Context, storage.Scope, []string) ([]string, error) {
	return []string{"https://example.test/reference.png"}, nil
}
func (f *fakeObjects) SaveGenerated(_ context.Context, _ string, _ string, scope storage.Scope) (storage.Asset, error) {
	f.calls.Add(1)
	if f.fail {
		return storage.Asset{}, errors.New("save failed")
	}
	return storage.Asset{Key: "test/generated.png", URL: "/api/assets/content?key=test%2Fgenerated.png", Kind: storage.Image, Source: storage.Generated, Scope: scope, Bytes: 123, ContentType: "image/png"}, nil
}
func TestPostgresResumeSavingWithoutModelAndRecordAsset(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	id := "30303030-3030-4030-8030-303030303030"
	_, _ = s.CreateTask(ctx, id, input())
	result := Result{ImageResult: ark.ImageResult{URL: "https://example.test/generated.png", Model: ark.DefaultImageModel}}
	_, err := s.Pool.Exec(ctx, `UPDATE generation_tasks SET status='saving',result=$2 WHERE id=$1`, id, result)
	if err != nil {
		t.Fatal(err)
	}
	objects := &fakeObjects{}
	stop := startWorker(t, s, generateFunc(func(context.Context, string, string, ...string) (ark.ImageResult, error) {
		t.Error("model called while recovering saved result")
		return ark.ImageResult{}, nil
	}), objects)
	defer stop()
	task := waitStatus(t, s, id, "succeeded")
	if task.Result.Asset == nil || objects.calls.Load() != 1 {
		t.Fatal("asset not saved")
	}
	c, err := s.Canvas(ctx, canvasID)
	if err != nil || len(c.Assets) != 1 || c.Assets[0].Key != task.Result.Asset.Key {
		t.Fatal("asset and task not committed together")
	}
}
func TestPostgresFailureAndStorageFailureRemainTraceable(t *testing.T) {
	for _, modelFails := range []bool{true, false} {
		t.Run(fmt.Sprint(modelFails), func(t *testing.T) {
			s := testDB(t)
			ctx := context.Background()
			id := "30303030-3030-4030-8030-303030303030"
			_, _ = s.CreateTask(ctx, id, input())
			var calls atomic.Int32
			stop := startWorker(t, s, generateFunc(func(context.Context, string, string, ...string) (ark.ImageResult, error) {
				calls.Add(1)
				if modelFails {
					return ark.ImageResult{}, &ark.APIError{Code: "MODEL_LIMITED", Message: "额度不足"}
				}
				return ark.ImageResult{URL: "https://example.test/a.png", Model: ark.DefaultImageModel}, nil
			}), &fakeObjects{fail: true})
			defer stop()
			want := "succeeded"
			if modelFails {
				want = "failed"
			}
			task := waitStatus(t, s, id, want)
			if calls.Load() != 1 {
				t.Fatal("model retried")
			}
			if modelFails {
				if task.Error.Code != "MODEL_LIMITED" {
					t.Fatal("lost failure")
				}
			} else if task.Result.URL == "" || task.Result.StorageError == "" {
				t.Fatal("lost temporary result")
			}
		})
	}
}
