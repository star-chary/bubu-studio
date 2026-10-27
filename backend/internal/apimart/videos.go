package apimart

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"frame-space/backend/internal/ark"
)

const VideoModel = "seedance-2.5"

var taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var errorCodePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,80}$`)

type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{apiKey: strings.TrimSpace(apiKey), baseURL: "https://api.apimart.ai/v1", http: &http.Client{Timeout: 65 * time.Second}}
}

type providerError struct {
	Code    json.RawMessage `json:"code"`
	Type    string          `json:"type"`
	Message string          `json:"message"`
}

func apiFailure(status int, detail providerError) *ark.APIError {
	code, message := "APIMART_REJECTED", "中转站拒绝了视频请求，请检查模型参数和参考图片。"
	switch status {
	case 401, 403:
		code, message = "APIMART_AUTH_FAILED", "中转站认证或模型访问权限不足，请检查后端配置。"
	case 402:
		code, message = "APIMART_BALANCE_INSUFFICIENT", "中转站账户余额不足，视频任务未能提交。"
	case 429:
		code, message = "APIMART_RATE_LIMITED", "中转站请求过于频繁，请稍后再试。"
	default:
		text := strings.ToLower(detail.Type + " " + detail.Message)
		if strings.Contains(text, "sensitive") || strings.Contains(text, "moderation") || strings.Contains(text, "nsfw") || strings.Contains(text, "safety") || strings.Contains(text, "审核") {
			code, message = "APIMART_CONTENT_REJECTED", "中转站内容审核未通过，请调整提示词或参考图片。"
		}
	}
	return &ark.APIError{Status: status, Code: code, Message: message}
}

func logFailure(operation, localID, providerID string, status int, detail providerError) {
	code := strings.Trim(string(detail.Code), `"`)
	if !errorCodePattern.MatchString(code) {
		code = "unknown"
	}
	// Do not log API keys, prompts, signed media URLs or the raw error body.
	log.Printf("APIMart video %s: task=%s provider_task=%s http_status=%d code=%s", operation, localID, providerID, status, code)
}

func (c *Client) CreateVideo(ctx context.Context, in ark.VideoRequest) (string, error) {
	if c.apiKey == "" {
		return "", &ark.APIError{Status: 503, Code: "MODEL_NOT_CONFIGURED", Message: "后端尚未配置 APIMart API Key。"}
	}
	payload, valid := videoPayload(in)
	if !valid {
		return "", &ark.APIError{Status: 400, Code: "INVALID_VIDEO_INPUT", Message: "视频参数、提示词或参考图片无效。"}
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/videos/generations", bytes.NewReader(body))
	if err != nil {
		return "", &ark.APIError{Status: 503, Code: "MODEL_NOT_CONFIGURED", Message: "中转站接口配置无效。"}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	// Submit once. A timeout or invalid success response cannot prove rejection.
	unknown := &ark.APIError{Status: 502, Code: "SUBMISSION_UNKNOWN", Message: "无法确认中转站是否已受理视频，已保留任务记录，请核查后再试，未自动重新提交。"}
	resp, err := c.http.Do(req)
	if err != nil {
		logFailure("create", in.TaskID, "", 0, providerError{})
		return "", unknown
	}
	defer resp.Body.Close()
	var wire struct {
		Code  int           `json:"code"`
		Error providerError `json:"error"`
		Data  []struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&wire)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 && resp.StatusCode != 499 {
		logFailure("create", in.TaskID, "", resp.StatusCode, wire.Error)
		return "", apiFailure(resp.StatusCode, wire.Error)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || decodeErr != nil || wire.Code != 200 || len(wire.Data) != 1 || !taskIDPattern.MatchString(wire.Data[0].TaskID) {
		logFailure("create", in.TaskID, "", resp.StatusCode, wire.Error)
		return "", unknown
	}
	return wire.Data[0].TaskID, nil
}

func validResultURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && (u.Port() == "" || u.Port() == "443")
}

func videoURL(raw json.RawMessage) string {
	var urls []string
	if json.Unmarshal(raw, &urls) == nil && len(urls) > 0 && validResultURL(urls[0]) {
		return urls[0]
	}
	var single string
	if json.Unmarshal(raw, &single) == nil && validResultURL(single) {
		return single
	}
	return ""
}

func (c *Client) QueryVideo(ctx context.Context, id string) (ark.VideoResponse, error) {
	var out ark.VideoResponse
	if !taskIDPattern.MatchString(id) {
		return out, &ark.PollError{}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/tasks/"+url.PathEscape(id)+"?language=zh", nil)
	if err != nil {
		return out, &ark.PollError{}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return out, &ark.PollError{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		logFailure("query", "", id, resp.StatusCode, providerError{})
		return out, &ark.PollError{RetryAfter: time.Duration(max(0, seconds)) * time.Second}
	}
	var wire struct {
		Code int `json:"code"`
		Data struct {
			ID     string        `json:"id"`
			Model  string        `json:"model"`
			Status string        `json:"status"`
			Error  providerError `json:"error"`
			Result struct {
				Videos []struct {
					URL json.RawMessage `json:"url"`
				} `json:"videos"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&wire) != nil || wire.Code != 200 || wire.Data.ID != id {
		return out, &ark.PollError{}
	}
	out.ID, out.Model = id, wire.Data.Model
	switch wire.Data.Status {
	case "pending":
		out.Status = "queued"
	case "processing":
		out.Status = "running"
	case "completed":
		if len(wire.Data.Result.Videos) == 0 {
			return out, &ark.PollError{}
		}
		out.Content.URL = videoURL(wire.Data.Result.Videos[0].URL)
		if out.Content.URL == "" {
			return out, &ark.PollError{}
		}
		out.Status = "succeeded"
	case "failed":
		out.Status = "failed"
		out.Failure = apiFailure(422, wire.Data.Error)
		if out.Failure.Code == "APIMART_REJECTED" {
			out.Failure.Code, out.Failure.Message = "APIMART_GENERATION_FAILED", "中转站视频生成失败，请调整提示词或参考图片后再试。"
		}
		logFailure("query", "", id, resp.StatusCode, wire.Data.Error)
	case "cancelled":
		out.Status = "cancelled"
		out.Failure = &ark.APIError{Status: 422, Code: "APIMART_CANCELLED", Message: "中转站视频任务已取消。"}
	default:
		return out, &ark.PollError{}
	}
	return out, nil
}
