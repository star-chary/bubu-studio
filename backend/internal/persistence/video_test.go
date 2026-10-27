package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
)

func TestCompilePromptOrderedIdentity(t *testing.T) {
	refs := []Reference{{NodeID: promptSourceID, AssetKey: "image-b", Kind: "image"}, {NodeID: "40404040-4040-4040-8040-404040404040", AssetKey: "video", Kind: "video"}, {NodeID: "50505050-5050-4050-8050-505050505050", AssetKey: "image-a", Kind: "image"}}
	parts := []PromptPart{{Type: "reference", NodeID: refs[2].NodeID, Kind: "image", Name: "重名"}, {Type: "text", Text: " @literal "}, {Type: "reference", NodeID: refs[1].NodeID, Kind: "video", Name: "镜头"}, {Type: "reference", NodeID: refs[2].NodeID, Kind: "image", Name: "重名"}}
	in := Input{NodeID: nodeID, Prompt: "@重名 @literal @镜头@重名", PromptParts: parts}
	got, err := CompilePrompt(in, "video", refs)
	if err != nil || got != "图片2 @literal 视频1图片2" {
		t.Fatal(got, err)
	}
	if _, err = CompilePrompt(in, "video", refs[:2]); !errors.Is(err, ErrPromptReferences) {
		t.Fatal("missing source accepted", err)
	}
	if _, err = CompilePrompt(in, "image", refs); err == nil {
		t.Fatal("video in image prompt accepted")
	}
}

type videoFake struct {
	creates, queries, saves atomic.Int32
	createError             error
	saveError               atomic.Bool
	query                   func(context.Context) (ark.VideoResponse, error)
}

func (v *videoFake) Generate(context.Context, string, string, *ark.ImageOptions, ...string) (ark.ImageResult, error) {
	panic("unexpected image generation")
}
func (v *videoFake) CreateVideo(_ context.Context, in ark.VideoRequest) (string, error) {
	v.creates.Add(1)
	return "cgt-test", v.createError
}
func (v *videoFake) QueryVideo(ctx context.Context, id string) (ark.VideoResponse, error) {
	v.queries.Add(1)
	if v.query != nil {
		return v.query(ctx)
	}
	r := ark.VideoResponse{ID: id, Status: "succeeded", Model: ark.VideoModel, Duration: 4}
	r.Content.URL = "https://provider.test/video.mp4?private=yes"
	return r, nil
}
func (v *videoFake) ReferenceURLs(context.Context, storage.Scope, []string) ([]string, error) {
	panic("unexpected image reference")
}
func (v *videoFake) SaveGenerated(context.Context, string, string, storage.Scope) (storage.Asset, error) {
	panic("unexpected image save")
}
func (v *videoFake) VideoReferenceURLsWithLimits(context.Context, storage.Scope, []string, storage.VideoReferenceLimits) ([]string, error) {
	return []string{"https://private.test/ref"}, nil
}
func (v *videoFake) SaveGeneratedVideo(_ context.Context, u, m string, scope storage.Scope) (storage.Asset, error) {
	v.saves.Add(1)
	if v.saveError.Load() {
		return storage.Asset{}, errors.New("private details")
	}
	return storage.Asset{Key: "output", URL: "/api/assets/content?key=output", Kind: storage.Video, Scope: scope, Source: storage.Generated, Media: &storage.MediaMetadata{Width: 854, Height: 480, DurationSeconds: 4, VideoCodec: "h264", AudioCodec: "aac"}}, nil
}
func videoFixture(t *testing.T) (*Store, Input, string) {
	db := testDB(t)
	ctx := context.Background()
	snap := testSnapshot()
	snap.Nodes[0].Data.Kind = "video"
	snap.Nodes = append(snap.Nodes, Node{ID: promptSourceID, Data: NodeData{Kind: "image", Name: "source", Origin: "upload"}})
	if _, err := db.SaveCanvas(ctx, canvasID, 1, snap); err != nil {
		t.Fatal(err)
	}
	a := storage.Asset{Key: "source", Kind: storage.Image, Scope: storage.Scope{CanvasID: canvasID, NodeID: promptSourceID}}
	if err := db.RecordAsset(ctx, a); err != nil {
		t.Fatal(err)
	}
	audio := false
	in := Input{CanvasID: canvasID, NodeID: nodeID, Model: ark.VideoModel, Mode: "reference", Resolution: "480p", Ratio: "16:9", Duration: 4, GenerateAudio: &audio, References: []Reference{{NodeID: promptSourceID, AssetKey: "source", Kind: "image"}}}
	return db, in, "60606060-6060-4060-8060-606060606060"
}
func awaitVideo(t *testing.T, db *Store, id, status string) Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := db.Task(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == status {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := db.Task(context.Background(), id)
	t.Fatalf("want %s got %+v", status, task)
	return task
}
func TestVideoWorkerStorageRetryDoesNotRegenerate(t *testing.T) {
	db, in, id := videoFixture(t)
	ctx := context.Background()
	if _, err := db.CreateVideoTask(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	fake := &videoFake{}
	fake.saveError.Store(true)
	stop := startWorker(t, db, fake, fake)
	task := awaitVideo(t, db, id, "storage_failed")
	stop()
	raw, _ := json.Marshal(task)
	if strings.Contains(string(raw), "provider.test") || strings.Contains(string(raw), "private=yes") {
		t.Fatal("provider URL leaked")
	}
	if _, err := db.RetryVideoStorage(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RetryVideoStorage(ctx, id); err != nil {
		t.Fatal("retry not idempotent", err)
	}
	fake.saveError.Store(false)
	stop = startWorker(t, db, fake, fake)
	defer stop()
	task = awaitVideo(t, db, id, "succeeded")
	if fake.creates.Load() != 1 || fake.queries.Load() != 1 || fake.saves.Load() != 2 || task.Result.Asset == nil || task.Result.DurationSeconds != 4 {
		t.Fatalf("unexpected calls/result: %d %d %d %+v", fake.creates.Load(), fake.queries.Load(), fake.saves.Load(), task)
	}
	if _, err := db.RetryVideoStorage(ctx, id); err != nil {
		t.Fatal(err)
	}
	// Existing task resolution precedes validation against a mutable canvas.
	_, _ = db.SaveCanvas(ctx, canvasID, 2, testSnapshot())
	if _, err := db.CreateVideoTask(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	changed := in
	changed.Duration = 5
	if _, err := db.CreateVideoTask(ctx, id, changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}
func TestVideoWorkerRestartResumesIDAndInterruptsUnknownSubmission(t *testing.T) {
	for _, status := range []string{"running", "submitting", "saving"} {
		t.Run(status, func(t *testing.T) {
			db, in, id := videoFixture(t)
			ctx := context.Background()
			_, err := db.CreateVideoTask(ctx, id, in)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Pool.Exec(ctx, `UPDATE generation_tasks SET status=$2,provider_task_id='cgt-test',provider_url='https://provider.test/video.mp4',result=$3 WHERE id=$1`, id, status, &Result{ImageResult: ark.ImageResult{Model: ark.VideoModel}})
			if err != nil {
				t.Fatal(err)
			}
			fake := &videoFake{}
			stop := startWorker(t, db, fake, fake)
			defer stop()
			wanted := "succeeded"
			if status == "submitting" {
				wanted = "interrupted"
			}
			task := awaitVideo(t, db, id, wanted)
			if fake.creates.Load() != 0 {
				t.Fatal("restart created another video")
			}
			if status == "saving" && fake.queries.Load() != 0 {
				t.Fatal("saving should only save")
			}
			if status == "running" && (task.ProviderTaskID != "cgt-test" || fake.queries.Load() != 1) {
				t.Fatal("did not recover provider ID")
			}
		})
	}
}
func TestVideoAmbiguousCreateAndInvalidBindings(t *testing.T) {
	db, in, id := videoFixture(t)
	ctx := context.Background()
	bad := in
	bad.References = []Reference{{NodeID: nodeID, AssetKey: "source", Kind: "image"}}
	if _, err := db.CreateVideoTask(ctx, id, bad); err == nil {
		t.Fatal("wrong node accepted")
	}
	bad.References = []Reference{{NodeID: promptSourceID, AssetKey: "source", Kind: "video"}}
	if _, err := db.CreateVideoTask(ctx, id, bad); err == nil {
		t.Fatal("wrong media accepted")
	}
	bad.References = append(in.References, in.References[0])
	if _, err := db.CreateVideoTask(ctx, id, bad); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := db.CreateVideoTask(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	fake := &videoFake{createError: &ark.APIError{Code: "SUBMISSION_UNKNOWN", Message: "unknown"}}
	stop := startWorker(t, db, fake, fake)
	awaitVideo(t, db, id, "interrupted")
	stop()
	stop = startWorker(t, db, fake, fake)
	defer stop()
	if fake.creates.Load() != 1 || fake.queries.Load() != 0 {
		t.Fatal("ambiguous create retried")
	}
}

func TestVideoDetailedFailuresPersistAndReleaseCreditsOnce(t *testing.T) {
	for _, phase := range []string{"submission", "generation"} {
		t.Run(phase, func(t *testing.T) {
			db, in, id := videoFixture(t)
			ctx := context.Background()
			if err := db.GrantCredits(ctx, testUserID, 1000, "video-errors-test-grant"); err != nil {
				t.Fatal(err)
			}
			quote, err := db.QuoteCredits(ctx, testUserID, "video", in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.CreateBilledTask(ctx, testUserID, id, "video", in, quote.Points, quote.PriceVersion); err != nil {
				t.Fatal(err)
			}
			failure := &ark.APIError{Code: "VIDEO_REJECTED_IMAGE_PRIVACY", Message: "参考图片 1被模型判定可能包含真人，未通过人像审核。"}
			fake := &videoFake{createError: failure}
			if phase == "generation" {
				failure = &ark.APIError{Code: "PROVIDER_OUTPUT_SENSITIVE", Message: "生成的视频未通过模型内容审核。"}
				fake.createError = nil
				fake.query = func(context.Context) (ark.VideoResponse, error) {
					return ark.VideoResponse{ID: "cgt-test", Status: "failed", Failure: failure}, nil
				}
			}
			stop := startWorker(t, db, fake, fake)
			task := awaitVideo(t, db, id, "failed")
			stop()
			// Duplicate completion must not return the reservation a second time.
			db.finish(ctx, id, "failed", nil, failure)
			account, err := db.Credits(ctx, testUserID)
			if err != nil || account != (CreditAccount{Available: 1000}) || task.CreditStatus != "released" || task.Error == nil || task.Error.Code != failure.Code || task.Error.Message != failure.Message {
				t.Fatalf("wrong persisted error/refund: task=%+v account=%+v err=%v", task, account, err)
			}
			var releases int
			if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_ledger WHERE task_id=$1 AND operation='release'`, id).Scan(&releases); err != nil || releases != 1 || fake.creates.Load() != 1 || fake.saves.Load() != 0 {
				t.Fatalf("wrong call/refund count: releases=%d creates=%d saves=%d err=%v", releases, fake.creates.Load(), fake.saves.Load(), err)
			}
		})
	}
}
