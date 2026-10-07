package ark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const ImagesEndpoint = "https://ark.cn-beijing.volces.com/api/v3/images/generations"

type ImageResult struct {
	URL   string `json:"url,omitempty"`
	Model string `json:"model"`
	Size  string `json:"size,omitempty"`
}

// APIError 只包含可向前端展示的固定消息，不透传供应商原始响应或凭据。
type APIError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return e.Message }

type Client struct {
	videoEndpoint string
	apiKey        string
	endpoint      string
	http          *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: strings.TrimSpace(apiKey), endpoint: ImagesEndpoint, videoEndpoint: VideosEndpoint,
		http: &http.Client{
			Timeout:       180 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (c *Client) Generate(ctx context.Context, prompt, model string, images ...string) (ImageResult, error) {
	model, err := ResolveImageModel(model)
	if err != nil {
		return ImageResult{}, err
	}
	if c.apiKey == "" {
		return ImageResult{}, &APIError{503, "MODEL_NOT_CONFIGURED", "后端尚未配置模型 API Key，请配置后重启后端。"}
	}
	if len(images) > MaxReferenceImages(model) {
		return ImageResult{}, &APIError{400, "TOO_MANY_REFERENCES", "参考图片数量超过所选模型的限制。"}
	}
	// 供应商差异在适配层处理：Pro 不接受组图模式和流式输出参数。
	input := struct {
		Model      string   `json:"model"`
		Prompt     string   `json:"prompt"`
		Sequential string   `json:"sequential_image_generation,omitempty"`
		Format     string   `json:"response_format"`
		Size       string   `json:"size"`
		Stream     *bool    `json:"stream,omitempty"`
		Watermark  bool     `json:"watermark"`
		Images     []string `json:"image,omitempty"`
	}{Model: model, Prompt: prompt, Format: "url", Size: "2K", Watermark: true, Images: images}
	if model == DefaultImageModel {
		stream := false
		input.Sequential = "disabled"
		input.Stream = &stream
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return ImageResult{}, &APIError{500, "REQUEST_FAILED", "无法创建生成请求。"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return ImageResult{}, &APIError{500, "REQUEST_FAILED", "无法创建生成请求。"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	// 不自动重试：超时或断连不代表供应商没有生成，重试可能再次消耗额度。
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ImageResult{}, &APIError{504, "GENERATION_TIMEOUT", "生成请求超时，未自动重试。请稍后再试。"}
		}
		return ImageResult{}, &APIError{502, "MODEL_UNREACHABLE", "暂时无法连接模型服务，请稍后再试。"}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		switch resp.StatusCode {
		case 401, 403:
			return ImageResult{}, &APIError{502, "MODEL_AUTH_FAILED", "模型鉴权或授权失败，请检查后端 API Key 和模型开通状态。"}
		case 404:
			return ImageResult{}, &APIError{502, "MODEL_NOT_AVAILABLE", "所选模型不存在或尚未开通，请检查方舟控制台中的模型开通状态。"}
		case 429:
			return ImageResult{}, &APIError{429, "MODEL_LIMITED", "模型调用频率或可用额度受限，请稍后重试或检查方舟控制台。"}
		case 400, 422:
			return ImageResult{}, &APIError{422, "GENERATION_REJECTED", "模型未接受本次生成请求，请调整提示词或检查模型配置。"}
		default:
			return ImageResult{}, &APIError{502, "MODEL_FAILED", "模型服务暂时无法完成生成，请稍后再试。"}
		}
	}
	var result struct {
		Data []struct {
			URL  string `json:"url"`
			Size string `json:"size"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil || len(result.Data) != 1 {
		return ImageResult{}, &APIError{502, "INVALID_MODEL_RESULT", "模型未返回可用图片，请稍后重试。"}
	}
	image := result.Data[0]
	imageURL, err := url.Parse(image.URL)
	if err != nil || imageURL.Scheme != "https" || imageURL.Host == "" || imageURL.User != nil {
		return ImageResult{}, &APIError{502, "INVALID_MODEL_RESULT", "模型未返回可用图片地址，请稍后重试。"}
	}
	return ImageResult{URL: image.URL, Model: model, Size: image.Size}, nil
}
