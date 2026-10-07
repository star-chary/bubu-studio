package storage

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

type memoryObjects struct {
	puts      int
	key, mime string
	data      []byte
	metadata  map[string]string
	failure   bool
}

func (m *memoryObjects) SignGet(_ context.Context, key string) (string, error) {
	return "https://oss.example.test/" + key + "?signature=test-only", nil
}

func (m *memoryObjects) Put(_ context.Context, key, contentType string, size int64, reader io.Reader, metadata map[string]string) error {
	m.puts++
	m.key = key
	m.mime = contentType
	m.metadata = metadata
	if m.failure {
		return errors.New("provider-private-details")
	}
	m.data, _ = io.ReadAll(reader)
	if int64(len(m.data)) != size {
		return errors.New("content length mismatch")
	}
	return nil
}
func (m *memoryObjects) Get(_ context.Context, key, _ string) (*Content, error) {
	return &Content{Body: io.NopCloser(bytes.NewReader(m.data)), Status: 200, Bytes: int64(len(m.data)), ContentType: m.mime}, nil
}
func samplePNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 8, 6))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
func testStore(t *testing.T) (*Store, *memoryObjects) {
	t.Helper()
	objects := &memoryObjects{}
	keys, _ := NewKeyBuilder("frame-space", "dev")
	return &Store{objects: objects, keys: keys, tempDir: t.TempDir(), uploadSlots: make(chan struct{}, 2), downloader: newDownloader()}, objects
}
func TestSaveUploadDetectsContentAndCleansTempFiles(t *testing.T) {
	store, objects := testStore(t)
	data := samplePNG(t)
	asset, err := store.SaveUpload(context.Background(), bytes.NewReader(data), testScope)
	if err != nil || asset.Kind != Image || asset.ContentType != "image/png" || !strings.Contains(asset.Key, "/uploads/images/"+testScope.NodeID+"/") || !bytes.Equal(objects.data, data) || objects.metadata["canvas-id"] != testScope.CanvasID {
		t.Fatalf("上传信息不符：%+v, %v", asset, err)
	}
	content, err := store.Read(context.Background(), asset.Key, "")
	if err != nil {
		t.Fatal(err)
	}
	defer content.Body.Close()
	got, _ := io.ReadAll(content.Body)
	if !bytes.Equal(got, data) {
		t.Fatal("预览内容与上传内容不一致")
	}
	for _, bad := range [][]byte{nil, []byte("<html>not an image</html>"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)} {
		if _, err := store.SaveUpload(context.Background(), bytes.NewReader(bad), testScope); err == nil {
			t.Fatal("不能保存不支持的文件")
		}
	}
	if objects.puts != 1 {
		t.Fatal("无效文件不能调用 OSS")
	}
	objects.failure = true
	_, err = store.SaveUpload(context.Background(), bytes.NewReader(data), testScope)
	if err == nil || strings.Contains(err.Error(), "provider-private") {
		t.Fatal("需报告保存失败且不泄露供应商详情")
	}
	files, _ := os.ReadDir(store.tempDir)
	if len(files) != 0 {
		t.Fatal("成功或失败后都应清理本次临时文件")
	}
	if _, err := detectMedia(bytes.NewReader(data), MaxImageBytes+1); err == nil {
		t.Fatal("大图必须拒绝")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestGeneratedResultSavedWithModelMetadataAndLimits(t *testing.T) {
	store, objects := testStore(t)
	data := samplePNG(t)
	calls := 0
	store.downloader = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "" {
			t.Fatal("下载结果不能附带模型凭据")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: int64(len(data)), Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
	})}
	asset, err := store.SaveGenerated(context.Background(), "https://images.example.test/a.png", "test-model", testScope)
	if err != nil || asset.Source != Generated || !strings.Contains(asset.Key, "/generated/images/") || objects.metadata["model"] != "test-model" {
		t.Fatalf("转存失败：%v", err)
	}
	for _, source := range []string{"http://example.test/a.png", "https://name:secret@example.test/a.png", "https://example.test:8080/a.png"} {
		if _, err := store.SaveGenerated(context.Background(), source, "test-model", testScope); err == nil {
			t.Fatal("应拒绝无效源地址")
		}
	}
	if calls != 1 {
		t.Fatal("无效源地址不能发起下载")
	}
	store.downloader = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: MaxImageBytes + 1, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	if _, err := store.SaveGenerated(context.Background(), "https://images.example.test/a.png", "test-model", testScope); err == nil || objects.puts != 1 {
		t.Fatal("超大生成结果不能写入 OSS")
	}
}

func TestReadBoundaryAndDownloadAddressRestrictions(t *testing.T) {
	store, _ := testStore(t)
	key, _ := store.keys.NewKey(testScope, Upload, Image, ".png")
	for _, bad := range []string{"other/a.png", strings.Replace(key, "/dev/", "/prod/", 1), key + "/../a.png", strings.Replace(key, ".png", ".html", 1)} {
		if _, err := store.Read(context.Background(), bad, ""); err == nil {
			t.Fatal("不能跨目录读取或预览主动内容")
		}
	}
	if _, err := store.Read(context.Background(), key, "bytes=0-1,4-5"); err == nil {
		t.Fatal("暂不支持多段读取")
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::1", "fc00::1", "::ffff:127.0.0.1", "64:ff9b::7f00:1"} {
		if publicAddress(netip.MustParseAddr(ip)) {
			t.Fatalf("不应允许内网地址 %s", ip)
		}
	}
	if !publicAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("应允许公网地址")
	}
	if newDownloader().CheckRedirect(&http.Request{}, nil) != http.ErrUseLastResponse {
		t.Fatal("不能跟随供应商结果重定向")
	}
}

func TestOSSAdapterSendsPrivateObjectAndPreservesRange(t *testing.T) {
	data := samplePNG(t)
	puts := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "OSS4-HMAC-SHA256 ") || r.URL.Path != "/test-bucket/frame-space/dev/example.png" {
			t.Error("SDK 的签名或对象地址不符")
		}
		if r.Method == "PUT" {
			puts++
			body, _ := io.ReadAll(r.Body)
			if !bytes.Equal(body, data) || r.Header.Get("Content-Type") != "image/png" || r.Header.Get("X-Oss-Object-Acl") != "private" || r.Header.Get("X-Oss-Forbid-Overwrite") != "true" || r.Header.Get("X-Oss-Meta-Canvas-Id") != testScope.CanvasID {
				t.Error("对象请求未按私有、唯一命名和所属画布发送")
			}
			w.WriteHeader(200)
			return
		}
		if r.Header.Get("Range") != "bytes=0-3" || r.Header.Get("X-Oss-Range-Behavior") != "standard" {
			t.Error("视频范围读取参数未转发")
		}
		w.Header().Set("Content-Range", "bytes 0-3/100")
		w.Header().Set("Content-Length", "4")
		w.WriteHeader(206)
		_, _ = w.Write(data[:4])
	}))
	defer upstream.Close()
	cfg := oss.LoadDefaultConfig().WithRegion("cn-hangzhou").WithEndpoint(upstream.URL).WithUsePathStyle(true).WithRetryMaxAttempts(1).
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test-only-id", "test-only-secret"))
	objects := &ossObjects{oss.NewClient(cfg), "test-bucket"}
	if err := objects.Put(context.Background(), "frame-space/dev/example.png", "image/png", int64(len(data)), bytes.NewReader(data), map[string]string{"canvas-id": testScope.CanvasID}); err != nil {
		t.Fatal("模拟 OSS 上传失败")
	}
	content, err := objects.Get(context.Background(), "frame-space/dev/example.png", "bytes=0-3")
	if err != nil {
		t.Fatal("模拟 OSS 读取失败")
	}
	defer content.Body.Close()
	body, _ := io.ReadAll(content.Body)
	if puts != 1 || content.Status != 206 || content.ContentRange != "bytes 0-3/100" || !bytes.Equal(body, data[:4]) {
		t.Fatal("范围读取返回不符")
	}
}

func TestEnabledStorageRequiresCompleteConfiguration(t *testing.T) {
	t.Setenv("OSS_ENABLED", "false")
	store, err := NewFromEnv()
	if err != nil || store != nil {
		t.Fatal("禁用 OSS 时应保持本地模式")
	}
	t.Setenv("OSS_ENABLED", "true")
	t.Setenv("OSS_REGION", "")
	if _, err := NewFromEnv(); err == nil || !strings.Contains(err.Error(), "OSS_REGION") {
		t.Fatal("不能静默忽略不完整的启用配置")
	}
}
