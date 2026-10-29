package apimart

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptrace"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

type submissionTrace struct {
	phase   atomic.Uint32
	started time.Time
}

var localTaskIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func traceSubmission(req *http.Request) (*http.Request, *submissionTrace) {
	trace := &submissionTrace{started: time.Now()}
	mark := func(next uint32) {
		for current := trace.phase.Load(); next > current; current = trace.phase.Load() {
			if trace.phase.CompareAndSwap(current, next) {
				return
			}
		}
	}
	ctx := httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { mark(1) },
		ConnectStart:      func(string, string) { mark(2) },
		TLSHandshakeStart: func() { mark(3) },
		GotConn:           func(httptrace.GotConnInfo) { mark(4) },
		WroteHeaders:      func() { mark(5) },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				mark(6)
			}
		},
		GotFirstResponseByte: func() { mark(7) },
	})
	return req.WithContext(ctx), trace
}

func (t *submissionTrace) log(kind, taskID string, status int, reason string) {
	phases := [...]string{"starting", "dns", "connecting", "tls", "connected", "headers_written", "request_written", "response_started"}
	if !localTaskIDPattern.MatchString(taskID) {
		taskID = "unknown"
	}
	log.Printf("APIMart %s create: task=%s phase=%s http_status=%d reason=%s elapsed_ms=%d", kind, taskID, phases[t.phase.Load()], status, reason, time.Since(t.started).Milliseconds())
}

func submissionErrorKind(err error) string {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns_error"
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return "connect_error"
	}
	return "transport_error"
}

func explicitRejection(status int, decodeErr error, detail providerError, hasTaskData bool) bool {
	if status < 400 || status >= 500 || status == 408 || status == 499 || decodeErr != nil {
		return false
	}
	if hasTaskData || (strings.TrimSpace(detail.Type) == "" && strings.TrimSpace(detail.Message) == "") {
		return false
	}
	var code any
	if json.Unmarshal(detail.Code, &code) != nil {
		return false
	}
	switch value := code.(type) {
	case string:
		return strings.TrimSpace(value) != ""
	case float64:
		return value != 0
	default:
		return false
	}
}
