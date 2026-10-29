package apimart

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"frame-space/backend/internal/ark"
)

func submitForTest(ctx context.Context, client *Client, kind, prompt string) error {
	if kind == "image" {
		_, err := client.CreateImage(ctx, ImageRequest{TaskID: prompt, Prompt: prompt, Model: "seedream-5-0-lite"})
		return err
	}
	input := validInput()
	input.TaskID, input.Prompt = prompt, prompt
	_, err := client.CreateVideo(ctx, input)
	return err
}

func TestSubmissionOnlyAcceptsClearRejections(t *testing.T) {
	for _, kind := range []string{"image", "video"} {
		for _, tc := range []struct {
			name, body, want string
			status           int
		}{
			{"clear auth rejection", `{"error":{"code":401,"type":"authentication_error","message":"private-error"}}`, "APIMART_AUTH_FAILED", 401},
			{"html", `<html>unauthorized</html>`, "SUBMISSION_UNKNOWN", 401},
			{"missing code", `{"error":{"message":"private-error"}}`, "SUBMISSION_UNKNOWN", 400},
			{"empty code", `{"error":{"code":"","message":"private-error"}}`, "SUBMISSION_UNKNOWN", 429},
			{"timeout", `{"error":{"code":408,"message":"private-error"}}`, "SUBMISSION_UNKNOWN", 408},
			{"client closed", `{"error":{"code":499,"message":"private-error"}}`, "SUBMISSION_UNKNOWN", 499},
			{"contradictory task", `{"error":{"code":400,"message":"private-error"},"data":[{"task_id":"task_accepted"}]}`, "SUBMISSION_UNKNOWN", 400},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprint(w, tc.body)
				}))
				defer server.Close()
				client := newTestClient(t)
				client.baseURL = server.URL
				err := submitForTest(t.Context(), client, kind, "private-prompt")
				var failure *ark.APIError
				if !errors.As(err, &failure) || failure.Code != tc.want || calls.Load() != 1 {
					t.Fatalf("unexpected outcome: %v; calls=%d", err, calls.Load())
				}
			})
		}
	}
}

func TestSubmissionDiagnosticsDoNotLogSecrets(t *testing.T) {
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(old) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":{"code":400,"message":"private-error https://private.example/?token=private-token"}}`)
	}))
	defer server.Close()
	client := newTestClient(t)
	client.baseURL = server.URL
	_ = submitForTest(t.Context(), client, "image", "private-prompt")
	_ = submitForTest(t.Context(), client, "video", "private-prompt")
	for _, secret := range []string{"test-secret", "private-prompt", "private-error", "private-token", "private.example"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("submission diagnostics logged sensitive data")
		}
	}
	if !strings.Contains(logs.String(), "phase=") {
		t.Fatal("missing network phase diagnostics")
	}
}

func requireUnknown(t *testing.T, err error) {
	t.Helper()
	var failure *ark.APIError
	if !errors.As(err, &failure) || failure.Code != "SUBMISSION_UNKNOWN" {
		t.Fatalf("expected uncertain submission, got %v", err)
	}
}

func TestSubmissionDoesNotFollowRedirects(t *testing.T) {
	for _, kind := range []string{"image", "video"} {
		for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
			t.Run(fmt.Sprintf("%s-%d", kind, status), func(t *testing.T) {
				var initial, redirected atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/redirected" {
						redirected.Add(1)
						_, _ = fmt.Fprint(w, `{"code":200,"data":[{"task_id":"duplicate"}]}`)
						return
					}
					initial.Add(1)
					w.Header().Set("Location", "/redirected")
					w.WriteHeader(status)
				}))
				defer server.Close()
				client := newTestClient(t)
				client.baseURL = server.URL
				requireUnknown(t, submitForTest(t.Context(), client, kind, "first"))
				if initial.Load() != 1 || redirected.Load() != 0 {
					t.Fatalf("submission was replayed: initial=%d redirected=%d", initial.Load(), redirected.Load())
				}
			})
		}
	}
}

func TestLostSubmissionIsNotReplayedAfterRouteSwitch(t *testing.T) {
	for _, kind := range []string{"image", "video"} {
		t.Run(kind, func(t *testing.T) {
			var route, routeA, routeB, first, second atomic.Int32
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Prompt string `json:"prompt"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "invalid input", http.StatusBadRequest)
					return
				}
				if body.Prompt == "first" {
					first.Add(1)
					route.Store(1) // A receives the POST; new connections now use B.
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = conn.Close() // Simulate acceptance followed by lost response.
					}
					return
				}
				second.Add(1)
				_, _ = fmt.Fprint(w, `{"code":200,"data":[{"task_id":"task_new"}]}`)
			}))
			defer origin.Close()
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodConnect || r.Host != origin.Listener.Addr().String() {
					http.Error(w, "unexpected destination", http.StatusBadRequest)
					return
				}
				if route.Load() == 0 {
					routeA.Add(1)
				} else {
					routeB.Add(1)
				}
				upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
				if err != nil {
					http.Error(w, "unavailable", http.StatusBadGateway)
					return
				}
				conn, rw, err := w.(http.Hijacker).Hijack()
				if err != nil {
					upstream.Close()
					return
				}
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
			client.baseURL = origin.URL
			client.submitHTTP.Transport.(*http.Transport).TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			requireUnknown(t, submitForTest(t.Context(), client, kind, "first"))
			if first.Load() != 1 || second.Load() != 0 || routeA.Load() != 1 || routeB.Load() != 0 {
				t.Fatalf("old task replayed after switch: first=%d second=%d A=%d B=%d", first.Load(), second.Load(), routeA.Load(), routeB.Load())
			}
			if err := submitForTest(t.Context(), client, kind, "second"); err != nil {
				t.Fatalf("new task could not use backup route: %v", err)
			}
			if first.Load() != 1 || second.Load() != 1 || routeA.Load() != 1 || routeB.Load() != 1 {
				t.Fatalf("unexpected post-switch requests: first=%d second=%d A=%d B=%d", first.Load(), second.Load(), routeA.Load(), routeB.Load())
			}
		})
	}
}

func TestSubmissionUsesHTTP1WithoutConnectionReuse(t *testing.T) {
	var connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 1 {
			t.Errorf("submission used HTTP/%d", r.ProtoMajor)
		}
		_, _ = fmt.Fprint(w, `{"code":200,"data":[{"task_id":"task_test"}]}`)
	}))
	server.EnableHTTP2 = true
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.StartTLS()
	defer server.Close()
	client := newTestClient(t)
	client.baseURL = server.URL
	// Preserve the real transport's negotiated protocols while trusting the
	// test server. Replacing its whole TLS config would hide inherited ALPN.
	transport := client.submitHTTP.Transport.(*http.Transport)
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.RootCAs = server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	for _, prompt := range []string{"first", "second"} {
		if err := submitForTest(t.Context(), client, "image", prompt); err != nil {
			t.Fatal(err)
		}
	}
	if connections.Load() != 2 {
		t.Fatalf("submissions reused connection: %d", connections.Load())
	}
}
