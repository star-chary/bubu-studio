package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

type ossObjects struct {
	client *oss.Client
	bucket string
}

const ossConnectTimeout = 15 * time.Second
const ossConnectAttempts = 3

// Disabled storage preserves the local canvas experiment. An explicitly enabled
// but incomplete configuration fails startup rather than silently losing files.
func NewFromEnv() (*Store, error) {
	enabled := strings.TrimSpace(os.Getenv("OSS_ENABLED"))
	if enabled == "" || enabled == "false" {
		return nil, nil
	}
	if enabled != "true" {
		return nil, fmt.Errorf("OSS_ENABLED 必须为 true 或 false")
	}
	for _, name := range []string{"OSS_REGION", "OSS_BUCKET", "OSS_ACCESS_KEY_ID", "OSS_ACCESS_KEY_SECRET"} {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			return nil, fmt.Errorf("缺少后端配置项 %s", name)
		}
	}
	region, bucket := strings.TrimSpace(os.Getenv("OSS_REGION")), strings.TrimSpace(os.Getenv("OSS_BUCKET"))
	if !regexp.MustCompile(`^[a-z][a-z0-9-]+$`).MatchString(region) || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`).MatchString(bucket) {
		return nil, fmt.Errorf("OSS_REGION 或 OSS_BUCKET 格式无效")
	}
	prefix, environment := os.Getenv("STORAGE_PREFIX"), os.Getenv("STORAGE_ENV")
	if prefix == "" {
		prefix = "frame-space"
	}
	if environment == "" {
		environment = "dev"
	}
	keys, err := NewKeyBuilder(prefix, environment)
	if err != nil {
		return nil, err
	}
	cfg := oss.LoadDefaultConfig().WithRegion(region).
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(os.Getenv("OSS_ACCESS_KEY_ID"), os.Getenv("OSS_ACCESS_KEY_SECRET"), os.Getenv("OSS_SESSION_TOKEN"))).
		WithRetryMaxAttempts(1).WithConnectTimeout(ossConnectTimeout).WithReadWriteTimeout(60 * time.Second)
	if endpoint := strings.TrimSpace(os.Getenv("OSS_ENDPOINT")); endpoint != "" {
		if !strings.Contains(endpoint, "://") {
			endpoint = "https://" + endpoint
		}
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Port() != "" || !strings.HasSuffix(u.Hostname(), ".aliyuncs.com") {
			return nil, fmt.Errorf("OSS_ENDPOINT 需为阿里云 HTTPS 服务地址，不含 Bucket、路径或查询参数")
		}
		cfg.WithEndpoint(endpoint)
	}
	const tempDir = ".storage-tmp"
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		return nil, fmt.Errorf("无法创建后端临时文件目录")
	}
	return &Store{objects: &ossObjects{oss.NewClient(cfg), bucket}, keys: keys, tempDir: tempDir, downloader: newDownloader(), uploadSlots: make(chan struct{}, 2)}, nil
}

func (o *ossObjects) Put(ctx context.Context, key, contentType string, size int64, reader io.Reader, metadata map[string]string) error {
	started := time.Now()
	seeker, seekable := reader.(io.Seeker)
	var offset int64
	if seekable {
		var err error
		offset, err = seeker.Seek(0, io.SeekCurrent)
		seekable = err == nil
	}
	for attempt := 1; ; attempt++ {
		var connected atomic.Bool
		trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { connected.Store(true) }}
		_, err := o.client.PutObject(httptrace.WithClientTrace(ctx, trace), &oss.PutObjectRequest{
			Bucket: oss.Ptr(o.bucket), Key: oss.Ptr(key), Body: reader,
			ContentType: oss.Ptr(contentType), ContentLength: oss.Ptr(size),
			ForbidOverwrite: oss.Ptr("true"), Acl: oss.ObjectACLPrivate, StorageClass: oss.StorageClassStandard,
			Metadata: metadata,
		})
		if err == nil {
			return nil
		}
		if attempt >= ossConnectAttempts || !seekable || !retryableOSSConnect(ctx, err, connected.Load()) {
			return reportOSSFailure(err, "PutObject", attempt, started)
		}
		// Same immutable key and same bytes. Only the connection is retried;
		// there has not yet been an HTTP connection that could carry this PUT.
		failure := classifyOSSFailure(err, "PutObject")
		log.Printf("OSS operation=PutObject category=%s attempt=%d retry=connect_only", failure.kind, attempt)
		timer := time.NewTimer(time.Duration(attempt) * 300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return reportOSSFailure(ctx.Err(), "PutObject", attempt, started)
		case <-timer.C:
		}
		if _, seekErr := seeker.Seek(offset, io.SeekStart); seekErr != nil {
			return reportOSSFailure(seekErr, "PutObject", attempt, started)
		}
	}
}

// Only the model receives this short-lived URL; it is never returned to the UI.
func (o *ossObjects) SignGet(ctx context.Context, key string) (string, error) {
	return o.SignGetFor(ctx, key, 10*time.Minute)
}
func (o *ossObjects) SignGetFor(ctx context.Context, key string, ttl time.Duration) (string, error) {
	result, err := o.client.Presign(ctx, &oss.GetObjectRequest{Bucket: oss.Ptr(o.bucket), Key: oss.Ptr(key)}, oss.PresignExpires(ttl))
	if err != nil {
		return "", &Error{502, "REFERENCE_SIGN_FAILED", "无法准备参考图片，请稍后重试。"}
	}
	return result.URL, nil
}

func (o *ossObjects) Get(ctx context.Context, key, byteRange string) (*Content, error) {
	started := time.Now()
	req := &oss.GetObjectRequest{Bucket: oss.Ptr(o.bucket), Key: oss.Ptr(key)}
	if byteRange != "" {
		req.Range = oss.Ptr(byteRange)
		req.RangeBehavior = oss.Ptr("standard")
	}
	result, err := o.client.GetObject(ctx, req)
	if err != nil {
		var serviceError *oss.ServiceError
		if errors.As(err, &serviceError) {
			if serviceError.StatusCode == http.StatusNotFound {
				return nil, &Error{404, "ASSET_NOT_FOUND", "文件不存在。"}
			}
			if serviceError.StatusCode == http.StatusRequestedRangeNotSatisfiable {
				return nil, &Error{416, "INVALID_RANGE", "文件读取范围超出边界。"}
			}
		}
		return nil, reportOSSFailure(err, "GetObject", 1, started)
	}
	return &Content{Body: result.Body, Status: result.StatusCode, Bytes: result.ContentLength, ContentRange: oss.ToString(result.ContentRange), ETag: oss.ToString(result.ETag)}, nil
}
