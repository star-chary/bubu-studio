package persistence

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const textNodeA = "40404040-4040-4040-8040-404040404040"
const textNodeB = "50505050-5050-4050-8050-505050505050"

func textSnapshot() Snapshot {
	s := testSnapshot()
	s.FormatVersion = 1
	s.Nodes[0].Data.TextReferenceIDs = []string{textNodeA, textNodeB}
	s.Nodes = append(s.Nodes,
		Node{ID: textNodeA, Data: NodeData{Kind: "text", Name: "场景", Text: "海边"}},
		Node{ID: textNodeB, Data: NodeData{Kind: "text", Name: "风格", Text: "水彩"}},
	)
	return s
}

func TestTextSnapshotValidation(t *testing.T) {
	if err := textSnapshot().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Snapshot){
		"old client format":             func(s *Snapshot) { s.FormatVersion = 0 },
		"overlong text":                 func(s *Snapshot) { s.Nodes[1].Data.Text = strings.Repeat("字", 10001) },
		"text as file reference":        func(s *Snapshot) { s.Nodes[0].Data.ReferenceIDs = []string{textNodeA} },
		"duplicate text reference":      func(s *Snapshot) { s.Nodes[0].Data.TextReferenceIDs = []string{textNodeA, textNodeA} },
		"missing text reference":        func(s *Snapshot) { s.Nodes[0].Data.TextReferenceIDs = []string{"60606060-6060-4060-8060-606060606060"} },
		"nontext as text reference":     func(s *Snapshot) { s.Nodes[0].Data.TextReferenceIDs = []string{nodeID} },
		"text cannot have a model":      func(s *Snapshot) { s.Nodes[1].Data.ModelSelection = &ModelSelection{Provider: ProviderAPIMart} },
		"uploaded node cannot use text": func(s *Snapshot) { s.Nodes[0].Data.Origin = "upload" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := textSnapshot()
			mutate(&s)
			if s.Validate() == nil {
				t.Fatal("invalid snapshot was accepted")
			}
		})
	}
}

func TestTextPromptFreezesTextAndPreservesMediaMentions(t *testing.T) {
	s := textSnapshot()
	s.Nodes[0].Data.Prompt = "参考 @素材"
	s.Nodes[0].Data.PromptParts = []PromptPart{{Type: "text", Text: "参考 "}, {Type: "reference", NodeID: "60606060-6060-4060-8060-606060606060", Kind: "image", Name: "素材"}}
	prompt, parts, err := s.EffectivePrompt(s.Nodes[0])
	if err != nil || prompt != "海边\n\n水彩\n\n参考 @素材" {
		t.Fatalf("wrong combined prompt: %q %v", prompt, err)
	}
	compiled, err := CompilePrompt(Input{NodeID: nodeID, Prompt: prompt, PromptParts: parts}, "image", []Reference{{NodeID: "60606060-6060-4060-8060-606060606060", Kind: "image"}})
	if err != nil || compiled != "海边\n\n水彩\n\n参考 图片1" {
		t.Fatalf("wrong compiled prompt: %q %v", compiled, err)
	}
	s.Nodes[1].Data.Text = "雪山"
	if prompt != "海边\n\n水彩\n\n参考 @素材" || parts[0].Text != "海边\n\n水彩\n\n" {
		t.Fatal("submitted input changed with the source")
	}
}

func TestPostgresTextSnapshotRejectsOlderClientDowngrade(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	s := textSnapshot()
	version, err := db.SaveOwnedCanvas(ctx, canvasID, testUserID, 1, s)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := db.Canvas(ctx, canvasID)
	if err != nil || loaded.Snapshot.Nodes[1].Data.Text != "海边" || loaded.Snapshot.FormatVersion != 1 {
		t.Fatalf("text was not restored: %+v %v", loaded.Snapshot, err)
	}
	// An old client can fetch the latest version, omit the new fields and even
	// remove every text node. The stored format must still prevent its save.
	legacy := testSnapshot()
	if _, err = db.SaveOwnedCanvas(ctx, canvasID, testUserID, version, legacy); !errors.Is(err, ErrConflict) {
		t.Fatalf("old client overwrote the text snapshot: %v", err)
	}
	loaded, err = db.Canvas(ctx, canvasID)
	if err != nil || loaded.Snapshot.Nodes[1].Data.Text != "海边" {
		t.Fatal("text was lost after the rejected save")
	}
	legacy.FormatVersion = 1 // A current client may intentionally remove text.
	if _, err = db.SaveOwnedCanvas(ctx, canvasID, testUserID, version, legacy); err != nil {
		t.Fatalf("current client could not remove text nodes: %v", err)
	}
}
