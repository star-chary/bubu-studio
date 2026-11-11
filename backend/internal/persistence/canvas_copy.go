package persistence

import (
	"context"
	"encoding/json"
	"errors"

	"frame-space/backend/internal/storage"
	"github.com/jackc/pgx/v5"
)

// CanvasCopySource accepts only an asset already belonging to this canvas.
// The pending key lives in the saved snapshot, so a failed copy can resume
// after a reload without accepting a new source from a later request.
func (s *Store) CanvasCopySource(ctx context.Context, canvasID, nodeID string) (storage.Asset, error) {
	var source storage.Asset
	if !ValidID(canvasID) || !ValidID(nodeID) {
		return source, ErrInvalid
	}
	node, err := s.Node(ctx, canvasID, nodeID)
	if err != nil {
		return source, err
	}
	if node.Data.CopySourceKey == "" {
		return source, ErrInvalid
	}
	err = s.Pool.QueryRow(ctx, `SELECT metadata FROM assets WHERE canvas_id=$1 AND key=$2`, canvasID, node.Data.CopySourceKey).Scan(&source)
	if err != nil {
		return source, mapError(err)
	}
	if !validCopySource(node, canvasID, source) {
		return storage.Asset{}, ErrInvalid
	}
	return source, nil
}

func validCopySource(node Node, canvasID string, source storage.Asset) bool {
	kinds := map[string]storage.Kind{"image": storage.Image, "video": storage.Video, "audio": storage.Audio}
	origins := map[string]storage.Source{"upload": storage.Upload, "generated": storage.Generated}
	return source.Key != "" && source.Key == node.Data.CopySourceKey && source.CanvasID == canvasID &&
		source.NodeID != node.ID && source.Kind == kinds[node.Data.Kind] && source.Source == origins[node.Data.Origin]
}

// A retry after the database committed returns the existing immutable copy
// without issuing a second OSS copy request.
func (s *Store) ExistingCanvasCopy(ctx context.Context, canvasID, nodeID, sourceKey string) (storage.Asset, bool, error) {
	var asset storage.Asset
	err := s.Pool.QueryRow(ctx, `SELECT metadata FROM assets WHERE canvas_id=$1 AND node_id=$2 AND metadata->>'copiedFrom'=$3 ORDER BY created_at DESC LIMIT 1`, canvasID, nodeID, sourceKey).Scan(&asset)
	if errors.Is(err, pgx.ErrNoRows) {
		return asset, false, nil
	}
	return asset, err == nil, err
}

// RecordCanvasCopy checks the still-current snapshot while holding the canvas
// row lock. This prevents a late copy response from attaching to a deleted or
// repurposed node, while ordinary snapshot writes retain their version checks.
func (s *Store) RecordCanvasCopy(ctx context.Context, canvasID, nodeID, sourceKey string, asset storage.Asset) error {
	if asset.CanvasID != canvasID || asset.NodeID != nodeID || asset.Key == "" || asset.Key == sourceKey || sourceKey == "" || asset.CopiedFrom != sourceKey {
		return ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var snapshot Snapshot
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT snapshot FROM canvases WHERE id=$1 FOR UPDATE`, canvasID).Scan(&raw); err != nil {
		return mapError(err)
	}
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	var target *Node
	for i := range snapshot.Nodes {
		if snapshot.Nodes[i].ID == nodeID {
			target = &snapshot.Nodes[i]
			break
		}
	}
	if target == nil || target.Data.CopySourceKey != sourceKey {
		return ErrConflict
	}
	var source storage.Asset
	if err = tx.QueryRow(ctx, `SELECT metadata FROM assets WHERE canvas_id=$1 AND key=$2`, canvasID, sourceKey).Scan(&source); err != nil {
		return mapError(err)
	}
	if !validCopySource(*target, canvasID, source) || source.Kind != asset.Kind || source.Source != asset.Source {
		return ErrInvalid
	}
	if _, err = tx.Exec(ctx, `INSERT INTO assets(key,canvas_id,node_id,metadata) VALUES($1,$2,$3,$4) ON CONFLICT(key) DO NOTHING`, asset.Key, canvasID, nodeID, asset); err != nil {
		return err
	}
	// The key is deterministic per destination node. Verify an existing row
	// belongs to this node before treating a repeated request as successful.
	var existing storage.Asset
	if err = tx.QueryRow(ctx, `SELECT metadata FROM assets WHERE key=$1 AND canvas_id=$2 AND node_id=$3`, asset.Key, canvasID, nodeID).Scan(&existing); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		}
		return err
	}
	return tx.Commit(ctx)
}
