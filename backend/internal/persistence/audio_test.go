package persistence

import (
	"context"
	"frame-space/backend/internal/storage"
	"testing"
)

const audioNodeID = "30303030-3030-4030-8030-303030303030"

func audioSnapshot() Snapshot {
	s := testSnapshot()
	s.Nodes[0].Data.Kind = "video"
	s.Nodes[0].Data.ReferenceIDs = []string{audioNodeID}
	s.Nodes = append(s.Nodes, Node{ID: audioNodeID, Position: Position{100, 200}, Data: NodeData{Kind: "audio", Name: "节奏.wav", Origin: "upload"}})
	return s
}

func TestAudioSnapshotReferenceBoundaries(t *testing.T) {
	s := audioSnapshot()
	if s.Validate() != nil {
		t.Fatal("video cannot reference audio")
	}
	s.Nodes[0].Data.Kind = "image"
	if s.Validate() == nil {
		t.Fatal("image can reference audio")
	}
	s = audioSnapshot()
	s.Nodes[1].Data.ReferenceIDs = []string{nodeID}
	if s.Validate() == nil {
		t.Fatal("audio can reference another node")
	}
	s = audioSnapshot()
	s.Nodes[1].Data.Origin = "generated"
	if s.Validate() == nil {
		t.Fatal("audio generation should not be exposed")
	}
}

func TestAudioMetadataAndReferencesSurviveDatabaseReload(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if _, err := db.SaveCanvas(ctx, canvasID, 1, audioSnapshot()); err != nil {
		t.Fatal(err)
	}
	asset := storage.Asset{Key: "audio-test-key", URL: "/api/assets/content?key=audio-test-key", Kind: storage.Audio, Source: storage.Upload, Scope: storage.Scope{CanvasID: canvasID, NodeID: audioNodeID}, ContentType: "audio/wav", Bytes: 192078, Media: &storage.MediaMetadata{DurationSeconds: 2, AudioCodec: "pcm_s16le"}, VideoReference: &storage.ReferenceCheck{Eligible: true}}
	if err := db.RecordAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	canvas, err := db.Canvas(ctx, canvasID)
	if err != nil {
		t.Fatal(err)
	}
	if len(canvas.Assets) != 1 || canvas.Assets[0].Media == nil || canvas.Assets[0].Media.DurationSeconds != 2 || canvas.Assets[0].VideoReference == nil || !canvas.Assets[0].VideoReference.Eligible || canvas.Snapshot.Nodes[0].Data.ReferenceIDs[0] != audioNodeID {
		t.Fatal("audio metadata/reference was lost")
	}
}
