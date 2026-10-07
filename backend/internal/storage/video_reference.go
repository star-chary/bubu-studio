package storage

import (
	"math"
	"strings"
)

// This checks a single asset for Seedance reference mode, not a generation
// request. Total counts/durations and DB ownership must also be checked at submit.
type ReferenceCheck struct {
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason,omitempty"`
}

func CheckVideoReference(a Asset) *ReferenceCheck {
	bad := func(reason string) *ReferenceCheck { return &ReferenceCheck{Reason: reason} }
	m := a.Media
	if m == nil {
		return bad("缺少服务端媒体参数，请重新上传以校验视频参考要求")
	}
	if a.Bytes <= 0 {
		return bad("素材大小无效")
	}
	if a.Kind == Image {
		switch a.ContentType {
		case "image/png", "image/jpeg", "image/webp", "image/gif", "image/bmp":
		default:
			return bad("参考图片请使用 PNG、JPEG、WebP、GIF 或 BMP")
		}
		if a.Bytes > MaxImageBytes {
			return bad("参考图片不能超过 20 MiB")
		}
	} else if a.Kind == Video {
		if a.ContentType != "video/mp4" && a.ContentType != "video/quicktime" {
			return bad("参考视频请使用 MP4 或 MOV")
		}
		if a.Bytes > 200_000_000 {
			return bad("参考视频不能超过 200 MB")
		}
		if m.VideoCodec != "h264" && m.VideoCodec != "hevc" {
			return bad("参考视频编码须为 H.264 或 H.265")
		}
		if m.AudioCodec != "" && m.AudioCodec != "aac" && m.AudioCodec != "mp3" && !(a.ContentType == "video/quicktime" && strings.HasPrefix(m.AudioCodec, "pcm_")) {
			return bad("参考视频音轨须为 AAC、MP3，或 MOV 中的 PCM")
		}
		if math.IsNaN(m.FrameRate) || math.IsInf(m.FrameRate, 0) || m.FrameRate < 24 || m.FrameRate > 60 {
			return bad("参考视频帧率须在 24～60 FPS 之间")
		}
	} else if a.Kind == Audio {
		if a.ContentType != "audio/mpeg" && a.ContentType != "audio/wav" {
			return bad("参考音频请使用 WAV 或 MP3")
		}
		if a.Bytes > MaxAudioBytes {
			return bad("参考音频不能超过 15 MB")
		}
		if m.AudioCodec == "" || m.VideoCodec != "" {
			return bad("参考音频须包含有效音轨")
		}
	} else {
		return bad("不支持的参考类型")
	}
	if a.Kind != Audio {
		w, h := int64(m.Width), int64(m.Height)
		if w < 300 || h < 300 || w > 6000 || h > 6000 || float64(w)/float64(h) < .4 || float64(w)/float64(h) > 2.5 {
			return bad("参考宽高须为 300～6000 像素，宽高比为 0.4～2.5")
		}
		if a.Kind == Video && (w*h < 407696 || w*h > 8295044) {
			return bad("参考视频总像素须为 407696～8295044")
		}
	}
	if a.Kind != Image && (math.IsNaN(m.DurationSeconds) || math.IsInf(m.DurationSeconds, 0) || m.DurationSeconds < 2 || m.DurationSeconds > 30) {
		return bad("单个参考音视频时长须为 2～30 秒")
	}
	return &ReferenceCheck{Eligible: true}
}
