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

// APIMart Seedance 2.0 supports 480P–720P reference video. The project keeps
// the existing conservative media constraints in addition to this boundary.
func CheckAPIMartSeedanceVideoReference(a Asset) *ReferenceCheck {
	check := CheckVideoReference(a)
	if !check.Eligible || a.Kind != Video {
		return check
	}
	shortSide := min(a.Media.Width, a.Media.Height)
	if shortSide < 480 || shortSide > 720 {
		return &ReferenceCheck{Reason: "参考视频短边须为 480～720 像素"}
	}
	return check
}

// Apply the provider's additional video constraints after the shared checks.
func CheckAPIMartVideoReference(a Asset, model string) *ReferenceCheck {
	if model == "seedance-2.0" || model == "seedance-2.0-fast" {
		return CheckAPIMartSeedanceVideoReference(a)
	}
	check := CheckVideoReference(a)
	if !check.Eligible {
		return check
	}
	motion := model == "kling-v2-6-motion-control" || model == "kling-v3-motion-control"
	if motion && a.Kind == Image {
		if a.ContentType != "image/jpeg" && a.ContentType != "image/png" && a.ContentType != "image/webp" {
			return &ReferenceCheck{Reason: "动作控制的参考图片请使用 JPEG、PNG 或 WebP"}
		}
		if a.Bytes > 10_000_000 {
			return &ReferenceCheck{Reason: "动作控制的参考图片不能超过 10 MB"}
		}
	}
	if a.Kind != Video {
		return check
	}
	switch model {
	case "seedance-2.5":
		if int64(a.Media.Width)*int64(a.Media.Height) < 409600 {
			return &ReferenceCheck{Reason: "参考视频总像素须至少为 409600"}
		}
		if a.Media.AudioCodec != "" && a.Media.AudioCodec != "aac" && a.Media.AudioCodec != "mp3" {
			return &ReferenceCheck{Reason: "参考视频音轨须为 AAC 或 MP3"}
		}
	case "kling-v2-6-motion-control", "kling-v3-motion-control":
		if a.Bytes > 100_000_000 {
			return &ReferenceCheck{Reason: "动作参考视频不能超过 100 MB"}
		}
		if a.Media.DurationSeconds < 3 {
			return &ReferenceCheck{Reason: "动作参考视频须至少 3 秒"}
		}
	default:
		return &ReferenceCheck{Reason: "当前模型不支持视频参考"}
	}
	return check
}
