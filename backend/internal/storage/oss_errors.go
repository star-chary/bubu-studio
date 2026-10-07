package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

// Keep diagnostics useful without logging SDK Error(): it may contain request
// URLs, object paths, signatures, service response bodies or credential details.
type ossFailure struct {
	public                    *Error
	kind, provider, requestID string
	status                    int
}

var safeDiagnosticToken = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func diagnosticToken(value string) string {
	if safeDiagnosticToken.MatchString(value) {
		return value
	}
	return "-"
}

func classifyOSSFailure(err error, operation string) ossFailure {
	action := "保存到"
	code := "STORAGE_WRITE_FAILED"
	if operation == "GetObject" {
		action, code = "读取", "STORAGE_READ_FAILED"
	}
	f := ossFailure{public: &Error{502, code, action + " OSS 失败，请稍后重试；后端已记录错误类别。"}, kind: "unknown"}
	var service *oss.ServiceError
	if errors.As(err, &service) {
		f.kind, f.provider, f.requestID, f.status = "service", diagnosticToken(service.Code), diagnosticToken(service.RequestID), service.StatusCode
		switch service.Code {
		case "InvalidAccessKeyId", "SignatureDoesNotMatch", "InvalidSecurityToken", "SecurityTokenExpired", "InvalidToken":
			f.public = &Error{503, "STORAGE_CREDENTIALS_INVALID", "OSS 后端凭据无效或已过期，请检查访问密钥及临时令牌配置。"}
		case "RequestTimeTooSkewed", "RequestExpired":
			f.public = &Error{503, "STORAGE_CLOCK_SKEW", "后端系统时间与 OSS 不一致，请同步系统时间后重试。"}
		case "NoSuchBucket", "PermanentRedirect", "InvalidBucketName":
			f.public = &Error{503, "STORAGE_BUCKET_CONFIG_INVALID", "OSS 存储桶名称或地域地址配置不匹配，请检查后端存储配置。"}
		case "AccessDenied":
			f.public = &Error{503, "STORAGE_ACCESS_DENIED", "OSS 拒绝访问，请检查后端账号权限及存储桶访问策略。"}
		case "UserDisable", "UserDisabled", "InsufficientBalance":
			f.public = &Error{503, "STORAGE_ACCOUNT_UNAVAILABLE", "OSS 账号状态或可用额度异常，请检查阿里云控制台。"}
		case "FileAlreadyExists":
			f.public = &Error{409, "STORAGE_OBJECT_EXISTS", "OSS 中已存在同名对象，系统未覆盖原文件，请重新保存。"}
		default:
			if service.StatusCode == 429 || service.StatusCode >= 500 {
				f.public = &Error{503, "STORAGE_SERVICE_UNAVAILABLE", "OSS 服务暂时不可用或请求受限，请稍后重新保存。"}
			}
		}
		return f
	}
	if errors.Is(err, context.Canceled) {
		f.kind, f.public = "canceled", &Error{408, "STORAGE_CANCELED", "文件保存已中断，原文件仍在当前页面时可点击重新保存。"}
		return f
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		f.kind, f.public = "dns", &Error{502, "STORAGE_DNS_FAILED", "无法解析 OSS 地址，请检查后端网络、DNS 和存储服务地址后重新保存。"}
		return f
	}
	var certificate *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostError x509.HostnameError
	var invalidCert x509.CertificateInvalidError
	if errors.As(err, &certificate) || errors.As(err, &unknownCA) || errors.As(err, &hostError) || errors.As(err, &invalidCert) {
		f.kind, f.public = "certificate", &Error{502, "STORAGE_TLS_FAILED", "无法验证 OSS 的 HTTPS 证书，请检查后端系统时间、代理或证书配置。"}
		return f
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() || errors.Is(err, context.DeadlineExceeded) {
		f.kind, f.public = "timeout", &Error{504, "STORAGE_TIMEOUT", "连接或传输到 OSS 超时，请检查后端网络、VPN/代理连接后重新保存。"}
		return f
	}
	var op *net.OpError
	if errors.As(err, &op) {
		f.kind, f.public = "network", &Error{502, "STORAGE_NETWORK_FAILED", "后端与 OSS 的网络连接失败，请检查网络、VPN/代理连接后重新保存。"}
	}
	return f
}

// Retry only transient connection failures before obtaining an HTTP connection.
// Once connected, the PUT may have reached OSS; automatic re-upload would make
// a lost success response ambiguous even with ForbidOverwrite enabled.
func retryableOSSConnect(ctx context.Context, err error, connected bool) bool {
	if connected || ctx.Err() != nil {
		return false
	}
	var service *oss.ServiceError
	if errors.As(err, &service) {
		return false
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return dns.IsTimeout || dns.IsTemporary
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	var network net.Error
	return errors.As(err, &network) && network.Timeout()
}

func reportOSSFailure(err error, operation string, attempts int, started time.Time) *Error {
	f := classifyOSSFailure(err, operation)
	log.Printf("OSS operation=%s error=%s category=%s http_status=%d provider_code=%s request_id=%s attempts=%d elapsed_ms=%d", operation, f.public.Code, f.kind, f.status, f.provider, f.requestID, attempts, time.Since(started).Milliseconds())
	return f.public
}

// Unexpected storage implementations must not bypass the public-error boundary.
func publicWriteFailure(err error) *Error {
	var failure *Error
	if errors.As(err, &failure) && strings.HasPrefix(failure.Code, "STORAGE_") {
		return failure
	}
	return &Error{502, "STORAGE_WRITE_FAILED", "保存到 OSS 失败，请稍后重新保存。"}
}
