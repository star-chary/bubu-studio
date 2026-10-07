package storage

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type TimedSigner interface {
	SignGetFor(context.Context, string, time.Duration) (string, error)
}

type VideoReferenceLimits struct {
	MaxImages, MaxVideos, MaxAudios int
	MaxMediaSeconds                 float64
	AllowAudioOnly                  bool
}

var defaultVideoReferenceLimits = VideoReferenceLimits{30, 10, 10, 30, true}

// Re-read and probe the actual object before signing. Database/browser metadata
// alone cannot prove that a reference is still readable or media is valid.
func (s *Store) VideoReferenceURLs(ctx context.Context, scope Scope, keys []string) ([]string, error) {
	return s.VideoReferenceURLsWithLimits(ctx, scope, keys, defaultVideoReferenceLimits)
}

func (s *Store) VideoReferenceURLsWithLimits(ctx context.Context, scope Scope, keys []string, limits VideoReferenceLimits) ([]string, error) {
	if ValidateScope(scope) != nil || len(keys) == 0 || limits.MaxImages < 1 || limits.MaxVideos < 0 || limits.MaxAudios < 0 || limits.MaxMediaSeconds < 2 || len(keys) > limits.MaxImages+limits.MaxVideos+limits.MaxAudios {
		return nil, &Error{422, "INVALID_REFERENCES", "参考素材数量无效或超过模型限制。"}
	}
	signer, ok := s.objects.(TimedSigner)
	if !ok {
		return nil, &Error{503, "REFERENCE_SIGN_FAILED", "未配置视频参考签名服务。"}
	}
	seen := map[string]bool{}
	counts := map[Kind]int{}
	durations := map[Kind]float64{}
	for _, key := range keys {
		media, err := s.keys.validateKey(key)
		if err != nil {
			return nil, err
		}
		parts := strings.Split(strings.TrimPrefix(key, s.keys.root+"/canvases/"), "/")
		if parts[0] != scope.CanvasID || parts[3] == scope.NodeID || seen[key] {
			return nil, &Error{422, "INVALID_REFERENCE_SCOPE", "参考素材归属无效或重复。"}
		}
		seen[key] = true
		counts[media.kind]++
		if counts[Image] > limits.MaxImages || counts[Video] > limits.MaxVideos || counts[Audio] > limits.MaxAudios {
			return nil, &Error{422, "TOO_MANY_REFERENCES", "参考素材数量超过模型限制。"}
		}
		asset, err := s.probeReference(ctx, key, media)
		if err != nil {
			return nil, err
		}
		check := CheckVideoReference(asset)
		if !check.Eligible {
			return nil, &Error{422, "INVALID_VIDEO_REFERENCE", check.Reason}
		}
		if media.kind != Image {
			if asset.Media.DurationSeconds > limits.MaxMediaSeconds {
				return nil, &Error{422, "REFERENCE_DURATION_EXCEEDED", "单个参考音视频时长超过模型限制。"}
			}
			durations[media.kind] += asset.Media.DurationSeconds
		}
	}
	if durations[Video] > limits.MaxMediaSeconds || durations[Audio] > limits.MaxMediaSeconds {
		return nil, &Error{422, "REFERENCE_DURATION_EXCEEDED", "参考视频或音频总时长超过模型限制。"}
	}
	if !limits.AllowAudioOnly && counts[Image]+counts[Video] == 0 {
		return nil, &Error{422, "INVALID_REFERENCES", "当前模型不能仅参考音频。"}
	}
	urls := make([]string, 0, len(keys))
	// Sign after all preflight reads so each URL has the full queue window.
	for _, key := range keys {
		u, err := signer.SignGetFor(ctx, key, 2*time.Hour)
		if err != nil {
			return nil, err
		}
		urls = append(urls, u)
	}
	return urls, nil
}
func (s *Store) probeReference(ctx context.Context, key string, expected mediaType) (Asset, error) {
	content, err := s.objects.Get(ctx, key, "")
	if err != nil {
		return Asset{}, err
	}
	defer content.Body.Close()
	limit := MaxImageBytes
	if expected.kind == Video {
		limit = 200_000_000
	}
	if expected.kind == Audio {
		limit = MaxAudioBytes
	}
	if content.Bytes > limit {
		return Asset{}, &Error{422, "REFERENCE_TOO_LARGE", "参考素材超过大小限制。"}
	}
	file, err := os.CreateTemp(s.tempDir, "reference-*")
	if err != nil {
		return Asset{}, err
	}
	defer func() { file.Close(); os.Remove(file.Name()) }()
	size, err := io.Copy(file, io.LimitReader(content.Body, limit+1))
	if err != nil {
		return Asset{}, err
	}
	if size > limit {
		return Asset{}, &Error{422, "REFERENCE_TOO_LARGE", "参考素材超过大小限制。"}
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return Asset{}, err
	}
	actual, err := detectMedia(file, size)
	if err != nil {
		return Asset{}, err
	}
	if actual.kind != expected.kind || actual.mime != expected.mime {
		return Asset{}, &Error{422, "INVALID_REFERENCE_MEDIA", "参考素材格式与登记信息不一致。"}
	}
	info, err := inspectMedia(ctx, file, actual.kind)
	if err != nil {
		return Asset{}, err
	}
	return Asset{Key: key, Kind: actual.kind, ContentType: actual.mime, Bytes: size, Media: info}, nil
}
func (s *Store) SaveGeneratedVideo(ctx context.Context, sourceURL, model string, scope Scope) (Asset, error) {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	u, err := url.Parse(sourceURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return Asset{}, &Error{502, "INVALID_SOURCE_URL", "模型结果地址无效。"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return Asset{}, err
	}
	client := *s.downloader
	client.Timeout = 300 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return Asset{}, &Error{502, "RESULT_DOWNLOAD_FAILED", "无法读取生成视频，请重试保存。"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength > MaxVideoBytes {
		return Asset{}, &Error{502, "RESULT_DOWNLOAD_FAILED", "视频结果不可读取或超过保存大小限制。"}
	}
	return s.save(ctx, io.LimitReader(resp.Body, MaxVideoBytes+1), scope, Generated, model, Video)
}
