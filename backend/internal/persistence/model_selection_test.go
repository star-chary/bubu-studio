package persistence

import (
	"context"
	"errors"
	"testing"

	"frame-space/backend/internal/ark"
)

func apimartSnapshot(kind string, params map[string]any) Snapshot {
	return Snapshot{
		Nodes: []Node{{
			ID:       nodeID,
			Position: Position{12, 34},
			Data: NodeData{
				Kind:   kind,
				Name:   "APIMart 节点",
				Prompt: "draft",
				ModelSelection: &ModelSelection{
					Provider:   ProviderAPIMart,
					ModelID:    "gpt-image-1",
					Parameters: params,
				},
			},
		}},
		Viewport: Viewport{X: 30, Y: 40, Zoom: 1.2},
	}
}

func TestSnapshotValidationAcceptsApimartDraft(t *testing.T) {
	// 草稿参数不受 Ark 白名单约束；目录下架或参数规则更新不影响保存。
	s := apimartSnapshot("image", map[string]any{
		"size": "1024x1024", "n": float64(4), "quality": "high", "nested": map[string]any{"a": []any{1, 2}},
	})
	s.Nodes[0].Data.ModelSelection.CatalogVersion = "abc123"
	s.Nodes[0].Data.ModelSelection.SchemaVersion = "2026-07-30"
	if err := s.Validate(); err != nil {
		t.Fatalf("apimart draft rejected: %v", err)
	}
	// 未选择状态（目录尚未就绪）：modelId 为空同样可以保存，不回退 Ark。
	s.Nodes[0].Data.ModelSelection.ModelID = ""
	if err := s.Validate(); err != nil {
		t.Fatalf("unselected draft rejected: %v", err)
	}
}

func TestSnapshotValidationSkipsArkRulesForApimartNodes(t *testing.T) {
	// APIMart 节点携带的旧 Ark 字段不再套用 Ark 白名单校验。
	s := apimartSnapshot("image", map[string]any{})
	s.Nodes[0].Data.ImageModel = "gpt-image-1"
	s.Nodes[0].Data.ImageOptions = &ImageNodeOptions{ImageOptions: ark.ImageOptions{Resolution: "8K", Ratio: "5:1"}, Count: 7}
	if err := s.Validate(); err != nil {
		t.Fatalf("ark rules applied to apimart node: %v", err)
	}
	v := apimartSnapshot("video", map[string]any{})
	v.Nodes[0].Data.VideoModel = "seedance-2-5"
	v.Nodes[0].Data.VideoOptions = &VideoOptions{Resolution: "1080p", Ratio: "32:9", Duration: 120}
	if err := v.Validate(); err != nil {
		t.Fatalf("ark rules applied to apimart video node: %v", err)
	}
}

func TestSnapshotValidationModelSelectionStructure(t *testing.T) {
	s := apimartSnapshot("image", map[string]any{})
	s.Nodes[0].Data.ModelSelection.Provider = "unknown"
	if s.Validate() == nil {
		t.Fatal("unknown provider accepted")
	}
	s = apimartSnapshot("image", map[string]any{})
	s.Nodes[0].Data.ModelSelection.ModelID = func() string {
		out := make([]rune, 201)
		for i := range out {
			out[i] = 'm'
		}
		return string(out)
	}()
	if s.Validate() == nil {
		t.Fatal("oversized model id accepted")
	}
	s = apimartSnapshot("image", map[string]any{"deep": map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": map[string]any{"e": map[string]any{"f": map[string]any{"g": map[string]any{"h": map[string]any{"i": 1}}}}}}}}}})
	if s.Validate() == nil {
		t.Fatal("over-deep parameters accepted")
	}
}

func TestSnapshotValidationLegacyNodesUnchanged(t *testing.T) {
	// 历史节点（没有 modelSelection）继续按旧 Ark 规则校验。
	s := testSnapshot()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.Nodes[0].Data.ImageOptions = &ImageNodeOptions{ImageOptions: ark.ImageOptions{Resolution: "8K", Ratio: "5:1"}, Count: 7}
	if s.Validate() == nil {
		t.Fatal("legacy ark node bypassed ark validation")
	}
}

func TestApimartNodeGenerationRejectedBeforeCredits(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	if err := s.GrantCredits(ctx, testUserID, 200, "apimart-gate-test-grant"); err != nil {
		t.Fatal(err)
	}
	snapshot := apimartSnapshot("image", map[string]any{})
	snapshot.Nodes[0].Data.Origin = "generated"
	if _, err := s.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	input := Input{Prompt: "draft", Model: "gpt-image-1", CanvasID: canvasID, NodeID: nodeID}
	if _, err := s.QuoteCredits(ctx, testUserID, "image", input); !errors.Is(err, ErrModelGenerationNotEnabled) {
		t.Fatalf("quote not rejected: %v", err)
	}
	taskID := "40404040-4040-4040-8040-404040404040"
	if _, err := s.CreateBilledTask(ctx, testUserID, taskID, "image", input, 0, CreditPriceVersion); !errors.Is(err, ErrModelGenerationNotEnabled) {
		t.Fatalf("task creation not rejected: %v", err)
	}
	// 拒绝发生在冻结积分与创建任务之前。
	account, err := s.Credits(ctx, testUserID)
	if err != nil || account.Available != 200 || account.Reserved != 0 {
		t.Fatalf("credits changed: %+v, %v", account, err)
	}
	if _, err := s.Task(ctx, taskID); !errors.Is(err, ErrNotFound) {
		t.Fatal("task row created for apimart node")
	}
	var ledgerCount int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_ledger WHERE operation <> 'grant'`).Scan(&ledgerCount); err != nil || ledgerCount != 0 {
		t.Fatalf("ledger rows created: %d, %v", ledgerCount, err)
	}
}

func TestApimartVideoNodeGenerationRejected(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	snapshot := apimartSnapshot("video", map[string]any{"duration": 5})
	snapshot.Nodes[0].Data.Origin = "generated"
	if _, err := s.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	input := Input{Prompt: "scene", Model: "seedance-2-5", Mode: "text", CanvasID: canvasID, NodeID: nodeID, Resolution: "480p", Ratio: "16:9", Duration: 5}
	if _, err := s.QuoteCredits(ctx, testUserID, "video", input); !errors.Is(err, ErrModelGenerationNotEnabled) {
		t.Fatalf("video quote not rejected: %v", err)
	}
}

func TestApimartDraftRoundTripPreservesSelection(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	params := map[string]any{"size": "1024x1024", "n": float64(4)}
	snapshot := apimartSnapshot("image", params)
	snapshot.Nodes[0].Data.ModelSelection.CatalogVersion = "v1"
	snapshot.Nodes[0].Data.ModelSelection.SchemaVersion = "2026-07-30"
	if _, err := s.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	canvas, err := s.Canvas(ctx, canvasID)
	if err != nil {
		t.Fatal(err)
	}
	sel := canvas.Snapshot.Nodes[0].Data.ModelSelection
	if sel == nil || sel.Provider != ProviderAPIMart || sel.ModelID != "gpt-image-1" || sel.CatalogVersion != "v1" || sel.SchemaVersion != "2026-07-30" {
		t.Fatalf("selection lost: %+v", sel)
	}
	if sel.Parameters["n"] != float64(4) {
		t.Fatalf("parameters lost: %+v", sel.Parameters)
	}
}

func TestLegacyCanvasWithoutSelectionStillReads(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	canvas, err := s.Canvas(ctx, canvasID)
	if err != nil || canvas.Snapshot.Nodes[0].Data.ModelSelection != nil {
		t.Fatalf("legacy canvas misread: %+v, %v", canvas.Snapshot.Nodes[0].Data, err)
	}
	if canvas.Snapshot.Nodes[0].Data.Prompt != "draft" {
		t.Fatalf("legacy draft lost: %+v", canvas.Snapshot.Nodes[0].Data)
	}
}
