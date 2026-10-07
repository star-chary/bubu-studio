package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"time"
)

type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string { return e.Message }

type Asset struct {
	Key            string          `json:"key"`
	URL            string          `json:"url"`
	Kind           Kind            `json:"kind"`
	ContentType    string          `json:"contentType"`
	Bytes          int64           `json:"bytes"`
	Source         Source          `json:"source"`
	Media          *MediaMetadata  `json:"media,omitempty"`
	VideoReference *ReferenceCheck `json:"videoReference,omitempty"`
	Scope
}

type Content struct {
	Body                            io.ReadCloser
	Status                          int
	Bytes                           int64
	ContentType, ContentRange, ETag string
}

type objectClient interface {
	SignGet(context.Context, string) (string, error)
	Put(context.Context, string, string, int64, io.Reader, map[string]string) error
	Get(context.Context, string, string) (*Content, error)
}

type Store struct {
	objects     objectClient
	keys        *KeyBuilder
	tempDir     string
	downloader  *http.Client
	uploadSlots chan struct{}
}

func (s *Store) SaveUpload(ctx context.Context, reader io.Reader, scope Scope) (Asset, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	return s.save(ctx, reader, scope, Upload, "")
}

func (s *Store) SaveGenerated(ctx context.Context, sourceURL, model string, scope Scope) (Asset, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	address, err := url.Parse(sourceURL)
	if err != nil || address.Scheme != "https" || address.Host == "" || address.User != nil || (address.Port() != "" && address.Port() != "443") {
		return Asset{}, &Error{502, "INVALID_SOURCE_URL", "模型结果地址无效，无法转存。"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return Asset{}, &Error{502, "INVALID_SOURCE_URL", "模型结果地址无效，无法转存。"}
	}
	response, err := s.downloader.Do(req)
	if err != nil {
		return Asset{}, &Error{502, "RESULT_DOWNLOAD_FAILED", "无法读取模型生成的图片。"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Asset{}, &Error{502, "RESULT_DOWNLOAD_FAILED", "模型结果地址暂时不可用。"}
	}
	if response.ContentLength > MaxImageBytes {
		return Asset{}, &Error{413, "FILE_TOO_LARGE", "生成图片超过 20 MB，无法转存。"}
	}
	return s.save(ctx, io.LimitReader(response.Body, MaxImageBytes+1), scope, Generated, model)
}

func (s *Store) save(ctx context.Context, reader io.Reader, scope Scope, source Source, model string, resultKinds ...Kind) (Asset, error) {
	if err := ValidateScope(scope); err != nil {
		return Asset{}, &Error{400, "INVALID_ASSET_SCOPE", err.Error()}
	}
	select {
	case s.uploadSlots <- struct{}{}:
		defer func() { <-s.uploadSlots }()
	default:
		return Asset{}, &Error{429, "STORAGE_BUSY", "正在保存其他素材，请稍后重试。"}
	}
	// Files spool in the backend directory on D:, not memory or the system temp directory.
	file, err := os.CreateTemp(s.tempDir, "asset-*")
	if err != nil {
		return Asset{}, &Error{500, "TEMP_FILE_FAILED", "无法准备上传文件，请检查后端临时目录。"}
	}
	defer func() { file.Close(); os.Remove(file.Name()) }()
	size, err := io.Copy(file, io.LimitReader(reader, MaxVideoBytes+1))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return Asset{}, &Error{413, "FILE_TOO_LARGE", "文件超过大小限制。"}
		}
		return Asset{}, &Error{400, "FILE_READ_FAILED", "文件传输中断，请重试保存。"}
	}
	if size > MaxVideoBytes {
		return Asset{}, &Error{413, "FILE_TOO_LARGE", "视频不能超过 200 MB。"}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Asset{}, &Error{500, "FILE_READ_FAILED", "无法读取待保存文件。"}
	}
	media, err := detectMedia(file, size)
	if err != nil {
		return Asset{}, err
	}
	expected := Image
	if len(resultKinds) > 0 {
		expected = resultKinds[0]
	}
	if source == Generated && (media.kind != expected || (expected == Video && media.mime != "video/mp4")) {
		return Asset{}, &Error{415, "INVALID_RESULT_MEDIA", "模型结果不是支持的图片格式。"}
	}
	info, err := inspectMedia(ctx, file, media.kind)
	if err != nil {
		return Asset{}, err
	}
	if source == Generated && expected == Video && (info == nil || info.Width <= 0 || info.Height <= 0 || info.DurationSeconds <= 0 || info.VideoCodec != "h264" || (info.AudioCodec != "" && info.AudioCodec != "aac")) {
		return Asset{}, &Error{415, "INVALID_RESULT_MEDIA", "模型视频不符合网页 MP4 播放要求。"}
	}
	key, err := s.keys.NewKey(scope, source, media.kind, media.extension)
	if err != nil {
		return Asset{}, &Error{400, "INVALID_ASSET_SCOPE", err.Error()}
	}
	metadata := map[string]string{"canvas-id": scope.CanvasID, "node-id": scope.NodeID, "source": string(source), "asset-file": path.Base(key)}
	if model != "" {
		metadata["model"] = model
	}
	if err := s.objects.Put(ctx, key, media.mime, size, file, metadata); err != nil {
		return Asset{}, publicWriteFailure(err)
	}
	asset := Asset{Key: key, URL: "/api/assets/content?key=" + url.QueryEscape(key), Kind: media.kind, ContentType: media.mime, Bytes: size, Source: source, Scope: scope, Media: info}
	asset.VideoReference = CheckVideoReference(asset)
	return asset, nil
}

var singleRange = regexp.MustCompile(`^bytes=(?:[0-9]+-[0-9]*|-[0-9]+)$`)

func (s *Store) Read(ctx context.Context, key, byteRange string) (*Content, error) {
	media, err := s.keys.validateKey(key)
	if err != nil {
		return nil, err
	}
	if byteRange != "" && (len(byteRange) > 80 || !singleRange.MatchString(byteRange)) {
		return nil, &Error{416, "INVALID_RANGE", "无效的文件读取范围。"}
	}
	content, err := s.objects.Get(ctx, key, byteRange)
	if err != nil {
		return nil, err
	}
	content.ContentType = media.mime
	return content, nil
}
