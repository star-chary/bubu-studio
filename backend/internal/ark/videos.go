package ark

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const VideoModel = "doubao-seedance-2-5-260628"
const VideoModel20 = "doubao-seedance-2-0-260128"
const VideoModel20Fast = "doubao-seedance-2-0-fast-260128"
const VideoModel20Mini = "doubao-seedance-2-0-mini-260615"
const VideosEndpoint = "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks"

type VideoModelSpec struct {
	MaxDuration         int
	MaxImages           int
	MaxVideos           int
	MaxAudios           int
	MaxReferenceSeconds float64
}

func VideoSpec(model string) (VideoModelSpec, bool) {
	switch model {
	case VideoModel:
		return VideoModelSpec{30, 30, 10, 10, 30}, true
	case VideoModel20, VideoModel20Fast, VideoModel20Mini:
		return VideoModelSpec{15, 9, 3, 3, 15}, true
	default:
		return VideoModelSpec{}, false
	}
}

type VideoReference struct{ Kind, URL string }
type VideoRequest struct {
	Prompt, Model, Mode, Resolution, Ratio string
	Duration                               int
	GenerateAudio                          bool
	References                             []VideoReference
}
type VideoResponse struct {
	ID              string  `json:"id"`
	Status          string  `json:"status"`
	Model           string  `json:"model"`
	Resolution      string  `json:"resolution"`
	Ratio           string  `json:"ratio"`
	Duration        int     `json:"duration"`
	FramesPerSecond float64 `json:"framespersecond"`
	Content         struct {
		URL string `json:"video_url"`
	} `json:"content"`
}
type PollError struct{ RetryAfter time.Duration }

func (e *PollError) Error() string {
	return "视频进度查询暂时失败，将继续查询同一个任务。"
}

var providerIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (c *Client) CreateVideo(ctx context.Context, in VideoRequest) (string, error) {
	if c.apiKey == "" {
		return "", &APIError{503, "MODEL_NOT_CONFIGURED", "后端尚未配置模型 API Key。"}
	}
	if in.Model == "" {
		in.Model = VideoModel // Compatibility with older queued video inputs.
	}
	if _, ok := VideoSpec(in.Model); !ok {
		return "", &APIError{400, "INVALID_VIDEO_INPUT", "视频模型或参考素材无效。"}
	}
	if in.Mode == "" {
		in.Mode = "reference" // Existing queued tasks predate the explicit mode.
	}
	if (in.Mode != "reference" && in.Mode != "text") || (in.Mode == "reference" && len(in.References) == 0) || (in.Mode == "text" && (strings.TrimSpace(in.Prompt) == "" || len(in.References) != 0)) {
		return "", &APIError{400, "INVALID_VIDEO_INPUT", "视频模式、提示词或参考素材无效。"}
	}
	content := []map[string]any{}
	if in.Prompt != "" {
		content = append(content, map[string]any{"type": "text", "text": in.Prompt})
	}
	for _, r := range in.References {
		if r.Kind != "image" && r.Kind != "video" && r.Kind != "audio" {
			return "", &APIError{400, "INVALID_VIDEO_INPUT", "参考类型无效。"}
		}
		content = append(content, map[string]any{"type": r.Kind + "_url", "role": "reference_" + r.Kind, r.Kind + "_url": map[string]string{"url": r.URL}})
	}
	parameters := map[string]any{"model": in.Model, "content": content, "resolution": in.Resolution, "ratio": in.Ratio, "duration": in.Duration, "generate_audio": in.GenerateAudio, "watermark": true, "execution_expires_after": 3600}
	if in.Model == VideoModel {
		parameters["output_format"] = "mp4"
		if in.Mode == "reference" {
			parameters["omni_reference_task_type"] = "reference"
		}
	}
	payload, _ := json.Marshal(parameters)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.videoEndpoint, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	// POST is intentionally performed once. Transport/5xx/invalid replies leave
	// acceptance ambiguous, so the caller must retain the local submission record.
	resp, err := c.http.Do(req)
	unknown := &APIError{502, "SUBMISSION_UNKNOWN", "无法确认视频是否已受理，可能已计费；请核查任务，未自动重新提交。"}
	if err != nil {
		return "", unknown
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 {
		code, message := "VIDEO_REJECTED", "模型未接受视频请求，请检查参考素材、提示词及模型权限。"
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			code, message = "MODEL_AUTH_FAILED", "模型鉴权或授权失败，请检查 API Key 和模型开通状态。"
		}
		if resp.StatusCode == 429 {
			code, message = "MODEL_LIMITED", "模型频率或额度受限，请检查方舟控制台。"
		}
		return "", &APIError{422, code, message}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", unknown
	}
	var out struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out) != nil || !providerIDPattern.MatchString(out.ID) {
		return "", unknown
	}
	return out.ID, nil
}
func (c *Client) QueryVideo(ctx context.Context, id string) (VideoResponse, error) {
	var out VideoResponse
	if !providerIDPattern.MatchString(id) {
		return out, &PollError{}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.videoEndpoint, "/")+"/"+url.PathEscape(id), nil)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return out, &PollError{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		delay := time.Duration(0)
		if n, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && n > 0 {
			delay = time.Duration(n) * time.Second
		} else if date, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil {
			delay = time.Until(date)
		}
		return out, &PollError{delay}
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out) != nil || out.ID != id {
		return VideoResponse{}, &PollError{}
	}
	return out, nil
}
