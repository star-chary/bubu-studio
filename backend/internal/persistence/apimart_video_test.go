package persistence

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"frame-space/backend/internal/apimart"
)

func TestAPIMartStartingImageAndNativeOptionsAreDurable(t *testing.T) {
	db, in, id := videoFixture(t)
	ctx := context.Background()
	snapshot := Snapshot{Nodes: []Node{
		{ID: nodeID, Data: NodeData{Kind: "video", Name: "native image task", VideoMode: "image", FirstFrameID: promptSourceID, ReferenceIDs: []string{promptSourceID}, ModelSelection: &ModelSelection{Provider: ProviderAPIMart, ModelID: "kling-v3-omni", Parameters: map[string]any{}}}},
		{ID: promptSourceID, Data: NodeData{Kind: "image", Name: "source", Origin: "upload"}},
	}, Viewport: Viewport{Zoom: 1}}
	if _, err := db.SaveCanvas(ctx, canvasID, 2, snapshot); err != nil {
		t.Fatal(err)
	}
	in.Provider, in.Model, in.Mode, in.Prompt = ProviderAPIMart, "kling-v3-omni", "image", "@source moves"
	in.Parameters = map[string]any{"mode": "pro", "aspect_ratio": "16:9", "duration": 6, "audio": false}
	in.References[0].Role = "first_frame"
	in.PromptParts = []PromptPart{{Type: "reference", NodeID: promptSourceID, Kind: "image", Name: "source"}, {Type: "text", Text: " moves"}}
	task, err := db.CreateVideoTask(ctx, id, in)
	if err != nil {
		t.Fatal(err)
	}
	if task.CompiledPrompt != "<<<image_1>>> moves" || task.Bindings[0].Role != "first_frame" || task.Input.Resolution != "pro" {
		t.Fatal("lost native input", task)
	}
	in.Parameters["mode"] = "std"
	snapshot.Nodes[0].Data.ModelSelection = nil
	snapshot.Nodes[0].Data.VideoMode = "reference"
	if _, err = db.SaveCanvas(ctx, canvasID, 3, snapshot); err != nil {
		t.Fatal(err)
	}
	saved, err := db.Task(ctx, id)
	if err != nil || saved.Input.Parameters["mode"] != "pro" || saved.Input.Provider != ProviderAPIMart {
		t.Fatal("task changed with draft", saved, err)
	}
	if saved.CreditStatus != "legacy" || saved.CreditPoints != 0 {
		t.Fatal("APIMart task charged platform points")
	}
}

func TestAPIMartImageModeRequiresAnExplicitFirstFrame(t *testing.T) {
	base := Input{Provider: ProviderAPIMart, Model: apimart.VideoModel, Mode: "image", Prompt: "scene", CanvasID: canvasID, NodeID: nodeID,
		Parameters: map[string]any{"size": "adaptive", "resolution": "720p", "duration": 4, "generate_audio": false},
		References: []Reference{{NodeID: promptSourceID, AssetKey: "source", Kind: "image", Role: "first_frame"}}}
	for name, change := range map[string]func(*Input){
		"implicit image":    func(in *Input) { in.References[0].Role = "" },
		"two frames":        func(in *Input) { in.References = append(in.References, in.References[0]) },
		"audio frame":       func(in *Input) { in.References[0].Kind = "audio" },
		"role in reference": func(in *Input) { in.Mode = "reference" },
		"unsupported mode":  func(in *Input) { in.Model = "grok-imagine-video-1.5" },
	} {
		t.Run(name, func(t *testing.T) {
			encoded, _ := json.Marshal(base)
			var in Input
			_ = json.Unmarshal(encoded, &in)
			change(&in)
			if NormalizeVideoInput(&in) == nil {
				t.Fatal("invalid starting frame accepted")
			}
		})
	}
	if err := NormalizeVideoInput(&base); err != nil {
		t.Fatal("explicit first frame rejected", err)
	}
}

type videoProvidersFake struct {
	*videoFake
	apimart *videoFake
	routes  atomic.Int32
}

func (f *videoProvidersFake) VideoGeneratorFor(provider string) VideoGenerator {
	if provider == ProviderAPIMart {
		f.routes.Add(1)
		return f.apimart
	}
	return f.videoFake
}

func TestAPIMartReferenceTaskKeepsSupplierAfterCanvasChanges(t *testing.T) {
	db, in, id := videoFixture(t)
	ctx := context.Background()
	snapshot := Snapshot{Nodes: []Node{
		{ID: nodeID, Data: NodeData{Kind: "video", Name: "APIMart", ModelSelection: &ModelSelection{Provider: ProviderAPIMart, ModelID: apimart.VideoModel, Parameters: map[string]any{}}}},
		{ID: promptSourceID, Data: NodeData{Kind: "image", Name: "source", Origin: "upload"}},
	}, Viewport: Viewport{Zoom: 1}}
	if _, err := db.SaveCanvas(ctx, canvasID, 2, snapshot); err != nil {
		t.Fatal(err)
	}
	in.Provider, in.Model, in.Prompt = ProviderAPIMart, apimart.VideoModel, "@source moves slowly"
	in.PromptParts = []PromptPart{{Type: "reference", NodeID: promptSourceID, Kind: "image", Name: "source"}, {Type: "text", Text: " moves slowly"}}
	task, err := db.CreateVideoTask(ctx, id, in)
	if err != nil || task.CompiledPrompt != "@图片1 moves slowly" {
		t.Fatal(task.CompiledPrompt, err)
	}
	snapshot.Nodes[0].Data.ModelSelection = nil
	if _, err := db.SaveCanvas(ctx, canvasID, 3, snapshot); err != nil {
		t.Fatal(err)
	}
	arkFake, apiFake := &videoFake{}, &videoFake{}
	providers := &videoProvidersFake{videoFake: arkFake, apimart: apiFake}
	stop := startWorker(t, db, providers, arkFake)
	defer stop()
	task = awaitVideo(t, db, id, "succeeded")
	if task.Input.Provider != ProviderAPIMart || apiFake.creates.Load() != 1 || apiFake.queries.Load() != 1 || arkFake.creates.Load() != 0 || arkFake.saves.Load() != 1 || providers.routes.Load() == 0 || task.Result.Asset == nil || task.CreditStatus != "legacy" {
		t.Fatalf("incorrect supplier routing: %+v, api=%d, ark=%d", task, apiFake.creates.Load(), arkFake.creates.Load())
	}
}

func TestAPIMartTaskNeverFallsBackToArk(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	snapshot := Snapshot{Nodes: []Node{{ID: nodeID, Data: NodeData{Kind: "video", Name: "APIMart", ModelSelection: &ModelSelection{Provider: ProviderAPIMart, ModelID: apimart.VideoModel, Parameters: map[string]any{}}}}}, Viewport: Viewport{Zoom: 1}}
	if _, err := db.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	in := Input{Provider: ProviderAPIMart, Model: apimart.VideoModel, Mode: "text", Prompt: "A paper boat", CanvasID: canvasID, NodeID: nodeID, Resolution: "720p", Ratio: "16:9", Duration: 5}
	id := "70707070-7070-4070-8070-707070707070"
	if _, err := db.CreateVideoTask(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	fake := &videoFake{}
	stop := startWorker(t, db, fake, fake)
	defer stop()
	task := awaitVideo(t, db, id, "failed")
	if task.Error == nil || task.Error.Code != "VIDEO_SERVICE_REQUIRED" || fake.creates.Load() != 0 {
		t.Fatal("APIMart task reached Ark", task, fake.creates.Load())
	}
}
