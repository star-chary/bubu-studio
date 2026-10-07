package storage

import (
	"context"
	"encoding/json"
	"image"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Metadata is measured from file contents on the server, never from the client.
type MediaMetadata struct {
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	FrameRate       float64 `json:"frameRate,omitempty"`
	VideoCodec      string  `json:"videoCodec,omitempty"`
	AudioCodec      string  `json:"audioCodec,omitempty"`
}

type probeResult struct {
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		FrameRate   string `json:"avg_frame_rate"`
		Duration    string `json:"duration"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func positiveNumber(value string) float64 {
	n, _ := strconv.ParseFloat(value, 64)
	if math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
		return 0
	}
	return n
}

func inspectMedia(ctx context.Context, file *os.File, kind Kind) (*MediaMetadata, error) {
	if kind == Image {
		config, _, err := image.DecodeConfig(io.LimitReader(file, 1<<20))
		if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
			return nil, &Error{500, "FILE_READ_FAILED", "无法读取待保存文件。"}
		}
		// Existing preview-only formats such as AVIF may lack a Go header decoder.
		if err != nil {
			return nil, nil
		}
		return &MediaMetadata{Width: config.Width, Height: config.Height}, nil
	}
	program := strings.TrimSpace(os.Getenv("FFPROBE_PATH"))
	if program == "" {
		program = "ffprobe"
	}
	program, err := exec.LookPath(program)
	if err != nil {
		return nil, &Error{503, "MEDIA_PROBE_UNAVAILABLE", "服务端尚未配置 ffprobe，无法校验音视频；文件仍可本地预览。"}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// No shell, URLs, playlists or client-supplied command arguments. Limit the
	// probe to local file access, metadata only, and bounded time/output.
	cmd := exec.CommandContext(ctx, program, "-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "format=duration:stream=codec_type,codec_name,width,height,avg_frame_rate,duration:stream_disposition=attached_pic", "-of", "json", file.Name())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &Error{500, "MEDIA_PROBE_FAILED", "无法读取音视频参数。"}
	}
	if err = cmd.Start(); err != nil {
		return nil, &Error{503, "MEDIA_PROBE_UNAVAILABLE", "无法启动 ffprobe，请检查服务端配置。"}
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, (256<<10)+1))
	if len(data) > 256<<10 || readErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	var result probeResult
	if readErr != nil || waitErr != nil || len(data) > 256<<10 || json.Unmarshal(data, &result) != nil {
		return nil, &Error{422, "INVALID_MEDIA_METADATA", "无法读取音视频参数，请检查文件是否损坏或转换为 MP4、WAV、MP3。"}
	}
	info := &MediaMetadata{DurationSeconds: positiveNumber(result.Format.Duration)}
	for _, stream := range result.Streams {
		info.DurationSeconds = math.Max(info.DurationSeconds, positiveNumber(stream.Duration))
		if stream.CodecType == "video" && stream.Disposition.AttachedPic == 0 && info.VideoCodec == "" {
			info.Width, info.Height, info.VideoCodec = stream.Width, stream.Height, stream.CodecName
			rate := strings.Split(stream.FrameRate, "/")
			if len(rate) == 2 && positiveNumber(rate[1]) > 0 {
				info.FrameRate = positiveNumber(rate[0]) / positiveNumber(rate[1])
			}
		}
		if stream.CodecType == "audio" && info.AudioCodec == "" {
			info.AudioCodec = stream.CodecName
		}
	}
	if info.DurationSeconds == 0 || (kind == Video && (info.VideoCodec == "" || info.Width <= 0 || info.Height <= 0)) || (kind == Audio && (info.AudioCodec == "" || info.VideoCodec != "")) {
		return nil, &Error{422, "INVALID_MEDIA_METADATA", "文件缺少有效的音视频轨道或时长，请重新导出素材。"}
	}
	return info, nil
}
