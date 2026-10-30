package persistence

import (
	"context"
	"errors"
	"testing"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/storage"
)

func TestAPIMartVideoPromptLabels(t *testing.T) {
	refs := []Reference{{NodeID: promptSourceID, Kind: "image"}, {NodeID: "40404040-4040-4040-8040-404040404040", Kind: "video"}}
	in := Input{Provider: ProviderAPIMart, Model: "seedance-2.0-fast", NodeID: nodeID, Prompt: "@图片 按 @动作", PromptParts: []PromptPart{
		{Type: "reference", NodeID: refs[0].NodeID, Kind: "image", Name: "图片"},
		{Type: "text", Text: " 按 "},
		{Type: "reference", NodeID: refs[1].NodeID, Kind: "video", Name: "动作"},
	}}
	prompt, err := CompilePrompt(in, "video", refs)
	if err != nil || prompt != "@图片1 按 @视频1" {
		t.Fatalf("prompt = %q, err = %v", prompt, err)
	}
	in.Model = "wan3.0-video"
	if _, err := CompilePrompt(in, "video", refs); err == nil {
		t.Fatal("unopened video label accepted")
	}
}

type multipleReferenceStorage struct {
	*videoFake
	checks chan storage.VideoReferenceLimits
}

func (f *multipleReferenceStorage) VideoReferenceURLsWithLimits(_ context.Context, _ storage.Scope, keys []string, limits storage.VideoReferenceLimits) ([]string, error) {
	f.checks <- limits
	urls := make([]string, 0, len(keys))
	for _, key := range keys {
		switch key {
		case "image-ref":
			urls = append(urls, "https://assets.example.test/image.png")
		case "video-ref":
			urls = append(urls, "https://assets.example.test/video.mp4")
		default:
			return nil, &storage.Error{Status: 422, Code: "INVALID_REFERENCES", Message: "wrong reference"}
		}
	}
	return urls, nil
}

func TestTwentySecondReferenceModelsAndCredits(t *testing.T) {
	for _, tc := range []struct {
		name, model string
		motion      bool
		fail        bool
		points      int64
	}{
		{"seedance25", "seedance-2.5", false, false, 400},
		{"kling26", "kling-v2-6-motion-control", true, false, 198},
		{"kling3", "kling-v3-motion-control", true, false, 356},
		{"kling26-rejected", "kling-v2-6-motion-control", true, true, 198},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, in, id := videoFixture(t)
			ctx := context.Background()
			const videoNodeID = "92929292-9292-4292-8292-929292929292"
			snapshot := Snapshot{Nodes: []Node{
				{ID: nodeID, Data: NodeData{Kind: "video", Name: "target", ModelSelection: &ModelSelection{Provider: ProviderAPIMart, ModelID: tc.model, Parameters: map[string]any{}}}},
				{ID: promptSourceID, Data: NodeData{Kind: "image", Name: "image", Origin: "upload"}},
				{ID: videoNodeID, Data: NodeData{Kind: "video", Name: "video", Origin: "upload"}},
			}, Viewport: Viewport{Zoom: 1}}
			if _, err := db.SaveCanvas(ctx, canvasID, 2, snapshot); err != nil {
				t.Fatal(err)
			}
			image := storage.Asset{Key: "image-ref", Kind: storage.Image, ContentType: "image/png", Bytes: 1000, Scope: storage.Scope{CanvasID: canvasID, NodeID: promptSourceID}, Media: &storage.MediaMetadata{Width: 960, Height: 540}}
			video := storage.Asset{Key: "video-ref", Kind: storage.Video, ContentType: "video/mp4", Bytes: 1000, Scope: storage.Scope{CanvasID: canvasID, NodeID: videoNodeID}, Media: &storage.MediaMetadata{Width: 1920, Height: 1080, VideoCodec: "h264", FrameRate: 24, DurationSeconds: 20}}
			if err := db.RecordAsset(ctx, image); err != nil {
				t.Fatal(err)
			}
			if err := db.RecordAsset(ctx, video); err != nil {
				t.Fatal(err)
			}
			if err := db.GrantCredits(ctx, testUserID, 1000, "twenty-second-video-test"); err != nil {
				t.Fatal(err)
			}
			in.Provider, in.Model, in.Mode, in.Prompt = ProviderAPIMart, tc.model, "reference", "参考动作生成视频"
			in.References = []Reference{{NodeID: videoNodeID, AssetKey: video.Key, Kind: "video"}}
			if tc.motion {
				in.Parameters = map[string]any{"mode": "std"}
				if _, err := db.QuoteCredits(ctx, testUserID, "video", in); !errors.Is(err, ErrInvalid) {
					t.Fatalf("video-only motion input accepted: %v", err)
				}
				in.References = append([]Reference{{NodeID: promptSourceID, AssetKey: image.Key, Kind: "image"}}, in.References...)
			} else {
				in.Parameters = map[string]any{"size": "16:9", "resolution": "720p", "duration": 20, "generate_audio": false}
				if _, err := db.QuoteCredits(ctx, testUserID, "video", in); !errors.Is(err, ErrInvalid) {
					t.Fatalf("unpriced 720p Seedance 2.5 accepted: %v", err)
				}
				in.Parameters["resolution"] = "480p"
			}
			quote, err := db.QuoteCredits(ctx, testUserID, "video", in)
			if err != nil || quote.Points != tc.points {
				t.Fatalf("quote: %+v %v", quote, err)
			}
			if _, err := db.CreateBilledTask(ctx, testUserID, id, "video", in, quote.Points, quote.PriceVersion); err != nil {
				t.Fatal(err)
			}
			account, err := db.Credits(ctx, testUserID)
			if err != nil || account != (CreditAccount{Available: 1000 - tc.points, Reserved: tc.points}) {
				t.Fatalf("reservation: %+v %v", account, err)
			}
			fake := &videoFake{}
			if tc.fail {
				fake.createError = &ark.APIError{Code: "APIMART_REJECTED", Message: "rejected"}
			}
			api := &recordingAPIMartVideo{videoFake: fake, inputs: make(chan ark.VideoRequest, 1)}
			providers := &recordingVideoProviders{videoFake: &videoFake{}, apimart: api}
			objects := &multipleReferenceStorage{videoFake: &videoFake{}, checks: make(chan storage.VideoReferenceLimits, 1)}
			stop := startWorker(t, db, providers, objects)
			defer stop()
			status, creditStatus, wantAccount := "succeeded", "settled", (CreditAccount{Available: 1000 - tc.points})
			if tc.fail {
				status, creditStatus, wantAccount = "failed", "released", CreditAccount{Available: 1000}
			}
			task := awaitVideo(t, db, id, status)
			account, err = db.Credits(ctx, testUserID)
			if err != nil || task.CreditStatus != creditStatus || account != wantAccount || api.creates.Load() != 1 {
				t.Fatalf("completion: status=%s account=%+v error=%v", task.CreditStatus, account, err)
			}
			request := <-api.inputs
			limits := <-objects.checks
			if request.Model != tc.model || len(request.References) != len(in.References) || limits.APIMartModel != tc.model || limits.MaxMediaSeconds != 30 || limits.ExpectedVideoSeconds[video.Key] != 20 {
				t.Fatalf("worker request=%+v preflight=%+v", request, limits)
			}
		})
	}
}

type recordingAPIMartVideo struct {
	*videoFake
	inputs chan ark.VideoRequest
}

func (f *recordingAPIMartVideo) CreateVideo(ctx context.Context, input ark.VideoRequest) (string, error) {
	f.inputs <- input
	return f.videoFake.CreateVideo(ctx, input)
}

type recordingVideoProviders struct {
	*videoFake
	apimart *recordingAPIMartVideo
}

func (f *recordingVideoProviders) VideoGeneratorFor(provider string) VideoGenerator {
	if provider == ProviderAPIMart {
		return f.apimart
	}
	return f.videoFake
}

type recordingReferenceStorage struct {
	*videoFake
	checks chan storage.VideoReferenceLimits
}

func (f *recordingReferenceStorage) VideoReferenceURLsWithLimits(_ context.Context, _ storage.Scope, keys []string, limits storage.VideoReferenceLimits) ([]string, error) {
	f.checks <- limits
	if len(keys) != 1 || keys[0] != "video-ref" {
		return nil, &storage.Error{Status: 422, Code: "INVALID_REFERENCES", Message: "wrong reference"}
	}
	return []string{"https://assets.example.test/video.mp4"}, nil
}

func TestAPIMartReferenceVideoBilledWorkerIntegration(t *testing.T) {
	db, in, id := videoFixture(t)
	ctx := context.Background()
	const videoNodeID = "92929292-9292-4292-8292-929292929292"
	snapshot := Snapshot{Nodes: []Node{
		{ID: nodeID, Data: NodeData{Kind: "video", Name: "target", ModelSelection: &ModelSelection{Provider: ProviderAPIMart, ModelID: "seedance-2.0-fast", Parameters: map[string]any{}}}},
		{ID: videoNodeID, Data: NodeData{Kind: "video", Name: "reference", Origin: "upload"}},
	}, Viewport: Viewport{Zoom: 1}}
	if _, err := db.SaveCanvas(ctx, canvasID, 2, snapshot); err != nil {
		t.Fatal(err)
	}
	asset := storage.Asset{Key: "video-ref", Kind: storage.Video, ContentType: "video/mp4", Bytes: 1000, Scope: storage.Scope{CanvasID: canvasID, NodeID: videoNodeID}, Media: &storage.MediaMetadata{Width: 854, Height: 480, VideoCodec: "h264", FrameRate: 24, DurationSeconds: 2}}
	if err := db.RecordAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	if err := db.GrantCredits(ctx, testUserID, 1000, "apimart-video-reference-worker-test"); err != nil {
		t.Fatal(err)
	}
	in.Provider, in.Model, in.Mode, in.Prompt = ProviderAPIMart, "seedance-2.0-fast", "reference", "@动作"
	in.Resolution, in.Duration = "720p", 5
	in.Parameters = map[string]any{"size": "16:9", "resolution": "720p", "duration": 5, "generate_audio": false}
	in.PromptParts = []PromptPart{{Type: "reference", NodeID: videoNodeID, Kind: "video", Name: "动作"}}
	in.References = []Reference{{NodeID: videoNodeID, AssetKey: asset.Key, Kind: "video"}}
	quote, err := db.QuoteCredits(ctx, testUserID, "video", in)
	if err != nil || quote.Points <= 0 {
		t.Fatalf("quote: %+v %v", quote, err)
	}
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "video", in, quote.Points, quote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	account, err := db.Credits(ctx, testUserID)
	if err != nil || account.Available != 1000-quote.Points || account.Reserved != quote.Points {
		t.Fatalf("reservation: %+v %v", account, err)
	}
	// A duplicate submission must recover the same task without another reservation.
	if _, err := db.CreateBilledTask(ctx, testUserID, id, "video", in, quote.Points, quote.PriceVersion); err != nil {
		t.Fatal(err)
	}
	api := &recordingAPIMartVideo{videoFake: &videoFake{}, inputs: make(chan ark.VideoRequest, 1)}
	providers := &recordingVideoProviders{videoFake: &videoFake{}, apimart: api}
	objects := &recordingReferenceStorage{videoFake: &videoFake{}, checks: make(chan storage.VideoReferenceLimits, 1)}
	stop := startWorker(t, db, providers, objects)
	defer stop()
	task := awaitVideo(t, db, id, "succeeded")
	account, err = db.Credits(ctx, testUserID)
	if err != nil || account.Available != 1000-quote.Points || account.Reserved != 0 || task.CreditStatus != "settled" || task.CreditPoints != quote.Points || task.CreditPriceVersion != quote.PriceVersion {
		t.Fatalf("settlement: task=%+v account=%+v err=%v", task, account, err)
	}
	if api.creates.Load() != 1 || api.queries.Load() != 1 || providers.videoFake.creates.Load() != 0 || objects.saves.Load() != 1 || task.Result == nil || task.Result.Asset == nil {
		t.Fatal("unexpected provider or storage calls")
	}
	select {
	case request := <-api.inputs:
		if request.Model != "seedance-2.0-fast" || request.Mode != "reference" || request.Prompt != "@视频1" || len(request.References) != 1 || request.References[0].Kind != "video" || request.References[0].URL != "https://assets.example.test/video.mp4" {
			t.Fatalf("wrong provider input: %+v", request)
		}
	default:
		t.Fatal("provider did not receive request")
	}
	select {
	case limits := <-objects.checks:
		if limits.APIMartModel != "seedance-2.0-fast" || limits.MaxVideos != 3 || limits.MaxMediaSeconds != 15 || limits.ExpectedVideoSeconds[asset.Key] != 2 {
			t.Fatalf("wrong preflight: %+v", limits)
		}
	default:
		t.Fatal("reference was not preflighted")
	}
}
