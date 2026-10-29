package apimart

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"frame-space/backend/internal/ark"
)

func TestAPIMartUsesConfiguredLocalProxyForHTTPS(t *testing.T) {
	var requests atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}
		requests.Add(1)
		switch r.URL.Path {
		case "/images/generations", "/videos/generations":
			_, _ = fmt.Fprint(w, `{"code":200,"data":[{"task_id":"task_test"}]}`)
		case "/tasks/task_test":
			_, _ = fmt.Fprint(w, `{"code":200,"data":{"id":"task_test","status":"pending"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()

	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != origin.Listener.Addr().String() {
			http.Error(w, "unexpected destination", http.StatusBadRequest)
			return
		}
		upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		connects.Add(1)
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		go func() {
			defer conn.Close()
			defer upstream.Close()
			go func() { _, _ = io.Copy(upstream, conn) }()
			_, _ = io.Copy(conn, upstream)
		}()
	}))
	defer proxy.Close()

	client, err := NewClient("test-secret", proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := client.http.Transport.(*http.Transport)
	transport.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	defer transport.CloseIdleConnections()
	client.baseURL = origin.URL
	imageID, err := client.CreateImage(t.Context(), ImageRequest{Prompt: "paper boat", Model: "seedream-5-0-lite"})
	if err != nil || imageID != "task_test" {
		t.Fatalf("image submission failed: %s %v", imageID, err)
	}
	if image, err := client.QueryImage(t.Context(), imageID, "seedream-5-0-lite"); err != nil || image.Status != "queued" {
		t.Fatalf("image polling failed: %v %v", image, err)
	}
	videoID, err := client.CreateVideo(t.Context(), validInput())
	if err != nil || videoID != "task_test" {
		t.Fatalf("video submission failed: %s %v", videoID, err)
	}
	if video, err := client.QueryVideo(t.Context(), videoID); err != nil || video.Status != "queued" {
		t.Fatalf("video polling failed: %v %v", video, err)
	}
	if connects.Load() < 1 || requests.Load() != 4 {
		t.Fatalf("requests did not traverse proxy: connects=%d requests=%d", connects.Load(), requests.Load())
	}
}

func TestAPIMartDoesNotBypassUnavailableProxy(t *testing.T) {
	var requests atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	proxy := httptest.NewServer(http.NotFoundHandler())
	proxyAddress := proxy.URL
	proxy.Close()
	client, err := NewClient("test-secret", proxyAddress)
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = origin.URL
	client.http.Transport.(*http.Transport).TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	_, err = client.CreateImage(t.Context(), ImageRequest{Prompt: "paper boat", Model: "seedream-5-0-lite"})
	var failure *ark.APIError
	if !errors.As(err, &failure) || failure.Code != "SUBMISSION_UNKNOWN" || requests.Load() != 0 {
		t.Fatalf("request bypassed unavailable proxy or lost its uncertainty: error=%v requests=%d", err, requests.Load())
	}
}

func TestAPIMartProxyRequiresLocalHTTPAddress(t *testing.T) {
	for _, address := range []string{
		"https://127.0.0.1:7897", "http://192.0.2.1:7897", "http://example.com:7897",
		"http://user:password@127.0.0.1:7897", "http://127.0.0.1:7897/path", "http://127.0.0.1:0",
	} {
		if _, err := NewClient("test-secret", address); err == nil {
			t.Errorf("invalid proxy accepted: %s", address)
		}
	}
	client, err := NewClient("test-secret", "")
	if err != nil || client.http.Transport != nil {
		t.Fatal("empty proxy should retain the default transport", err)
	}
}
