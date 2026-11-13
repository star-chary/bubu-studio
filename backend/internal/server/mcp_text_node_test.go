package server

import (
	"context"
	"testing"

	"frame-space/backend/internal/identity"
	"frame-space/backend/internal/persistence"
)

func TestMCPGenerationFreezesLinkedText(t *testing.T) {
	f := setupMCP(t)
	ctx := context.Background()
	canvasID, targetID, textID := identity.ID(), identity.ID(), identity.ID()
	if err := f.db.CreateCanvas(ctx, canvasID, "文本节点测试", f.user.session.User.ID); err != nil {
		t.Fatal(err)
	}
	snapshot := persistence.Snapshot{FormatVersion: 1, Viewport: persistence.Viewport{Zoom: 1}, Nodes: []persistence.Node{
		{ID: targetID, Data: persistence.NodeData{Kind: "image", Name: "图片", Prompt: "小猫", TextReferenceIDs: []string{textID}, ModelSelection: &persistence.ModelSelection{Provider: persistence.ProviderAPIMart, ModelID: "seedream-5-0-lite", Parameters: map[string]any{"size": "1:1", "resolution": "2K", "n": 1}}}},
		{ID: textID, Data: persistence.NodeData{Kind: "text", Name: "场景", Text: "雨中的街道"}},
	}}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	version, err := f.db.SaveOwnedCanvas(ctx, canvasID, f.user.session.User.ID, 0, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	session := f.sdkClient(t, f.token(t))
	quote, _ := callMCPResult[quoteGenerationOutput](t, session, "quote_generation", map[string]any{"canvasId": canvasID, "nodeId": targetID})
	taskID := identity.ID()
	args := map[string]any{"canvasId": canvasID, "nodeId": targetID, "taskId": taskID, "canvasVersion": quote.CanvasVersion, "points": quote.Points, "priceVersion": quote.PriceVersion, "quoteHash": quote.QuoteHash}
	callMCPResult[generationTaskOutput](t, session, "start_generation", args)
	task, err := f.db.Task(ctx, taskID)
	if err != nil || task.Input.Prompt != "雨中的街道\n\n小猫" {
		t.Fatalf("wrong submitted text: %+v %v", task.Input, err)
	}
	snapshot.Nodes[1].Data.Text = "晴朗的草原"
	if _, err = f.db.SaveOwnedCanvas(ctx, canvasID, f.user.session.User.ID, version, snapshot); err != nil {
		t.Fatal(err)
	}
	task, err = f.db.Task(ctx, taskID)
	if err != nil || task.Input.Prompt != "雨中的街道\n\n小猫" {
		t.Fatal("submitted task changed with the text node")
	}
	args["taskId"] = identity.ID()
	expectMCPFailure(t, session, "start_generation", args, "重新报价")
}
