package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"

	"frame-space/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

// CanvasNodeCreation describes an empty node saved in an existing canvas.
type CanvasNodeCreation struct {
	CanvasID string   `json:"canvasId"`
	NodeID   string   `json:"nodeId"`
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	Position Position `json:"position"`
	Version  int64    `json:"version"`
	Created  bool     `json:"created"`
}

var numberedMediaNode = regexp.MustCompile(`^(?:图片|视频)节点 ([0-9]+)$`)

// CreateOwnedCanvasNode appends to the latest saved canvas while holding its
// row lock. Concurrent whole-snapshot saves retain their existing version check.
// A supplied node ID makes retries safe while that node remains on the canvas.
func (s *Store) CreateOwnedCanvasNode(ctx context.Context, userID, canvasID, nodeID, kind string, position *Position) (CanvasNodeCreation, error) {
	var result CanvasNodeCreation
	if !ValidID(userID) || !ValidID(canvasID) || (kind != "image" && kind != "video") {
		return result, ErrInvalid
	}
	if nodeID == "" {
		nodeID = identity.ID()
	} else if !ValidID(nodeID) {
		return result, ErrInvalid
	}
	if position != nil && (math.IsNaN(position.X) || math.IsInf(position.X, 0) || math.Abs(position.X) > 1e7 || math.IsNaN(position.Y) || math.IsInf(position.Y, 0) || math.Abs(position.Y) > 1e7) {
		return result, ErrInvalid
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	var rawSnapshot []byte
	var version int64
	err = tx.QueryRow(ctx, `SELECT snapshot,version FROM canvases
		WHERE id=$1 AND owner_user_id=$2 AND EXISTS (SELECT 1 FROM users WHERE id=$2 AND status='active') FOR UPDATE`, canvasID, userID).Scan(&rawSnapshot, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	var snapshot Snapshot
	if err = json.Unmarshal(rawSnapshot, &snapshot); err != nil {
		return result, err
	}
	for _, node := range snapshot.Nodes {
		if node.ID != nodeID {
			continue
		}
		if node.Data.Kind != kind {
			return result, ErrConflict
		}
		return CanvasNodeCreation{canvasID, nodeID, kind, node.Data.Name, node.Position, version, false}, nil
	}
	if len(snapshot.Nodes) >= 300 {
		return result, ErrInvalid
	}
	sequence := 1
	for _, node := range snapshot.Nodes {
		matches := numberedMediaNode.FindStringSubmatch(node.Data.Name)
		if len(matches) == 2 {
			if n, err := strconv.Atoi(matches[1]); err == nil && n >= sequence && n < 1_000_000_000 {
				sequence = n + 1
			}
		}
	}
	label := "图片"
	if kind == "video" {
		label = "视频"
	}
	point := defaultCanvasNodePosition(snapshot.Nodes)
	if position != nil {
		point = *position
	}
	data := NodeData{Kind: kind, Name: fmt.Sprintf("%s节点 %d", label, sequence), ModelSelection: &ModelSelection{Provider: ProviderAPIMart, ModelID: "", Parameters: map[string]any{}}}
	if kind == "video" {
		data.VideoMode = "reference"
	}
	newNode := Node{ID: nodeID, Position: point, Data: data}
	snapshot.Nodes = append(snapshot.Nodes, newNode)
	if err = snapshot.Validate(); err != nil {
		return result, err
	}
	// Validate using the current model, but append to the raw snapshot so that
	// fields from other versions and exact existing JSON numbers are preserved.
	var document map[string]json.RawMessage
	if err = json.Unmarshal(rawSnapshot, &document); err != nil {
		return result, err
	}
	var rawNodes []json.RawMessage
	if err = json.Unmarshal(document["nodes"], &rawNodes); err != nil {
		return result, err
	}
	rawNode, err := json.Marshal(newNode)
	if err != nil {
		return result, err
	}
	document["nodes"], err = json.Marshal(append(rawNodes, rawNode))
	if err != nil {
		return result, err
	}
	payload, err := json.Marshal(document)
	if err != nil || len(payload) > (2<<20)-256 {
		return result, ErrInvalid
	}
	err = tx.QueryRow(ctx, `UPDATE canvases SET snapshot=$3,version=version+1,updated_at=now()
		WHERE id=$1 AND owner_user_id=$2 RETURNING version`, canvasID, userID, payload).Scan(&version)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	return CanvasNodeCreation{canvasID, nodeID, kind, data.Name, point, version, true}, nil
}

// Empty media cards are at most 480x328. Find a free slot near the last saved
// node; this uses canvas coordinates and does not assume a browser viewport.
func defaultCanvasNodePosition(nodes []Node) Position {
	if len(nodes) == 0 {
		return Position{}
	}
	anchor := nodes[len(nodes)-1].Position
	for i := 1; i <= len(nodes)*2+1; i++ {
		for _, direction := range []int{1, -1} {
			point := Position{X: anchor.X + float64(direction*i*560), Y: anchor.Y}
			if math.Abs(point.X) > 1e7 {
				continue
			}
			occupied := false
			for _, node := range nodes {
				if math.Abs(point.X-node.Position.X) < 520 && math.Abs(point.Y-node.Position.Y) < 330 {
					occupied = true
					break
				}
			}
			if !occupied {
				return point
			}
		}
	}
	return Position{}
}
