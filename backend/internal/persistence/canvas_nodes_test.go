package persistence

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"frame-space/backend/internal/identity"
)

func TestCanvasNodeCreationSerializesRetriesAndPreservesExistingSnapshot(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	// Old/new clients can leave fields that the current Go structs do not know.
	// Creating an unrelated node must preserve them, including large numbers.
	_, err := s.Pool.Exec(ctx, `UPDATE canvases SET snapshot=jsonb_set(
		jsonb_set(snapshot, '{future}', '{"serial":9007199254740993}'::jsonb),
		'{nodes,0,data,future}', '{"serial":9007199254740993}'::jsonb) WHERE id=$1`, canvasID)
	if err != nil {
		t.Fatal(err)
	}
	id := identity.ID()
	var created atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			result, err := s.CreateOwnedCanvasNode(ctx, testUserID, canvasID, id, "video", nil)
			if err != nil || result.NodeID != id || result.Version != 2 {
				t.Errorf("retry: %+v %v", result, err)
				return
			}
			if result.Created {
				created.Add(1)
			}
		})
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatalf("expected one creation, got %d", created.Load())
	}
	canvas, err := s.Canvas(ctx, canvasID)
	if err != nil || canvas.Version != 2 || len(canvas.Snapshot.Nodes) != 2 || canvas.Snapshot.Nodes[0].Data.Prompt != "draft" || canvas.Snapshot.Nodes[1].Data.VideoMode != "reference" {
		t.Fatalf("snapshot was not preserved: %+v %v", canvas, err)
	}
	var futurePreserved, nodeFuturePreserved bool
	err = s.Pool.QueryRow(ctx, `SELECT snapshot->'future' = '{"serial":9007199254740993}'::jsonb,
		snapshot->'nodes'->0->'data'->'future' = '{"serial":9007199254740993}'::jsonb FROM canvases WHERE id=$1`, canvasID).Scan(&futurePreserved, &nodeFuturePreserved)
	if err != nil || !futurePreserved || !nodeFuturePreserved {
		t.Fatalf("unrecognized data was not preserved: %v %v %v", futurePreserved, nodeFuturePreserved, err)
	}
	if _, err := s.CreateOwnedCanvasNode(ctx, testUserID, canvasID, id, "image", nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("different kind with same ID: %v", err)
	}
	if _, err := s.CreateOwnedCanvasNode(ctx, identity.ID(), canvasID, identity.ID(), "image", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user could access canvas: %v", err)
	}
}

func TestCanvasNodeCreationPreservesConcurrentDistinctNodes(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := s.CreateOwnedCanvasNode(ctx, testUserID, canvasID, identity.ID(), "image", nil); err != nil {
				t.Errorf("concurrent node creation: %v", err)
			}
		})
	}
	wg.Wait()
	canvas, err := s.Canvas(ctx, canvasID)
	if err != nil || canvas.Version != 9 || len(canvas.Snapshot.Nodes) != 9 {
		t.Fatalf("concurrent nodes were lost: %+v %v", canvas, err)
	}
}

func TestCanvasNodeCreationAndBrowserSaveDoNotOverwriteEachOther(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	snapshot := testSnapshot()
	snapshot.Nodes[0].Data.Prompt = "browser change"
	var saveErr error
	var createErr error
	var wg sync.WaitGroup
	wg.Go(func() { _, saveErr = s.SaveOwnedCanvas(ctx, canvasID, testUserID, 1, snapshot) })
	wg.Go(func() { _, createErr = s.CreateOwnedCanvasNode(ctx, testUserID, canvasID, identity.ID(), "image", nil) })
	wg.Wait()
	if createErr != nil || (saveErr != nil && !errors.Is(saveErr, ErrConflict)) {
		t.Fatalf("unexpected result: create=%v save=%v", createErr, saveErr)
	}
	canvas, err := s.Canvas(ctx, canvasID)
	if err != nil || len(canvas.Snapshot.Nodes) != 2 {
		t.Fatalf("created node lost: %+v %v", canvas, err)
	}
	if saveErr == nil && canvas.Snapshot.Nodes[0].Data.Prompt != "browser change" {
		t.Fatal("successful browser save was overwritten")
	}
	if errors.Is(saveErr, ErrConflict) && canvas.Snapshot.Nodes[0].Data.Prompt != "draft" {
		t.Fatal("a rejected browser save unexpectedly changed the snapshot")
	}
}
