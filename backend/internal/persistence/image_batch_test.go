package persistence

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"frame-space/backend/internal/apimart"
	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/storage"
)

type nanoBatchFake struct {
	creates  atomic.Int32
	queries  atomic.Int32
	urls     []string
	failure  error
	failSave string
}

func (f *nanoBatchFake) Generate(context.Context, string, string, *ark.ImageOptions, ...string) (ark.ImageResult, error) {
	panic("Nano must not use Ark")
}
func (f *nanoBatchFake) APIMartImageGenerator() APIMartImageGenerator { return f }
func (f *nanoBatchFake) CreateImage(_ context.Context, input apimart.ImageRequest) (string, error) {
	f.creates.Add(1)
	if input.Model != nanoImageModel || (integerParameter(input.Parameters["n"]) != 2 && integerParameter(input.Parameters["n"]) != 4) {
		return "", &ark.APIError{Code: "INVALID_IMAGE_INPUT", Message: "unexpected model or count"}
	}
	return "task_nano", f.failure
}
func (f *nanoBatchFake) QueryImage(context.Context, string, string) (apimart.ImageResponse, error) {
	f.queries.Add(1)
	return apimart.ImageResponse{Status: "succeeded", URLs: f.urls}, nil
}
func (f *nanoBatchFake) ReferenceURLs(context.Context, storage.Scope, []string) ([]string, error) {
	panic("unexpected reference")
}
func (f *nanoBatchFake) SaveGenerated(_ context.Context, _ string, _ string, scope storage.Scope) (storage.Asset, error) {
	if scope.NodeID == f.failSave {
		return storage.Asset{}, errors.New("simulated save failure")
	}
	key := "nano-output-" + scope.NodeID
	return storage.Asset{Key: key, URL: "/api/assets/content?key=" + key, Kind: storage.Image, Bytes: 100, Source: storage.Generated, Scope: scope}, nil
}

func nanoBatchFixture(t *testing.T, count int) (*Store, Input, string, CreditQuote) {
	t.Helper()
	db := testDB(t)
	ctx := context.Background()
	snapshot := testSnapshot()
	snapshot.Nodes[0].Data.ModelSelection = &ModelSelection{Provider: ProviderAPIMart, ModelID: nanoImageModel, Parameters: map[string]any{"n": count}}
	ids := []string{nodeID}
	for i := 1; i < count; i++ {
		id := identity.ID()
		ids = append(ids, id)
		snapshot.Nodes = append(snapshot.Nodes, Node{ID: id, Data: NodeData{Kind: "image", Name: fmt.Sprintf("output %d", i+1), ModelSelection: &ModelSelection{Provider: ProviderAPIMart, ModelID: nanoImageModel, Parameters: map[string]any{"n": 1}}}})
	}
	if _, err := db.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := db.GrantCredits(ctx, testUserID, 200, "nano-batch-test"); err != nil {
		t.Fatal(err)
	}
	input := Input{Provider: ProviderAPIMart, Model: nanoImageModel, Prompt: "paper boat", CanvasID: canvasID, NodeID: nodeID, Parameters: map[string]any{"size": "1:1", "resolution": "1K", "n": count}}
	quote, err := db.QuoteCredits(ctx, testUserID, "image", input)
	if err != nil || quote.Points != int64(6*count) {
		t.Fatalf("unexpected batch quote: %+v %v", quote, err)
	}
	input.OutputNodeIDs = ids
	return db, input, identity.ID(), quote
}

func waitNanoCredit(t *testing.T, db *Store, id, status string) Task {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		task, err := db.Task(context.Background(), id)
		if err == nil && task.CreditStatus == status {
			return task
		}
		time.Sleep(30 * time.Millisecond)
	}
	task, _ := db.Task(context.Background(), id)
	t.Fatalf("wanted credit %s, got %+v", status, task)
	return Task{}
}

func TestNanoBatchOneSubmissionAndAtomicCreditSettlement(t *testing.T) {
	for _, count := range []int{2, 4} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			db, input, id, quote := nanoBatchFixture(t, count)
			ctx := context.Background()
			if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", input, quote.Points, quote.PriceVersion); err != nil {
				t.Fatal(err)
			}
			if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", input, quote.Points, quote.PriceVersion); err != nil {
				t.Fatalf("same task ID must be idempotent: %v", err)
			}
			urls := []string{"https://provider.test/1.png", "https://provider.test/2.png", "https://provider.test/3.png", "https://provider.test/4.png"}
			fake := &nanoBatchFake{urls: urls[:count]}
			stop := startWorker(t, db, fake, fake)
			defer stop()
			parent := waitNanoCredit(t, db, id, "settled")
			tasks, err := db.LatestTasks(ctx, canvasID, false)
			account, accountErr := db.Credits(ctx, testUserID)
			if err != nil || accountErr != nil || len(tasks) != count || parent.Status != "succeeded" || account != (CreditAccount{Available: 200 - quote.Points}) || fake.creates.Load() != 1 || fake.queries.Load() != 1 {
				t.Fatalf("batch did not settle: parent=%+v tasks=%d account=%+v calls=%d/%d errors=%v/%v", parent, len(tasks), account, fake.creates.Load(), fake.queries.Load(), err, accountErr)
			}
			for _, task := range tasks {
				if task.Status != "succeeded" || task.Result == nil || task.Result.Asset == nil || task.Result.Asset.NodeID != task.Input.NodeID {
					t.Fatalf("output was not saved to its node: %+v", task)
				}
			}
			var reserves, settlements int
			if err := db.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE operation='reserve'),count(*) FILTER (WHERE operation='settle') FROM credit_ledger WHERE task_id=$1`, id).Scan(&reserves, &settlements); err != nil || reserves != 1 || settlements != 1 {
				t.Fatalf("incorrect credit ledger: %d/%d %v", reserves, settlements, err)
			}
		})
	}
}

func TestNanoReferenceSizeLimits(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	snapshot := testSnapshot()
	snapshot.Nodes[0].Data.ModelSelection = &ModelSelection{Provider: ProviderAPIMart, ModelID: nanoImageModel, Parameters: map[string]any{"n": 1}}
	keys := []string{"nano-ref-a", "nano-ref-b", "nano-ref-c"}
	for i, size := range []int64{20_000_000, 20_000_000, 10_000_000} {
		id := identity.ID()
		snapshot.Nodes = append(snapshot.Nodes, Node{ID: id, Data: NodeData{Kind: "image", Name: keys[i], Origin: "upload"}})
		if err := db.RecordAsset(ctx, storage.Asset{Key: keys[i], Kind: storage.Image, Bytes: size, Scope: storage.Scope{CanvasID: canvasID, NodeID: id}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	input := Input{Provider: ProviderAPIMart, Model: nanoImageExtModel, Prompt: "paper boat", CanvasID: canvasID, NodeID: nodeID, ReferenceKeys: keys, Parameters: map[string]any{"size": "1:1", "resolution": "1K", "n": 1}}
	if _, err := db.QuoteCredits(ctx, testUserID, "image", input); err != nil {
		t.Fatalf("ext should accept 50 MB total and 20 MB per image: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE assets SET metadata=jsonb_set(metadata,'{bytes}',to_jsonb($2::bigint)) WHERE key=$1`, keys[2], 10_000_001); err != nil {
		t.Fatal(err)
	}
	if _, err := db.QuoteCredits(ctx, testUserID, "image", input); !errors.Is(err, ErrNanoReferenceSize) {
		t.Fatalf("ext must reject more than 50 MB in total: %v", err)
	}
	snapshot.Nodes[0].Data.ModelSelection.Parameters["n"] = 2
	if _, err := db.SaveCanvas(ctx, canvasID, 2, snapshot); err != nil {
		t.Fatal(err)
	}
	input.Model = nanoImageModel
	input.Parameters["n"] = 2
	if _, err := db.QuoteCredits(ctx, testUserID, "image", input); err != nil {
		t.Fatalf("official model should accept over 50 MB total within per-image limit: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE assets SET metadata=jsonb_set(metadata,'{bytes}',to_jsonb($2::bigint)) WHERE key=$1`, keys[0], 20_000_001); err != nil {
		t.Fatal(err)
	}
	if _, err := db.QuoteCredits(ctx, testUserID, "image", input); !errors.Is(err, ErrNanoReferenceSize) {
		t.Fatalf("official model must reject an image larger than 20 MB: %v", err)
	}
}

func TestNanoBatchPartialResultKeepsReservationAndDoesNotResubmit(t *testing.T) {
	db, input, id, quote := nanoBatchFixture(t, 2)
	ctx := context.Background()
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", input, quote.Points, quote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	fake := &nanoBatchFake{urls: []string{"https://provider.test/1.png"}}
	stop := startWorker(t, db, fake, fake)
	parent := waitNanoCredit(t, db, id, "review")
	stop()
	if parent.Result == nil || parent.Result.Asset == nil || parent.Status != "succeeded" {
		t.Fatalf("available image was lost: %+v", parent)
	}
	tasks, err := db.LatestTasks(ctx, canvasID, false)
	account, accountErr := db.Credits(ctx, testUserID)
	if err != nil || accountErr != nil || len(tasks) != 2 || account != (CreditAccount{Available: 188, Reserved: 12}) {
		t.Fatalf("partial result should remain under review: %+v %+v %v %v", tasks, account, err, accountErr)
	}
	var interrupted int
	for _, task := range tasks {
		if task.Status == "interrupted" && task.Error != nil && task.Error.Code == "BATCH_OUTPUT_MISSING" {
			interrupted++
		}
	}
	if interrupted != 1 {
		t.Fatalf("missing output not reported: %+v", tasks)
	}
	stop = startWorker(t, db, fake, fake)
	stop()
	if fake.creates.Load() != 1 {
		t.Fatal("partial result caused another supplier submission")
	}
}

func TestNanoBatchSaveFailureKeepsReservationForReview(t *testing.T) {
	db, input, id, quote := nanoBatchFixture(t, 2)
	ctx := context.Background()
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", input, quote.Points, quote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	fake := &nanoBatchFake{urls: []string{"https://provider.test/1.png", "https://provider.test/2.png"}, failSave: input.OutputNodeIDs[1]}
	stop := startWorker(t, db, fake, fake)
	defer stop()
	parent := waitNanoCredit(t, db, id, "review")
	tasks, err := db.LatestTasks(ctx, canvasID, false)
	account, accountErr := db.Credits(ctx, testUserID)
	if err != nil || accountErr != nil || parent.Status != "succeeded" || len(tasks) != 2 || account != (CreditAccount{Available: 188, Reserved: 12}) || fake.creates.Load() != 1 {
		t.Fatalf("storage failure was not held for review: %+v %+v %+v %v %v", parent, tasks, account, err, accountErr)
	}
	var failed int
	for _, task := range tasks {
		if task.Result != nil && task.Result.StorageError != "" && task.Result.Asset == nil {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("failed storage was not reported: %+v", tasks)
	}
}

func TestNanoBatchDefiniteRejectionReleasesOneReservation(t *testing.T) {
	db, input, id, quote := nanoBatchFixture(t, 2)
	ctx := context.Background()
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", input, quote.Points, quote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	fake := &nanoBatchFake{failure: &ark.APIError{Code: "APIMART_REJECTED", Message: "rejected"}}
	stop := startWorker(t, db, fake, fake)
	defer stop()
	parent := waitNanoCredit(t, db, id, "released")
	tasks, err := db.LatestTasks(ctx, canvasID, false)
	account, accountErr := db.Credits(ctx, testUserID)
	if err != nil || accountErr != nil || parent.Status != "failed" || len(tasks) != 2 || account != (CreditAccount{Available: 200}) || fake.creates.Load() != 1 {
		t.Fatalf("definite rejection was not released: %+v %+v %+v %v %v", parent, tasks, account, err, accountErr)
	}
	for _, task := range tasks {
		if task.Status != "failed" || task.Error == nil || task.Error.Code != "APIMART_REJECTED" {
			t.Fatalf("child failure not propagated: %+v", task)
		}
	}
}

func TestNanoBatchUnknownSubmissionRecoversWithoutResubmitting(t *testing.T) {
	for _, restartDuringSubmit := range []bool{false, true} {
		t.Run(fmt.Sprint(restartDuringSubmit), func(t *testing.T) {
			db, input, id, quote := nanoBatchFixture(t, 2)
			ctx := context.Background()
			if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", input, quote.Points, quote.PriceVersion); err != nil {
				t.Fatal(err)
			}
			fake := &nanoBatchFake{failure: &ark.APIError{Code: "SUBMISSION_UNKNOWN", Message: "unknown"}}
			if restartDuringSubmit {
				if _, err := db.Pool.Exec(ctx, `UPDATE generation_tasks SET status='submitting' WHERE id=$1`, id); err != nil {
					t.Fatal(err)
				}
			}
			stop := startWorker(t, db, fake, fake)
			parent := waitNanoCredit(t, db, id, "review")
			stop()
			tasks, err := db.LatestTasks(ctx, canvasID, false)
			if err != nil || len(tasks) != 2 || parent.Status != "interrupted" {
				t.Fatalf("uncertain batch was not preserved: %+v %+v %v", parent, tasks, err)
			}
			for _, task := range tasks {
				if task.Status != "interrupted" || task.Error == nil || task.Error.Code != "SUBMISSION_UNKNOWN" {
					t.Fatalf("uncertain child not interrupted: %+v", task)
				}
			}
			want := int32(1)
			if restartDuringSubmit {
				want = 0
			}
			if fake.creates.Load() != want {
				t.Fatalf("unexpected supplier calls: %d", fake.creates.Load())
			}
		})
	}
}

func TestNanoBatchDoesNotReserveCreditsWhenOutputNodeBusy(t *testing.T) {
	db, input, id, quote := nanoBatchFixture(t, 2)
	ctx := context.Background()
	child := Input{Provider: ProviderAPIMart, Model: nanoImageExtModel, Prompt: "another image", CanvasID: canvasID, NodeID: input.OutputNodeIDs[1], Parameters: map[string]any{"size": "1:1", "resolution": "1K", "n": 1}}
	childQuote, err := db.QuoteCredits(ctx, testUserID, "image", child)
	if err != nil || childQuote.Points != 4 {
		t.Fatalf("single image quote: %+v %v", childQuote, err)
	}
	if _, err := db.CreateBilledTask(ctx, testUserID, identity.ID(), "image", child, childQuote.Points, childQuote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "image", input, quote.Points, quote.PriceVersion); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy output node was accepted: %v", err)
	}
	account, err := db.Credits(ctx, testUserID)
	if err != nil || account != (CreditAccount{Available: 196, Reserved: 4}) {
		t.Fatalf("failed batch changed credits: %+v %v", account, err)
	}
	if _, err := db.Task(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed batch parent persisted: %v", err)
	}
}
