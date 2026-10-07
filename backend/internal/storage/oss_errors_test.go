package storage

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

type ossTimeout struct{}

func (ossTimeout) Error() string   { return "private-host-and-secret timeout" }
func (ossTimeout) Timeout() bool   { return true }
func (ossTimeout) Temporary() bool { return true }

func TestOSSFailureClassificationAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		code   string
		status int
	}{
		{"timeout", &url.Error{Op: "Put", URL: "https://private-host/?Signature=secret", Err: ossTimeout{}}, "STORAGE_TIMEOUT", 504},
		{"deadline", context.DeadlineExceeded, "STORAGE_TIMEOUT", 504},
		{"cancel", context.Canceled, "STORAGE_CANCELED", 408},
		{"dns", &net.DNSError{Name: "private-host", Err: "secret", IsNotFound: true}, "STORAGE_DNS_FAILED", 502},
		{"network", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("private-host secret")}, "STORAGE_NETWORK_FAILED", 502},
		{"certificate", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "STORAGE_TLS_FAILED", 502},
		{"permission", &oss.ServiceError{Code: "AccessDenied", StatusCode: 403, Message: "secret"}, "STORAGE_ACCESS_DENIED", 503},
		{"credentials", &oss.ServiceError{Code: "InvalidAccessKeyId", StatusCode: 403, Message: "secret"}, "STORAGE_CREDENTIALS_INVALID", 503},
		{"region", &oss.ServiceError{Code: "PermanentRedirect", StatusCode: 301, Message: "secret"}, "STORAGE_BUCKET_CONFIG_INVALID", 503},
		{"clock", &oss.ServiceError{Code: "RequestTimeTooSkewed", StatusCode: 403, Message: "secret"}, "STORAGE_CLOCK_SKEW", 503},
		{"upstream", &oss.ServiceError{Code: "InternalError", StatusCode: 500, Message: "secret"}, "STORAGE_SERVICE_UNAVAILABLE", 503},
		{"unknown", errors.New("private-host secret"), "STORAGE_WRITE_FAILED", 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := classifyOSSFailure(fmt.Errorf("wrapped: %w", tc.err), "PutObject")
			if f.public.Code != tc.code || f.public.Status != tc.status {
				t.Fatalf("got %+v", f.public)
			}
			if strings.Contains(f.public.Message, "secret") || strings.Contains(f.public.Message, "private-host") {
				t.Fatal("leaked detail")
			}
		})
	}
	var output bytes.Buffer
	original := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(original)
	reportOSSFailure(&oss.ServiceError{Code: "AccessDenied", StatusCode: 403, RequestID: "safe-request-123", Message: "secret", RequestTarget: "https://private-host/?Signature=secret"}, "PutObject", 1, time.Now())
	reportOSSFailure(&oss.ServiceError{Code: "untrusted\nsecret", RequestID: "https://private-host/?Signature=secret"}, "PutObject", 1, time.Now())
	if !strings.Contains(output.String(), "safe-request-123") || strings.Contains(output.String(), "secret") || strings.Contains(output.String(), "private-host") {
		t.Fatalf("invalid sanitized log: %s", output.String())
	}
}

func fakeOSSClient(t *testing.T, roundTrip roundTripFunc) *ossObjects {
	t.Helper()
	cfg := oss.LoadDefaultConfig().WithRegion("cn-hangzhou").WithEndpoint("https://oss.example.test").WithRetryMaxAttempts(1).
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test-only-id", "test-only-secret")).WithHttpClient(&http.Client{Transport: roundTrip})
	return &ossObjects{oss.NewClient(cfg), "test-bucket"}
}

func TestOSSPutReconnectsBeforeSendingAndPreservesInput(t *testing.T) {
	data := []byte("reference bytes")
	calls := 0
	objects := fakeOSSClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: ossTimeout{}}
		}
		httptrace.ContextClientTrace(r.Context()).GotConn(httptrace.GotConnInfo{})
		body, _ := io.ReadAll(r.Body)
		if !bytes.Equal(body, data) || !strings.HasSuffix(r.URL.Path, "/same-key.png") || r.Header.Get("X-Oss-Forbid-Overwrite") != "true" {
			t.Error("changed bytes/key/overwrite guard")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if err := objects.Put(context.Background(), "same-key.png", "image/png", int64(len(data)), bytes.NewReader(data), nil); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("want 2 connection attempts, got %d", calls)
	}
}

func TestOSSPutRetryIsBoundedAndNeverRetriesAmbiguousWrites(t *testing.T) {
	for _, mode := range []string{"unconnected", "connected", "permission", "dns-not-found", "nonseekable", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			objects := fakeOSSClient(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if mode == "canceled" {
					cancel()
					return nil, context.Canceled
				}
				if mode == "connected" {
					httptrace.ContextClientTrace(r.Context()).GotConn(httptrace.GotConnInfo{})
				}
				if mode == "permission" {
					return &http.Response{StatusCode: 403, Header: http.Header{"Content-Type": []string{"application/xml"}}, Body: io.NopCloser(strings.NewReader(`<Error><Code>AccessDenied</Code><Message>secret</Message><RequestId>test-request</RequestId></Error>`)), Request: r}, nil
				}
				if mode == "dns-not-found" {
					return nil, &net.DNSError{IsNotFound: true, Name: "private-host", Err: "secret"}
				}
				return nil, &net.OpError{Op: "dial", Net: "tcp", Err: ossTimeout{}}
			})
			var reader io.Reader = strings.NewReader("data")
			if mode == "nonseekable" {
				reader = bytes.NewBufferString("data")
			}
			err := objects.Put(ctx, "same-key.png", "image/png", 4, reader, nil)
			var failure *Error
			if !errors.As(err, &failure) {
				t.Fatal("missing safe storage error", err)
			}
			want := 1
			if mode == "unconnected" {
				want = ossConnectAttempts
			}
			if calls != want {
				t.Fatalf("want %d attempts got %d", want, calls)
			}
		})
	}
}

func TestSaveUploadPreservesTypedOSSFailure(t *testing.T) {
	store, _ := testStore(t)
	store.objects = fakeOSSClient(t, func(r *http.Request) (*http.Response, error) {
		httptrace.ContextClientTrace(r.Context()).GotConn(httptrace.GotConnInfo{})
		return nil, ossTimeout{}
	})
	_, err := store.SaveUpload(context.Background(), bytes.NewReader(samplePNG(t)), testScope)
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != "STORAGE_TIMEOUT" || failure.Status != 504 {
		t.Fatal("specific error replaced by generic message", err)
	}
}
