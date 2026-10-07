package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const promptSourceID = "30303030-3030-4030-8030-303030303030"

func promptSnapshot() Snapshot {
	s := testSnapshot()
	s.Nodes[0].Data.Prompt = "参考@茶叶.png 的颜色"
	s.Nodes[0].Data.PromptParts = []PromptPart{{Type: "text", Text: "参考"}, {Type: "reference", NodeID: promptSourceID, Kind: "image", Name: "茶叶.png"}, {Type: "text", Text: " 的颜色"}}
	return s
}

func TestPromptDraftContract(t *testing.T) {
	// A missing source is retained for an explicit invalid-chip display, not
	// silently converted to a different resource or removed from the user's text.
	s := promptSnapshot()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(s)
	var restored Snapshot
	if err := json.Unmarshal(data, &restored); err != nil || !reflect.DeepEqual(restored, s) {
		t.Fatal("draft did not round trip", err)
	}
	cases := []struct {
		name   string
		change func(*Snapshot)
	}{
		{"text mismatch", func(s *Snapshot) { s.Nodes[0].Data.Prompt = "different" }},
		{"unknown type", func(s *Snapshot) { s.Nodes[0].Data.PromptParts[1].Type = "html" }},
		{"url as identity", func(s *Snapshot) { s.Nodes[0].Data.PromptParts[1].NodeID = "https://example.test/image.png" }},
		{"self reference", func(s *Snapshot) { s.Nodes[0].Data.PromptParts[1].NodeID = nodeID }},
		{"video in image", func(s *Snapshot) { s.Nodes[0].Data.PromptParts[1].Kind = "video" }},
		{"unsupported audio", func(s *Snapshot) { s.Nodes[0].Data.PromptParts[1].Kind = "audio" }},
		{"mixed shape", func(s *Snapshot) { s.Nodes[0].Data.PromptParts[1].Text = "extra" }},
		{"empty name", func(s *Snapshot) { s.Nodes[0].Data.PromptParts[1].Name = " " }},
		{"long name", func(s *Snapshot) { s.Nodes[0].Data.PromptParts[1].Name = strings.Repeat("名", 256) }},
		{"too many parts", func(s *Snapshot) { s.Nodes[0].Data.PromptParts = make([]PromptPart, 1001) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := promptSnapshot()
			tc.change(&s)
			if s.Validate() == nil {
				t.Fatal("invalid draft accepted")
			}
		})
	}
	if err := testSnapshot().Validate(); err != nil {
		t.Fatal("old text-only draft broken", err)
	}
}

func TestMissingPromptReferencesRejectedAndExistingTaskStable(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	s := promptSnapshot()
	if _, err := db.SaveCanvas(ctx, canvasID, 1, s); err != nil {
		t.Fatal(err)
	}
	canvas, err := db.Canvas(ctx, canvasID)
	if err != nil || !reflect.DeepEqual(canvas.Snapshot, s) {
		t.Fatal("database lost draft references", err)
	}
	mentioned := input()
	mentioned.Prompt = s.Nodes[0].Data.Prompt
	mentioned.PromptParts = s.Nodes[0].Data.PromptParts
	if _, err := db.CreateTask(ctx, promptSourceID, mentioned); !errors.Is(err, ErrPromptReferences) {
		t.Fatal("new mention task should be rejected before queueing", err)
	}
	if _, err := db.Task(ctx, promptSourceID); !errors.Is(err, ErrNotFound) {
		t.Fatal("rejected task was created", err)
	}
	// Removing all chips restores the existing image path; retries of an existing
	// task still return its immutable input even if the draft later gains a chip.
	if _, err := db.SaveCanvas(ctx, canvasID, 2, testSnapshot()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateTask(ctx, promptSourceID, input()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveCanvas(ctx, canvasID, 3, s); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateTask(ctx, promptSourceID, input()); err != nil {
		t.Fatal("idempotent retry should still resolve old task", err)
	}
}
