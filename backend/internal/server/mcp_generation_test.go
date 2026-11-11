package server

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// No worker is started in these tests. Any attempt to reach external storage is
// a test failure; queued tasks live only in the isolated temporary schema.
type mcpTestAssets struct{ stubAssets }

func (*mcpTestAssets) VideoReferenceURLsWithLimits(context.Context, storage.Scope, []string, storage.VideoReferenceLimits) ([]string, error) {
	panic("unexpected external storage call")
}
func (*mcpTestAssets) SaveGeneratedVideo(context.Context, string, string, storage.Scope) (storage.Asset, error) {
	panic("unexpected external storage call")
}

func expectMCPFailure(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, message string) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return // A schema rejection is also a valid rejection.
	}
	data, _ := json.Marshal(result)
	if !result.IsError || (message != "" && !strings.Contains(string(data), message)) {
		t.Fatalf("%s unexpectedly accepted %v: %s", name, args, data)
	}
}

func TestMCPGenerationToolsQueueWithoutCallingProviders(t *testing.T) {
	f := setupMCP(t)
	ctx := context.Background()
	canvasID, nodeID := identity.ID(), identity.ID()
	if err := f.db.CreateCanvas(ctx, canvasID, "生成工具测试", f.user.session.User.ID); err != nil {
		t.Fatal(err)
	}
	session := f.sdkClient(t, f.token(t))
	models, _ := callMCPResult[listGenerationModelsOutput](t, session, "list_generation_models", map[string]any{})
	foundImage, foundVideo := false, false
	for _, model := range models.Models {
		foundImage = foundImage || model.Kind == "image" && model.Provider == "apimart" && model.ModelID == "seedream-5-0-lite"
		foundVideo = foundVideo || model.Kind == "video" && model.Provider == "apimart" && model.ModelID == "seedance-2.5"
		if model.ModelID == "gemini-nano-banana-2.1-ext" || model.ModelID == "gpt-image-2.5-flare" {
			t.Fatalf("internal or unpriced model exposed: %+v", model)
		}
	}
	if !foundImage || !foundVideo {
		t.Fatal("missing billable model definitions")
	}
	created, _ := callMCPResult[createCanvasNodeOutput](t, session, "create_canvas_node", map[string]any{"canvasId": canvasID, "nodeId": nodeID, "kind": "image"})
	if _, err := f.db.Pool.Exec(ctx, `UPDATE canvases SET snapshot=jsonb_set(jsonb_set(snapshot,'{future}','{"serial":9007199254740993}'::jsonb),'{nodes,0,data,future}','{"flag":true}'::jsonb) WHERE id=$1`, canvasID); err != nil {
		t.Fatal(err)
	}
	configured, _ := callMCPResult[configureCanvasNodeOutput](t, session, "configure_canvas_node", map[string]any{"canvasId": canvasID, "nodeId": nodeID, "expectedVersion": created.Version, "patch": map[string]any{"prompt": "海边的小猫", "model": map[string]any{"provider": "apimart", "modelId": "seedream-5-0-lite", "parameters": map[string]any{"aspect_ratio": "9:16", "resolution": "2K", "n": 1}}}})
	if configured.Node.Data.Prompt != "海边的小猫" || configured.Node.Data.ModelSelection == nil || configured.Node.Data.ModelSelection.Parameters["size"] != "9:16" || configured.Version != created.Version+1 {
		t.Fatalf("node was not configured: %+v", configured)
	}
	var outerKept, innerKept bool
	if err := f.db.Pool.QueryRow(ctx, `SELECT snapshot->'future' = '{"serial":9007199254740993}'::jsonb, snapshot->'nodes'->0->'data'->'future' = '{"flag":true}'::jsonb FROM canvases WHERE id=$1`, canvasID).Scan(&outerKept, &innerKept); err != nil || !outerKept || !innerKept {
		t.Fatalf("unrecognized fields were lost: %v %v %v", outerKept, innerKept, err)
	}
	before, err := f.db.Credits(ctx, f.user.session.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	quote, _ := callMCPResult[quoteGenerationOutput](t, session, "quote_generation", map[string]any{"canvasId": canvasID, "nodeId": nodeID})
	afterQuote, err := f.db.Credits(ctx, f.user.session.User.ID)
	if err != nil || afterQuote != before || quote.Points < 1 || len(quote.QuoteHash) != 64 || quote.CanvasVersion != configured.Version {
		t.Fatalf("invalid quote or unexpected charge: %+v %+v %v", quote, afterQuote, err)
	}
	taskID := identity.ID()
	args := map[string]any{"canvasId": canvasID, "nodeId": nodeID, "taskId": taskID, "canvasVersion": quote.CanvasVersion, "points": quote.Points, "priceVersion": quote.PriceVersion, "quoteHash": quote.QuoteHash}
	bad := map[string]any{}
	for key, value := range args {
		bad[key] = value
	}
	bad["quoteHash"] = strings.Repeat("0", 64)
	expectMCPFailure(t, session, "start_generation", bad, "重新报价")
	var count int
	if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM generation_tasks`).Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid quote created a task", err)
	}
	task, _ := callMCPResult[generationTaskOutput](t, session, "start_generation", args)
	if task.TaskID != taskID || task.Kind != "image" || task.Status != "queued" || task.CreditStatus != "reserved" || task.CreditPoints != quote.Points {
		t.Fatalf("invalid queued task: %+v", task)
	}
	account, err := f.db.Credits(ctx, f.user.session.User.ID)
	if err != nil || account.Available != before.Available-quote.Points || account.Reserved != before.Reserved+quote.Points {
		t.Fatalf("isolated credit reservation: %+v %v", account, err)
	}
	secondNode := identity.ID()
	callMCPResult[createCanvasNodeOutput](t, session, "create_canvas_node", map[string]any{"canvasId": canvasID, "nodeId": secondNode, "kind": "video"})
	retry, _ := callMCPResult[generationTaskOutput](t, session, "start_generation", args)
	accountAfterRetry, _ := f.db.Credits(ctx, f.user.session.User.ID)
	if retry.TaskID != taskID || accountAfterRetry != account {
		t.Fatal("retry created another task or reserved credits twice")
	}
	queried, raw := callMCPResult[generationTaskOutput](t, session, "get_generation_task", map[string]any{"taskId": taskID})
	if queried.TaskID != taskID || queried.CanvasID != canvasID || queried.Status != "queued" || strings.Contains(raw, "provider_url") {
		t.Fatalf("wrong task result: %s", raw)
	}
	stale := map[string]any{}
	for key, value := range args {
		stale[key] = value
	}
	stale["taskId"] = identity.ID()
	expectMCPFailure(t, session, "start_generation", stale, "重新报价")
	expectMCPFailure(t, session, "configure_canvas_node", map[string]any{"canvasId": canvasID, "nodeId": nodeID, "expectedVersion": configured.Version, "patch": map[string]any{"prompt": "旧版本"}}, "冲突")
	other := signIn(t, f.h, "generation-other@example.test", "regular password")
	otherID, otherTaskID := identity.ID(), identity.ID()
	if err := f.db.CreateCanvas(ctx, otherID, "他人画布", other.session.User.ID); err != nil {
		t.Fatal(err)
	}
	otherNode := identity.ID()
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO generation_tasks(id,canvas_id,node_id,input,status,kind) VALUES($1,$2,$3,$4,'queued','image')`, otherTaskID, otherID, otherNode, persistence.Input{CanvasID: otherID, NodeID: otherNode}); err != nil {
		t.Fatal(err)
	}
	expectMCPFailure(t, session, "get_generation_task", map[string]any{"taskId": otherTaskID}, "不属于当前用户")
	asset := &storage.Asset{Key: "private-result-key", URL: "https://fixture.invalid/private-asset", Kind: storage.Image, Source: storage.Generated, ContentType: "image/png", Bytes: 123, Scope: storage.Scope{CanvasID: canvasID, NodeID: nodeID}}
	fakeResult := persistence.Result{ImageResult: ark.ImageResult{URL: "https://fixture.invalid/provider-result", Model: "seedream-5-0-lite"}, Asset: asset}
	if _, err := f.db.Pool.Exec(ctx, `UPDATE generation_tasks SET status='succeeded',credit_status='settled',result=$2 WHERE id=$1`, taskID, fakeResult); err != nil {
		t.Fatal(err)
	}
	finished, finishedRaw := callMCPResult[generationTaskOutput](t, session, "get_generation_task", map[string]any{"taskId": taskID})
	if finished.Status != "succeeded" || finished.Result == nil || !finished.Result.Saved || finished.Asset == nil || strings.Contains(finishedRaw, "fixture.invalid") || strings.Contains(finishedRaw, "private-result-key") {
		t.Fatalf("saved task result was not safely summarized: %s", finishedRaw)
	}
}

func TestMCPVideoQuoteUsesSavedReferenceAndQueuesOnly(t *testing.T) {
	f := setupMCP(t)
	ctx := context.Background()
	canvasID, sourceID, videoID := identity.ID(), identity.ID(), identity.ID()
	if err := f.db.CreateCanvas(ctx, canvasID, "视频工具测试", f.user.session.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, `UPDATE credit_accounts SET available=100000 WHERE user_id=$1`, f.user.session.User.ID); err != nil {
		t.Fatal(err)
	}
	session := f.sdkClient(t, f.token(t))
	callMCPResult[createCanvasNodeOutput](t, session, "create_canvas_node", map[string]any{"canvasId": canvasID, "nodeId": sourceID, "kind": "image"})
	video, _ := callMCPResult[createCanvasNodeOutput](t, session, "create_canvas_node", map[string]any{"canvasId": canvasID, "nodeId": videoID, "kind": "video"})
	asset := storage.Asset{Key: "fixture-private-reference", URL: "https://fixture.invalid/private", Kind: storage.Image, Source: storage.Upload, ContentType: "image/png", Bytes: 1000, Scope: storage.Scope{CanvasID: canvasID, NodeID: sourceID}, Media: &storage.MediaMetadata{Width: 1024, Height: 1024}}
	if err := f.db.RecordAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	configured, _ := callMCPResult[configureCanvasNodeOutput](t, session, "configure_canvas_node", map[string]any{"canvasId": canvasID, "nodeId": videoID, "expectedVersion": video.Version, "patch": map[string]any{"prompt": "让小猫挥手", "model": map[string]any{"provider": "apimart", "modelId": "seedance-2.5", "parameters": map[string]any{"resolution": "480p", "duration": 5, "generate_audio": true}}, "videoMode": "image", "referenceIds": []string{sourceID}, "firstFrameId": sourceID}})
	quote, _ := callMCPResult[quoteGenerationOutput](t, session, "quote_generation", map[string]any{"canvasId": canvasID, "nodeId": videoID})
	if quote.Kind != "video" || quote.CanvasVersion != configured.Version || quote.Points < 1 {
		t.Fatalf("invalid video quote: %+v", quote)
	}
	taskID := identity.ID()
	task, _ := callMCPResult[generationTaskOutput](t, session, "start_generation", map[string]any{"canvasId": canvasID, "nodeId": videoID, "taskId": taskID, "canvasVersion": quote.CanvasVersion, "points": quote.Points, "priceVersion": quote.PriceVersion, "quoteHash": quote.QuoteHash})
	if task.Kind != "video" || task.Status != "queued" {
		t.Fatalf("unexpected video task: %+v", task)
	}
	stored, err := f.db.Task(ctx, taskID)
	if err != nil || len(stored.Input.References) != 1 || stored.Input.References[0].NodeID != sourceID || stored.Input.References[0].AssetKey != asset.Key || stored.Input.References[0].Role != "first_frame" {
		t.Fatalf("saved reference was not bound: %+v %v", stored, err)
	}
	retried, _ := callMCPResult[generationTaskOutput](t, session, "start_generation", map[string]any{"canvasId": canvasID, "nodeId": videoID, "taskId": taskID, "canvasVersion": quote.CanvasVersion, "points": quote.Points, "priceVersion": quote.PriceVersion, "quoteHash": quote.QuoteHash})
	if retried.TaskID != taskID || retried.Status != "queued" {
		t.Fatal("video task retry did not return the original task")
	}
}

func TestMCPListedTextModelsCanBeConfiguredAndQuoted(t *testing.T) {
	f := setupMCP(t)
	ctx := context.Background()
	canvasID := identity.ID()
	if err := f.db.CreateCanvas(ctx, canvasID, "模型报价测试", f.user.session.User.ID); err != nil {
		t.Fatal(err)
	}
	session := f.sdkClient(t, f.token(t))
	models, _ := callMCPResult[listGenerationModelsOutput](t, session, "list_generation_models", map[string]any{})
	checked := 0
	for _, model := range models.Models {
		if !slices.Contains(model.Modes, "text") {
			continue
		}
		nodeID := identity.ID()
		created, _ := callMCPResult[createCanvasNodeOutput](t, session, "create_canvas_node", map[string]any{"canvasId": canvasID, "nodeId": nodeID, "kind": model.Kind})
		patch := map[string]any{"prompt": "一只海边的小猫", "model": map[string]any{"provider": model.Provider, "modelId": model.ModelID}}
		if model.Kind == "video" {
			patch["videoMode"] = "text"
		}
		callMCPResult[configureCanvasNodeOutput](t, session, "configure_canvas_node", map[string]any{"canvasId": canvasID, "nodeId": nodeID, "expectedVersion": created.Version, "patch": patch})
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "quote_generation", Arguments: map[string]any{"canvasId": canvasID, "nodeId": nodeID}})
		if err != nil || result.IsError {
			data, _ := json.Marshal(result)
			t.Errorf("default quote failed for %s/%s: %v %s", model.Provider, model.ModelID, err, data)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no text models were checked")
	}
}
