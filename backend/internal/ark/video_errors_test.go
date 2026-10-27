package ark

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVideoRejectionReasonsAndReferenceNumbers(t *testing.T) {
	refs := []VideoReference{{Kind: "image"}, {Kind: "video"}, {Kind: "image"}, {Kind: "audio"}, {Kind: "image"}}
	for _, tc := range []struct {
		name, code, message, param, wantCode, wantText string
		status                                         int
		prompt                                         string
	}{
		{"image privacy", "InputImageSensitiveContentDetected.PrivacyInformation", "input image 'content[1]' 'content[3]' 'content[5]' may contain real person", "", "VIDEO_REJECTED_IMAGE_PRIVACY", "参考图片 1、2、3被模型判定可能包含真人", 400, "scene"},
		{"no text item", "InputImageSensitiveContentDetected.PrivacyInformation", "content[0] content[2]", "", "VIDEO_REJECTED_IMAGE_PRIVACY", "参考图片 1、2被模型判定", 400, ""},
		{"mixed video", "InputVideoSensitiveContentDetected.PrivacyInformation", "content[2]", "", "VIDEO_REJECTED_VIDEO_PRIVACY", "参考视频 1被模型判定", 400, "scene"},
		{"duplicate and invalid indices", "InputImageSensitiveContentDetected", "content[3] content[3] content[999] content[0] content[2]", "", "VIDEO_REJECTED_IMAGE_SENSITIVE", "参考图片 2未通过", 400, "scene"},
		{"missing indices", "InputImageSensitiveContentDetected", "no index available", "", "VIDEO_REJECTED_IMAGE_SENSITIVE", "参考图片未通过", 400, "scene"},
		{"copyright", "InputImageSensitiveContentDetected.PolicyViolation", "content[1]", "", "VIDEO_REJECTED_IMAGE_COPYRIGHT", "参考图片 1被模型判定可能涉及版权限制", 400, "scene"},
		{"audio moderation", "InputAudioSensitiveContentDetected", "", "content[4].audio_url", "VIDEO_REJECTED_AUDIO_SENSITIVE", "参考音频 1未通过", 400, "scene"},
		{"text moderation", "InputTextSensitiveContentDetected", "private prompt", "", "VIDEO_REJECTED_TEXT_SENSITIVE", "提示词未通过", 400, "scene"},
		{"duration", "InvalidParameter", "", "duration", "VIDEO_REJECTED_PARAMETER", "视频时长不符合", 400, "scene"},
		{"unknown param", "InvalidParameter", "", "credential-secret", "VIDEO_REJECTED_PARAMETER", "视频参数不符合", 400, "scene"},
		{"missing param", "MissingParameter.Model", "", "", "VIDEO_REJECTED_MISSING_PARAMETER", "缺少模型要求的参数", 400, "scene"},
		{"task type", "InvalidParameter.TaskTypeMismatch", "", "", "VIDEO_REJECTED_TASK_TYPE", "任务类型", 400, "scene"},
		{"download", "InvalidParameter", "Failed to download https://private.example.test/?Signature=credential-secret", "", "VIDEO_REJECTED_REFERENCE_DOWNLOAD", "读取参考素材失败", 400, "scene"},
		{"invalid image", "InvalidImageURL.InvalidFormat", "", "", "VIDEO_REJECTED_REFERENCE_FORMAT", "无法解析参考图片", 400, "scene"},
		{"auth", "AuthenticationError", "", "", "VIDEO_REJECTED_AUTH", "访问凭据无效", 401, "scene"},
		{"authorization", "AccessDenied", "", "", "VIDEO_REJECTED_ACCESS", "没有调用权限", 403, "scene"},
		{"unopened model", "ModelNotOpen", "", "", "VIDEO_REJECTED_MODEL_NOT_OPEN", "尚未开通", 404, "scene"},
		{"overdue is not auth", "AccountOverdueError", "", "", "VIDEO_REJECTED_ACCOUNT_OVERDUE", "账号欠费", 403, "scene"},
		{"rpm", "RateLimitExceeded.EndpointRPMExceeded", "", "", "VIDEO_REJECTED_RATE_LIMIT", "调用频率", 429, "scene"},
		{"quota not necessarily balance", "QuotaExceeded", "", "", "VIDEO_REJECTED_QUOTA", "配额或排队任务数", 429, "scene"},
		{"usage limit", "SetLimitExceeded", "", "", "VIDEO_REJECTED_USAGE_LIMIT", "用量上限", 429, "scene"},
		{"concurrency", "InflightBatchsizeExceeded", "", "", "VIDEO_REJECTED_CONCURRENCY_LIMIT", "同时处理的视频任务", 429, "scene"},
		{"busy", "ServerOverloaded", "", "", "VIDEO_REJECTED_BUSY", "当前繁忙", 429, "scene"},
		{"unknown", "FutureError", "credential-secret private prompt", "", "VIDEO_REJECTED", "具体原因暂未识别", 400, "scene"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(videoErrorEnvelope{Error: videoProviderError{Code: tc.code, Message: tc.message, Param: tc.param}})
			}))
			defer srv.Close()
			client := NewClient("credential-secret")
			client.videoEndpoint = srv.URL
			_, err := client.CreateVideo(context.Background(), VideoRequest{TaskID: "local-task", Model: VideoModel, Mode: "reference", Prompt: tc.prompt, References: refs})
			failure, ok := err.(*APIError)
			if !ok || failure.Code != tc.wantCode || !strings.Contains(failure.Message, tc.wantText) || calls != 1 {
				t.Fatalf("wrong error/call count: %+v calls=%d", err, calls)
			}
			if strings.Contains(failure.Message, "credential-secret") || strings.Contains(failure.Message, "private") || strings.Contains(failure.Message, "content[") {
				t.Fatal("raw provider details leaked", failure.Message)
			}
		})
	}
}

func TestVideoActualPrivacyResponseMapsFiveImages(t *testing.T) {
	in := VideoRequest{Prompt: "scene", References: []VideoReference{{Kind: "image"}, {Kind: "image"}, {Kind: "image"}, {Kind: "image"}, {Kind: "image"}}}
	failure := videoSubmissionError(400, videoProviderError{
		Code:    "InputImageSensitiveContentDetected.PrivacyInformation",
		Message: "The request failed because the input image 'content[1]' 'content[2]' 'content[3]' 'content[5]' may contain real person. Request id: safe-request-123",
	}, in)
	if !strings.HasPrefix(failure.Message, "参考图片 1、2、3、5被模型判定可能包含真人") {
		t.Fatal(failure.Message)
	}
}

func TestVideoErrorBodiesAreBoundedAndDiagnosticLogsAreRedacted(t *testing.T) {
	var output bytes.Buffer
	original := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(original)
	for _, tc := range []struct {
		name, body, code, state string
		status                  int
	}{
		{"non json", "<html>private-url credential-secret</html>", "VIDEO_REJECTED", "invalid_json", 400},
		{"too large", strings.Repeat("x", (64<<10)+1), "VIDEO_REJECTED", "oversized", 400},
		{"auth fallback", "", "MODEL_AUTH_FAILED", "invalid_json", 401},
		{"model fallback", "", "VIDEO_REJECTED_MODEL_UNAVAILABLE", "invalid_json", 404},
		{"size fallback", "", "VIDEO_REJECTED_INPUT_TOO_LARGE", "invalid_json", 413},
		{"rate fallback", "", "MODEL_LIMITED", "invalid_json", 429},
		{"5xx is ambiguous", `{"error":{"code":"InvalidParameter","message":"private-url credential-secret"}}`, "SUBMISSION_UNKNOWN", "json", 500},
		{"499 is ambiguous", `{"error":{"code":"InvalidParameter"}}`, "SUBMISSION_UNKNOWN", "json", 499},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output.Reset()
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("X-Request-Id", "safe-request-123")
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			client := NewClient("credential-secret")
			client.videoEndpoint = srv.URL
			_, err := client.CreateVideo(context.Background(), VideoRequest{TaskID: "local-task-123", Mode: "text", Prompt: "secret prompt"})
			if err == nil || err.(*APIError).Code != tc.code || calls != 1 {
				t.Fatal(err, calls)
			}
			logs := output.String()
			if !strings.Contains(logs, "request_id=safe-request-123") || !strings.Contains(logs, "task_id=local-task-123") || !strings.Contains(logs, "response="+tc.state) || strings.Contains(logs+err.Error(), "credential-secret") || strings.Contains(logs+err.Error(), "private-url") || strings.Contains(logs, "secret prompt") {
				t.Fatal("invalid diagnostic boundary", logs)
			}
		})
	}
	output.Reset()
	client := NewClient("credential-secret")
	client.logVideoFailure("CreateVideo", 400, videoErrorEnvelope{Error: videoProviderError{Code: "evil\nprivate-url", Message: "Request id: safe-body-id private-url", Param: "private-url"}}, http.Header{"X-Request-Id": []string{"credential-secret"}}, "local-task", "", "VIDEO_REJECTED", "json")
	if !strings.Contains(output.String(), "request_id=safe-body-id") || strings.Contains(output.String(), "private-url") || strings.Contains(output.String(), "credential-secret") {
		t.Fatal("unsafe diagnostic token", output.String())
	}
}

func TestVideoAsyncFailureIsTranslatedWithoutLeakingProviderMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "safe-query-id")
		w.Write([]byte(`{"id":"cgt-test","status":"failed","error":{"code":"OutputVideoSensitiveContentDetected","message":"private signed URL and credential-secret"}}`))
	}))
	defer srv.Close()
	client := NewClient("credential-secret")
	client.videoEndpoint = srv.URL
	out, err := client.QueryVideo(context.Background(), "cgt-test")
	if err != nil || out.Failure == nil || out.Failure.Code != "PROVIDER_OUTPUT_SENSITIVE" || !strings.Contains(out.Failure.Message, "生成的视频未通过") {
		t.Fatalf("missing translated failure: %+v %v", out, err)
	}
	encoded, _ := json.Marshal(out)
	if strings.Contains(string(encoded)+out.Failure.Message, "private") || strings.Contains(string(encoded)+out.Failure.Message, "credential-secret") {
		t.Fatal("raw provider error exposed")
	}
}
