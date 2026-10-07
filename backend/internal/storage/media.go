package storage

import (
	"io"
	"mime"
	"path"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

const MaxImageBytes int64 = 20 << 20
const MaxVideoBytes int64 = 200 << 20
const MaxAudioBytes int64 = 15_000_000

type mediaType struct {
	kind            Kind
	mime, extension string
}

var allowedMedia = map[string]mediaType{
	"image/jpeg": {Image, "image/jpeg", ".jpg"}, "image/png": {Image, "image/png", ".png"},
	"image/webp": {Image, "image/webp", ".webp"}, "image/gif": {Image, "image/gif", ".gif"},
	"image/avif": {Image, "image/avif", ".avif"}, "image/bmp": {Image, "image/bmp", ".bmp"},
	"video/mp4": {Video, "video/mp4", ".mp4"}, "video/webm": {Video, "video/webm", ".webm"},
	"video/quicktime": {Video, "video/quicktime", ".mov"}, "video/x-m4v": {Video, "video/x-m4v", ".m4v"},
	"video/ogg":   {Video, "video/ogg", ".ogv"},
	"audio/mpeg":  {Audio, "audio/mpeg", ".mp3"},
	"audio/wav":   {Audio, "audio/wav", ".wav"},
	"audio/x-wav": {Audio, "audio/wav", ".wav"},
}

func detectMedia(reader io.ReadSeeker, size int64) (mediaType, error) {
	if size <= 0 {
		return mediaType{}, &Error{400, "EMPTY_FILE", "不能上传空文件。"}
	}
	detected, err := mimetype.DetectReader(reader)
	if err != nil {
		return mediaType{}, &Error{400, "INVALID_MEDIA", "无法读取文件内容。"}
	}
	mimeType, _, _ := mime.ParseMediaType(detected.String())
	media, ok := allowedMedia[mimeType]
	if !ok {
		return mediaType{}, &Error{415, "UNSUPPORTED_MEDIA", "请使用 PNG、JPEG、WebP、GIF、AVIF、BMP 图片，MP4、WebM、MOV 等视频，或 WAV、MP3 音频。"}
	}
	if (media.kind == Image && size > MaxImageBytes) || (media.kind == Audio && size > MaxAudioBytes) || size > MaxVideoBytes {
		return mediaType{}, &Error{413, "FILE_TOO_LARGE", "图片不能超过 20 MiB，视频不能超过 200 MiB，音频不能超过 15 MB。"}
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return mediaType{}, &Error{500, "FILE_READ_FAILED", "无法读取待保存文件。"}
	}
	return media, nil
}

// Only keys allocated under this application's current environment can be read.
func (b *KeyBuilder) validateKey(key string) (mediaType, error) {
	root := b.root + "/canvases/"
	if !strings.HasPrefix(key, root) {
		return mediaType{}, &Error{400, "INVALID_ASSET_KEY", "无效的资源路径。"}
	}
	parts := strings.Split(strings.TrimPrefix(key, root), "/")
	if len(parts) != 5 || ValidateScope(Scope{parts[0], parts[3]}) != nil || (parts[1] != string(Upload) && parts[1] != string(Generated)) {
		return mediaType{}, &Error{400, "INVALID_ASSET_KEY", "无效的资源路径。"}
	}
	extension := path.Ext(parts[4])
	if !uuid.MatchString(strings.TrimSuffix(parts[4], extension)) {
		return mediaType{}, &Error{400, "INVALID_ASSET_KEY", "无效的资源路径。"}
	}
	for _, media := range allowedMedia {
		if string(media.kind) == parts[2] && media.extension == extension {
			return media, nil
		}
	}
	return mediaType{}, &Error{400, "INVALID_ASSET_KEY", "无效的资源路径。"}
}
