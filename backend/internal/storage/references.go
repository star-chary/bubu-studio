package storage

import (
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"
	"time"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

// Reference keys come from saved canvas assets, never arbitrary client URLs.
// Canvas scoping is not user authorization; the local MVP has no accounts yet.
func (s *Store) ReferenceURLs(ctx context.Context, scope Scope, keys []string) ([]string, error) {
	if ValidateScope(scope) != nil || len(keys) == 0 || len(keys) > 14 {
		return nil, &Error{400, "INVALID_REFERENCES", "参考图片参数无效。"}
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		media, err := s.keys.validateKey(key)
		if err != nil || media.kind != Image || media.mime == "image/avif" {
			return nil, &Error{400, "INVALID_REFERENCE", "请选择已保存的 PNG、JPEG、WebP、GIF 或 BMP 参考图片。"}
		}
		parts := strings.Split(strings.TrimPrefix(key, s.keys.root+"/canvases/"), "/")
		if parts[0] != scope.CanvasID || parts[3] == scope.NodeID || seen[key] {
			return nil, &Error{400, "INVALID_REFERENCE_SCOPE", "参考图片必须来自当前画布的其他节点，且不能重复。"}
		}
		seen[key] = true
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	urls := make([]string, 0, len(keys))
	for _, key := range keys {
		content, err := s.objects.Get(ctx, key, "")
		if err != nil {
			return nil, &Error{422, "REFERENCE_UNAVAILABLE", "参考图片无法读取，请确认素材已保存，或重新上传。"}
		}
		// Decode only the header, not the full pixels; cap metadata reads as well.
		config, format, decodeErr := image.DecodeConfig(io.LimitReader(content.Body, 1<<20))
		content.Body.Close()
		if content.Bytes <= 0 || content.Bytes > MaxImageBytes || decodeErr != nil || !validReferenceDimensions(config) {
			return nil, &Error{422, "INVALID_REFERENCE_IMAGE", "参考图片须不超过 20 MB，宽高均大于 14 像素、宽高比在 1:16～16:1 之间，总像素不超过 3600 万；请检查图片或转换为 PNG/JPEG。"}
		}
		media, _ := s.keys.validateKey(key)
		if "image/"+format != media.mime {
			return nil, &Error{422, "INVALID_REFERENCE_IMAGE", "参考图片实际格式与保存信息不一致，请重新上传。"}
		}
		address, err := s.objects.SignGet(ctx, key)
		if err != nil {
			return nil, &Error{502, "REFERENCE_SIGN_FAILED", "无法准备参考图片，请稍后重试。"}
		}
		urls = append(urls, address)
	}
	return urls, nil
}

func validReferenceDimensions(config image.Config) bool {
	w, h := int64(config.Width), int64(config.Height)
	return w > 14 && h > 14 && w <= 36_000_000 && h <= 36_000_000 && w*h <= 36_000_000 && w <= 16*h && h <= 16*w
}
