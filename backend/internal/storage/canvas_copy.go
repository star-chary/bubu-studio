package storage

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"
)

// CopyCanvasAsset copies an existing private object inside OSS. The target key
// is stable for this new node, so a lost response can safely be retried.
func (s *Store) CopyCanvasAsset(ctx context.Context, source Asset, target Scope) (Asset, error) {
	if ValidateScope(target) != nil || ValidateScope(source.Scope) != nil || source.CanvasID != target.CanvasID || source.NodeID == target.NodeID {
		return Asset{}, &Error{400, "INVALID_ASSET_SCOPE", "素材归属无效，无法复制。"}
	}
	media, err := s.keys.validateKey(source.Key)
	if err != nil {
		return Asset{}, err
	}
	root := fmt.Sprintf("%s/canvases/%s/%s/%s/%s/", s.keys.root, source.CanvasID, source.Source, source.Kind, source.NodeID)
	if !strings.HasPrefix(source.Key, root) || media.kind != source.Kind || media.mime != source.ContentType || source.Bytes <= 0 {
		return Asset{}, &Error{400, "INVALID_ASSET_KEY", "素材信息无效，无法复制。"}
	}
	copier, ok := s.objects.(interface {
		Copy(context.Context, string, string, string, map[string]string) error
	})
	if !ok {
		return Asset{}, &Error{503, "STORAGE_NOT_CONFIGURED", "素材复制服务暂不可用。"}
	}
	// Object UUIDs are generated independently of node IDs for normal uploads.
	// A copied node gets one deterministic key for retry and recovery.
	key := fmt.Sprintf("%s/canvases/%s/%s/%s/%s/%s%s", s.keys.root, target.CanvasID, source.Source, source.Kind, target.NodeID, target.NodeID, media.extension)
	metadata := map[string]string{"canvas-id": target.CanvasID, "node-id": target.NodeID, "source": string(source.Source), "asset-file": path.Base(key)}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := copier.Copy(ctx, source.Key, key, source.ContentType, metadata); err != nil {
		return Asset{}, publicWriteFailure(err)
	}
	result := source
	result.Key = key
	result.URL = "/api/assets/content?key=" + url.QueryEscape(key)
	result.Scope = target
	result.CopiedFrom = source.Key
	return result, nil
}
