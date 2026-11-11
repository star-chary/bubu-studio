package persistence

import (
	"context"
	"errors"
	"testing"

	"frame-space/backend/internal/storage"
)

func TestCanvasCopySourceValidation(t *testing.T) {
	const targetID = "30303030-3030-4030-8030-303030303030"
	node := Node{ID: targetID, Data: NodeData{Kind: "image", Origin: "upload", CopySourceKey: "source"}}
	source := storage.Asset{Key: "source", Scope: storage.Scope{CanvasID: canvasID, NodeID: nodeID}, Kind: storage.Image, Source: storage.Upload}
	if !validCopySource(node, canvasID, source) {
		t.Fatal("valid copy rejected")
	}
	wrong := source
	wrong.CanvasID = targetID
	if validCopySource(node, canvasID, wrong) {
		t.Fatal("other canvas accepted")
	}
	wrong = source
	wrong.NodeID = targetID
	if validCopySource(node, canvasID, wrong) {
		t.Fatal("self copy accepted")
	}
	wrong = source
	wrong.Kind = storage.Video
	if validCopySource(node, canvasID, wrong) {
		t.Fatal("wrong media kind accepted")
	}
	wrong = source
	wrong.Source = storage.Generated
	if validCopySource(node, canvasID, wrong) {
		t.Fatal("wrong origin accepted")
	}
}

func TestCanvasCopyPersistenceRejectsLateCompletion(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	const targetID = "30303030-3030-4030-8030-303030303030"
	source := storage.Asset{Key: "source", Scope: storage.Scope{CanvasID: canvasID, NodeID: nodeID}, Kind: storage.Image, Source: storage.Upload}
	if err := s.RecordAsset(ctx, source); err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot()
	snapshot.Nodes = append(snapshot.Nodes, Node{ID: targetID, Data: NodeData{Kind: "image", Name: "copy", Origin: "upload", CopySourceKey: source.Key}})
	if _, err := s.SaveCanvas(ctx, canvasID, 1, snapshot); err != nil {
		t.Fatal(err)
	}
	got, err := s.CanvasCopySource(ctx, canvasID, targetID)
	if err != nil || got.Key != source.Key {
		t.Fatalf("source: %+v %v", got, err)
	}
	copy := source
	copy.Key = "copy"
	copy.NodeID = targetID
	copy.CopiedFrom = source.Key
	if err = s.RecordCanvasCopy(ctx, canvasID, targetID, source.Key, copy); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordCanvasCopy(ctx, canvasID, targetID, source.Key, copy); err != nil {
		t.Fatal("retry should be idempotent", err)
	}
	snapshot.Nodes = snapshot.Nodes[:1]
	if _, err = s.SaveCanvas(ctx, canvasID, 2, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordCanvasCopy(ctx, canvasID, targetID, source.Key, copy); !errors.Is(err, ErrConflict) {
		t.Fatalf("late copy attached after deletion: %v", err)
	}
}
