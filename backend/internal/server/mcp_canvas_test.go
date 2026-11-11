package server

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"frame-space/backend/internal/ark"
	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
	"frame-space/backend/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callMCPResult[T any](t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (T, string) {
	t.Helper()
	var output T
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil || result.IsError {
		t.Fatalf("%s failed: %v %+v", name, err, result)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil || json.Unmarshal(data, &output) != nil {
		t.Fatalf("invalid %s result: %s %v", name, data, err)
	}
	return output, string(data)
}

func TestMCPCanvasReadToolsAreOwnedPagedAndReadOnly(t *testing.T) {
	f := setupMCP(t)
	ctx := context.Background()
	other := signIn(t, f.h, "other-canvas@example.test", "regular password")
	canvasID, secondID, otherID := identity.ID(), identity.ID(), identity.ID()
	for _, item := range []struct{ id, title, owner string }{
		{canvasID, "我的故事", f.user.session.User.ID}, {secondID, "另一张画布", f.user.session.User.ID}, {otherID, "另一个用户的画布", other.session.User.ID},
	} {
		if err := f.db.CreateCanvas(ctx, item.id, item.title, item.owner); err != nil {
			t.Fatal(err)
		}
	}
	nodeIDs := []string{identity.ID(), identity.ID(), identity.ID()}
	for i, kind := range []string{"image", "video", "image"} {
		if _, err := f.db.CreateOwnedCanvasNode(ctx, f.user.session.User.ID, canvasID, nodeIDs[i], kind, &persistence.Position{X: float64(i * 600), Y: 10}); err != nil {
			t.Fatal(err)
		}
	}
	canvas, err := f.db.Canvas(ctx, canvasID)
	if err != nil {
		t.Fatal(err)
	}
	canvas.Snapshot.Nodes[0].Data.Prompt = "海边的小猫"
	canvas.Snapshot.Nodes[0].Data.ModelSelection.ModelID = "seedream-5-0-lite"
	canvas.Snapshot.Nodes[0].Data.ModelSelection.Parameters = map[string]any{"resolution": "2K", "extra": map[string]any{"enabled": true}}
	canvas.Snapshot.Nodes[1].Data.ReferenceIDs = []string{nodeIDs[0]}
	canvas.Snapshot.Nodes[1].Data.Prompt = "让@图片节点 1挥手"
	canvas.Snapshot.Nodes[1].Data.PromptParts = []persistence.PromptPart{{Type: "text", Text: "让"}, {Type: "reference", NodeID: nodeIDs[0], Kind: "image", Name: "图片节点 1"}, {Type: "text", Text: "挥手"}}
	version, err := f.db.SaveOwnedCanvas(ctx, canvasID, f.user.session.User.ID, canvas.Version, canvas.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{0, 2} {
		asset := storage.Asset{Key: "private-key-" + nodeIDs[i], URL: "https://private-asset.example/secret", Kind: storage.Image, Source: storage.Upload,
			ContentType: "image/png", Bytes: 123, Scope: storage.Scope{CanvasID: canvasID, NodeID: nodeIDs[i]}, Media: &storage.MediaMetadata{Width: 960, Height: 540}}
		if err := f.db.RecordAsset(ctx, asset); err != nil {
			t.Fatal(err)
		}
	}
	videoTaskID, temporaryTaskID := identity.ID(), identity.ID()
	videoInput := persistence.Input{CanvasID: canvasID, NodeID: nodeIDs[1], Prompt: "挥手"}
	if _, err = f.db.Pool.Exec(ctx, `INSERT INTO generation_tasks(id,canvas_id,node_id,input,status,kind) VALUES($1,$2,$3,$4,'queued','video')`, videoTaskID, canvasID, nodeIDs[1], videoInput); err != nil {
		t.Fatal(err)
	}
	temporaryInput := persistence.Input{CanvasID: canvasID, NodeID: nodeIDs[2], Prompt: "新图"}
	temporaryResult := persistence.Result{ImageResult: ark.ImageResult{URL: "https://provider.example/private-result", Model: "test"}, StorageError: "保存失败"}
	if _, err = f.db.Pool.Exec(ctx, `INSERT INTO generation_tasks(id,canvas_id,node_id,input,status,kind,result) VALUES($1,$2,$3,$4,'succeeded','image',$5)`, temporaryTaskID, canvasID, nodeIDs[2], temporaryInput, temporaryResult); err != nil {
		t.Fatal(err)
	}

	session := f.sdkClient(t, f.token(t))
	firstList, _ := callMCPResult[listCanvasesOutput](t, session, "list_canvases", map[string]any{"limit": 1})
	if len(firstList.Canvases) != 1 || firstList.Canvases[0].CanvasID != canvasID || firstList.Canvases[0].NodeCount != 3 || !firstList.HasMore || firstList.NextOffset == nil || *firstList.NextOffset != 1 {
		t.Fatalf("incorrect first canvas page: %+v", firstList)
	}
	secondList, _ := callMCPResult[listCanvasesOutput](t, session, "list_canvases", map[string]any{"limit": 1, "offset": *firstList.NextOffset})
	if len(secondList.Canvases) != 1 || secondList.Canvases[0].CanvasID != secondID || secondList.HasMore {
		t.Fatalf("canvas list is not isolated: %+v", secondList)
	}
	empty, _ := callMCPResult[getCanvasOutput](t, session, "get_canvas", map[string]any{"canvasId": secondID})
	if empty.NodeCount != 0 || empty.Nodes == nil || len(empty.Nodes) != 0 || empty.HasMore {
		t.Fatalf("empty canvas could not be read: %+v", empty)
	}
	first, raw := callMCPResult[getCanvasOutput](t, session, "get_canvas", map[string]any{"canvasId": canvasID, "limit": 2})
	if first.CanvasID != canvasID || first.Title != "我的故事" || first.Version != version || first.NodeCount != 3 || len(first.Nodes) != 2 || !first.HasMore || first.NextOffset == nil || *first.NextOffset != 2 {
		t.Fatalf("incorrect canvas page: %+v", first)
	}
	if first.Nodes[0].ID != nodeIDs[0] || first.Nodes[0].Data.Prompt != "海边的小猫" || first.Nodes[0].Position.X != 0 || first.Nodes[0].Data.ModelSelection == nil || first.Nodes[0].Data.ModelSelection.ModelID != "seedream-5-0-lite" || first.Nodes[0].Data.ModelSelection.Parameters["resolution"] != "2K" || first.Nodes[0].Asset == nil || first.Nodes[0].Asset.Media == nil || first.Nodes[0].Asset.Media.Width != 960 {
		t.Fatalf("missing node data or saved asset: %+v", first.Nodes[0])
	}
	if first.Nodes[1].ID != nodeIDs[1] || len(first.Nodes[1].Data.ReferenceIDs) != 1 || first.Nodes[1].Data.ReferenceIDs[0] != nodeIDs[0] || len(first.Nodes[1].Data.PromptParts) != 3 || first.Nodes[1].Data.PromptParts[1].NodeID != nodeIDs[0] || first.Nodes[1].LatestTask == nil || first.Nodes[1].LatestTask.TaskID != videoTaskID || first.Nodes[1].LatestTask.Status != "queued" {
		t.Fatalf("missing node references or task: %+v", first.Nodes[1])
	}
	second, rawSecond := callMCPResult[getCanvasOutput](t, session, "get_canvas", map[string]any{"canvasId": canvasID, "limit": 2, "offset": *first.NextOffset})
	if len(second.Nodes) != 1 || second.Nodes[0].ID != nodeIDs[2] || second.Nodes[0].Asset != nil || second.Nodes[0].LatestResult == nil || second.Nodes[0].LatestResult.Saved || second.Nodes[0].LatestResult.TaskID != temporaryTaskID || second.HasMore {
		t.Fatalf("temporary result incorrectly reused an older asset: %+v", second)
	}
	if strings.Contains(raw+rawSecond, "private-key-") || strings.Contains(raw+rawSecond, "private-asset.example") || strings.Contains(raw+rawSecond, "provider.example") {
		t.Fatal("private object details were returned")
	}
	for _, args := range []map[string]any{{"canvasId": otherID}, {"canvasId": identity.ID()}} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_canvas", Arguments: args})
		if err != nil || !result.IsError {
			t.Fatalf("access to absent or other user's canvas was accepted: %v %+v", err, result)
		}
		data, _ := json.Marshal(result)
		if !strings.Contains(string(data), "画布不存在或不属于当前用户") {
			t.Fatalf("unexpected access failure: %s", data)
		}
	}
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"list_canvases", map[string]any{"userId": other.session.User.ID}}, {"list_canvases", map[string]any{"limit": 0}},
		{"list_canvases", map[string]any{"limit": 51}}, {"list_canvases", map[string]any{"offset": -1}},
		{"get_canvas", map[string]any{}}, {"get_canvas", map[string]any{"canvasId": "bad"}},
		{"get_canvas", map[string]any{"canvasId": canvasID, "offset": 301}}, {"get_canvas", map[string]any{"canvasId": canvasID, "userId": other.session.User.ID}},
	} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		if err == nil && !result.IsError {
			t.Fatalf("%s accepted invalid arguments: %v", call.name, call.args)
		}
	}
	after, err := f.db.Canvas(ctx, canvasID)
	if err != nil || after.Version != version || len(after.Snapshot.Nodes) != 3 || len(after.Tasks) != 2 || len(after.Assets) != 2 {
		t.Fatalf("read tools changed the canvas: %+v %v", after, err)
	}
}

func TestMCPScopeRequiresExplicitReadingConsent(t *testing.T) {
	f := setupMCP(t)
	verifier := identity.Token()
	v := f.authorization(verifier)
	v.Set("scope", mcpWriteScope)
	assertStatus(t, f.request("GET", "/oauth/authorize?"+v.Encode(), nil, f.user, nil), 400)
	v.Set("scope", mcpWriteScope+" "+mcpReadScope+" "+mcpGenerateScope)
	page := f.request("GET", "/oauth/authorize?"+v.Encode(), nil, f.user, nil)
	assertStatus(t, page, 200)
	if !strings.Contains(page.Body.String(), "允许读取") || !strings.Contains(page.Body.String(), "提示词") || !strings.Contains(page.Body.String(), "允许编辑") || !strings.Contains(page.Body.String(), "允许生成") || !strings.Contains(page.Body.String(), "积分") {
		t.Fatal("the consent page does not describe the granted permissions")
	}
	v.Set("csrf_token", f.user.session.CSRFToken)
	v.Set("decision", "allow")
	consent := f.request("POST", "/oauth/authorize", v, f.user, nil)
	assertStatus(t, consent, 303)
	redirect, err := url.Parse(consent.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	exchange := f.request("POST", "/oauth/token", f.exchange(redirect.Query().Get("code"), verifier), nil, nil)
	assertStatus(t, exchange, 200)
	var granted struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(exchange.Body.Bytes(), &granted); err != nil || granted.Scope != mcpScope {
		t.Fatal("reordered scope was not issued canonically")
	}
	verifier = identity.Token()
	code := f.code(t, verifier)
	if _, err := f.db.Pool.Exec(context.Background(), `UPDATE mcp_oauth_tokens SET metadata=jsonb_set(metadata,'{Scope}','"canvases:write"') WHERE token_hash=$1`, identity.TokenHash(code)); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, f.request("POST", "/oauth/token", f.exchange(code, verifier), nil, nil), 400)
}
