package apimart

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"frame-space/backend/internal/ark"
)

type ImageResponse struct {
	Status  string
	URL     string
	Failure *ark.APIError
}

func imageFailure(status int, detail providerError) *ark.APIError {
	failure := apiFailure(status, detail)
	if failure.Code == "APIMART_REJECTED" {
		failure.Message = "中转站拒绝了图片请求，请检查模型参数和参考图片。"
	} else if failure.Code == "APIMART_BALANCE_INSUFFICIENT" {
		failure.Message = "中转站账户余额不足，图片任务未能提交。"
	}
	return failure
}

func (c *Client) CreateImage(ctx context.Context, in ImageRequest) (string, error) {
	if c.apiKey == "" {
		return "", &ark.APIError{Status: 503, Code: "MODEL_NOT_CONFIGURED", Message: "后端尚未配置 APIMart API Key。"}
	}
	if !NormalizeImageRequest(&in, len(in.Images)) {
		return "", &ark.APIError{Status: 400, Code: "INVALID_IMAGE_INPUT", Message: "图片模型、参数或参考图片无效。"}
	}
	for _, image := range in.Images {
		if !validResultURL(image) {
			return "", &ark.APIError{Status: 400, Code: "INVALID_IMAGE_INPUT", Message: "参考图片地址无效。"}
		}
	}
	payload := map[string]any{"prompt": in.Prompt}
	path := "/images/generations"
	if in.Model == "midjourney" {
		path = "/midjourney/generations"
	} else {
		payload["model"] = in.Model
	}
	for key, value := range in.Parameters {
		payload[key] = value
	}
	if len(in.Images) > 0 {
		payload["image_urls"] = in.Images
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return "", &ark.APIError{Status: 503, Code: "MODEL_NOT_CONFIGURED", Message: "中转站接口配置无效。"}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	unknown := &ark.APIError{Status: 502, Code: "SUBMISSION_UNKNOWN", Message: "无法确认中转站是否已受理图片，已保留任务记录，请核查后再试，未自动重新提交。"}
	resp, err := c.http.Do(req)
	if err != nil {
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
		return "", imageFailure(resp.StatusCode, wire.Error)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || decodeErr != nil || wire.Code != 200 || len(wire.Data) != 1 || !taskIDPattern.MatchString(wire.Data[0].TaskID) {
		return "", unknown
	}
	return wire.Data[0].TaskID, nil
}

func (c *Client) QueryImage(ctx context.Context, id, model string) (ImageResponse, error) {
	var out ImageResponse
	if !taskIDPattern.MatchString(id) {
		return out, &ark.PollError{}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	path := "/tasks/" + url.PathEscape(id) + "?language=zh"
	if model == "midjourney" {
		path = "/midjourney/" + url.PathEscape(id)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
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
		return out, &ark.PollError{RetryAfter: time.Duration(max(0, seconds)) * time.Second}
	}
	if model == "midjourney" {
		var wire struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Grid   string `json:"grid_image_url"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&wire) != nil || wire.ID != id {
			return out, &ark.PollError{}
		}
		switch wire.Status {
		case "NOT_START", "SUBMITTED":
			out.Status = "queued"
		case "IN_PROGRESS":
			out.Status = "running"
		case "SUCCESS":
			if !validResultURL(wire.Grid) {
				return out, &ark.PollError{}
			}
			out.Status, out.URL = "succeeded", wire.Grid
		case "FAILURE":
			out.Status = "failed"
		default:
			return out, &ark.PollError{}
		}
	} else {
		var wire struct {
			Code int `json:"code"`
			Data struct {
				ID     string        `json:"id"`
				Status string        `json:"status"`
				Error  providerError `json:"error"`
				Result struct {
					Images []struct {
						URL json.RawMessage `json:"url"`
					} `json:"images"`
				} `json:"result"`
			} `json:"data"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&wire) != nil || wire.Code != 200 || wire.Data.ID != id {
			return out, &ark.PollError{}
		}
		switch wire.Data.Status {
		case "pending":
			out.Status = "queued"
		case "processing":
			out.Status = "running"
		case "completed":
			if len(wire.Data.Result.Images) == 0 {
				return out, &ark.PollError{}
			}
			out.URL = videoURL(wire.Data.Result.Images[0].URL)
			if out.URL == "" {
				return out, &ark.PollError{}
			}
			out.Status = "succeeded"
		case "failed":
			out.Status = "failed"
			out.Failure = imageFailure(422, wire.Data.Error)
		case "cancelled":
			out.Status = "failed"
		default:
			return out, &ark.PollError{}
		}
	}
	if out.Status == "failed" && (out.Failure == nil || out.Failure.Code == "APIMART_REJECTED") {
		out.Failure = &ark.APIError{Status: 422, Code: "APIMART_GENERATION_FAILED", Message: "中转站图片生成失败，请检查提示词、模型参数或参考图片。"}
	}
	return out, nil
}
