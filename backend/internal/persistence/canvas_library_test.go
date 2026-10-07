package persistence

import (
	"context"
	"reflect"
	"testing"
)

func TestCanvasLibraryIsolation(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	otherID := "50505050-5050-4050-8050-505050505050"
	if err := s.CreateCanvas(ctx, otherID, "第二张画布", testUserID); err != nil {
		t.Fatal(err)
	}
	other, err := s.Canvas(ctx, otherID)
	if err != nil || len(other.Snapshot.Nodes) != 0 || len(other.Tasks) != 0 || len(other.Assets) != 0 {
		t.Fatalf("new canvas is not empty: %+v %v", other, err)
	}
	secondSnapshot := testSnapshot()
	// Even identical node IDs must resolve through the owning canvas.
	secondSnapshot.Nodes[0].Data.Prompt = "第二张草稿"
	secondSnapshot.Viewport.Zoom = .5
	if _, err := s.SaveCanvas(ctx, otherID, 0, secondSnapshot); err != nil {
		t.Fatal(err)
	}
	first, err := s.Canvas(ctx, canvasID)
	if err != nil || !reflect.DeepEqual(first.Snapshot, testSnapshot()) {
		t.Fatalf("first canvas changed: %+v %v", first, err)
	}
	if err := s.CreateCanvas(ctx, otherID, "重试创建", testUserID); err != nil {
		t.Fatal(err)
	}
	other, err = s.Canvas(ctx, otherID)
	if err != nil || other.Title != "第二张画布" || !reflect.DeepEqual(other.Snapshot, secondSnapshot) {
		t.Fatalf("retry changed canvas: %+v %v", other, err)
	}
	items, err := s.Canvases(ctx, testUserID, 10, 0)
	if err != nil || len(items) != 2 || items[0].ID != otherID || items[1].ID != canvasID {
		t.Fatalf("unexpected list: %+v %v", items, err)
	}
	if _, err := s.SaveCanvas(ctx, canvasID, first.Version, first.Snapshot); err != nil {
		t.Fatal(err)
	}
	items, err = s.Canvases(ctx, testUserID, 10, 0)
	if err != nil || items[0].ID != canvasID {
		t.Fatalf("saved canvas not first: %+v %v", items, err)
	}
}

func TestCanvasLibraryMigrationKeepsExistingContent(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	// Recreate the pre-library schema inside this test's isolated schema only.
	if _, err := s.Pool.Exec(ctx, `ALTER TABLE canvases DROP COLUMN title; DROP INDEX canvases_recent_idx; DELETE FROM schema_migrations WHERE version='003_canvas_library.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	canvas, err := s.Canvas(ctx, canvasID)
	if err != nil || canvas.Title != "未命名画布" || canvas.Version != 1 || !reflect.DeepEqual(canvas.Snapshot, testSnapshot()) {
		t.Fatalf("migration changed existing canvas: %+v %v", canvas, err)
	}
}
