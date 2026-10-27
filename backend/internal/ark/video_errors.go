package ark

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Provider messages may contain signed URLs, account identifiers or prompts.
// Only fixed translations and verified reference numbers cross the API boundary.
type videoProviderError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Param     string `json:"param"`
	RequestID string `json:"request_id"`
}

type videoErrorEnvelope struct {
	Error     videoProviderError `json:"error"`
	RequestID string             `json:"request_id"`
}

var (
	videoDiagnosticToken = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	videoRequestID       = regexp.MustCompile(`(?i)\brequest[ _-]*id\s*:\s*([A-Za-z0-9_-]{1,128})\b`)
	videoContentIndex    = regexp.MustCompile(`\bcontent\[(\d{1,3})\]`)
)

func readVideoError(body io.Reader) (videoErrorEnvelope, string) {
	const limit = 64 << 10
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	var out videoErrorEnvelope
	if err != nil {
		return out, "unreadable"
	}
	if len(data) > limit {
		return out, "oversized"
	}
	if json.Unmarshal(data, &out) != nil {
		return videoErrorEnvelope{}, "invalid_json"
	}
	return out, "json"
}

func safeVideoDiagnostic(value, secret string) string {
	if !videoDiagnosticToken.MatchString(value) || (secret != "" && strings.Contains(value, secret)) {
		return "-"
	}
	return value
}

func (c *Client) logVideoFailure(operation string, status int, detail videoErrorEnvelope, header http.Header, taskID, providerID, code, bodyState string) {
	requestID := "-"
	candidates := []string{header.Get("X-Request-Id"), header.Get("X-Tt-Logid"), detail.RequestID, detail.Error.RequestID}
	if match := videoRequestID.FindStringSubmatch(detail.Error.Message); len(match) > 1 {
		candidates = append(candidates, match[1])
	}
	for _, candidate := range candidates {
		if safe := safeVideoDiagnostic(candidate, c.apiKey); safe != "-" {
			requestID = safe
			break
		}
	}
	log.Printf("ARK operation=%s http_status=%d error=%s provider_code=%s request_id=%s task_id=%s provider_task_id=%s response=%s",
		operation, status, code, safeVideoDiagnostic(detail.Error.Code, c.apiKey), requestID,
		safeVideoDiagnostic(taskID, c.apiKey), safeVideoDiagnostic(providerID, c.apiKey), bodyState)
}

// content may start with a text item; image/video/audio numbering is separate.
// Reject out-of-range indices and indices of another media kind instead of
// guessing a reference number from an untrusted provider message.
func videoReferenceSubject(kind string, detail videoProviderError, in VideoRequest) string {
	name := map[string]string{"image": "图片", "video": "视频", "audio": "音频"}[kind]
	subject := "参考" + name
	indices := map[int]bool{}
	for _, match := range videoContentIndex.FindAllStringSubmatch(detail.Param+" "+detail.Message, -1) {
		index, _ := strconv.Atoi(match[1])
		indices[index] = true
	}
	offset := 0
	if in.Prompt != "" {
		offset = 1
	}
	numbers := []string{}
	ordinal := 0
	for index, ref := range in.References {
		if ref.Kind == kind {
			ordinal++
			if indices[index+offset] {
				numbers = append(numbers, strconv.Itoa(ordinal))
			}
		}
	}
	if len(numbers) > 0 {
		subject += " " + strings.Join(numbers, "、")
	}
	return subject
}

func videoCodeFamily(code, family string) bool {
	return code == family || strings.HasPrefix(code, family+".")
}

// The reason suffix is shared by submission and asynchronous task errors.
// Their prefixes retain the distinction between rejection and accepted failure.
func videoFailureReason(e videoProviderError, in VideoRequest) (reason, message string) {
	for _, media := range []struct{ kind, code string }{{"image", "Image"}, {"video", "Video"}, {"audio", "Audio"}} {
		base := "Input" + media.code + "SensitiveContentDetected"
		subject := videoReferenceSubject(media.kind, e, in)
		if e.Code == base+".PrivacyInformation" && media.kind != "audio" {
			return strings.ToUpper(media.kind) + "_PRIVACY", subject + "被模型判定可能包含真人，未通过人像审核；请更换素材或使用方舟已审核的人像素材。"
		}
		if e.Code == base+".PolicyViolation" {
			return strings.ToUpper(media.kind) + "_COPYRIGHT", subject + "被模型判定可能涉及版权限制，请检查素材授权并更换参考素材。"
		}
		if videoCodeFamily(e.Code, base) || e.Code == "Input"+media.code+"RiskDetection" {
			return strings.ToUpper(media.kind) + "_SENSITIVE", subject + "未通过模型内容审核，请检查并调整参考素材。"
		}
	}
	if e.Code == "InputTextSensitiveContentDetected.PolicyViolation" {
		return "TEXT_COPYRIGHT", "提示词被模型判定可能涉及版权限制，请检查并调整提示词。"
	}
	if videoCodeFamily(e.Code, "InputTextSensitiveContentDetected") || videoCodeFamily(e.Code, "SensitiveContentDetected") || e.Code == "InputTextRiskDetection" {
		return "TEXT_SENSITIVE", "提示词未通过模型内容审核，请检查并调整提示词。"
	}
	for _, media := range []struct{ code, label string }{{"Video", "视频"}, {"Audio", "音轨"}, {"Image", "画面"}, {"Text", "文本"}} {
		if videoCodeFamily(e.Code, "Output"+media.code+"SensitiveContentDetected") || e.Code == "Output"+media.code+"RiskDetection" {
			if strings.HasSuffix(e.Code, ".PolicyViolation") {
				return "OUTPUT_COPYRIGHT", "生成的" + media.label + "被模型判定可能涉及版权限制，未能返回结果；请检查提示词和素材授权。"
			}
			return "OUTPUT_SENSITIVE", "生成的" + media.label + "未通过模型内容审核，未能返回结果；请调整提示词或参考素材。"
		}
	}
	switch e.Code {
	case "InvalidParameter.TaskTypeMismatch", "InvalidParameter.TaskTypeConstraint":
		return "TASK_TYPE", "模型识别的任务类型与所选生成模式或参数不兼容，请检查提示词、参考素材和生成模式。"
	case "AuthenticationError":
		return "AUTH", "模型访问凭据无效或已过期，请联系管理员检查后端 API Key。"
	case "AccessDenied", "OperationDenied.PermissionDenied", "InvalidEndpointOrModel.ModelIDAccessDisabled":
		return "ACCESS", "当前模型账号没有调用权限，请联系管理员检查模型或推理接入点授权。"
	case "ModelNotOpen", "OperationDenied.ServiceNotOpen":
		return "MODEL_NOT_OPEN", "所选视频模型尚未开通或服务不可用，请联系管理员检查方舟模型开通状态。"
	case "InvalidEndpointOrModel.NotFound", "InvalidEndpoint.ClosedEndpoint":
		return "MODEL_UNAVAILABLE", "所选模型或推理接入点不存在、已关闭或无法访问，请联系管理员检查模型配置。"
	case "AccountOverdueError", "OperationDenied.ServiceOverdue":
		return "ACCOUNT_OVERDUE", "模型供应商账号欠费，暂时无法生成视频，请联系管理员处理。"
	case "InvalidAccountStatus":
		return "ACCOUNT_STATUS", "模型供应商账号状态异常，暂时无法生成视频，请联系管理员检查。"
	case "SetLimitExceeded":
		return "USAGE_LIMIT", "模型账号已达到设置的用量上限，请联系管理员检查方舟用量限制。"
	case "InflightBatchsizeExceeded":
		return "CONCURRENCY_LIMIT", "模型同时处理的视频任务已达到上限，请等待现有任务完成后再试。"
	case "ServerOverloaded", "RequestBurstTooFast":
		return "BUSY", "模型服务当前繁忙，暂时无法处理本次请求，请稍后再试。"
	case "ContentSecurityDetectionError":
		return "MODERATION_UNAVAILABLE", "模型内容审核服务暂时异常，本次请求未能完成，请稍后再试。"
	case "OperationDenied.ArkAccessRoleNotFound", "OperationDenied.TosAccessDenied":
		return "REFERENCE_ACCESS", "模型无法访问参考素材，请联系管理员检查素材存储的访问授权。"
	case "OutofContextError":
		return "INPUT_TOO_LARGE", "提示词和参考素材的总输入量超过模型上限，请精简提示词或减少参考素材。"
	}
	if videoCodeFamily(e.Code, "QuotaExceeded") {
		// This code also denotes the supplier queue limit, not only account funds.
		return "QUOTA", "模型账号的可用配额或排队任务数已达上限，请稍后重试或联系管理员检查方舟配额。"
	}
	if videoCodeFamily(e.Code, "RateLimitExceeded") || e.Code == "AccountRateLimitExceeded" || (strings.HasSuffix(e.Code, "RateLimitExceeded") && strings.HasPrefix(e.Code, "ModelAccount")) || e.Code == "APIAccountRpmRateLimitExceeded" {
		return "RATE_LIMIT", "模型调用频率已达到上限，请稍后再试。"
	}
	if videoCodeFamily(e.Code, "InvalidImageURL") {
		return "REFERENCE_FORMAT", "模型无法解析参考图片，请检查图片格式、文件完整性和访问地址。"
	}
	if videoCodeFamily(e.Code, "MissingParameter") {
		return "MISSING_PARAMETER", "视频请求缺少模型要求的参数，请联系管理员检查请求配置。"
	}
	if videoCodeFamily(e.Code, "InvalidParameter") {
		lower := strings.ToLower(e.Message)
		if strings.Contains(lower, "failed to download") || strings.Contains(lower, "download failed") || strings.Contains(lower, "download timeout") || strings.Contains(lower, "failed to fetch") {
			return "REFERENCE_DOWNLOAD", "模型读取参考素材失败，请检查素材是否可访问，或重新上传后再试。"
		}
		labels := map[string]string{"duration": "视频时长", "resolution": "分辨率", "ratio": "画面比例", "generate_audio": "声音设置", "content": "提示词或参考素材", "model": "模型", "omni_reference_task_type": "生成模式", "output_format": "输出格式"}
		if label := labels[e.Param]; label != "" {
			return "PARAMETER", label + "不符合模型要求，请调整后再试。"
		}
		return "PARAMETER", "视频参数不符合模型要求，请检查时长、分辨率、画面比例及参考素材。"
	}
	return "", ""
}

func videoSubmissionError(status int, e videoProviderError, in VideoRequest) *APIError {
	if reason, message := videoFailureReason(e, in); reason != "" {
		return &APIError{422, "VIDEO_REJECTED_" + reason, message}
	}
	switch status {
	case http.StatusUnauthorized:
		return &APIError{422, "MODEL_AUTH_FAILED", "模型访问凭据无效或已过期，请联系管理员检查后端 API Key。"}
	case http.StatusForbidden:
		return &APIError{422, "VIDEO_REJECTED_ACCESS", "模型拒绝访问，请联系管理员检查模型授权和账号状态。"}
	case http.StatusNotFound:
		return &APIError{422, "VIDEO_REJECTED_MODEL_UNAVAILABLE", "模型接口或所选模型不可用，请联系管理员检查模型配置。"}
	case http.StatusTooManyRequests:
		return &APIError{422, "MODEL_LIMITED", "模型调用频率或配额受限，请稍后重试或联系管理员检查方舟控制台。"}
	case http.StatusRequestEntityTooLarge:
		return &APIError{422, "VIDEO_REJECTED_INPUT_TOO_LARGE", "视频请求超过模型接口大小限制，请减少参考素材或精简提示词。"}
	}
	return &APIError{422, "VIDEO_REJECTED", "模型拒绝了本次视频请求，具体原因暂未识别；请联系管理员查看任务诊断记录。"}
}
